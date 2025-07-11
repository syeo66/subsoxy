# Credentials Module ✅ **ENHANCED WITH TOKEN SUPPORT**

The credentials module provides secure multi-mode credential management with AES-256-GCM encryption for authenticating with the upstream Subsonic server.

## Overview

This module handles:
- **Multi-Mode Authentication**: Supports both password-based and token-based Subsonic authentication
- **AES-256-GCM Encryption**: All credentials (passwords/tokens) are encrypted in memory using industry-standard authenticated encryption
- **Dynamic credential capture** from client requests (both authentication modes)
- **Multi-format validation** against upstream server (password and token authentication)
- Thread-safe encrypted credential storage
- Background operation authentication
- Secure credential cleanup with memory zeroing
- **Client Compatibility**: Full support for modern Subsonic clients (Symfonium, DSub, etc.)

## Features

### Security
- **Multi-Mode Authentication**: Supports both password-based (`u` + `p`) and token-based (`u` + `t` + `s`) authentication
- **AES-256-GCM Encryption**: All credentials (passwords/tokens) encrypted using authenticated encryption with unique instance keys
- **Memory Protection**: Credentials never stored in plain text, protecting against memory dumps
- **Secure Cleanup**: Encrypted data is securely zeroed before deallocation
- **Per-Instance Security**: Each server instance generates unique 32-byte encryption keys
- **Forward Security**: New encryption keys generated on each server restart
- **No Hardcoded Credentials**: All credentials come from authenticated client requests
- **Real-time Validation**: Credentials are validated against the upstream server using `/rest/ping` with appropriate auth method
- **Token Validation**: Full support for validating token-based authentication against upstream server
- **Thread-Safe Storage**: Concurrent access is protected with read-write mutexes
- **Timeout Protection**: Validation requests have configurable timeouts
- **Automatic Cleanup**: Invalid credentials are securely removed from storage

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
// Validate and store password-based credentials (async) - returns whether it's a new credential
isNew, err := credManager.ValidateAndStore("username", "password")

// Validate and store token-based credentials (async) - returns whether it's a new credential
isNew, err := credManager.ValidateAndStore("username", "TOKEN:token_value:salt_value")

// Get valid credentials for background operations
username, password := credManager.GetValid()

// Get all valid credentials (multi-user support)
allCreds := credManager.GetAllValid()

// Clear invalid credentials
credManager.ClearInvalid()
```

## Implementation Details

### Validation Process
1. **Duplicate Check**: Verify if credentials are already stored and valid
2. **New Credential Detection**: Determine if this is a first-time credential capture
3. **Authentication Mode Detection**: Determine if using password-based or token-based authentication
4. **Upstream Validation**: Make a `/rest/ping` request to the upstream server with appropriate auth parameters
5. **Response Parsing**: Parse the JSON response to check status
6. **Storage**: Store valid credentials (encrypted) in thread-safe map
7. **Immediate Sync Trigger ✅ NEW**: If credentials are new, trigger immediate library sync
8. **Logging**: Log validation results for monitoring with auth mode indication

### Data Structures
```go
// Encrypted credential storage
type encryptedCredential struct {
    EncryptedPassword []byte `json:"encrypted_password"`
    Nonce            []byte `json:"nonce"`
}

type Manager struct {
    validCredentials map[string]encryptedCredential  // Encrypted storage
    mutex            sync.RWMutex                    // Protects concurrent access
    logger           *logrus.Logger
    upstreamURL      string
    encryptionKey    []byte                         // AES-256 key (32 bytes)
}
```

### Encryption Implementation
```go
// AES-256-GCM encryption
func (cm *Manager) encryptPassword(password string) (encryptedCredential, error) {
    block, err := aes.NewCipher(cm.encryptionKey)
    if err != nil {
        return encryptedCredential{}, err
    }
    
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return encryptedCredential{}, err
    }
    
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return encryptedCredential{}, err
    }
    
    encryptedPassword := gcm.Seal(nil, nonce, []byte(password), nil)
    
    return encryptedCredential{
        EncryptedPassword: encryptedPassword,
        Nonce:            nonce,
    }, nil
}
```

### Validation Request
```go
func (cm *Manager) validate(username, password string) error {
    baseURL, _ := url.Parse(cm.upstreamURL + "/rest/ping")
    params := url.Values{}
    params.Add("u", username)
    
    // Check authentication mode
    if strings.HasPrefix(password, "TOKEN:") {
        // Token-based authentication
        parts := strings.Split(password, ":")
        token, salt := parts[1], parts[2]
        params.Add("t", token)
        params.Add("s", salt)
    } else {
        // Password-based authentication
        params.Add("p", password)
    }
    
    params.Add("v", "1.15.0")
    params.Add("c", "subsoxy")
    params.Add("f", "json")
    baseURL.RawQuery = params.Encode()
    
    client := &http.Client{
        Timeout: 10 * time.Second,  // Prevent hanging
    }
    
    // ... validation logic
}
```

## Usage Patterns

### Client Request Handling
```go
// In the proxy server - now supports both auth modes with immediate sync
if strings.HasPrefix(endpoint, "/rest/") {
    username, password := extractCredentials(r)  // Extracts both password and token auth
    if username != "" && password != "" {
        // Validate asynchronously to avoid blocking the request
        go func() {
            isNewCredential, err := credManager.ValidateAndStore(username, password)
            if err != nil {
                logger.WithError(err).WithField("username", username).Warn("Failed to validate credentials")
            } else if isNewCredential {
                logger.WithField("username", username).Info("New credentials captured, triggering immediate sync")
                // Trigger immediate sync for new credentials
                server.fetchAndStoreSongs()
            }
        }()
    }
}

