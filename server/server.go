package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"

	"github.com/syeo66/subsoxy/config"
	"github.com/syeo66/subsoxy/credentials"
	"github.com/syeo66/subsoxy/database"
	"github.com/syeo66/subsoxy/errors"
	"github.com/syeo66/subsoxy/handlers"
	"github.com/syeo66/subsoxy/logsafe"
	"github.com/syeo66/subsoxy/middleware"
	"github.com/syeo66/subsoxy/models"
	"github.com/syeo66/subsoxy/shuffle"
)

const (
	MaxUsernameLength = 100
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
	LibraryPageSize = 500
	// A sync that would remove more than this share of a user's stored songs is held back
	// until a later sync, at least MassDeletionConfirmDelay after it, sees the same
	MassDeletionFraction     = 0.5
	MassDeletionConfirmDelay = 30 * time.Minute
	ReadHeaderTimeout        = 10 * time.Second
	// Path of the health check, answered by subsoxy itself rather than forwarded
	HealthEndpoint = "/healthz"
	// How long the health check waits for the database
	HealthCheckTimeout = 2 * time.Second
	// Subsonic error code for a wrong username or password
	SubsonicErrorWrongCredentials = 40
)

type ProxyServer struct {
	config         *config.Config
	logger         *logrus.Logger
	proxy          *httputil.ReverseProxy
	hooks          map[string][]models.Hook
	authRequired   map[string]bool // Endpoints whose hooks act on the user's data and need verified credentials
	db             *database.DB
	credentials    *credentials.Manager
	handlers       *handlers.Handler
	shuffle        *shuffle.Service
	upstreamClient *http.Client
	server         *http.Server
	// Cancelled on shutdown, which stops the sync loop and aborts its upstream requests
	ctx               context.Context
	cancel            context.CancelFunc
	rateLimiter       *middleware.ClientRateLimiter
	credentialWorkers chan struct{}  // Semaphore for limiting concurrent credential validations
	credentialWg      sync.WaitGroup // WaitGroup for tracking in-flight credential validations and the syncs they start
	syncWg            sync.WaitGroup // Tracks the periodic sync loop
	syncingUsers      sync.Map       // Usernames with a library sync in progress
	// Usernames whose last sync held back a mass deletion, with when it was first seen
	pendingMassDeletions sync.Map
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

	var rateLimiter *middleware.ClientRateLimiter
	if cfg.RateLimitEnabled {
		rateLimiter = middleware.NewClientRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst, cfg.TrustedProxies)
		logger.WithFields(logrus.Fields{
			"rps":             cfg.RateLimitRPS,
			"burst":           cfg.RateLimitBurst,
			"trusted_proxies": len(cfg.TrustedProxies),
		}).Info("Per-client rate limiting enabled")
	} else {
		logger.Info("Rate limiting disabled")
	}

	// Initialize credential validation worker pool
	credentialWorkers := make(chan struct{}, cfg.CredentialWorkers)
	logger.WithFields(logrus.Fields{
		"max_workers": cfg.CredentialWorkers,
	}).Info("Credential validation worker pool configured")

	ctx, cancel := context.WithCancel(context.Background())

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
		ctx:               ctx,
		cancel:            cancel,
		rateLimiter:       rateLimiter,
		credentialWorkers: credentialWorkers,
	}

	server.shuffle.SetSimilarSongsFetcher(server.fetchSimilarSongs)

	server.syncWg.Go(server.syncSongs)

	return server, nil
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
func (ps *ProxyServer) isAuthenticated(r *http.Request, username string, cred credentials.Credential) bool {
	if username == "" || cred.IsZero() {
		return false
	}

	// Hooks identify the user by the "u" parameter
	if r.FormValue("u") != username {
		ps.logger.WithFields(logrus.Fields{
			"username": logsafe.Username(username),
			"endpoint": logsafe.String(r.URL.Path),
			"remote":   ps.clientAddr(r),
		}).Warn("Request user does not match credentials, forwarding without local handling")
		return false
	}

	isNewCredential, err := ps.credentials.ValidateAndStore(r.Context(), username, cred)
	if err != nil {
		ps.logger.WithError(err).WithField("username", logsafe.Username(username)).Warn("Failed to validate credentials, forwarding without local handling")
		return false
	}

	if isNewCredential {
		ps.logger.WithField("username", logsafe.Username(username)).Info("New credentials captured, triggering immediate sync")
		ps.credentialWg.Go(func() { ps.syncUser(username) })
	}

	return true
}

