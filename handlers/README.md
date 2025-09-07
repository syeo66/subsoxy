# Handlers Module

The handlers module provides HTTP request handlers for different Subsonic API endpoints with **multi-tenant support** and business logic implementation.

## Overview

This module handles:
- Endpoint-specific request processing with comprehensive input validation
- Input sanitization and security protection against log injection attacks
- Response formatting and serialization
- Integration with shuffle service
- Error handling and logging with sanitized inputs
- Custom business logic for enhanced features

## Handler Types

### Shuffle Handler
Provides intelligent weighted song shuffling for `/rest/getRandomSongs` with **JSON and XML format support** and **cover art information** ✅ **NEW**.

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
func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, string, *string), addPendingFunc func(string, string), setStartedFunc func(string, string)) bool {
    userID := r.URL.Query().Get("u")
    songID := r.URL.Query().Get("id")
    if songID != "" {
        // Add song to pending list (no immediate skip detection)
        addPendingFunc(userID, songID)
        // Record that this song started (for compatibility)
        setStartedFunc(userID, songID)
        recordFunc(userID, songID, "start", nil)
    }
    return false // Continue with normal proxy behavior
}

func (h *Handler) HandleScrobble(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, string, *string), setLastPlayed func(string, string), processScrobbleFunc func(string, string, bool)) bool {
    userID := r.URL.Query().Get("u")
    songID := r.URL.Query().Get("id")
    submission := r.URL.Query().Get("submission")
    isSubmission := submission == "true"
    
    if songID != "" {
        // Process pending songs first (may mark earlier songs as skipped)
        processScrobbleFunc(userID, songID, isSubmission)
        
        if isSubmission {
            recordFunc(userID, songID, "play", nil)
            setLastPlayed(userID, songID)
        }
        // Note: submission=false is processed for pending song cleanup,
        // but doesn't count as a play. True skips occur when songs are
        // never scrobbled or when later songs get scrobbled first.
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
    return handlers.HandleStream(w, r, endpoint, server.RecordPlayEvent, server.AddPendingSong, server.SetLastStarted)
})

