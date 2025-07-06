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
	
	"github.com/syeo66/subsoxy/config"
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
}

func New(cfg *config.Config) (*ProxyServer, error) {
	logger := logrus.New()
	level, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	upstreamURL, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream URL: %w", err)
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
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	credManager := credentials.New(logger, cfg.UpstreamURL)
	shuffleService := shuffle.New(db, logger)
	handlersService := handlers.New(logger, shuffleService)

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

	if strings.HasPrefix(endpoint, "/rest/") {
		username := r.URL.Query().Get("u")
		password := r.URL.Query().Get("p")
		if username != "" && password != "" && len(username) > 0 && len(password) > 0 {
			go ps.credentials.ValidateAndStore(username, password)
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
			return err
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
		ps.logger.Warn("No valid credentials available for song syncing")
		return
	}
	
	url := fmt.Sprintf("%s/rest/search3?query=*&songCount=10000&f=json&v=1.15.0&c=subsoxy&u=%s&p=%s", 
		ps.config.UpstreamURL, username, password)
	
	resp, err := http.Get(url)
	if err != nil {
		ps.logger.WithError(err).Error("Failed to fetch songs from Subsonic API")
		return
	}
	defer resp.Body.Close()

	var subsonicResp models.SubsonicResponse
	if err := json.NewDecoder(resp.Body).Decode(&subsonicResp); err != nil {
		ps.logger.WithError(err).Error("Failed to decode Subsonic response")
		return
	}

	if subsonicResp.SubsonicResponse.Status != "ok" {
		ps.logger.Error("Subsonic API returned error status - possibly authentication failed")
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