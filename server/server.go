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
	"github.com/syeo66/subsoxy/credentials"
	"github.com/syeo66/subsoxy/database"
	"github.com/syeo66/subsoxy/errors"
	"github.com/syeo66/subsoxy/handlers"
	"github.com/syeo66/subsoxy/middleware"
	"github.com/syeo66/subsoxy/models"
	"github.com/syeo66/subsoxy/shuffle"
)

const (
	MaxEndpointLength   = 1000
	MaxUsernameLength   = 100
	MaxRemoteAddrLength = 100
)

// Server operation constants
const (
	SongSyncInterval     = 1 * time.Hour
	UserSyncStaggerDelay = 2 * time.Second
	CORSMaxAge           = "86400"
	SubsonicAPIVersion   = "1.15.0"
	ClientName           = "subsoxy"
)

// ASCII control character constants
const (
	ASCIIControlCharMin = 32
	ASCIIControlCharMax = 127
)

type ProxyServer struct {
	config            *config.Config
	logger            *logrus.Logger
	proxy             *httputil.ReverseProxy
	hooks             map[string][]models.Hook
	db                *database.DB
	credentials       *credentials.Manager
	handlers          *handlers.Handler
	shuffle           *shuffle.Service
	server            *http.Server
	syncTicker        *time.Ticker
	syncMutex         sync.RWMutex
	shutdownChan      chan struct{}
	rateLimiter       *rate.Limiter
	credentialWorkers chan struct{}   // Semaphore for limiting concurrent credential validations
	credentialWg      sync.WaitGroup   // WaitGroup for tracking in-flight credential validations
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
		"max_open_conns":     cfg.DBMaxOpenConns,
		"max_idle_conns":     cfg.DBMaxIdleConns,
		"conn_max_lifetime":  cfg.DBConnMaxLifetime,
		"conn_max_idle_time": cfg.DBConnMaxIdleTime,
		"health_check":       cfg.DBHealthCheck,
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

	// Initialize credential validation worker pool
	credentialWorkers := make(chan struct{}, cfg.CredentialWorkers)
	logger.WithFields(logrus.Fields{
		"max_workers": cfg.CredentialWorkers,
	}).Info("Credential validation worker pool configured")

	server := &ProxyServer{
		config:            cfg,
		logger:            logger,
		proxy:             proxy,
		hooks:             make(map[string][]models.Hook),
		db:                db,
		credentials:       credManager,
		handlers:          handlersService,
		shuffle:           shuffleService,
		shutdownChan:      make(chan struct{}),
		rateLimiter:       rateLimiter,
		credentialWorkers: credentialWorkers,
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
		username, password := ps.extractCredentials(r)

		// Validate input lengths
		if len(username) > MaxUsernameLength {
			ps.logger.WithFields(logrus.Fields{
				"username_length": len(username),
				"max_length":      MaxUsernameLength,
			}).Warn("Username too long, truncating")
			username = username[:MaxUsernameLength]
		}

		if username != "" && password != "" && len(username) > 0 && len(password) > 0 {
			ps.logger.WithField("username", sanitizeUsername(username)).Debug("Extracted credentials from request, attempting validation")

			// Acquire a worker slot (blocks if all workers are busy)
			ps.credentialWorkers <- struct{}{}
			ps.credentialWg.Add(1)

			go func() {
				defer func() {
					<-ps.credentialWorkers // Release worker slot
					ps.credentialWg.Done()  // Mark goroutine as complete
				}()

				isNewCredential, err := ps.credentials.ValidateAndStore(username, password)
				if err != nil {
					ps.logger.WithError(err).WithField("username", sanitizeUsername(username)).Warn("Failed to validate credentials")
				} else if isNewCredential {
					ps.logger.WithField("username", sanitizeUsername(username)).Info("New credentials captured, triggering immediate sync")
					// Trigger immediate sync for new credentials
					ps.fetchAndStoreSongs()
				}
			}()
		} else {
			// Log details about request without exposing any credential information
			ps.logger.WithFields(logrus.Fields{
				"has_username":  username != "",
				"has_password":  password != "",
				"endpoint":      sanitizedEndpoint,
				"method":        r.Method,
				"content_type":  r.Header.Get("Content-Type"),
				"authorization": r.Header.Get("Authorization") != "",
				"user_agent":    r.Header.Get("User-Agent"),
			}).Debug("No credentials found in request")
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

	// Safely stop the ticker
	ps.syncMutex.RLock()
	if ps.syncTicker != nil {
		ps.syncTicker.Stop()
	}
	ps.syncMutex.RUnlock()

	// Wait for in-flight credential validations to complete
	ps.logger.Info("Waiting for credential validation workers to finish...")
	done := make(chan struct{})
	go func() {
		ps.credentialWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		ps.logger.Info("All credential validation workers finished")
	case <-ctx.Done():
		ps.logger.Warn("Shutdown timeout reached, forcing shutdown")
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

// cleanupPendingSongs method removed - no longer needed with simplified skip detection

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

// syncSongsForUser handles song synchronization for a single user using directory traversal
func (ps *ProxyServer) syncSongsForUser(username, password string) error {
	ps.logger.WithField("user", sanitizeUsername(username)).Info("Syncing songs for user")

	// First, get all music folders
	musicFolders, err := ps.getMusicFolders(username, password)
	if err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "MUSIC_FOLDERS_FAILED", "failed to get music folders").
			WithContext("username", username)
	}

	var allSongs []models.Song

	// Traverse each music folder
	for _, folder := range musicFolders {
		// Convert folder ID to string
		folderID := fmt.Sprintf("%v", folder.ID)

		ps.logger.WithFields(logrus.Fields{
			"user":        sanitizeUsername(username),
			"folder_id":   folderID,
			"folder_name": folder.Name,
		}).Debug("Processing music folder")

		// Get indexes for this folder to get artists
		indexes, err := ps.getIndexes(username, password, folderID)
		if err != nil {
			ps.logger.WithError(err).WithFields(logrus.Fields{
				"user":      sanitizeUsername(username),
				"folder_id": folderID,
			}).Warn("Failed to get indexes for folder, skipping")
			continue
		}

		// Process each artist
		for _, index := range indexes {
			for _, artist := range index.Artists {
				ps.logger.WithFields(logrus.Fields{
					"user":        sanitizeUsername(username),
					"artist_id":   artist.ID,
					"artist_name": artist.Name,
				}).Debug("Processing artist")

				// Get albums for this artist
				albums, err := ps.getMusicDirectory(username, password, artist.ID)
				if err != nil {
					ps.logger.WithError(err).WithFields(logrus.Fields{
						"user":      sanitizeUsername(username),
						"artist_id": artist.ID,
					}).Warn("Failed to get albums for artist, skipping")
					continue
				}

				// Process each album
				for _, album := range albums {
					if album.IsDir {
						ps.logger.WithFields(logrus.Fields{
							"user":        sanitizeUsername(username),
							"album_id":    album.ID,
							"album_title": album.Title,
						}).Debug("Processing album")

						// Get songs for this album
						songs, err := ps.getMusicDirectory(username, password, album.ID)
						if err != nil {
							ps.logger.WithError(err).WithFields(logrus.Fields{
								"user":     sanitizeUsername(username),
								"album_id": album.ID,
							}).Warn("Failed to get songs for album, skipping")
							continue
						}

						// Add songs (filter out directories)
						for _, song := range songs {
							if !song.IsDir {
								allSongs = append(allSongs, song)
							}
						}
					}
				}
			}
		}
	}

	// Implement differential sync - get existing songs to determine what to add/update/delete
	existingSongIDs, err := ps.db.GetExistingSongIDs(username)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "EXISTING_SONGS_FAILED", "failed to get existing song IDs").
			WithContext("username", username)
	}

	// Create a map of current upstream songs for efficient lookup
	upstreamSongIDs := make(map[string]bool)
	for _, song := range allSongs {
		upstreamSongIDs[song.ID] = true
	}

	// Determine songs to delete (exist locally but not upstream)
	var songsToDelete []string
	for existingSongID := range existingSongIDs {
		if !upstreamSongIDs[existingSongID] {
			songsToDelete = append(songsToDelete, existingSongID)
		}
	}

	// Delete removed songs first
	if len(songsToDelete) > 0 {
		if err := ps.db.DeleteSongs(username, songsToDelete); err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "DELETE_FAILED", "failed to delete removed songs").
				WithContext("username", username).
				WithContext("songs_to_delete", len(songsToDelete))
		}
		ps.logger.WithFields(logrus.Fields{
			"user":    sanitizeUsername(username),
			"deleted": len(songsToDelete),
		}).Info("Removed songs no longer in upstream library")
	}

	// Calculate actually new songs (not just updated ones)
	var newSongs []models.Song
	var existingSongsToCheck []string
	for _, song := range allSongs {
		if !existingSongIDs[song.ID] {
			newSongs = append(newSongs, song)
		} else {
			existingSongsToCheck = append(existingSongsToCheck, song.ID)
		}
	}

	// Fetch existing songs to compare for actual changes
	var actuallyUpdatedCount int
	if len(existingSongsToCheck) > 0 {
		existingSongs, err := ps.db.GetSongsByIDs(username, existingSongsToCheck)
		if err != nil {
			ps.logger.WithError(err).WithField("user", sanitizeUsername(username)).Warn("Failed to fetch existing songs for comparison, counting all as updated")
			actuallyUpdatedCount = len(existingSongsToCheck)
		} else {
			// Compare each existing song with its new version to detect actual changes
			for _, song := range allSongs {
				if existingSong, exists := existingSongs[song.ID]; exists {
					if songHasChanged(existingSong, song) {
						actuallyUpdatedCount++
					}
				}
			}
		}
	}

	// Store/update current upstream songs (preserves existing play counts)
	if err := ps.db.StoreSongs(username, allSongs); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "STORAGE_FAILED", "failed to store songs for user").
			WithContext("username", username)
	}

	ps.logger.WithFields(logrus.Fields{
		"user":       sanitizeUsername(username),
		"total":      len(allSongs),
		"deleted":    len(songsToDelete),
		"added":      len(newSongs),
		"updated":    actuallyUpdatedCount,
		"unchanged":  len(existingSongsToCheck) - actuallyUpdatedCount,
	}).Info("Successfully completed differential sync for user")

	// Calculate artist statistics after sync completes
	if err := ps.db.CalculateInitialArtistStats(username); err != nil {
		ps.logger.WithError(err).WithField("user", sanitizeUsername(username)).Warn("Failed to calculate artist statistics")
		// Don't fail the entire sync if artist stats calculation fails
	}

	return nil
}

