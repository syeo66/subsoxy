package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
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
	// Largest form-encoded POST body parsed for credentials, the same limit net/http uses
	MaxFormBodySize = 10 << 20
)

// Server operation constants
const (
	SongSyncInterval     = 1 * time.Hour
	UserSyncStaggerDelay = 2 * time.Second
	// Forwarded requests only capture credentials for sync, so a user whose stored
	// credential upstream confirmed this recently isn't validated again
	CredentialRecaptureInterval = 1 * time.Hour
	CORSMaxAge                  = "86400"
	// Upper bound for one library sync request. Generous, since a directory listing can be large.
	UpstreamRequestTimeout = 30 * time.Second
	// Similarity lookups run while a client waits for getRandomSongs, so they give up sooner.
	SimilarSongsTimeout = 5 * time.Second
	// Songs requested per search3 page during library sync
	LibraryPageSize    = 500
	ReadHeaderTimeout  = 10 * time.Second
	SubsonicAPIVersion = "1.15.0"
	ClientName         = "subsoxy"
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
	authRequired      map[string]bool // Endpoints whose hooks act on the user's data and need verified credentials
	db                *database.DB
	credentials       *credentials.Manager
	handlers          *handlers.Handler
	shuffle           *shuffle.Service
	upstreamClient    *http.Client
	server            *http.Server
	syncTicker        *time.Ticker
	syncMutex         sync.RWMutex
	shutdownChan      chan struct{}
	rateLimiter       *rate.Limiter
	credentialWorkers chan struct{}  // Semaphore for limiting concurrent credential validations
	credentialWg      sync.WaitGroup // WaitGroup for tracking in-flight credential validations and the syncs they start
	syncingUsers      sync.Map       // Usernames with a library sync in progress
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
		// Go moves the Host header into req.Host, so read it there before replacing it
		req.Header.Set("X-Forwarded-Host", req.Host)
		req.Host = upstreamURL.Host
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
		authRequired:      make(map[string]bool),
		db:                db,
		credentials:       credManager,
		handlers:          handlersService,
		shuffle:           shuffleService,
		upstreamClient:    &http.Client{Timeout: UpstreamRequestTimeout},
		shutdownChan:      make(chan struct{}),
		rateLimiter:       rateLimiter,
		credentialWorkers: credentialWorkers,
	}

	server.shuffle.SetSimilarSongsFetcher(server.fetchSimilarSongs)

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

// AddAuthenticatedHook registers a hook that only runs once the request's credentials
// have been verified against upstream for the user named in "u". Use it for hooks that
// answer locally or record data for the user. When verification fails the hooks are
// skipped and the request is forwarded unchanged, so upstream answers it with its own
// authentication handling.
func (ps *ProxyServer) AddAuthenticatedHook(endpoint string, hook models.Hook) {
	ps.authRequired[endpoint] = true
	ps.AddHook(endpoint, hook)
}

