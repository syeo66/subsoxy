package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
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
	"github.com/syeo66/subsoxy/middleware"
)

const (
	MaxEndpointLength = 1000
	MaxUsernameLength = 100
	MaxRemoteAddrLength = 100
)

// Server operation constants
const (
	SongSyncInterval     = 1 * time.Hour
	UserSyncStaggerDelay = 2 * time.Second
	CORSMaxAge           = "86400"
	SongSyncCount        = "10000"
	SubsonicAPIVersion   = "1.15.0"
	ClientName           = "subsoxy"
)

// ASCII control character constants
const (
	ASCIIControlCharMin = 32
	ASCIIControlCharMax = 127
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
	syncMutex   sync.RWMutex
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

	// Create database connection pool configuration
	poolConfig := &database.ConnectionPool{
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
		HealthCheck:     cfg.DBHealthCheck,
	}

	db, err := database.NewWithPool(cfg.DatabasePath, logger, poolConfig)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryServer, "INITIALIZATION_FAILED", "failed to initialize database").
			WithContext("database_path", cfg.DatabasePath)
	}

	logger.WithFields(logrus.Fields{
		"max_open_conns":      cfg.DBMaxOpenConns,
		"max_idle_conns":      cfg.DBMaxIdleConns,
		"conn_max_lifetime":   cfg.DBConnMaxLifetime,
		"conn_max_idle_time":  cfg.DBConnMaxIdleTime,
		"health_check":        cfg.DBHealthCheck,
	}).Info("Database connection pool configured")

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

// sanitizeForLogging removes control characters and limits length to prevent log injection
func sanitizeForLogging(input string) string {
	// Remove control characters (ASCII 0-31 and 127)
	sanitized := strings.Map(func(r rune) rune {
		if r < ASCIIControlCharMin || r == ASCIIControlCharMax {
			return -1
		}
		return r
	}, input)
	
	// Limit length to prevent resource exhaustion
	if len(sanitized) > MaxEndpointLength {
		sanitized = sanitized[:MaxEndpointLength] + "..."
	}
	
	return sanitized
}

// sanitizeRemoteAddr sanitizes remote address for logging
func sanitizeRemoteAddr(remoteAddr string) string {
	if len(remoteAddr) > MaxRemoteAddrLength {
		return remoteAddr[:MaxRemoteAddrLength] + "..."
	}
	return remoteAddr
}

// sanitizeUsername sanitizes username for logging
func sanitizeUsername(username string) string {
	// Remove control characters
	sanitized := strings.Map(func(r rune) rune {
		if r < ASCIIControlCharMin || r == ASCIIControlCharMax {
			return -1
		}
		return r
	}, username)
	
	// Limit length
	if len(sanitized) > MaxUsernameLength {
		sanitized = sanitized[:MaxUsernameLength] + "..."
	}
	
	return sanitized
}

func (ps *ProxyServer) AddHook(endpoint string, hook models.Hook) {
	ps.hooks[endpoint] = append(ps.hooks[endpoint], hook)
}

// setCORSHeaders sets CORS headers based on configuration
func (ps *ProxyServer) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	
	// Set Access-Control-Allow-Origin
	if len(ps.config.CORSAllowOrigins) > 0 {
		if ps.config.CORSAllowOrigins[0] == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			// Check if the origin is in the allowed list
			for _, allowedOrigin := range ps.config.CORSAllowOrigins {
				if origin == allowedOrigin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
		}
	}
	
	// Set Access-Control-Allow-Methods
	if len(ps.config.CORSAllowMethods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(ps.config.CORSAllowMethods, ", "))
	}
	
	// Set Access-Control-Allow-Headers
	if len(ps.config.CORSAllowHeaders) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(ps.config.CORSAllowHeaders, ", "))
	}
	
	// Set Access-Control-Allow-Credentials
	if ps.config.CORSAllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	
	// Set Access-Control-Max-Age for preflight cache (24 hours)
	w.Header().Set("Access-Control-Max-Age", CORSMaxAge)
}