// Enhanced credential extraction
func extractCredentials(r *http.Request) (username, password string) {
    username = r.URL.Query().Get("u")
    password = r.URL.Query().Get("p")
    
    // If no password, try token-based auth
    if password == "" {
        token := r.URL.Query().Get("t")
        salt := r.URL.Query().Get("s")
        if token != "" && salt != "" {
            password = "TOKEN:" + token + ":" + salt
        }
    }
    
    return username, password
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
The validation uses the standard Subsonic ping endpoint with appropriate authentication:

**Password-based authentication:**
```
/rest/ping?u={username}&p={password}&v=1.15.0&c=subsoxy&f=json
```

**Token-based authentication:**
```
/rest/ping?u={username}&t={token}&s={salt}&v=1.15.0&c=subsoxy&f=json
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

### Encrypted Memory Storage
- **AES-256-GCM Protection**: All passwords encrypted with authenticated encryption
- **Unique Instance Keys**: Each server instance has independent 32-byte encryption keys
- **Memory Dump Protection**: Plain text passwords never exist in memory after validation
- **Secure Cleanup**: Encrypted data is zeroed before deallocation
- Credentials are stored in memory only (not persisted)
- Application restart clears all stored credentials and generates new encryption keys
- No credential exposure in logs or files

### Validation Security
- Uses HTTPS when upstream server supports it
- Validates against actual Subsonic server (not local simulation)
- Timeout prevents resource exhaustion attacks

### Access Control
- Only validated credentials are stored (encrypted)
- Read-write mutex prevents race conditions
- All encryption/decryption operations are thread-safe
- Automatic secure cleanup of invalid credentials
- Per-instance encryption keys provide isolation between server instances

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
- Monitor encryption/decryption operation performance

## Encryption Details

### Algorithm
- **Cipher**: AES-256 (Advanced Encryption Standard with 256-bit keys)
- **Mode**: GCM (Galois/Counter Mode) for authenticated encryption
- **Key Size**: 32 bytes (256 bits)
- **Nonce Size**: 12 bytes (96 bits) - standard for GCM

### Key Management
- **Generation**: Cryptographically secure random key generation using `crypto/rand`
- **Scope**: Per-instance keys (not shared between server instances)
- **Lifetime**: Keys exist only for the lifetime of the server process
- **Fallback**: Deterministic key generation as fallback if `crypto/rand` fails

### Security Properties
- **Confidentiality**: AES-256 provides strong encryption
- **Authenticity**: GCM mode ensures data hasn't been tampered with
- **Unique Nonces**: Each encryption uses a unique nonce for semantic security
- **Forward Security**: New keys generated on each server restart
- **Memory Safety**: Encrypted data is securely zeroed on cleanup

### Testing
```go
// Test encryption/decryption
func TestEncryptionDecryption(t *testing.T) {
    manager := New(logger, "http://localhost:4533")
    
    password := "test-password-123"
    encryptedCred, err := manager.encryptPassword(password)
    // Verify encrypted data is different from original
    // Verify decryption returns original password
}

// Test different managers have different keys
func TestEncryptionWithDifferentKeys(t *testing.T) {
    manager1 := New(logger, "http://localhost:4533")
    manager2 := New(logger, "http://localhost:4533")
    
    // Verify different managers can't decrypt each other's data
}
```