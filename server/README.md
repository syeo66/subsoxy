# Server Module

The server module provides the main proxy server implementation with **complete multi-tenancy support**, request routing, lifecycle management, database connection pooling, and integration of all other modules.

## Overview

This module handles:
- **Multi-tenant HTTP server setup** and configuration with user context validation
- **User-isolated database connection pool** initialization and management
- **Per-user reverse proxy implementation** with comprehensive input validation and user context
- **Multi-tenant hook system** for request interception with user isolation
- **User-aware rate limiting** and DoS protection
- **User context input sanitization** and log injection prevention
- **Per-user background task management** (user-specific song synchronization)
- **Multi-tenant graceful shutdown** handling
- **User-specific request logging** and monitoring with sanitized inputs
- **Connection pool health monitoring** and statistics for multi-user environments

## Core Components

### ProxyServer
The main server struct that coordinates all functionality:

```go
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
```

## API

### Initialization
```go
import "github.com/syeo66/subsoxy/server"

// Server automatically configures database connection pool from config
proxyServer, err := server.New(config)
if err != nil {
    log.Fatal("Failed to create server:", err)
}

// The server logs connection pool configuration on startup:
// time="..." level=info msg="Database connection pool configured" 
//   max_open_conns=25 max_idle_conns=5 conn_max_lifetime=30m0s
//   conn_max_idle_time=5m0s health_check=true
```

### Server Lifecycle
```go
// Start the server
if err := proxyServer.Start(); err != nil {
    log.Fatal("Failed to start server:", err)
}

// Graceful shutdown
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := proxyServer.Shutdown(ctx); err != nil {
    log.Error("Server forced shutdown:", err)
}
```

### Hook Management
```go
// Add request hooks
proxyServer.AddHook("/rest/ping", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    log.Info("Ping endpoint accessed")
    return false // Continue with proxy
})

proxyServer.AddHook("/rest/getRandomSongs", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    // Custom shuffle logic
    return true // Block upstream forwarding
})
```

## Request Flow

### 1. Request Reception
```go
func (ps *ProxyServer) proxyHandler(w http.ResponseWriter, r *http.Request) {
    endpoint := r.URL.Path
    
    // Log incoming request
    ps.logger.WithFields(logrus.Fields{
        "method":   r.Method,
        "endpoint": endpoint,
        "remote":   r.RemoteAddr,
    }).Info("Incoming request")
    
    // ... processing continues
}
```

### 2. Credential Capture
```go
// Extract and validate credentials from Subsonic API requests
if strings.HasPrefix(endpoint, "/rest/") {
    username := r.URL.Query().Get("u")
    password := r.URL.Query().Get("p")
    if username != "" && password != "" {
        // Validate asynchronously to avoid blocking
        go ps.credentials.ValidateAndStore(username, password)
    }
}
```

### 3. Hook Processing
```go
// Execute registered hooks for the endpoint
if hooks, exists := ps.hooks[endpoint]; exists {
    for _, hook := range hooks {
        if hook(w, r, endpoint) {
            return // Hook blocked the request
        }
    }
}
```

### 4. Proxy Forwarding
```go
// Forward to upstream server if not blocked by hooks
ps.proxy.ServeHTTP(w, r)
```

## Database Connection Pool Integration ✅

The server module automatically initializes and manages the database connection pool:

### Connection Pool Setup
```go
// Server extracts pool configuration from config and creates pooled connection
poolConfig := &database.ConnectionPool{
    MaxOpenConns:    cfg.DBMaxOpenConns,
    MaxIdleConns:    cfg.DBMaxIdleConns,
    ConnMaxLifetime: cfg.DBConnMaxLifetime,
    ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
    HealthCheck:     cfg.DBHealthCheck,
}

db, err := database.NewWithPool(cfg.DatabasePath, logger, poolConfig)
```