// clientAddr is the address of the client that sent the request, for logging. Behind a
// trusted reverse proxy that's the forwarded client rather than the proxy.
func (ps *ProxyServer) clientAddr(r *http.Request) string {
	return middleware.ClientIP(r, ps.config.TrustedProxies).String()
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
	sanitizedEndpoint := logsafe.String(endpoint)
	clientAddr := ps.clientAddr(r)

	ps.logger.WithFields(logrus.Fields{
		"method":   r.Method,
		"endpoint": sanitizedEndpoint,
		"remote":   clientAddr,
	}).Debug("Incoming request")

	if ps.rateLimiter != nil {
		if client, allowed := ps.rateLimiter.Allow(r); !allowed {
			ps.logger.WithFields(logrus.Fields{
				"endpoint": sanitizedEndpoint,
				"remote":   clientAddr,
				"client":   client,
			}).Warn("Rate limit exceeded")

			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	// Hooks read parameters with r.FormValue, which must not consume the forwarded body
	ps.parseForm(r)

	// Subsonic clients may call any endpoint with a ".view" suffix, which upstream accepts
	// too, so hooks are looked up without it. The request is still forwarded as sent.
	hookEndpoint := endpoint
	if strings.HasPrefix(endpoint, "/rest/") {
		hookEndpoint = strings.TrimSuffix(endpoint, ".view")
	}

	needsAuth := ps.authRequired[hookEndpoint]
	authenticated := false

	if strings.HasPrefix(endpoint, "/rest/") || needsAuth {
		username, cred := ps.extractCredentials(r)

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
			authenticated = ps.isAuthenticated(r, username, cred)
		} else if username != "" && !cred.IsZero() {
			if !ps.credentials.HasRecent(username, CredentialRecaptureInterval) {
				ps.captureCredentials(username, cred)
			}
		} else {
			// Log details about request without exposing any credential information
			ps.logger.WithFields(logrus.Fields{
				"has_username":  username != "",
				"has_secret":    !cred.IsZero(),
				"endpoint":      sanitizedEndpoint,
				"method":        r.Method,
				"content_type":  r.Header.Get("Content-Type"),
				"authorization": r.Header.Get("Authorization") != "",
				"user_agent":    r.Header.Get("User-Agent"),
			}).Debug("No credentials found in request")
		}
	}

	if hooks, exists := ps.hooks[hookEndpoint]; exists && (!needsAuth || authenticated) {
		for _, hook := range hooks {
			if hook(w, r, hookEndpoint) {
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
func (ps *ProxyServer) captureCredentials(username string, cred credentials.Credential) {
	select {
	case ps.credentialWorkers <- struct{}{}:
	default:
		ps.logger.WithField("username", logsafe.Username(username)).Debug("All credential workers busy, skipping credential capture")
		return
	}
	ps.credentialWg.Add(1)

	go func() {
		defer func() {
			<-ps.credentialWorkers // Release worker slot
			ps.credentialWg.Done() // Mark goroutine as complete
		}()

		ps.logger.WithField("username", logsafe.Username(username)).Debug("Extracted credentials from request, attempting validation")
		// Not tied to the request, which may finish first, but abandoned on shutdown
		isNewCredential, err := ps.credentials.ValidateAndStore(ps.ctx, username, cred)
		if err != nil {
			ps.logger.WithError(err).WithField("username", logsafe.Username(username)).Warn("Failed to validate credentials")
		} else if isNewCredential {
			ps.logger.WithField("username", logsafe.Username(username)).Info("New credentials captured, triggering immediate sync")
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

	// Registered ahead of the proxy, so health checks skip its logging and rate limiting
	router.HandleFunc(HealthEndpoint, ps.healthHandler).Methods(http.MethodGet, http.MethodHead)
	router.PathPrefix("/").HandlerFunc(ps.proxyHandler)

	// Listen before returning, so a port that can't be bound is reported to the caller,
	// which can then shut down cleanly
	listener, err := net.Listen("tcp", ":"+ps.config.ProxyPort)
	if err != nil {
		return errors.Wrap(err, errors.CategoryServer, "START_FAILED", "failed to listen").
			WithContext("port", ps.config.ProxyPort)
	}

	ps.server = &http.Server{
		Handler:           router,
		ReadHeaderTimeout: ReadHeaderTimeout,
	}

	ps.logger.WithFields(logrus.Fields{
		"port":     ps.config.ProxyPort,
		"upstream": ps.config.UpstreamURL,
		"url":      fmt.Sprintf("http://localhost:%s", ps.config.ProxyPort),
	}).Info("Starting proxy server")

	go func() {
		if err := ps.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			ps.logger.WithError(err).Error("Server stopped unexpectedly")
		}
	}()

	return nil
}

// healthHandler answers container health checks: 200 while the database answers, 503
// otherwise. Upstream isn't checked, so an upstream outage doesn't get the proxy restarted.
func (ps *ProxyServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), HealthCheckTimeout)
	defer cancel()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := ps.db.Ping(ctx); err != nil {
		ps.logger.WithError(err).Warn("Health check failed: database unavailable")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("database unavailable\n"))
		return
	}
	w.Write([]byte("ok\n"))
}

func (ps *ProxyServer) Shutdown(ctx context.Context) error {
	ps.logger.Info("Shutting down proxy server...")

	// Stop syncing: the sync loop exits and running syncs give up on their upstream requests
	ps.cancel()

	// Stop accepting requests and let in-flight handlers finish while the database is still open
	var shutdownErr error
	if ps.server != nil {
		if err := ps.server.Shutdown(ctx); err != nil {
			ps.logger.WithError(err).Error("Failed to shutdown HTTP server")
			shutdownErr = errors.Wrap(err, errors.CategoryServer, "SHUTDOWN_FAILED", "failed to shutdown HTTP server")
		}
	}

	// Wait for credential validations and syncs, which use the database, before closing it
	ps.logger.Info("Waiting for credential validation workers and syncs to finish...")
	done := make(chan struct{})
	go func() {
		ps.credentialWg.Wait()
		ps.syncWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		ps.logger.Info("All credential validation workers and syncs finished")
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
	ticker := time.NewTicker(SongSyncInterval)
	defer ticker.Stop()

	// Skip initial sync - wait for credentials to be captured from client requests
	ps.logger.Info("Song sync routine started - waiting for valid credentials from client requests")

	for {
		select {
		case <-ticker.C:
			ps.fetchAndStoreSongs()
		case <-ps.ctx.Done():
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
	for i, username := range slices.Sorted(maps.Keys(allCredentials)) {
		if i > 0 {
			select {
			case <-time.After(UserSyncStaggerDelay):
			case <-ps.ctx.Done():
				ps.logger.Info("Shutdown requested, stopping multi-user song sync")
				return
			}
		}

		ps.syncUserWithCredential(username, allCredentials[username])
	}

	ps.logger.Info("Multi-user song sync completed")
}

// syncUser syncs one user's library with their stored credentials
func (ps *ProxyServer) syncUser(username string) {
	cred, ok := ps.credentials.Get(username)
	if !ok {
		ps.logger.WithField("user", logsafe.Username(username)).Warn("No stored credentials for user, skipping sync")
		return
	}
	ps.syncUserWithCredential(username, cred)
}

// syncUserWithCredential syncs one user's library unless a sync for that user is already running
func (ps *ProxyServer) syncUserWithCredential(username string, cred credentials.Credential) {
	if _, running := ps.syncingUsers.LoadOrStore(username, struct{}{}); running {
		ps.logger.WithField("user", logsafe.Username(username)).Debug("Sync already in progress for user, skipping")
		return
	}
	defer ps.syncingUsers.Delete(username)

	err := ps.syncSongsForUser(username, cred)
	if isRejectedCredential(err) {
		// The password changed or the account is gone: stop syncing with this credential
		// until the client sends one upstream accepts
		removed := ps.credentials.Forget(username, cred)
		ps.logger.WithError(err).WithFields(logrus.Fields{
			"user":    logsafe.Username(username),
			"removed": removed,
		}).Warn("Upstream rejected stored credentials, dropping them")
		return
	}
	if err != nil {
		ps.logger.WithError(err).WithField("user", logsafe.Username(username)).Error("Failed to sync songs for user")
	}
}

// isRejectedCredential reports whether err means upstream rejected the credentials
func isRejectedCredential(err error) bool {
	return errors.IsCode(err, errors.ErrInvalidCredentials.Code)
}

// syncSongsForUser fetches a user's library from upstream and applies the differences locally
func (ps *ProxyServer) syncSongsForUser(username string, cred credentials.Credential) error {
	ps.logger.WithField("user", logsafe.Username(username)).Info("Syncing songs for user")

	allSongs, incomplete, err := ps.fetchLibrary(username, cred)
	if err != nil {
		return err
	}
	// A folder walk cut short by shutdown returns what it got so far; don't store that
	if err := ps.ctx.Err(); err != nil {
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

	// Deleting a song also deletes its play statistics, so only do it after a complete fetch
	if incomplete && len(songsToDelete) > 0 {
		ps.logger.WithFields(logrus.Fields{
			"user":            logsafe.Username(username),
			"songs_to_delete": len(songsToDelete),
		}).Warn("Library fetch was incomplete, keeping songs missing from upstream until the next complete sync")
		songsToDelete = nil
	}

	if !ps.confirmDeletion(username, len(songsToDelete), len(existingSongs)) {
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
			"user":    logsafe.Username(username),
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
		"user":      logsafe.Username(username),
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

// confirmDeletion reports whether a sync may delete toDelete of the user's stored songs. A
// complete fetch missing most of the library more likely means upstream answered wrongly (a
// permission change, a misconfigured music folder) than that the music is gone, so a removal
// that large is only applied once a sync at least MassDeletionConfirmDelay later sees it too.
func (ps *ProxyServer) confirmDeletion(username string, toDelete, stored int) bool {
	if toDelete == 0 || float64(toDelete) <= MassDeletionFraction*float64(stored) {
		ps.pendingMassDeletions.Delete(username)
		return true
	}

	fields := logrus.Fields{
		"user":            logsafe.Username(username),
		"songs_to_delete": toDelete,
		"stored_songs":    stored,
	}
	firstSeen, pending := ps.pendingMassDeletions.LoadOrStore(username, time.Now())
	if pending && time.Since(firstSeen.(time.Time)) >= MassDeletionConfirmDelay {
		ps.pendingMassDeletions.Delete(username)
		ps.logger.WithFields(fields).Warn("Upstream library still missing most stored songs, deleting them")
		return true
	}

	ps.logger.WithFields(fields).Warn("Upstream library is missing most stored songs, keeping them until a later sync confirms the removal")
	return false
}

// fetchLibrary returns every song in the user's library. It pages through search3 with an
// empty query, which takes a few dozen requests even for a large library, and falls back to
// walking the folder tree (a request per artist and album) when the server doesn't answer
// that search. incomplete is set when part of the library could not be fetched: the list is
// then partial, so songs missing from it must not be treated as removed upstream.
func (ps *ProxyServer) fetchLibrary(username string, cred credentials.Credential) (songs []models.Song, incomplete bool, err error) {
	songs, err = ps.fetchLibraryBySearch(username, cred)
	if err == nil && len(songs) > 0 {
		return songs, false, nil
	}
	// Walking the folders with the same credentials would be rejected too
	if isRejectedCredential(err) {
		return nil, false, err
	}

	// No songs could also mean the server doesn't treat an empty query as "everything",
	// so an empty library is only believed once the folder walk agrees
	ps.logger.WithError(err).WithField("user", logsafe.Username(username)).
		Info("Library search returned no songs, syncing by walking the music folders")
	return ps.fetchLibraryByDirectories(username, cred)
}

// fetchLibraryBySearch pages through all songs with search3 and an empty query
func (ps *ProxyServer) fetchLibraryBySearch(username string, cred credentials.Credential) ([]models.Song, error) {
	var songs []models.Song
	seen := make(map[string]bool)

	for offset := 0; ; offset += LibraryPageSize {
		params := ps.buildAuthParams(username, cred)
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
func (ps *ProxyServer) fetchLibraryByDirectories(username string, cred credentials.Credential) ([]models.Song, bool, error) {
	// First, get all music folders
	musicFolders, err := ps.getMusicFolders(username, cred)
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
			"user":        logsafe.Username(username),
			"folder_id":   folderID,
			"folder_name": folder.Name,
		}).Debug("Processing music folder")

		// Get indexes for this folder to get artists
		indexes, err := ps.getIndexes(username, cred, folderID)
		if err != nil {
			ps.logger.WithError(err).WithFields(logrus.Fields{
				"user":      logsafe.Username(username),
				"folder_id": folderID,
			}).Warn("Failed to get indexes for folder, skipping")
			incomplete = true
			continue
		}

		// Process each artist
		for _, index := range indexes {
			for _, artist := range index.Artists {
				ps.logger.WithFields(logrus.Fields{
					"user":        logsafe.Username(username),
					"artist_id":   artist.ID,
					"artist_name": artist.Name,
				}).Debug("Processing artist")

				// Get albums for this artist
				albums, err := ps.getMusicDirectory(username, cred, artist.ID)
				if err != nil {
					ps.logger.WithError(err).WithFields(logrus.Fields{
						"user":      logsafe.Username(username),
						"artist_id": artist.ID,
					}).Warn("Failed to get albums for artist, skipping")
					incomplete = true
					continue
				}

				// Process each album
				for _, album := range albums {
					if album.IsDir {
						ps.logger.WithFields(logrus.Fields{
							"user":        logsafe.Username(username),
							"album_id":    album.ID,
							"album_title": album.Title,
						}).Debug("Processing album")

						// Get songs for this album
						songs, err := ps.getMusicDirectory(username, cred, album.ID)
						if err != nil {
							ps.logger.WithError(err).WithFields(logrus.Fields{
								"user":     logsafe.Username(username),
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

// subsonicErrorEnvelope reads the error code of a failed Subsonic response
type subsonicErrorEnvelope struct {
	SubsonicResponse struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	} `json:"subsonic-response"`
}

// getSubsonic calls a Subsonic endpoint upstream and decodes its JSON response into
// response, failing unless the response status is "ok". When upstream rejects the
// credentials the error is errors.ErrInvalidCredentials, unwrapped so callers can check it.
func (ps *ProxyServer) getSubsonic(endpoint string, params url.Values, response subsonicResponse) error {
	baseURL, err := url.Parse(ps.config.UpstreamURL + "/rest/" + endpoint)
	if err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "URL_PARSE_FAILED", "failed to parse upstream URL")
	}
	baseURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ps.ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "REQUEST_FAILED", "failed to create request for "+endpoint)
	}

	resp, err := ps.upstreamClient.Do(req)
	if err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to fetch "+endpoint)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to read response")
	}
	if err := json.Unmarshal(body, response); err != nil {
		return errors.Wrap(err, errors.CategoryNetwork, "UPSTREAM_ERROR", "failed to decode response")
	}

	if response.ResponseStatus() != "ok" {
		var failed subsonicErrorEnvelope
		if json.Unmarshal(body, &failed) == nil && failed.SubsonicResponse.Error.Code == SubsonicErrorWrongCredentials {
			return errors.ErrInvalidCredentials.WithContext("endpoint", endpoint).
				WithContext("reason", "rejected by upstream")
		}
		return errors.New(errors.CategoryNetwork, "UPSTREAM_ERROR", "API returned error status")
	}

	return nil
}

// getMusicFolders fetches all music folders for a user
func (ps *ProxyServer) getMusicFolders(username string, cred credentials.Credential) ([]models.MusicFolder, error) {
	var response models.SubsonicResponse
	if err := ps.getSubsonic("getMusicFolders", ps.buildAuthParams(username, cred), &response); err != nil {
		return nil, err
	}
	return response.SubsonicResponse.MusicFolders.MusicFolder, nil
}

// getIndexes fetches artist indexes for a music folder
func (ps *ProxyServer) getIndexes(username string, cred credentials.Credential, musicFolderId string) ([]models.Index, error) {
	params := ps.buildAuthParams(username, cred)
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
func (ps *ProxyServer) getMusicDirectory(username string, cred credentials.Credential, id string) ([]models.Song, error) {
	params := ps.buildAuthParams(username, cred)
	params.Add("id", id)
	var response models.SubsonicResponse
	if err := ps.getSubsonic("getMusicDirectory", params, &response); err != nil {
		return nil, err
	}
	return response.SubsonicResponse.Directory.Child, nil
}

// buildAuthParams builds authentication parameters for API calls
func (ps *ProxyServer) buildAuthParams(username string, cred credentials.Credential) url.Values {
	params := url.Values{}
	params.Add("u", username)
	cred.AddTo(params)
	params.Add("f", "json")
	params.Add("v", models.SubsonicAPIVersion)
	params.Add("c", models.ClientName)

	return params
}

// extractCredentials extracts the username and credential from various sources in the request
func (ps *ProxyServer) extractCredentials(r *http.Request) (string, credentials.Credential) {
	if r.Form == nil {
		ps.parseForm(r)
	}

	// Query parameters or form-encoded POST body (most common)
	username := r.Form.Get("u")
	password := r.Form.Get("p")

	if username != "" && password != "" {
		ps.logger.Debug("Credentials extracted from request parameters (password-based)")
		return username, credentials.Password(password)
	}

	// Try token-based authentication (t + s parameters)
	token := r.Form.Get("t")
	salt := r.Form.Get("s")

	if username != "" && token != "" && salt != "" {
		ps.logger.Debug("Token-based authentication detected - will validate with upstream")
		return username, credentials.Token(token, salt)
	}

	// Try Authorization header (Basic Auth format)
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		// Handle Basic Auth: "Basic base64(username:password)"
		if strings.HasPrefix(authHeader, "Basic ") {
			if headerUsername, headerPassword, ok := r.BasicAuth(); ok {
				ps.logger.Debug("Credentials extracted from Authorization header (Basic Auth)")
				return headerUsername, credentials.Password(headerPassword)
			}
		}
	}

	// Try custom headers (some clients use these)
	if headerUsername := r.Header.Get("X-Subsonic-Username"); headerUsername != "" {
		if headerPassword := r.Header.Get("X-Subsonic-Password"); headerPassword != "" {
			ps.logger.Debug("Credentials extracted from X-Subsonic headers")
			return headerUsername, credentials.Password(headerPassword)
		}
	}

	ps.logger.Debug("No credentials found in request")
	return "", credentials.Credential{}
}

// parseForm fills r.Form and r.PostForm from the query and a form-encoded POST body
// (OpenSubsonic formPost extension), like r.ParseForm but without consuming the body,
// so hooks can read parameters with r.FormValue and the request can still be forwarded.
func (ps *ProxyServer) parseForm(r *http.Request) {
	postForm := url.Values{}
	if r.Method == http.MethodPost {
		parsed, err := readPostForm(r)
		if err != nil {
			ps.logger.WithError(err).WithField("endpoint", logsafe.String(r.URL.Path)).Warn("Failed to parse POST form, using query parameters only")
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

func (ps *ProxyServer) RecordPlayEvent(userID, songID, eventType string) {
	ps.RecordPlayEventAt(userID, songID, eventType, time.Now())
}

// RecordPlayEventAt records a play or skip that happened at the given time
func (ps *ProxyServer) RecordPlayEventAt(userID, songID, eventType string, at time.Time) {
	if err := ps.db.RecordPlayEventAt(userID, songID, eventType, at); err != nil {
		ps.logger.WithError(err).WithField("userID", logsafe.Username(userID)).Error("Failed to record play event")
		return
	}
	ps.shuffle.InvalidateEmpiricalPriors(userID)

	ps.logger.WithFields(logrus.Fields{
		"userID":    logsafe.Username(userID),
		"songId":    logsafe.String(songID),
		"eventType": eventType,
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
		existing.CoverArt != new.CoverArt ||
		existing.Genre != new.Genre ||
		existing.Year != new.Year ||
		existing.AlbumID != new.AlbumID ||
		existing.ArtistID != new.ArtistID ||
		existing.Track != new.Track ||
		existing.DiscNumber != new.DiscNumber ||
		existing.Suffix != new.Suffix ||
		existing.ContentType != new.ContentType ||
		existing.BitRate != new.BitRate ||
		existing.Size != new.Size
}

// ProcessScrobble processes a scrobble event and handles pending songs
// Returns true if a play event should be recorded, false if it's a duplicate submission
func (ps *ProxyServer) ProcessScrobble(userID, client, songID string, isSubmission bool) bool {
	recordSkipFunc := func(userID string, song *models.Song) {
		err := ps.db.RecordPlayEvent(userID, song.ID, "skip")
		if err != nil {
			ps.logger.WithError(err).WithFields(logrus.Fields{
				"user_id": userID,
				"song_id": song.ID,
			}).Error("Failed to record skip event from pending song processing")
			return
		}
		ps.shuffle.InvalidateEmpiricalPriors(userID)
	}
	return ps.shuffle.ProcessScrobble(userID, client, songID, isSubmission, recordSkipFunc)
}

// fetchSimilarSongs queries the upstream's getSonicSimilarTracks endpoint provided by the
// AudioMuse-AI-NV-plugin (Navidrome v0.62+ OpenSubsonic sonicSimilarity extension).
// Returns nil without error when the plugin is not available so the caller gracefully
// skips the similarity weight.
func (ps *ProxyServer) fetchSimilarSongs(userID, songID string) (map[string]float64, error) {
	cred, ok := ps.credentials.Get(userID)
	if !ok {
		return nil, nil
	}

	baseURL, err := url.Parse(ps.config.UpstreamURL + "/rest/getSonicSimilarTracks")
	if err != nil {
		return nil, err
	}

	params := ps.buildAuthParams(userID, cred)
	params.Add("id", songID)
	params.Add("count", "50")
	baseURL.RawQuery = params.Encode()

	// Lookups are shared by every shuffle waiting on them, so they aren't tied to one
	// request, but they are abandoned on shutdown
	ctx, cancel := context.WithTimeout(ps.ctx, SimilarSongsTimeout)
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
		"userID": logsafe.Username(userID),
		"songID": logsafe.String(songID),
		"count":  len(scores),
	}).Debug("Fetched similar songs from AudioMuse-AI plugin (getSonicSimilarTracks)")

	return scores, nil
}

func (ps *ProxyServer) GetHandlers() *handlers.Handler {
	return ps.handlers
}
