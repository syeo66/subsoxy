package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	
	"github.com/syeo66/subsoxy/config"
	"github.com/syeo66/subsoxy/errors"
	"github.com/syeo66/subsoxy/models"
	"github.com/syeo66/subsoxy/database"
	"github.com/syeo66/subsoxy/credentials"
	"github.com/syeo66/subsoxy/handlers"
	"github.com/syeo66/subsoxy/shuffle"
)

type ProxyServer struct {
	config      *config.Config
	logger      *logrus.Logger
	proxy       *httputil.ReverseProxy
	hooks       map[string][]models.Hook
	db          *database.DB
	credentials *credentials.Manager
	handlers    *handlers.Handler
	shuffle     *shuffle.Service
	server      *http.Server
	syncTicker  *time.Ticker
	shutdownChan chan struct{}
	rateLimiter *rate.Limiter
}

func New(cfg *config.Config) (*ProxyServer, error) {
	logger := logrus.New()
	level, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
		logger.WithError(err).Warn("Invalid log level, defaulting to info")
	}
	logger.SetLevel(level)

	upstreamURL, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryServer, "PROXY_SETUP_FAILED", "invalid upstream URL").
			WithContext("upstream_url", cfg.UpstreamURL)
	}

	proxy := httputil.NewSingleHostReverseProxy(upstreamURL)
	
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = upstreamURL.Host
		req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
	}

	db, err := database.New(cfg.DatabasePath, logger)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryServer, "INITIALIZATION_FAILED", "failed to initialize database").
			WithContext("database_path", cfg.DatabasePath)
	}

	credManager := credentials.New(logger, cfg.UpstreamURL)
	shuffleService := shuffle.New(db, logger)
	handlersService := handlers.New(logger, shuffleService)

	var rateLimiter *rate.Limiter
	if cfg.RateLimitEnabled {
		rateLimiter = rate.NewLimiter(rate.Limit(cfg.RateLimitRPS), cfg.RateLimitBurst)
		logger.WithFields(logrus.Fields{
			"rps":   cfg.RateLimitRPS,
			"burst": cfg.RateLimitBurst,
		}).Info("Rate limiting enabled")
	} else {
		logger.Info("Rate limiting disabled")
	}

	server := &ProxyServer{
		config:      cfg,
		logger:      logger,
		proxy:       proxy,
		hooks:       make(map[string][]models.Hook),
		db:          db,
		credentials: credManager,
		handlers:    handlersService,
		shuffle:     shuffleService,
		shutdownChan: make(chan struct{}),
		rateLimiter: rateLimiter,
	}

	go server.syncSongs()

	return server, nil
}

func (ps *ProxyServer) AddHook(endpoint string, hook models.Hook) {
	ps.hooks[endpoint] = append(ps.hooks[endpoint], hook)
}