server.AddHook("/rest/scrobble", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    return handlers.HandleScrobble(w, r, endpoint, server.RecordPlayEvent, server.SetLastPlayed, server.ProcessScrobble)
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
The handlers module integrates with the shuffle service for intelligent song recommendations with thread safety:
```go
type Handler struct {
    logger  *logrus.Logger
    shuffle *shuffle.Service  // Thread-safe concurrent access
}
```

The shuffle service provides thread-safe operations allowing multiple simultaneous requests from different users without race conditions.

### Event Recording
Handlers accept callback functions for recording play events:
```go
recordFunc func(string, string, string, *string)  // userID, songID, eventType, previousSong
setLastPlayed func(string, string)                // userID, songID
addPendingFunc func(string, string)               // userID, songID - adds song to pending list
processScrobbleFunc func(string, string, bool)    // userID, songID, isSubmission - processes scrobbles
setStartedFunc func(string, string)               // userID, songID - tracks started songs (compatibility)
```

### Enhanced Skip Detection Logic
The system uses robust, preload-resistant skip detection logic:

- **Stream Handler**: Adds songs to pending list without immediate skip detection
- **Scrobble Handler**: Processes pending songs and marks earlier unscrobbled songs as skipped
- **Pending Song Tracking**: Handles multiple preloaded tracks without false positives
- **Timeout Cleanup**: Songs pending >5 minutes without scrobble are automatically marked as skipped
- **Skip Criteria**: Songs are skipped when later songs get scrobbled first, or when they timeout
- **Preload Support**: Multiple concurrent stream requests don't trigger false skip detection

This ensures accurate skip detection even with aggressive client preloading strategies.

## Error Handling

### Internal Errors
```go
if err != nil {
    h.logger.WithError(err).Error("Failed to get weighted shuffled songs")
    http.Error(w, "Internal server error", http.StatusInternalServerError)
    return true
}
```

### Input Validation and Sanitization

**Parameter Validation with Size Limits**:
```go
// Parse and validate size parameter with comprehensive error handling
sizeStr := r.URL.Query().Get("size")
size := 50  // default
if sizeStr != "" {
    if parsedSize, err := strconv.Atoi(sizeStr); err == nil {
        if parsedSize > 10000 { // Prevent extremely large requests
            validationErr := errors.ErrValidationFailed.WithContext("field", "size").
                WithContext("value", parsedSize).
                WithContext("max_allowed", 10000)
            h.logger.WithError(validationErr).Warn("Size parameter too large")
            http.Error(w, "Size parameter too large (max: 10000)", http.StatusBadRequest)
            return true
        }
        if parsedSize > 0 { // Only use valid positive sizes
            size = parsedSize
        }
    } else {
        validationErr := errors.ErrInvalidInput.WithContext("field", "size").
            WithContext("value", sizeStr)
        h.logger.WithError(validationErr).Warn("Invalid size parameter")
        http.Error(w, "Invalid size parameter", http.StatusBadRequest)
        return true
    }
}
```

**Song ID Validation and Sanitization**:
```go
// Validate song ID format and length
func ValidateSongID(songID string) error {
    if len(songID) == 0 {
        return errors.ErrMissingParameter.WithContext("parameter", "songID")
    }
    if len(songID) > MaxSongIDLength {
        return errors.ErrInvalidInput.WithContext("field", "songID").
            WithContext("length", len(songID)).
            WithContext("max_length", MaxSongIDLength)
    }
    return nil
}

// Sanitize inputs for logging to prevent log injection
func SanitizeForLogging(input string) string {
    // Remove control characters (ASCII 0-31 and 127)
    sanitized := strings.Map(func(r rune) rune {
        if r < 32 || r == 127 {
            return -1
        }
        return r
    }, input)
    
    // Limit length to prevent resource exhaustion
    if len(sanitized) > MaxInputLength {
        sanitized = sanitized[:MaxInputLength] + "..."
    }
    
    return sanitized
}

// Example usage in stream handler
songID := r.URL.Query().Get("id")
if songID != "" {
    if err := ValidateSongID(songID); err != nil {
        h.logger.WithError(err).Warn("Invalid song ID in stream request")
        return false
    }
    recordFunc(songID, "start", nil)
    h.logger.WithField("song_id", SanitizeForLogging(songID)).Debug("Recorded stream start event")
}
```

**Security Constants**:
```go
const (
    MaxSongIDLength = 255
    MaxInputLength = 1000
)
```

## Response Formats

### Subsonic API Compliance ✅ **ENHANCED WITH XML SUPPORT**
Responses follow the standard Subsonic API format and support both JSON and XML output via the `f` parameter:

#### JSON Format (Default)
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
          "duration": 180,
          "coverArt": "cover456"
        }
      ]
    }
  }
}
```

#### XML Format ✅ **NEW**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<subsonic-response status="ok" version="1.15.0">
  <songs>
    <song id="123" title="Song Title" artist="Artist Name" album="Album Name" duration="180" coverArt="cover456"/>
  </songs>
</subsonic-response>
```

#### Cover Art Support ✅ **NEW**
- Cover art information is automatically included in song responses when available
- Uses `omitempty` tags - only appears when cover art data exists
- Cover art identifiers can be used with the Subsonic `/rest/getCoverArt` endpoint
- Backward compatible with existing clients that don't expect cover art

### Format Selection
```go
// Check format parameter (defaults to json if not specified)
format := r.URL.Query().Get("f")
if format == "" {
    format = "json"
}

if format == "xml" {
    // XML response
    xmlResponse := models.XMLSubsonicResponse{
        Status:  "ok",
        Version: "1.15.0",
        Songs: &models.XMLSongs{
            Song: songs,
        },
    }
    
    w.Header().Set("Content-Type", "application/xml")
    w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>`))
    xml.NewEncoder(w).Encode(xmlResponse)
} else {
    // JSON response (default)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}
```

### Content-Type Headers
```go
// JSON format
w.Header().Set("Content-Type", "application/json")

// XML format ✅ **NEW**
w.Header().Set("Content-Type", "application/xml")
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