### Pool Monitoring
```go
// The server logs connection pool configuration on startup
logger.WithFields(logrus.Fields{
    "max_open_conns":      cfg.DBMaxOpenConns,
    "max_idle_conns":      cfg.DBMaxIdleConns,
    "conn_max_lifetime":   cfg.DBConnMaxLifetime,
    "conn_max_idle_time":  cfg.DBConnMaxIdleTime,
    "health_check":        cfg.DBHealthCheck,
}).Info("Database connection pool configured")
```

### Benefits for Server Operations
- **High Concurrency**: Multiple request handlers can access database simultaneously
- **Resource Efficiency**: Connection reuse reduces overhead for database operations
- **Health Monitoring**: Background health checks ensure database availability
- **Performance**: Optimized for high-load scenarios with configurable pool sizes

## Background Tasks

### Database Health Monitoring
Background health checks monitor connection pool status:
```go
// Health checks run every 30 seconds (when enabled)
// - Validates database connectivity
// - Updates connection statistics
// - Logs pool performance metrics
```

### Song Synchronization
Automatically fetches songs from the upstream Subsonic server with smart credential-aware timing and immediate sync on new credentials:

```go
func (ps *ProxyServer) syncSongs() {
    ps.syncTicker = time.NewTicker(1 * time.Hour)
    defer ps.syncTicker.Stop()

    // Skip initial sync - wait for credentials to be captured from client requests
    ps.logger.Info("Song sync routine started - waiting for valid credentials from client requests")

    // Periodic sync (only runs when credentials are available)
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
```

### Immediate Sync on New Credentials ✅ **NEW**

When new credentials are captured for the first time, the system triggers an immediate sync:

```go
// In proxyHandler - credential validation
go func() {
    isNewCredential, err := ps.credentials.ValidateAndStore(username, password)
    if err != nil {
        ps.logger.WithError(err).WithField("username", sanitizeUsername(username)).Warn("Failed to validate credentials")
    } else if isNewCredential {
        ps.logger.WithField("username", sanitizeUsername(username)).Info("New credentials captured, triggering immediate sync")
        // Trigger immediate sync for new credentials
        ps.fetchAndStoreSongs()
    }
}()
```

### Song Fetching Process
```go
func (ps *ProxyServer) fetchAndStoreSongs() {
    // Get all valid credentials for multi-user sync
    allCredentials := ps.credentials.GetAllValid()
    if len(allCredentials) == 0 {
        ps.logger.Debug("Skipping song sync - no valid credentials available yet (waiting for client requests)")
        return
    }
    
    ps.logger.Info("Syncing songs from Subsonic API")
    
    // Sync songs for each user with staggered delays
    for i, username := range getSortedUsernames(allCredentials) {
        password := allCredentials[username]
        
        // Add staggered delay to avoid overwhelming upstream server
        if i > 0 {
            time.Sleep(time.Duration(i) * 2 * time.Second)
        }
        
        // Sync songs for this specific user using directory traversal
        if err := ps.syncSongsForUser(username, password); err != nil {
            ps.logger.WithError(err).WithField("user", username).Error("Failed to sync songs for user")
            continue
        }
    }
}

func (ps *ProxyServer) syncSongsForUser(username, password string) error {
    // Uses proper Subsonic API directory traversal:
    // 1. getMusicFolders - Get all music folders
    // 2. getIndexes - Get artist indexes for each folder
    // 3. getMusicDirectory - Get albums for each artist
    // 4. getMusicDirectory - Get songs for each album
    
    musicFolders, err := ps.getMusicFolders(username, password)
    if err != nil {
        return err
    }
    
    var allSongs []models.Song
    
    // Traverse each music folder
    for _, folder := range musicFolders {
        // Get indexes for this folder
        indexes, err := ps.getIndexes(username, password, folder.ID)
        if err != nil {
            continue
        }
        
        // Process each artist and album
        for _, index := range indexes {
            for _, artist := range index.Artists {
                albums, err := ps.getMusicDirectory(username, password, artist.ID)
                if err != nil {
                    continue
                }
                
                for _, album := range albums {
                    if album.IsDir {
                        songs, err := ps.getMusicDirectory(username, password, album.ID)
                        if err != nil {
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
    
    // Store all songs for this user
    return ps.db.StoreSongs(username, allSongs)
}
```

