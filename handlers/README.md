# Handlers Module

The handlers module provides HTTP request handlers for different Subsonic API endpoints with business logic implementation.

## Overview

This module handles:
- Endpoint-specific request processing
- Response formatting and serialization
- Integration with shuffle service
- Error handling and logging
- Custom business logic for enhanced features

## Handler Types

### Shuffle Handler
Provides intelligent weighted song shuffling for `/rest/getRandomSongs`.

```go
func (h *Handler) HandleShuffle(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    // Parse size parameter (default: 50)
    sizeStr := r.URL.Query().Get("size")
    size := 50
    if sizeStr != "" {
        if parsedSize, err := strconv.Atoi(sizeStr); err == nil && parsedSize > 0 {
            size = parsedSize
        }
    }
    
    // Get weighted shuffled songs
    songs, err := h.shuffle.GetWeightedShuffledSongs(size)
    if err != nil {
        h.logger.WithError(err).Error("Failed to get weighted shuffled songs")
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return true
    }
    
    // Format response in Subsonic API format
    response := map[string]interface{}{
        "subsonic-response": map[string]interface{}{
            "status":  "ok",
            "version": "1.15.0",
            "songs": map[string]interface{}{
                "song": songs,
            },
        },
    }
    
    // Return JSON response
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
    return true // Block forwarding to upstream
}
```

### Logging Handlers
Simple handlers that log endpoint access and allow normal proxy behavior.

```go
func (h *Handler) HandlePing(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    h.logger.Info("Ping endpoint accessed")
    return false // Continue with normal proxy behavior
}

func (h *Handler) HandleGetLicense(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    h.logger.Info("License endpoint accessed")
    return false // Continue with normal proxy behavior
}
```

### Event Recording Handlers
Handlers that record play events for analytics and machine learning.

```go
func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, *string)) bool {
    songID := r.URL.Query().Get("id")
    if songID != "" {
        recordFunc(songID, "start", nil)
    }
    return false // Continue with normal proxy behavior
}

func (h *Handler) HandleScrobble(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, *string), setLastPlayed func(string)) bool {
    songID := r.URL.Query().Get("id")
    submission := r.URL.Query().Get("submission")
    if songID != "" {
        if submission == "true" {
            recordFunc(songID, "play", nil)
            setLastPlayed(songID)
        } else {
            recordFunc(songID, "skip", nil)
        }
    }
    return false // Continue with normal proxy behavior
}
```

## API

### Initialization
```go
import (
    "github.com/syeo66/subsoxy/handlers"
    "github.com/syeo66/subsoxy/shuffle"
)

shuffleService := shuffle.New(database, logger)
handlersService := handlers.New(logger, shuffleService)
```

### Handler Registration
```go
// In main.go or server setup
server.AddHook("/rest/getRandomSongs", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    return handlers.HandleShuffle(w, r, endpoint)
})

server.AddHook("/rest/stream", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    return handlers.HandleStream(w, r, endpoint, server.RecordPlayEvent)
})
```

## Handler Behavior

### Return Values
- `true`: Block request from being forwarded to upstream server
- `false`: Allow request to continue to upstream server

### Response Handling
- Handlers that return `true` must handle the complete HTTP response
- Handlers that return `false` allow normal proxy behavior
- Error responses should use appropriate HTTP status codes

### Logging
All handlers include structured logging:
```go
h.logger.WithFields(logrus.Fields{
    "size": size,
    "returned": len(songs),
}).Info("Served weighted shuffle request")
```

## Integration Points

### Shuffle Service
The handlers module integrates with the shuffle service for intelligent song recommendations:
```go
type Handler struct {
    logger  *logrus.Logger
    shuffle *shuffle.Service
}
```

### Event Recording
Handlers accept callback functions for recording play events:
```go
recordFunc func(string, string, *string)  // songID, eventType, previousSong
setLastPlayed func(string)                // songID
```

## Error Handling

### Internal Errors
```go
if err != nil {
    h.logger.WithError(err).Error("Failed to get weighted shuffled songs")
    http.Error(w, "Internal server error", http.StatusInternalServerError)
    return true
}
```

### Parameter Validation
```go
// Parse and validate size parameter
sizeStr := r.URL.Query().Get("size")
size := 50  // default
if sizeStr != "" {
    if parsedSize, err := strconv.Atoi(sizeStr); err == nil && parsedSize > 0 {
        size = parsedSize
    }
    // Invalid sizes are ignored, default is used
}
```

## Response Formats

### Subsonic API Compliance
All responses follow the standard Subsonic API format:
```json
{
  "subsonic-response": {
    "status": "ok",
    "version": "1.15.0",
    "songs": {
      "song": [
        {
          "id": "123",
          "title": "Song Title",
          "artist": "Artist Name",
          "album": "Album Name",
          "duration": 180
        }
      ]
    }
  }
}
```

### Content-Type Headers
```go
w.Header().Set("Content-Type", "application/json")
```

## Extension Points

### Adding New Handlers
```go
func (h *Handler) HandleNewEndpoint(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    // Custom logic here
    h.logger.Info("New endpoint accessed")
    
    // Process request parameters
    param := r.URL.Query().Get("param")
    
    // Perform business logic
    result, err := h.someService.ProcessRequest(param)
    if err != nil {
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return true
    }
    
    // Format and return response
    response := map[string]interface{}{
        "subsonic-response": map[string]interface{}{
            "status": "ok",
            "version": "1.15.0",
            "result": result,
        },
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
    return true
}
```

### Service Integration
```go
type Handler struct {
    logger      *logrus.Logger
    shuffle     *shuffle.Service
    newService  *somepackage.NewService  // Add new services as needed
}

func New(logger *logrus.Logger, shuffle *shuffle.Service, newService *somepackage.NewService) *Handler {
    return &Handler{
        logger:     logger,
        shuffle:    shuffle,
        newService: newService,
    }
}
```