// isAuthenticated verifies the request's credentials with the same upstream validation
// used for credential capture, and checks that they belong to the user the hooks act on.
func (ps *ProxyServer) isAuthenticated(r *http.Request, username, password string) bool {
	if username == "" || password == "" {
		return false
	}

	// Hooks identify the user by the "u" parameter
	if r.FormValue("u") != username {
		ps.logger.WithFields(logrus.Fields{
			"username": sanitizeUsername(username),
			"endpoint": sanitizeForLogging(r.URL.Path),
			"remote":   sanitizeRemoteAddr(r.RemoteAddr),
		}).Warn("Request user does not match credentials, forwarding without local handling")
		return false
	}

	isNewCredential, err := ps.credentials.ValidateAndStore(username, password)
	if err != nil {
		ps.logger.WithError(err).WithField("username", sanitizeUsername(username)).Warn("Failed to validate credentials, forwarding without local handling")
		return false
	}

	if isNewCredential {
		ps.logger.WithField("username", sanitizeUsername(username)).Info("New credentials captured, triggering immediate sync")
		ps.credentialWg.Go(func() { ps.syncUser(username) })
	}

	return true
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

	// Hooks read parameters with r.FormValue, which must not consume the forwarded body
	ps.parseForm(r)

	needsAuth := ps.authRequired[endpoint]
	authenticated := false

	if strings.HasPrefix(endpoint, "/rest/") || needsAuth {
		username, password := ps.extractCredentials(r)

		// Validate input lengths
		if len(username) > MaxUsernameLength {
			ps.logger.WithFields(logrus.Fields{
				"username_length": len(username),
				"max_length":      MaxUsernameLength,
			}).Warn("Username too long, truncating")
			username = username[:MaxUsernameLength]
		}

		if needsAuth {
			// Validated synchronously, which also captures the credentials for sync
			authenticated = ps.isAuthenticated(r, username, password)
		} else if username != "" && password != "" {
			if !ps.credentials.HasRecent(username, CredentialRecaptureInterval) {
				ps.captureCredentials(username, password)
			}
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

	if hooks, exists := ps.hooks[endpoint]; exists && (!needsAuth || authenticated) {
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

// captureCredentials validates credentials from a forwarded request in the background and
// stores them for sync. It's best effort: when every worker is busy the credentials are
// left for a later request, so forwarding is never held up.
func (ps *ProxyServer) captureCredentials(username, password string) {
	select {
	case ps.credentialWorkers <- struct{}{}:
	default:
		ps.logger.WithField("username", sanitizeUsername(username)).Debug("All credential workers busy, skipping credential capture")
		return
	}
	ps.credentialWg.Add(1)

	go func() {
		defer func() {
			<-ps.credentialWorkers // Release worker slot
			ps.credentialWg.Done() // Mark goroutine as complete
		}()

		ps.logger.WithField("username", sanitizeUsername(username)).Debug("Extracted credentials from request, attempting validation")
		isNewCredential, err := ps.credentials.ValidateAndStore(username, password)
		if err != nil {
			ps.logger.WithError(err).WithField("username", sanitizeUsername(username)).Warn("Failed to validate credentials")
		} else if isNewCredential {
			ps.logger.WithField("username", sanitizeUsername(username)).Info("New credentials captured, triggering immediate sync")
			// Sync outside the worker slot, which is only meant for validation
			ps.credentialWg.Go(func() { ps.syncUser(username) })
		}
	}()
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
		Addr:              ":" + ps.config.ProxyPort,
		Handler:           router,
		ReadHeaderTimeout: ReadHeaderTimeout,
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

	// Stop accepting requests and let in-flight handlers finish while the database is still open
	var shutdownErr error
	if ps.server != nil {
		if err := ps.server.Shutdown(ctx); err != nil {
			ps.logger.WithError(err).Error("Failed to shutdown HTTP server")
			shutdownErr = errors.Wrap(err, errors.CategoryServer, "SHUTDOWN_FAILED", "failed to shutdown HTTP server")
		}
	}

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

	if shutdownErr != nil {
		return shutdownErr
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

	// Sync users one after another, pausing between them to avoid overwhelming upstream
	for i, username := range getSortedUsernames(allCredentials) {
		if i > 0 {
			select {
			case <-time.After(UserSyncStaggerDelay):
			case <-ps.shutdownChan:
				ps.logger.Info("Shutdown requested, stopping multi-user song sync")
				return
			}
		}

		ps.syncUserWithPassword(username, allCredentials[username])
	}

	ps.logger.Info("Multi-user song sync completed")
}

// syncUser syncs one user's library with their stored credentials
func (ps *ProxyServer) syncUser(username string) {
	password, ok := ps.credentials.Get(username)
	if !ok {
		ps.logger.WithField("user", sanitizeUsername(username)).Warn("No stored credentials for user, skipping sync")
		return
	}
	ps.syncUserWithPassword(username, password)
}

// syncUserWithPassword syncs one user's library unless a sync for that user is already running
func (ps *ProxyServer) syncUserWithPassword(username, password string) {
	if _, running := ps.syncingUsers.LoadOrStore(username, struct{}{}); running {
		ps.logger.WithField("user", sanitizeUsername(username)).Debug("Sync already in progress for user, skipping")
		return
	}
	defer ps.syncingUsers.Delete(username)

	if err := ps.syncSongsForUser(username, password); err != nil {
		ps.logger.WithError(err).WithField("user", sanitizeUsername(username)).Error("Failed to sync songs for user")
	}
}

// syncSongsForUser fetches a user's library from upstream and applies the differences locally
func (ps *ProxyServer) syncSongsForUser(username, password string) error {
	ps.logger.WithField("user", sanitizeUsername(username)).Info("Syncing songs for user")

	allSongs, incomplete, err := ps.fetchLibrary(username, password)
	if err != nil {
		return err
	}

	// Differential sync: compare upstream with the stored songs to find what to add, update
	// and delete, so only those are written
	storedSongs, err := ps.db.GetAllSongs(username)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "EXISTING_SONGS_FAILED", "failed to get existing songs").
			WithContext("username", username)
	}
	existingSongs := make(map[string]models.Song, len(storedSongs))
	for _, song := range storedSongs {
		existingSongs[song.ID] = song
	}

	// Create a map of current upstream songs for efficient lookup
	upstreamSongIDs := make(map[string]bool)
	for _, song := range allSongs {
		upstreamSongIDs[song.ID] = true
	}

	// Determine songs to delete (exist locally but not upstream)
	var songsToDelete []string
	for existingSongID := range existingSongs {
		if !upstreamSongIDs[existingSongID] {
			songsToDelete = append(songsToDelete, existingSongID)
		}
	}

	// Deleting a song also deletes its play history, so only do it after a complete fetch
	if incomplete && len(songsToDelete) > 0 {
		ps.logger.WithFields(logrus.Fields{
			"user":            sanitizeUsername(username),
			"songs_to_delete": len(songsToDelete),
		}).Warn("Library fetch was incomplete, keeping songs missing from upstream until the next complete sync")
		songsToDelete = nil
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

	// Only new songs and songs whose metadata changed need writing
	var songsToStore []models.Song
	addedCount, updatedCount := 0, 0
	for _, song := range allSongs {
		existingSong, exists := existingSongs[song.ID]
		switch {
		case !exists:
			addedCount++
		case songHasChanged(existingSong, song):
			updatedCount++
		default:
			continue
		}
		songsToStore = append(songsToStore, song)
	}

	// Store/update the changed songs (preserves existing play counts)
	if len(songsToStore) > 0 {
		if err := ps.db.StoreSongs(username, songsToStore); err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "STORAGE_FAILED", "failed to store songs for user").
				WithContext("username", username)
		}
	}

	ps.logger.WithFields(logrus.Fields{
		"user":      sanitizeUsername(username),
		"total":     len(allSongs),
		"deleted":   len(songsToDelete),
		"added":     addedCount,
		"updated":   updatedCount,
		"unchanged": len(allSongs) - addedCount - updatedCount,
	}).Info("Successfully completed differential sync for user")

	// Priors average over the user's songs and artists, which may have just changed
	ps.shuffle.InvalidateEmpiricalPriors(username)

	return nil
}

// fetchLibrary returns every song in the user's library. It pages through search3 with an
// empty query, which takes a few dozen requests even for a large library, and falls back to
// walking the folder tree (a request per artist and album) when the server doesn't answer
// that search. incomplete is set when part of the library could not be fetched: the list is
// then partial, so songs missing from it must not be treated as removed upstream.
func (ps *ProxyServer) fetchLibrary(username, password string) (songs []models.Song, incomplete bool, err error) {
	songs, err = ps.fetchLibraryBySearch(username, password)
	if err == nil && len(songs) > 0 {
		return songs, false, nil
	}

	// No songs could also mean the server doesn't treat an empty query as "everything",
	// so an empty library is only believed once the folder walk agrees
	ps.logger.WithError(err).WithField("user", sanitizeUsername(username)).
		Info("Library search returned no songs, syncing by walking the music folders")
	return ps.fetchLibraryByDirectories(username, password)
}

// fetchLibraryBySearch pages through all songs with search3 and an empty query
func (ps *ProxyServer) fetchLibraryBySearch(username, password string) ([]models.Song, error) {
	var songs []models.Song
	seen := make(map[string]bool)

	for offset := 0; ; offset += LibraryPageSize {
		params := ps.buildAuthParams(username, password)
		params.Set("query", "")
		params.Set("artistCount", "0")
		params.Set("albumCount", "0")
		params.Set("songCount", strconv.Itoa(LibraryPageSize))
		params.Set("songOffset", strconv.Itoa(offset))

		var response models.SearchResult3Response
		if err := ps.getSubsonic("search3", params, &response); err != nil {
			return nil, err
		}

		page := response.SubsonicResponse.SearchResult3.Song
		added := 0
		for _, song := range page {
			// Pages can overlap when the library changes during the sync
			if !song.IsDir && !seen[song.ID] {
				seen[song.ID] = true
				songs = append(songs, song)
				added++
			}
		}

		if len(page) < LibraryPageSize {
			return songs, nil
		}
		// A full page of songs already seen means the server ignores songOffset
		if added == 0 {
			return nil, errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", "search3 paging returned no new songs")
		}
	}
}

// fetchLibraryByDirectories walks music folders, artists and albums to collect all songs
func (ps *ProxyServer) fetchLibraryByDirectories(username, password string) ([]models.Song, bool, error) {
	// First, get all music folders
	musicFolders, err := ps.getMusicFolders(username, password)
	if err != nil {
		return nil, false, errors.Wrap(err, errors.CategoryNetwork, "MUSIC_FOLDERS_FAILED", "failed to get music folders").
			WithContext("username", username)
	}

	var allSongs []models.Song
	incomplete := false

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
			incomplete = true
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
					incomplete = true
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
							incomplete = true
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

	return allSongs, incomplete, nil
}

// subsonicResponse is a decoded Subsonic response that reports its status
type subsonicResponse interface {
	ResponseStatus() string
}

// getSubsonic calls a Subsonic endpoint upstream and decodes its JSON response into
// response, failing unless the response status is "ok"
func (ps *ProxyServer) getSubsonic(endpoint string, params url.Values, response subsonicResponse) error {
	baseURL, err := url.Parse(ps.config.UpstreamURL + "/rest/" + endpoint)
	if err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "URL_PARSE_FAILED", "failed to parse upstream URL")
	}
	baseURL.RawQuery = params.Encode()

	resp, err := ps.upstreamClient.Get(baseURL.String())
	if err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to fetch "+endpoint)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode))
	}

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to decode response")
	}

	if response.ResponseStatus() != "ok" {
		return errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", "API returned error status")
	}

	return nil
}