// getMusicFolders fetches all music folders for a user
func (ps *ProxyServer) getMusicFolders(username, password string) ([]models.MusicFolder, error) {
	baseURL, err := url.Parse(ps.config.UpstreamURL + "/rest/getMusicFolders")
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryNetwork, "URL_PARSE_FAILED", "failed to parse upstream URL")
	}

	params := ps.buildAuthParams(username, password)
	baseURL.RawQuery = params.Encode()

	resp, err := http.Get(baseURL.String())
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to fetch music folders")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode))
	}

	var response models.SubsonicResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to decode response")
	}

	if response.SubsonicResponse.Status != "ok" {
		return nil, errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", "API returned error status")
	}

	return response.SubsonicResponse.MusicFolders.MusicFolder, nil
}

// getIndexes fetches artist indexes for a music folder
func (ps *ProxyServer) getIndexes(username, password, musicFolderId string) ([]models.Index, error) {
	baseURL, err := url.Parse(ps.config.UpstreamURL + "/rest/getIndexes")
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryNetwork, "URL_PARSE_FAILED", "failed to parse upstream URL")
	}

	params := ps.buildAuthParams(username, password)
	if musicFolderId != "" {
		params.Add("musicFolderId", musicFolderId)
	}
	baseURL.RawQuery = params.Encode()

	resp, err := http.Get(baseURL.String())
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to fetch indexes")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode))
	}

	var response models.SubsonicResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to decode response")
	}

	if response.SubsonicResponse.Status != "ok" {
		return nil, errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", "API returned error status")
	}

	return response.SubsonicResponse.Indexes.Index, nil
}