func (ps *ProxyServer) proxyHandler(w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path
	
	// Add CORS headers if enabled
	if ps.config.CORSEnabled {
		ps.setCORSHeaders(w, r)
		
		// Handle preflight OPTIONS requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	
	// Sanitize inputs for logging
	sanitizedEndpoint := sanitizeForLogging(endpoint)
	sanitizedRemoteAddr := sanitizeRemoteAddr(r.RemoteAddr)
	
	ps.logger.WithFields(logrus.Fields{
		"method":   r.Method,
		"endpoint": sanitizedEndpoint,
		"remote":   sanitizedRemoteAddr,
	}).Info("Incoming request")

	if ps.rateLimiter != nil {
		if !ps.rateLimiter.Allow() {
			ps.logger.WithFields(logrus.Fields{
				"endpoint": sanitizedEndpoint,
				"remote":   sanitizedRemoteAddr,
			}).Warn("Rate limit exceeded")
			
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	if strings.HasPrefix(endpoint, "/rest/") {
		username := r.URL.Query().Get("u")
		password := r.URL.Query().Get("p")
		
		// Validate input lengths
		if len(username) > MaxUsernameLength {
			ps.logger.WithFields(logrus.Fields{
				"username_length": len(username),
				"max_length": MaxUsernameLength,
			}).Warn("Username too long, truncating")
			username = username[:MaxUsernameLength]
		}
		
		if username != "" && password != "" && len(username) > 0 && len(password) > 0 {
			go func() {
				if err := ps.credentials.ValidateAndStore(username, password); err != nil {
					ps.logger.WithError(err).WithField("username", sanitizeUsername(username)).Debug("Failed to validate credentials")
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
		ps.logger.WithField("endpoint", sanitizedEndpoint).Debug("Subsonic API endpoint")
	}

	ps.proxy.ServeHTTP(w, r)
}

func (ps *ProxyServer) Start() error {
	if ps.server != nil {
		return errors.ErrServerStart.WithContext("reason", "server already started")
	}

	router := mux.NewRouter()
	
	// Add security headers middleware
	if ps.config.SecurityHeadersEnabled {
		securityMiddleware := middleware.NewSecurityHeaders(ps.config, ps.logger)
		router.Use(securityMiddleware.Handler)
		ps.logger.WithField("dev_mode", ps.config.IsDevMode()).Info("Security headers middleware enabled")
	} else {
		ps.logger.Info("Security headers middleware disabled")
	}
	
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
	
	// Safely stop the sync ticker
	ps.syncMutex.RLock()
	if ps.syncTicker != nil {
		ps.syncTicker.Stop()
	}
	ps.syncMutex.RUnlock()
	
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
	// Safely create and store the ticker
	ps.syncMutex.Lock()
	ps.syncTicker = time.NewTicker(SongSyncInterval)
	ps.syncMutex.Unlock()
	
	defer func() {
		ps.syncMutex.Lock()
		if ps.syncTicker != nil {
			ps.syncTicker.Stop()
		}
		ps.syncMutex.Unlock()
	}()

	// Skip initial sync - wait for credentials to be captured from client requests
	ps.logger.Info("Song sync routine started - waiting for valid credentials from client requests")

	for {
		ps.syncMutex.RLock()
		ticker := ps.syncTicker
		ps.syncMutex.RUnlock()
		
		if ticker == nil {
			return
		}
		
		select {
		case <-ticker.C:
			ps.fetchAndStoreSongs()
		case <-ps.shutdownChan:
			ps.logger.Info("Stopping song sync goroutine")
			return
		}
	}
}

func (ps *ProxyServer) fetchAndStoreSongs() {
	// Get all valid credentials for multi-user sync
	allCredentials := ps.credentials.GetAllValid()
	if len(allCredentials) == 0 {
		ps.logger.Debug("Skipping song sync - no valid credentials available yet (waiting for client requests)")
		return
	}
	
	ps.logger.Info("Syncing songs from Subsonic API")
	
	ps.logger.WithField("user_count", len(allCredentials)).Info("Starting multi-user song sync")
	
	// Sync songs for each user with staggered delays
	for i, username := range getSortedUsernames(allCredentials) {
		password := allCredentials[username]
		
		// Add staggered delay to avoid overwhelming upstream server (except for first user)
		if i > 0 {
			delay := time.Duration(i) * UserSyncStaggerDelay
			ps.logger.WithFields(logrus.Fields{
				"user":  sanitizeUsername(username),
				"delay": delay,
			}).Debug("Adding staggered delay for user sync")
			time.Sleep(delay)
		}
		
		// Sync songs for this specific user
		if err := ps.syncSongsForUser(username, password); err != nil {
			ps.logger.WithError(err).WithField("user", sanitizeUsername(username)).Error("Failed to sync songs for user")
			// Continue with other users even if one fails
			continue
		}
	}
	
	ps.logger.Info("Multi-user song sync completed")
}

// syncSongsForUser handles song synchronization for a single user
func (ps *ProxyServer) syncSongsForUser(username, password string) error {
	ps.logger.WithField("user", sanitizeUsername(username)).Info("Syncing songs for user")
	
	// Construct URL with proper encoding to prevent credential exposure in logs
	baseURL, err := url.Parse(ps.config.UpstreamURL + "/rest/search3")
	if err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "URL_PARSE_FAILED", "failed to parse upstream URL").
			WithContext("upstream_url", ps.config.UpstreamURL).
			WithContext("username", username)
	}
	
	// Use URL query parameters to safely encode credentials
	params := url.Values{}
	params.Add("u", username)
	params.Add("p", password)
	params.Add("query", "*")
	params.Add("songCount", SongSyncCount)
	params.Add("f", "json")
	params.Add("v", SubsonicAPIVersion)
	params.Add("c", ClientName)
	baseURL.RawQuery = params.Encode()
	
	resp, err := http.Get(baseURL.String())
	if err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to fetch songs from Subsonic API").
			WithContext("url", ps.config.UpstreamURL).
			WithContext("username", username)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode)).
			WithContext("status_code", resp.StatusCode).
			WithContext("url", ps.config.UpstreamURL).
			WithContext("username", username)
	}

	var subsonicResp models.SubsonicResponse
	if err := json.NewDecoder(resp.Body).Decode(&subsonicResp); err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to decode Subsonic response").
			WithContext("url", ps.config.UpstreamURL).
			WithContext("username", username)
	}

	if subsonicResp.SubsonicResponse.Status != "ok" {
		authErr := errors.ErrUpstreamAuth.WithContext("status", subsonicResp.SubsonicResponse.Status).
			WithContext("username", username)
		ps.logger.WithError(authErr).Error("Subsonic API returned error status for user - possibly authentication failed")
		// Note: We don't clear ALL credentials here, only handle per-user errors
		return authErr
	}

	if err := ps.db.StoreSongs(username, subsonicResp.SubsonicResponse.Songs.Song); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "STORAGE_FAILED", "failed to store songs for user").
			WithContext("username", username)
	}

	ps.logger.WithFields(logrus.Fields{
		"user":  sanitizeUsername(username),
		"count": len(subsonicResp.SubsonicResponse.Songs.Song),
	}).Info("Successfully synced songs for user")
	
	return nil
}