// getMusicFolders fetches all music folders for a user
func (ps *ProxyServer) getMusicFolders(username, password string) ([]models.MusicFolder, error) {
	var response models.SubsonicResponse
	if err := ps.getSubsonic("getMusicFolders", ps.buildAuthParams(username, password), &response); err != nil {
		return nil, err
	}
	return response.SubsonicResponse.MusicFolders.MusicFolder, nil
}

// getIndexes fetches artist indexes for a music folder
func (ps *ProxyServer) getIndexes(username, password, musicFolderId string) ([]models.Index, error) {
	params := ps.buildAuthParams(username, password)
	if musicFolderId != "" {
		params.Add("musicFolderId", musicFolderId)
	}
	var response models.SubsonicResponse
	if err := ps.getSubsonic("getIndexes", params, &response); err != nil {
		return nil, err
	}
	return response.SubsonicResponse.Indexes.Index, nil
}

// getMusicDirectory fetches directory contents (albums or songs)
func (ps *ProxyServer) getMusicDirectory(username, password, id string) ([]models.Song, error) {
	params := ps.buildAuthParams(username, password)
	params.Add("id", id)
	var response models.SubsonicResponse
	if err := ps.getSubsonic("getMusicDirectory", params, &response); err != nil {
		return nil, err
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
	if r.Form == nil {
		ps.parseForm(r)
	}

	// Query parameters or form-encoded POST body (most common)
	username = r.Form.Get("u")
	password = r.Form.Get("p")

	if username != "" && password != "" {
		ps.logger.Debug("Credentials extracted from request parameters (password-based)")
		return username, password
	}

	// Try token-based authentication (t + s parameters)
	token := r.Form.Get("t")
	salt := r.Form.Get("s")

	if username != "" && token != "" && salt != "" {
		ps.logger.Debug("Token-based authentication detected - will validate with upstream")
		// For token-based auth, we return a special marker to indicate token validation needed
		// The validation will be done directly against the upstream server
		return username, "TOKEN:" + token + ":" + salt
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

// parseForm fills r.Form and r.PostForm from the query and a form-encoded POST body
// (OpenSubsonic formPost extension), like r.ParseForm but without consuming the body,
// so hooks can read parameters with r.FormValue and the request can still be forwarded.
func (ps *ProxyServer) parseForm(r *http.Request) {
	postForm := url.Values{}
	if r.Method == http.MethodPost {
		parsed, err := readPostForm(r)
		if err != nil {
			ps.logger.WithError(err).WithField("endpoint", sanitizeForLogging(r.URL.Path)).Warn("Failed to parse POST form, using query parameters only")
		} else {
			postForm = parsed
		}
	}

	// Body values come first, as with r.ParseForm
	form := url.Values{}
	for key, values := range postForm {
		form[key] = append(form[key], values...)
	}
	for key, values := range r.URL.Query() {
		form[key] = append(form[key], values...)
	}

	r.PostForm = postForm
	r.Form = form
}

// readPostForm parses a form-encoded POST body without consuming it, so the request can
// still be forwarded upstream with its body intact.
func readPostForm(r *http.Request) (url.Values, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return url.Values{}, nil
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/x-www-form-urlencoded" {
		return url.Values{}, nil
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxFormBodySize+1))
	// Put back what was read in front of whatever is left, so upstream gets the full body
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(body), r.Body), r.Body}
	if err != nil {
		return nil, err
	}
	if len(body) > MaxFormBodySize {
		return nil, errors.ErrInvalidInput.WithContext("reason", "form body too large")
	}

	return url.ParseQuery(string(body))
}