**Security Note**: This implementation ensures passwords are never exposed in server logs, debug output, or error messages by using proper URL parameter encoding instead of direct string formatting.

## Multi-Tenant Event Recording ✅ **UPDATED**

### Per-User Play Event Recording
```go
func (ps *ProxyServer) RecordPlayEvent(userID, songID, eventType string, previousSong *string) {
    // Record in database with user context isolation
    if err := ps.db.RecordPlayEvent(userID, songID, eventType, previousSong); err != nil {
        ps.logger.WithError(err).WithField("user_id", userID).Error("Failed to record play event")
        return
    }

    // Update user-specific transition data
    if previousSong != nil {
        if err := ps.db.RecordTransition(userID, *previousSong, songID, eventType); err != nil {
            ps.logger.WithError(err).WithField("user_id", userID).Error("Failed to record transition")
        }
    }

    // Log the event with user context
    ps.logger.WithFields(logrus.Fields{
        "user_id":      userID,
        "songId":       songID,
        "eventType":    eventType,
        "previousSong": previousSong,
    }).Debug("Recorded play event")
}
```

### User-Specific Tracking and Skip Detection
```go
func (ps *ProxyServer) SetLastPlayed(userID, songID string) {
    song := &models.Song{ID: songID}
    ps.shuffle.SetLastPlayed(userID, song)
}

// CheckAndRecordSkip checks if the previous song was skipped and records it
func (ps *ProxyServer) CheckAndRecordSkip(userID, newSongID string) error {
    newSong := &models.Song{ID: newSongID}
    
    // Check if the previous song was skipped
    skippedSong, wasSkipped := ps.shuffle.CheckForSkip(userID, newSong)
    if wasSkipped {
        // Record the skip event
        return ps.db.RecordPlayEvent(userID, skippedSong.ID, "skip", nil)
    }
    
    return nil
}

// SetLastStarted records when a song starts streaming
func (ps *ProxyServer) SetLastStarted(userID, songID string) {
    song := &models.Song{ID: songID}
    ps.shuffle.SetLastStarted(userID, song)
}
```

These methods work together to implement accurate skip detection:
- **SetLastStarted**: Called when a song begins streaming (`/rest/stream`)
- **SetLastPlayed**: Called when a song is successfully played (`/rest/scrobble` with `submission=true`)
- **CheckAndRecordSkip**: Called before starting a new song to detect if the previous song was skipped

## Reverse Proxy Configuration

### Upstream Setup
```go
upstreamURL, err := url.Parse(cfg.UpstreamURL)
if err != nil {
    return nil, fmt.Errorf("invalid upstream URL: %w", err)
}

proxy := httputil.NewSingleHostReverseProxy(upstreamURL)
```

### Request Modification
```go
originalDirector := proxy.Director
proxy.Director = func(req *http.Request) {
    originalDirector(req)
    req.Host = upstreamURL.Host
    req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
}
```

## Error Handling

### Database Errors
```go
if err := ps.db.StoreSongs(songs); err != nil {
    ps.logger.WithError(err).Error("Failed to store songs")
    return
}
```

### Network Errors
```go
resp, err := http.Get(url)
if err != nil {
    ps.logger.WithError(err).Error("Failed to fetch songs from Subsonic API")
    return
}
defer resp.Body.Close()
```

### Authentication Errors
```go
if subsonicResp.SubsonicResponse.Status != "ok" {
    ps.logger.Error("Subsonic API returned error status - possibly authentication failed")
    ps.credentials.ClearInvalid()
    return
}
```

## Graceful Shutdown

