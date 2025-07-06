# Credentials Module

The credentials module provides secure credential management for authenticating with the upstream Subsonic server.

## Overview

This module handles:
- Dynamic credential capture from client requests
- Credential validation against upstream server
- Thread-safe credential storage
- Background operation authentication
- Automatic cleanup of invalid credentials

## Features

### Security
- **No Hardcoded Credentials**: All credentials come from authenticated client requests
- **Real-time Validation**: Credentials are validated against the upstream server using `/rest/ping`
- **Thread-Safe Storage**: Concurrent access is protected with read-write mutexes
- **Timeout Protection**: Validation requests have configurable timeouts
- **Automatic Cleanup**: Invalid credentials are removed from storage

### Performance
- **Asynchronous Validation**: Credential validation doesn't block client requests
- **Caching**: Valid credentials are cached to avoid repeated validation
- **Efficient Storage**: In-memory storage with minimal overhead

## API

### Initialization
```go
import "github.com/syeo66/subsoxy/credentials"

credManager := credentials.New(logger, upstreamURL)
```

### Credential Management
```go
// Validate and store credentials (async)
credManager.ValidateAndStore("username", "password")

// Get valid credentials for background operations
username, password := credManager.GetValid()

// Clear invalid credentials
credManager.ClearInvalid()
```

## Implementation Details

### Validation Process
1. **Duplicate Check**: Verify if credentials are already stored and valid
2. **Upstream Validation**: Make a `/rest/ping` request to the upstream server
3. **Response Parsing**: Parse the JSON response to check status
4. **Storage**: Store valid credentials in thread-safe map
5. **Logging**: Log validation results for monitoring

### Thread Safety
```go
type Manager struct {
    validCredentials map[string]string
    mutex            sync.RWMutex  // Protects concurrent access
    logger           *logrus.Logger
    upstreamURL      string
}
```

### Validation Request
```go
func (cm *Manager) validate(username, password string) bool {
    url := fmt.Sprintf("%s/rest/ping?u=%s&p=%s&v=1.15.0&c=subsoxy&f=json", 
        cm.upstreamURL, username, password)
    
    client := &http.Client{
        Timeout: 10 * time.Second,  // Prevent hanging
    }
    
    // ... validation logic
}
```

## Usage Patterns

### Client Request Handling
```go
// In the proxy server
if strings.HasPrefix(endpoint, "/rest/") {
    username := r.URL.Query().Get("u")
    password := r.URL.Query().Get("p")
    if username != "" && password != "" {
        // Validate asynchronously to avoid blocking the request
        go credManager.ValidateAndStore(username, password)
    }
}
```

### Background Operations
```go
// For automated tasks like song syncing
username, password := credManager.GetValid()
if username == "" || password == "" {
    logger.Warn("No valid credentials available for background operation")
    return
}

// Use credentials for upstream API call
url := fmt.Sprintf("%s/rest/search3?u=%s&p=%s...", upstreamURL, username, password)
```

### Error Handling
```go
// When upstream operations fail due to authentication
if response.Status != "ok" {
    logger.Error("Authentication failed - clearing invalid credentials")
    credManager.ClearInvalid()
    return
}
```

## Configuration

### Validation Timeout
The validation timeout can be adjusted:

```go
client := &http.Client{
    Timeout: 10 * time.Second,  // Configurable timeout
}
```

### Upstream Endpoint
The validation uses the standard Subsonic ping endpoint:
```
/rest/ping?u={username}&p={password}&v=1.15.0&c=subsoxy&f=json
```

## Error Scenarios

### Network Issues
- Connection timeouts are handled gracefully
- Network errors don't affect stored credentials
- Failed validations are logged but don't block requests

### Invalid Credentials
- Invalid username/password combinations are rejected
- Failed authentication clears all stored credentials
- Clients receive normal proxy responses (authentication happens upstream)

### Upstream Server Issues
- Server unavailability doesn't affect proxy operation
- Background operations gracefully handle missing credentials
- Automatic retry logic can be implemented

## Security Considerations

### Memory Storage
- Credentials are stored in memory only (not persisted)
- Application restart clears all stored credentials
- No credential exposure in logs or files

### Validation Security
- Uses HTTPS when upstream server supports it
- Validates against actual Subsonic server (not local simulation)
- Timeout prevents resource exhaustion attacks

### Access Control
- Only validated credentials are stored
- Read-write mutex prevents race conditions
- Automatic cleanup of invalid credentials

## Monitoring

### Logging
```go
// Successful validation
logger.WithField("username", username).Info("Credentials validated and stored")

// Failed validation
logger.WithField("username", username).Warn("Invalid credentials provided")

// Cleanup
logger.Warn("Clearing potentially invalid credentials")
```

### Metrics
- Track credential validation success/failure rates
- Monitor background operation authentication
- Log credential usage patterns