func (ps *ProxyServer) proxyHandler(w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path
	
	ps.logger.WithFields(logrus.Fields{
		"method":   r.Method,
		"endpoint": endpoint,
		"remote":   r.RemoteAddr,
	}).Info("Incoming request")

	if ps.rateLimiter != nil {
		if !ps.rateLimiter.Allow() {
			ps.logger.WithFields(logrus.Fields{
				"endpoint": endpoint,
				"remote":   r.RemoteAddr,
			}).Warn("Rate limit exceeded")
			
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	if strings.HasPrefix(endpoint, "/rest/") {
		username := r.URL.Query().Get("u")
		password := r.URL.Query().Get("p")
		if username != "" && password != "" && len(username) > 0 && len(password) > 0 {
			go func() {
				if err := ps.credentials.ValidateAndStore(username, password); err != nil {
					ps.logger.WithError(err).WithField("username", username).Debug("Failed to validate credentials")
				}
			}()
		}
	}

	if hooks, exists := ps.hooks[endpoint]; exists {
		for _, hook := range hooks {
			if hook(w, r, endpoint) {
				return
			}
		}
	}

	if strings.HasPrefix(endpoint, "/rest/") {
		ps.logger.WithField("endpoint", endpoint).Debug("Subsonic API endpoint")
	}

	ps.proxy.ServeHTTP(w, r)
}

func (ps *ProxyServer) Start() error {
	if ps.server != nil {
		return errors.ErrServerStart.WithContext("reason", "server already started")
	}

	router := mux.NewRouter()
	router.PathPrefix("/").HandlerFunc(ps.proxyHandler)

	ps.server = &http.Server{
		Addr:    ":" + ps.config.ProxyPort,
		Handler: router,
	}

	ps.logger.WithFields(logrus.Fields{
		"port":     ps.config.ProxyPort,
		"upstream": ps.config.UpstreamURL,
		"url":      fmt.Sprintf("http://localhost:%s", ps.config.ProxyPort),
	}).Info("Starting proxy server")

	go func() {
		if err := ps.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ps.logger.WithError(err).Fatal("Server failed to start")
		}
	}()

	return nil
}

func (ps *ProxyServer) Shutdown(ctx context.Context) error {
	ps.logger.Info("Shutting down proxy server...")
	
	close(ps.shutdownChan)
	
	if ps.syncTicker != nil {
		ps.syncTicker.Stop()
	}
	
	if ps.db != nil {
		if err := ps.db.Close(); err != nil {
			ps.logger.WithError(err).Error("Failed to close database connection")
		}
	}
	
	if ps.server != nil {
		if err := ps.server.Shutdown(ctx); err != nil {
			ps.logger.WithError(err).Error("Failed to shutdown HTTP server")
			return errors.Wrap(err, errors.CategoryServer, "SHUTDOWN_FAILED", "failed to shutdown HTTP server")
		}
	}
	
	ps.logger.Info("Proxy server shut down successfully")
	return nil
}

func (ps *ProxyServer) syncSongs() {
	ps.syncTicker = time.NewTicker(1 * time.Hour)
	defer ps.syncTicker.Stop()

	ps.fetchAndStoreSongs()

	for {
		select {
		case <-ps.syncTicker.C:
			ps.fetchAndStoreSongs()
		case <-ps.shutdownChan:
			ps.logger.Info("Stopping song sync goroutine")
			return
		}
	}
}

func (ps *ProxyServer) fetchAndStoreSongs() {
	ps.logger.Info("Syncing songs from Subsonic API")
	
	username, password := ps.credentials.GetValid()
	if username == "" || password == "" {
		ps.logger.WithError(errors.ErrNoValidCredentials).Warn("No valid credentials available for song syncing")
		return
	}
	
	// Construct URL with proper encoding to prevent credential exposure in logs
	baseURL, err := url.Parse(ps.config.UpstreamURL + "/rest/search3")
	if err != nil {
		parseErr := errors.Wrap(err, errors.CategoryNetwork, "URL_PARSE_FAILED", "failed to parse upstream URL").
			WithContext("upstream_url", ps.config.UpstreamURL).
			WithContext("username", username)
		ps.logger.WithError(parseErr).Error("Failed to parse upstream URL for song syncing")
		return
	}
	
	// Use URL query parameters to safely encode credentials
	params := url.Values{}
	params.Add("u", username)
	params.Add("p", password)
	params.Add("query", "*")
	params.Add("songCount", "10000")
	params.Add("f", "json")
	params.Add("v", "1.15.0")
	params.Add("c", "subsoxy")
	baseURL.RawQuery = params.Encode()
	
	resp, err := http.Get(baseURL.String())
	if err != nil {
		networkErr := errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to fetch songs from Subsonic API").
			WithContext("url", ps.config.UpstreamURL).
			WithContext("username", username)
		ps.logger.WithError(networkErr).Error("Failed to fetch songs from Subsonic API")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		httpErr := errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode)).
			WithContext("status_code", resp.StatusCode).
			WithContext("url", ps.config.UpstreamURL)
		ps.logger.WithError(httpErr).Error("Upstream server returned non-200 status")
		return
	}

	var subsonicResp models.SubsonicResponse
	if err := json.NewDecoder(resp.Body).Decode(&subsonicResp); err != nil {
		decodeErr := errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to decode Subsonic response").
			WithContext("url", ps.config.UpstreamURL)
		ps.logger.WithError(decodeErr).Error("Failed to decode Subsonic response")
		return
	}

	if subsonicResp.SubsonicResponse.Status != "ok" {
		authErr := errors.ErrUpstreamAuth.WithContext("status", subsonicResp.SubsonicResponse.Status).
			WithContext("username", username)
		ps.logger.WithError(authErr).Error("Subsonic API returned error status - possibly authentication failed")
		ps.credentials.ClearInvalid()
		return
	}

	if err := ps.db.StoreSongs(subsonicResp.SubsonicResponse.Songs.Song); err != nil {
		ps.logger.WithError(err).Error("Failed to store songs")
		return
	}

	ps.logger.WithField("count", len(subsonicResp.SubsonicResponse.Songs.Song)).Info("Successfully synced songs")
}

func (ps *ProxyServer) RecordPlayEvent(songID, eventType string, previousSong *string) {
	if err := ps.db.RecordPlayEvent(songID, eventType, previousSong); err != nil {
		ps.logger.WithError(err).Error("Failed to record play event")
		return
	}

	if previousSong != nil {
		if err := ps.db.RecordTransition(*previousSong, songID, eventType); err != nil {
			ps.logger.WithError(err).Error("Failed to record transition")
		}
	}

	ps.logger.WithFields(logrus.Fields{
		"songId":       songID,
		"eventType":    eventType,
		"previousSong": previousSong,
	}).Debug("Recorded play event")
}

func (ps *ProxyServer) SetLastPlayed(songID string) {
	song := &models.Song{ID: songID}
	ps.shuffle.SetLastPlayed(song)
}

func (ps *ProxyServer) GetHandlers() *handlers.Handler {
	return ps.handlers
}