// getSortedUsernames returns a sorted slice of usernames for consistent ordering
func getSortedUsernames(credentials map[string]string) []string {
	usernames := make([]string, 0, len(credentials))
	for username := range credentials {
		usernames = append(usernames, username)
	}
	// Sort to ensure consistent ordering across sync runs
	for i := 0; i < len(usernames)-1; i++ {
		for j := i + 1; j < len(usernames); j++ {
			if usernames[i] > usernames[j] {
				usernames[i], usernames[j] = usernames[j], usernames[i]
			}
		}
	}
	return usernames
}

func (ps *ProxyServer) RecordPlayEvent(userID, songID, eventType string, previousSong *string) {
	if err := ps.db.RecordPlayEvent(userID, songID, eventType, previousSong); err != nil {
		ps.logger.WithError(err).WithField("userID", sanitizeUsername(userID)).Error("Failed to record play event")
		return
	}

	if previousSong != nil {
		if err := ps.db.RecordTransition(userID, *previousSong, songID, eventType); err != nil {
			ps.logger.WithError(err).WithField("userID", sanitizeUsername(userID)).Error("Failed to record transition")
		}
	}

	// Sanitize inputs for logging
	sanitizedUserID := sanitizeUsername(userID)
	sanitizedSongID := sanitizeForLogging(songID)
	var sanitizedPreviousSong *string
	if previousSong != nil {
		sanitized := sanitizeForLogging(*previousSong)
		sanitizedPreviousSong = &sanitized
	}

	ps.logger.WithFields(logrus.Fields{
		"userID":       sanitizedUserID,
		"songId":       sanitizedSongID,
		"eventType":    eventType,
		"previousSong": sanitizedPreviousSong,
	}).Debug("Recorded play event")
}

func (ps *ProxyServer) SetLastPlayed(userID, songID string) {
	song := &models.Song{ID: songID}
	ps.shuffle.SetLastPlayed(userID, song)
}

func (ps *ProxyServer) GetHandlers() *handlers.Handler {
	return ps.handlers
}