### Shutdown Process
```go
func (ps *ProxyServer) Shutdown(ctx context.Context) error {
    ps.logger.Info("Shutting down proxy server...")
    
    // 1. Signal background goroutines to stop
    close(ps.shutdownChan)
    
    // 2. Stop periodic tasks
    if ps.syncTicker != nil {
        ps.syncTicker.Stop()
    }
    
    // 3. Close database connection
    if ps.db != nil {
        if err := ps.db.Close(); err != nil {
            ps.logger.WithError(err).Error("Failed to close database connection")
        }
    }
    
    // 4. Shutdown HTTP server
    if ps.server != nil {
        if err := ps.server.Shutdown(ctx); err != nil {
            ps.logger.WithError(err).Error("Failed to shutdown HTTP server")
            return err
        }
    }
    
    ps.logger.Info("Proxy server shut down successfully")
    return nil
}
```

## Configuration

### Server Setup
```go
ps.server = &http.Server{
    Addr:    ":" + ps.config.ProxyPort,
    Handler: router,
}
```

### Routing
```go
router := mux.NewRouter()
router.PathPrefix("/").HandlerFunc(ps.proxyHandler)
```

## Input Validation and Security

### Input Sanitization Functions

**Log Injection Prevention**:
```go
// sanitizeForLogging removes control characters and limits length to prevent log injection
func sanitizeForLogging(input string) string {
    // Remove control characters (ASCII 0-31 and 127)
    sanitized := strings.Map(func(r rune) rune {
        if r < 32 || r == 127 {
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

// sanitizeUsername sanitizes username for logging
func sanitizeUsername(username string) string {
    // Remove control characters
    sanitized := strings.Map(func(r rune) rune {
        if r < 32 || r == 127 {
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
```

**Security Constants**:
```go
const (
    MaxEndpointLength = 1000
    MaxUsernameLength = 100
    MaxRemoteAddrLength = 100
)
```

### Secure Request Processing

**Safe Request Logging**:
```go
func (ps *ProxyServer) proxyHandler(w http.ResponseWriter, r *http.Request) {
    endpoint := r.URL.Path
    
    // Sanitize inputs for logging
    sanitizedEndpoint := sanitizeForLogging(endpoint)
    sanitizedRemoteAddr := sanitizeRemoteAddr(r.RemoteAddr)
    
    ps.logger.WithFields(logrus.Fields{
        "method":   r.Method,
        "endpoint": sanitizedEndpoint,
        "remote":   sanitizedRemoteAddr,
    }).Info("Incoming request")
    
    // Rate limiting check...
    // Username validation with length limits...
    
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
    
    // Hook processing and proxy forwarding...
}
```

### Security Benefits

- **Log Injection Prevention**: All user inputs sanitized before logging
- **Control Character Filtering**: Newlines, carriage returns, tabs, and escape sequences removed
- **DoS Protection**: Input length limits prevent memory exhaustion attacks
- **Username Validation**: Long usernames truncated with warnings
- **Endpoint Sanitization**: Malicious paths sanitized for safe logging
- **Remote Address Filtering**: Client addresses sanitized to prevent log pollution

## Monitoring and Logging

### Request Logging
```go
// Sanitized logging with security protections
ps.logger.WithFields(logrus.Fields{
    "method":   r.Method,
    "endpoint": sanitizedEndpoint,
    "remote":   sanitizedRemoteAddr,
}).Info("Incoming request")
```

### Sync Logging
```go
ps.logger.WithField("count", len(songs)).Info("Successfully synced songs")
```

### Debug Logging
```go
if strings.HasPrefix(endpoint, "/rest/") {
    ps.logger.WithField("endpoint", endpoint).Debug("Subsonic API endpoint")
}
```

## Integration Points

### Module Dependencies
- **Config**: Server configuration and environment variables
- **Database**: Song storage and event recording
- **Credentials**: Authentication management
- **Handlers**: Request processing logic
- **Shuffle**: Intelligent song recommendation
- **Models**: Data structures and types

### External Dependencies
- **Gorilla Mux**: HTTP routing
- **Logrus**: Structured logging
- **Go HTTP**: Reverse proxy and server implementation