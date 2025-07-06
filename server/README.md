# Server Module

The server module provides the main proxy server implementation with request routing, lifecycle management, and integration of all other modules.

## Overview

This module handles:
- HTTP server setup and configuration
- Reverse proxy implementation
- Hook system for request interception
- Background task management (song synchronization)
- Graceful shutdown handling
- Request logging and monitoring

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
}
```

## API

### Initialization
```go
import "github.com/syeo66/subsoxy/server"

proxyServer, err := server.New(config)
if err != nil {
    log.Fatal("Failed to create server:", err)
}
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

## Background Tasks

### Song Synchronization
Automatically fetches songs from the upstream Subsonic server:

```go
func (ps *ProxyServer) syncSongs() {
    ps.syncTicker = time.NewTicker(1 * time.Hour)
    defer ps.syncTicker.Stop()

    // Initial sync
    ps.fetchAndStoreSongs()

    // Periodic sync
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

### Song Fetching Process
```go
func (ps *ProxyServer) fetchAndStoreSongs() {
    // Get valid credentials
    username, password := ps.credentials.GetValid()
    if username == "" || password == "" {
        ps.logger.Warn("No valid credentials available for song syncing")
        return
    }
    
    // Fetch from Subsonic API
    url := fmt.Sprintf("%s/rest/search3?query=*&songCount=10000&f=json&v=1.15.0&c=subsoxy&u=%s&p=%s", 
        ps.config.UpstreamURL, username, password)
    
    // ... fetch and store songs
}
```

## Event Recording

### Play Event Recording
```go
func (ps *ProxyServer) RecordPlayEvent(songID, eventType string, previousSong *string) {
    // Record in database
    if err := ps.db.RecordPlayEvent(songID, eventType, previousSong); err != nil {
        ps.logger.WithError(err).Error("Failed to record play event")
        return
    }

    // Update transition data
    if previousSong != nil {
        if err := ps.db.RecordTransition(*previousSong, songID, eventType); err != nil {
            ps.logger.WithError(err).Error("Failed to record transition")
        }
    }

    // Log the event
    ps.logger.WithFields(logrus.Fields{
        "songId":       songID,
        "eventType":    eventType,
        "previousSong": previousSong,
    }).Debug("Recorded play event")
}
```

### Last Played Tracking
```go
func (ps *ProxyServer) SetLastPlayed(songID string) {
    song := &models.Song{ID: songID}
    ps.shuffle.SetLastPlayed(song)
}
```

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

## Monitoring and Logging

### Request Logging
```go
ps.logger.WithFields(logrus.Fields{
    "method":   r.Method,
    "endpoint": endpoint,
    "remote":   r.RemoteAddr,
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