// getMusicDirectory fetches directory contents (albums or songs)
func (ps *ProxyServer) getMusicDirectory(username, password, id string) ([]models.Song, error) {
	baseURL, err := url.Parse(ps.config.UpstreamURL + "/rest/getMusicDirectory")
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryNetwork, "URL_PARSE_FAILED", "failed to parse upstream URL")
	}

	params := ps.buildAuthParams(username, password)
	params.Add("id", id)
	baseURL.RawQuery = params.Encode()

	resp, err := http.Get(baseURL.String())
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to fetch music directory")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode))
	}

	var response models.SubsonicResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to decode response")
	}

	if response.SubsonicResponse.Status != "ok" {
		return nil, errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", "API returned error status")
	}

	return response.SubsonicResponse.Directory.Child, nil
}

// buildAuthParams builds authentication parameters for API calls
func (ps *ProxyServer) buildAuthParams(username, password string) url.Values {
	params := url.Values{}
	params.Add("u", username)

	// Check if this is token-based authentication
	if strings.HasPrefix(password, "TOKEN:") {
		// Extract token and salt from the special format: "TOKEN:token:salt"
		parts := strings.Split(password, ":")
		if len(parts) == 3 {
			params.Add("t", parts[1])
			params.Add("s", parts[2])
		}
	} else {
		// Traditional password-based authentication
		params.Add("p", password)
	}

	params.Add("f", "json")
	params.Add("v", SubsonicAPIVersion)
	params.Add("c", ClientName)

	return params
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

// extractCredentials extracts username and password from various sources in the request
func (ps *ProxyServer) extractCredentials(r *http.Request) (username, password string) {
	// First try URL query parameters (most common)
	username = r.URL.Query().Get("u")
	password = r.URL.Query().Get("p")

	if username != "" && password != "" {
		ps.logger.Debug("Credentials extracted from URL query parameters (password-based)")
		return username, password
	}

	// Try token-based authentication (t + s parameters)
	token := r.URL.Query().Get("t")
	salt := r.URL.Query().Get("s")

	if username != "" && token != "" && salt != "" {
		ps.logger.Debug("Token-based authentication detected - will validate with upstream")
		// For token-based auth, we return a special marker to indicate token validation needed
		// The validation will be done directly against the upstream server
		return username, "TOKEN:" + token + ":" + salt
	}

	// Try form-encoded POST data
	if r.Method == "POST" {
		if err := r.ParseForm(); err == nil {
			formUsername := r.PostForm.Get("u")
			formPassword := r.PostForm.Get("p")
			if formUsername != "" && formPassword != "" {
				ps.logger.Debug("Credentials extracted from POST form data (password-based)")
				return formUsername, formPassword
			}

			// Try token-based form auth
			formToken := r.PostForm.Get("t")
			formSalt := r.PostForm.Get("s")
			if formUsername != "" && formToken != "" && formSalt != "" {
				ps.logger.Debug("Token-based authentication detected in POST form")
				return formUsername, "TOKEN:" + formToken + ":" + formSalt
			}
		}
	}

	// Try Authorization header (Basic Auth format)
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		// Handle Basic Auth: "Basic base64(username:password)"
		if strings.HasPrefix(authHeader, "Basic ") {
			if headerUsername, headerPassword, ok := r.BasicAuth(); ok {
				ps.logger.Debug("Credentials extracted from Authorization header (Basic Auth)")
				return headerUsername, headerPassword
			}
		}
	}

	// Try custom headers (some clients use these)
	if headerUsername := r.Header.Get("X-Subsonic-Username"); headerUsername != "" {
		if headerPassword := r.Header.Get("X-Subsonic-Password"); headerPassword != "" {
			ps.logger.Debug("Credentials extracted from X-Subsonic headers")
			return headerUsername, headerPassword
		}
	}

	ps.logger.Debug("No credentials found in request")
	return "", ""
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


// songHasChanged compares two songs to detect if metadata has actually changed
func songHasChanged(existing, new models.Song) bool {
	return existing.Title != new.Title ||
		existing.Artist != new.Artist ||
		existing.Album != new.Album ||
		existing.Duration != new.Duration ||
		existing.CoverArt != new.CoverArt
}


// ProcessScrobble processes a scrobble event and handles pending songs
// Returns true if a play event should be recorded, false if it's a duplicate submission
func (ps *ProxyServer) ProcessScrobble(userID, songID string, isSubmission bool) bool {
	recordSkipFunc := func(userID string, song *models.Song) {
		err := ps.db.RecordPlayEvent(userID, song.ID, "skip", nil)
		if err != nil {
			ps.logger.WithError(err).WithFields(logrus.Fields{
				"user_id": userID,
				"song_id": song.ID,
			}).Error("Failed to record skip event from pending song processing")
		}
	}
	return ps.shuffle.ProcessScrobble(userID, songID, isSubmission, recordSkipFunc)
}


func (ps *ProxyServer) GetHandlers() *handlers.Handler {
	return ps.handlers
}