func (ps *ProxyServer) RecordPlayEvent(userID, songID, eventType string, previousSong *string) {
	ps.recordPlayEvent(userID, songID, eventType, previousSong, time.Now())
}

// RecordPlayEventAt records a play or skip that happened at the given time
func (ps *ProxyServer) RecordPlayEventAt(userID, songID, eventType string, at time.Time) {
	ps.recordPlayEvent(userID, songID, eventType, nil, at)
}

func (ps *ProxyServer) recordPlayEvent(userID, songID, eventType string, previousSong *string, at time.Time) {
	if err := ps.db.RecordPlayEventAt(userID, songID, eventType, previousSong, at); err != nil {
		ps.logger.WithError(err).WithField("userID", sanitizeUsername(userID)).Error("Failed to record play event")
		return
	}
	ps.shuffle.InvalidateEmpiricalPriors(userID)

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
			return
		}
		ps.shuffle.InvalidateEmpiricalPriors(userID)
	}
	return ps.shuffle.ProcessScrobble(userID, songID, isSubmission, recordSkipFunc)
}

// fetchSimilarSongs queries the upstream's getSonicSimilarTracks endpoint provided by the
// AudioMuse-AI-NV-plugin (Navidrome v0.62+ OpenSubsonic sonicSimilarity extension).
// Returns nil without error when the plugin is not available so the caller gracefully
// skips the similarity weight.
func (ps *ProxyServer) fetchSimilarSongs(userID, songID string) (map[string]float64, error) {
	password, ok := ps.credentials.Get(userID)
	if !ok {
		return nil, nil
	}

	baseURL, err := url.Parse(ps.config.UpstreamURL + "/rest/getSonicSimilarTracks")
	if err != nil {
		return nil, err
	}

	params := ps.buildAuthParams(userID, password)
	params.Add("id", songID)
	params.Add("count", "50")
	baseURL.RawQuery = params.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), SimilarSongsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := ps.upstreamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var response models.SonicSimilarityResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	if response.SubsonicResponse.Status != "ok" {
		return nil, nil
	}

	matches := response.SubsonicResponse.SonicMatch
	scores := make(map[string]float64, len(matches))
	for _, m := range matches {
		if m.Entry.ID != "" {
			scores[m.Entry.ID] = m.Similarity
		}
	}

	ps.logger.WithFields(logrus.Fields{
		"userID": sanitizeUsername(userID),
		"songID": sanitizeForLogging(songID),
		"count":  len(scores),
	}).Debug("Fetched similar songs from AudioMuse-AI plugin (getSonicSimilarTracks)")

	return scores, nil
}

func (ps *ProxyServer) GetHandlers() *handlers.Handler {
	return ps.handlers
}
