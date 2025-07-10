# Subsonic API Proxy Server

A Go-based proxy server that relays requests to a Subsonic API server with configurable endpoint hooks for monitoring and interception. Includes SQLite3 database functionality for tracking played songs and building transition probability analysis with **complete multi-tenancy support** for isolated user data and personalized recommendations.

## Architecture

This application uses a modular architecture with the following components:

- **`config/`**: Configuration management with comprehensive validation and environment variable support
- **`models/`**: Data structures and type definitions
- **`database/`**: SQLite3 database operations with structured error handling and schema management
- **`handlers/`**: HTTP request handlers for different Subsonic API endpoints with input validation
- **`middleware/`**: HTTP middleware components including security headers with intelligent development mode detection
- **`server/`**: Main proxy server logic and lifecycle management with error recovery
- **`credentials/`**: Secure authentication and credential validation with AES-256-GCM encryption and timeout protection
- **`shuffle/`**: Weighted song shuffling algorithm with intelligent preference learning and thread safety
- **`errors/`**: Structured error handling with categorization and context
- **`main.go`**: Entry point that wires all modules together

## Features

- **Reverse Proxy**: Forwards all requests to upstream Subsonic server with health monitoring
- **Multi-Tenancy ✅**: Complete user data isolation with per-user song libraries, play history, and personalized recommendations
- **Hook System**: Intercept and process requests at any endpoint with comprehensive error handling
- **Credential Management**: Secure credential handling with AES-256-GCM encryption, dynamic validation, timeout protection, and thread-safe storage
- **User-Specific Song Tracking**: SQLite3 database tracks played songs with play/skip statistics per user with comprehensive validation
- **Database Connection Pooling**: Advanced connection pool management with health monitoring for optimal performance
- **Per-User Transition Analysis**: Builds transition probabilities between songs for personalized intelligent recommendations
- **Personalized Weighted Shuffle**: Thread-safe intelligent song shuffling with memory-efficient algorithms for large libraries based on individual user play history, preferences, and transition probabilities
- **Thread-Safe Server Operations**: Race condition-free background synchronization with proper mutex protection and graceful shutdown handling
- **User-Isolated Automatic Sync**: Fetches and updates song library from Subsonic API per user with error recovery and smart credential-aware timing
- **Rate Limiting**: Configurable DoS protection using token bucket algorithm with intelligent request throttling
- **CORS Support**: Comprehensive CORS header management for web application integration with configurable origins, methods, and headers
- **Security Headers Middleware ✅**: Advanced security headers with intelligent development mode detection, protecting against XSS, clickjacking, MIME sniffing, and other web vulnerabilities
- **Structured Error Handling**: Comprehensive error categorization, context, and logging for better debugging
- **Input Validation & Security**: Comprehensive input validation, sanitization, user context validation, and log injection prevention
- **Logging**: Structured logging with configurable levels and error context
- **Configuration**: Command-line flags and environment variables with validation and helpful error messages

## Multi-Tenancy ✅ **NEW**

The proxy implements **complete multi-tenancy** with full user data isolation at the database level. Each user has their own isolated music library, play history, and statistics.

### Key Benefits

- **Complete User Isolation**: Each user has their own isolated song collection, play events, and transition data
- **Personalized Recommendations**: Individual users receive song recommendations based on their personal listening history
- **Scalable Architecture**: Supports unlimited users with optimal performance through user-specific database indexes
- **Security Compliance**: Full data isolation meets privacy requirements with no data bleeding between users
- **Individual Preferences**: Each user's play/skip behavior is tracked independently for personalized experiences

### Multi-Tenant Database Schema

- **`songs`**: Primary key `(id, user_id)` ensures each song is isolated per user
- **`play_events`**: Includes `user_id` column for per-user event tracking
- **`song_transitions`**: Primary key `(user_id, from_song_id, to_song_id)` for isolated transition data
- **Performance Indexes**: User-specific indexes on all tables for optimal query performance

### API Usage

All Subsonic API endpoints require user context via the `u` parameter:

```bash
# User-specific song shuffle - each user gets personalized recommendations
curl "http://localhost:8080/rest/getRandomSongs?u=alice&p=password&size=50"
curl "http://localhost:8080/rest/getRandomSongs?u=bob&p=password&size=50"

# User-specific stream tracking - events recorded per user
curl "http://localhost:8080/rest/stream?u=alice&p=password&id=song123"

# User-specific play/skip recording - statistics tracked per user
curl "http://localhost:8080/rest/scrobble?u=alice&p=password&id=song123&submission=true"
```

### Migration & Compatibility

- **Automatic Migration**: Seamless upgrade from single-tenant to multi-tenant schema
- **Zero Downtime**: Migration runs automatically on server startup
- **Data Backup**: Existing data is backed up before migration
- **Backward Compatibility**: Handles existing installations gracefully

### Security & Validation

- **Required User Parameter**: All endpoints validate the presence of `u` parameter
- **Input Sanitization**: User IDs are sanitized to prevent log injection attacks
- **Error Handling**: Clear error messages for missing or invalid user parameters
- **User Context Enforcement**: All database operations strictly filter by user ID

## Security

This application implements comprehensive security measures to protect credentials, data, and network communications:

### Credential Security ✅

- **AES-256-GCM Encryption**: All passwords are encrypted in memory using industry-standard authenticated encryption
- **Memory Protection**: Credentials are never stored in plain text, protecting against memory dumps and process inspection
- **Unique Instance Keys**: Each server instance generates random 32-byte encryption keys for isolation
- **Secure Memory Management**: Encrypted data is securely zeroed before deallocation
- **Forward Security**: New encryption keys generated on each server restart
- **No Password Logging**: Passwords are never exposed in server logs, debug output, or error messages
- **Secure URL Encoding**: All credentials are properly encoded using `url.Values{}` to prevent logging vulnerabilities
- **Dynamic Validation**: Credentials are validated against the upstream Subsonic server with timeout protection
- **Thread-Safe Storage**: Valid encrypted credentials are stored in memory with mutex protection
- **Automatic Cleanup**: Invalid credentials are automatically and securely removed from storage
- **No Hardcoded Credentials**: All credentials come from authenticated client requests

### Network Security

- **Timeout Protection**: All network requests have configurable timeouts to prevent hanging connections
- **Upstream Validation**: All requests to upstream servers are validated before forwarding
- **Error Context**: Network errors provide context without exposing sensitive information

### Input Validation & Security ✅

- **Log Injection Prevention**: All user inputs sanitized to remove control characters before logging
- **Input Length Limits**: Maximum lengths enforced for song IDs (255), usernames (100), and general inputs (1000)
- **Song ID Validation**: Format and length validation for all song identifiers
- **Control Character Filtering**: Removes newlines, carriage returns, tabs, and escape sequences
- **DoS Protection**: Input truncation prevents memory exhaustion attacks
- **Parameter Validation**: All API parameters validated with structured error responses
- **Database Protection**: SQLite database operations use prepared statements to prevent injection
- **Structured Errors**: Error handling provides context while protecting sensitive information
- **Graceful Degradation**: System continues operating even when individual components fail

### Security Best Practices

- **Minimal Exposure**: Only necessary information is logged or exposed in error messages
- **Secure Defaults**: All security-sensitive configurations use secure default values
- **Comprehensive Testing**: Security features are thoroughly tested with unit tests
- **Regular Updates**: Security implementations follow Go best practices and are regularly reviewed

### Rate Limiting ✅

- **DoS Protection**: Comprehensive rate limiting using token bucket algorithm to prevent abuse
- **Configurable Limits**: Adjustable requests per second (RPS) and burst size for different environments
- **Early Filtering**: Rate limiting applied before request processing to maximize security
- **HTTP 429 Responses**: Clean error responses for rate-limited requests with proper logging
- **Hook Protection**: All endpoints including built-in hooks are protected from rapid requests
- **Flexible Configuration**: Can be disabled for development or tuned for production environments

### Recent Security Improvements

- **Input Validation & Sanitization**: ✅ **IMPLEMENTED** - Comprehensive protection against log injection, control character attacks, and DoS attempts
- **Rate Limiting**: ✅ **IMPLEMENTED** - Complete DoS protection with configurable token bucket rate limiting
- **Password Logging Fix**: ✅ **RESOLVED** - Eliminated password exposure in server logs during song synchronization  
- **Secure Authentication**: Enhanced credential validation with proper error handling
- **Network Security**: Improved timeout handling and error context

### Recent Performance & Stability Improvements

- **Race Condition Fixes**: ✅ **RESOLVED** - Eliminated all race conditions in shuffle and server modules
  - **Server Module**: Fixed race condition between `syncSongs()` and `Shutdown()` methods with proper mutex protection
  - **Shuffle Module**: Protected `lastPlayed` map access with `sync.RWMutex` for thread-safe operations
  - **Verified Thread Safety**: All tests pass with Go race detector - no race conditions detected
- **Performance Optimizations**: ✅ **IMPLEMENTED** - Memory-efficient shuffle algorithms with automatic selection
- **Database Connection Pooling**: ✅ **IMPLEMENTED** - Advanced connection pool management with health monitoring
- **Comprehensive Testing**: ✅ **VERIFIED** - Fully tested with curl, including error handling and rate limiting

## Installation

```bash
go build -o subsoxy
```

## Usage

```bash
./subsoxy [options]
```

### Configuration

Configuration can be set via command-line flags or environment variables. Command-line flags take precedence over environment variables. All configuration parameters are validated at startup with helpful error messages.

#### Command-line flags
- `-port string`: Proxy server port, must be 1-65535 (default: 8080)
- `-upstream string`: Upstream Subsonic server URL, must be valid HTTP/HTTPS URL (default: http://localhost:4533)
- `-log-level string`: Log level - debug, info, warn, error (default: info)
- `-db-path string`: SQLite database file path, directories will be created if needed (default: subsoxy.db)
- `-rate-limit-rps int`: Rate limit requests per second (default: 100)
- `-rate-limit-burst int`: Rate limit burst size (default: 200)
- `-rate-limit-enabled`: Enable rate limiting (default: true)
- `-db-max-open-conns int`: Maximum number of open database connections (default: 25)
- `-db-max-idle-conns int`: Maximum number of idle database connections (default: 5)
- `-db-conn-max-lifetime duration`: Maximum connection lifetime (default: 30m)
- `-db-conn-max-idle-time duration`: Maximum connection idle time (default: 5m)
- `-db-health-check`: Enable database health checks (default: true)
- `-cors-enabled`: Enable CORS headers (default: true)
- `-cors-allow-origins string`: CORS allowed origins, comma-separated (default: "*")
- `-cors-allow-methods string`: CORS allowed methods, comma-separated (default: "GET,POST,PUT,DELETE,OPTIONS")
- `-cors-allow-headers string`: CORS allowed headers, comma-separated (default: "Content-Type,Authorization,X-Requested-With")
- `-cors-allow-credentials`: CORS allow credentials (default: false)
- `-security-headers-enabled`: Enable security headers (default: true)
- `-security-dev-mode`: Enable development mode (relaxed security headers for localhost) (default: false)
- `-x-content-type-options string`: X-Content-Type-Options header value (default: nosniff)
- `-x-frame-options string`: X-Frame-Options header value (default: DENY)
- `-x-xss-protection string`: X-XSS-Protection header value (default: 1; mode=block)
- `-strict-transport-security string`: Strict-Transport-Security header value (default: max-age=31536000; includeSubDomains)
- `-content-security-policy string`: Content-Security-Policy header value (default: default-src 'self'; script-src 'self'; object-src 'none';)
- `-referrer-policy string`: Referrer-Policy header value (default: strict-origin-when-cross-origin)

#### Environment variables
- `PORT`: Proxy server port (1-65535)
- `UPSTREAM_URL`: Upstream Subsonic server URL (HTTP/HTTPS)
- `LOG_LEVEL`: Log level (debug, info, warn, error)
- `DB_PATH`: SQLite database file path
- `RATE_LIMIT_RPS`: Rate limit requests per second (default: 100)
- `RATE_LIMIT_BURST`: Rate limit burst size (default: 200)
- `RATE_LIMIT_ENABLED`: Enable rate limiting (default: true)
- `DB_MAX_OPEN_CONNS`: Maximum number of open database connections (default: 25)
- `DB_MAX_IDLE_CONNS`: Maximum number of idle database connections (default: 5)
- `DB_CONN_MAX_LIFETIME`: Maximum connection lifetime (default: 30m)
- `DB_CONN_MAX_IDLE_TIME`: Maximum connection idle time (default: 5m)
- `DB_HEALTH_CHECK`: Enable database health checks (default: true)
- `CORS_ENABLED`: Enable CORS headers (default: true)
- `CORS_ALLOW_ORIGINS`: CORS allowed origins, comma-separated (default: "*")
- `CORS_ALLOW_METHODS`: CORS allowed methods, comma-separated (default: "GET,POST,PUT,DELETE,OPTIONS")
- `CORS_ALLOW_HEADERS`: CORS allowed headers, comma-separated (default: "Content-Type,Authorization,X-Requested-With")
- `CORS_ALLOW_CREDENTIALS`: CORS allow credentials (default: false)
- `SECURITY_HEADERS_ENABLED`: Enable security headers (default: true)
- `SECURITY_DEV_MODE`: Enable development mode (default: false)
- `X_CONTENT_TYPE_OPTIONS`: X-Content-Type-Options header value (default: nosniff)
- `X_FRAME_OPTIONS`: X-Frame-Options header value (default: DENY)
- `X_XSS_PROTECTION`: X-XSS-Protection header value (default: 1; mode=block)
- `STRICT_TRANSPORT_SECURITY`: Strict-Transport-Security header value (default: max-age=31536000; includeSubDomains)
- `CONTENT_SECURITY_POLICY`: Content-Security-Policy header value (default: default-src 'self'; script-src 'self'; object-src 'none';)
- `REFERRER_POLICY`: Referrer-Policy header value (default: strict-origin-when-cross-origin)

#### Configuration Validation

The application validates all configuration parameters at startup:
- **Port**: Must be a valid number between 1 and 65535
- **Upstream URL**: Must be a valid HTTP or HTTPS URL with a host
- **Log Level**: Must be one of: debug, info, warn, error (case-insensitive)
- **Database Path**: Parent directories will be created automatically if they don't exist
- **Rate Limit RPS**: Must be at least 1 request per second
- **Rate Limit Burst**: Must be at least 1 and greater than or equal to RPS
- **DB Max Open Connections**: Must be at least 1 connection
- **DB Max Idle Connections**: Cannot be negative or exceed max open connections
- **DB Connection Lifetimes**: Cannot be negative durations
- **CORS Origins**: Cannot be empty when CORS is enabled
- **CORS Methods**: Must be valid HTTP methods (GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH)
- **CORS Headers**: Can be empty (optional)

If any configuration is invalid, the application will exit with a detailed error message explaining what needs to be fixed.

### Examples

```bash
# Basic usage (creates subsoxy.db in current directory)
./subsoxy

# Quick start with environment variables (uses dotenvx)
./start_server.sh

# Custom port and upstream
./subsoxy -port 9090 -upstream http://my-subsonic-server:4533

# Custom database path
./subsoxy -db-path /path/to/music-stats.db

# Debug logging
./subsoxy -log-level debug

# Rate limiting examples
./subsoxy -rate-limit-rps 50 -rate-limit-burst 100    # Moderate rate limiting
./subsoxy -rate-limit-rps 10 -rate-limit-burst 20     # Strict rate limiting  
./subsoxy -rate-limit-enabled=false                   # Disable rate limiting

# Database connection pool examples
./subsoxy -db-max-open-conns 50 -db-max-idle-conns 10 -db-conn-max-lifetime 1h  # High-performance
./subsoxy -db-max-open-conns 10 -db-max-idle-conns 2 -db-conn-max-lifetime 15m  # Conservative
./subsoxy -db-health-check=false                      # Disable health checks

# CORS configuration examples
./subsoxy -cors-allow-origins "http://localhost:3000,https://myapp.com"  # Specific origins
./subsoxy -cors-allow-credentials=true                # Enable credentials
./subsoxy -cors-enabled=false                         # Disable CORS entirely
./subsoxy -cors-allow-methods "GET,POST,OPTIONS"      # Restrict methods

# Using environment variables
PORT=9090 UPSTREAM_URL=http://my-subsonic-server:4533 LOG_LEVEL=debug DB_PATH=/path/to/music.db RATE_LIMIT_RPS=50 DB_MAX_OPEN_CONNS=30 CORS_ALLOW_ORIGINS="http://localhost:3000" ./subsoxy
```

## CORS Support ✅ **NEW**

The server includes comprehensive Cross-Origin Resource Sharing (CORS) support to enable web applications running on different domains/ports to access the Subsonic API proxy.

### Why CORS is Important

CORS is essential for web-based music players and applications that need to:
- Access the proxy from different domains or ports
- Make API requests from browser-based JavaScript applications
- Integrate with single-page applications (SPAs) and web frameworks
- Support development environments with different origins

### CORS Configuration

CORS can be configured via command-line flags or environment variables:

```bash
# Default configuration (allows all origins)
./subsoxy  # CORS enabled, allows "*", basic methods and headers

# Restrict to specific origins
./subsoxy -cors-allow-origins "http://localhost:3000,https://myapp.com"

# Enable credentials for authenticated requests
./subsoxy -cors-allow-credentials=true -cors-allow-origins "http://localhost:3000"

# Disable CORS entirely (for server-to-server usage)
./subsoxy -cors-enabled=false

# Custom methods and headers
./subsoxy -cors-allow-methods "GET,POST,OPTIONS" -cors-allow-headers "Content-Type,Authorization"

# Environment variable configuration
export CORS_ENABLED=true
export CORS_ALLOW_ORIGINS="http://localhost:3000,https://app.example.com"
export CORS_ALLOW_CREDENTIALS=true
./subsoxy
```

### CORS Parameters

- **Enabled**: Whether CORS headers are added to responses (default: true)
- **Allow Origins**: Comma-separated list of allowed origins (default: "*")
- **Allow Methods**: Comma-separated list of allowed HTTP methods (default: "GET,POST,PUT,DELETE,OPTIONS")
- **Allow Headers**: Comma-separated list of allowed request headers (default: "Content-Type,Authorization,X-Requested-With")
- **Allow Credentials**: Whether to allow credentials in cross-origin requests (default: false)

### CORS Security

- **Origin Validation**: When specific origins are configured, only matching origins receive CORS headers
- **Wildcard Handling**: Using "*" allows all origins (convenient for development, consider restrictions for production)
- **Credentials Security**: When credentials are enabled, wildcard origins are not allowed for security
- **Preflight Requests**: OPTIONS requests are handled automatically for complex CORS requests

### Testing CORS

You can test CORS functionality using curl:

```bash
# Test basic CORS headers
curl -H "Origin: http://localhost:3000" -i http://localhost:8080/rest/ping

# Test preflight request
curl -X OPTIONS -H "Origin: http://localhost:3000" -H "Access-Control-Request-Method: GET" -i http://localhost:8080/rest/ping

# Expected headers in response:
# Access-Control-Allow-Origin: http://localhost:3000 (or *)
# Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
# Access-Control-Allow-Headers: Content-Type, Authorization, X-Requested-With
# Access-Control-Max-Age: 86400
```

### Production Recommendations

- **Development**: Use wildcard "*" for origins during development
- **Production**: Specify exact origins for security: `-cors-allow-origins "https://myapp.com"`
- **Credentials**: Only enable when necessary and with specific origins
- **Methods**: Restrict to only needed HTTP methods for tighter security

## Security Headers ✅ **NEW**

The server includes advanced security headers middleware that automatically protects against common web vulnerabilities while providing an excellent development experience.

### Security Protection

The middleware protects against:
- **XSS Attacks**: X-XSS-Protection and Content Security Policy
- **Clickjacking**: X-Frame-Options header
- **MIME Sniffing**: X-Content-Type-Options header
- **HTTPS Attacks**: Strict-Transport-Security (HTTPS only)
- **Information Leakage**: Referrer-Policy header

### Development Mode Detection

Security headers automatically adapt based on the environment:

**Development Mode Triggers**:
- Server running on default port 8080
- Requests from localhost (`localhost`, `127.0.0.1`, `::1`, `0.0.0.0`)
- Explicit development mode: `-security-dev-mode=true`

**Development Headers (Relaxed)**:
- CSP: `default-src 'self' 'unsafe-inline' 'unsafe-eval'; connect-src 'self' ws: wss:; img-src 'self' data: blob:;`
- X-Frame-Options: `SAMEORIGIN` (allows dev tools)
- No HSTS (safer for development)

**Production Headers (Strict)**:
- CSP: `default-src 'self'; script-src 'self'; object-src 'none';`
- X-Frame-Options: `DENY` (maximum protection)
- HSTS: `max-age=31536000; includeSubDomains` (HTTPS only)

### Configuration Examples

```bash
# Default configuration (recommended)
./subsoxy  # Security headers enabled with smart detection

# Force development mode
./subsoxy -security-dev-mode=true

# Custom security headers
./subsoxy -x-frame-options="SAMEORIGIN" -content-security-policy="default-src 'self' data:"

# Disable security headers (API-only usage)
./subsoxy -security-headers-enabled=false

# Environment variable configuration
export SECURITY_HEADERS_ENABLED=true
export SECURITY_DEV_MODE=false
export X_FRAME_OPTIONS="DENY"
./subsoxy
```

### Testing Security Headers

You can verify security headers with curl:

```bash
# Test development mode (localhost)
curl -I http://localhost:8080/rest/ping

# Test production mode (external hostname)
curl -I -H "Host: example.com:9090" http://localhost:9090/rest/ping

# Expected security headers:
# X-Content-Type-Options: nosniff
# X-Frame-Options: DENY (production) or SAMEORIGIN (development)
# X-XSS-Protection: 1; mode=block
# Content-Security-Policy: (varies by mode)
# Referrer-Policy: strict-origin-when-cross-origin
```

### Production Recommendations

- **Default Settings**: Security headers are enabled by default with secure settings
- **Development**: Automatic detection provides relaxed headers for localhost
- **Production**: Strict headers are automatically applied for external requests
- **Custom CSP**: Customize Content Security Policy based on your application needs
- **HTTPS**: Use HTTPS in production to enable HSTS protection

## Rate Limiting

The server includes comprehensive rate limiting to protect against DoS attacks and excessive usage patterns. The rate limiting uses a token bucket algorithm that allows for burst traffic while maintaining overall throughput limits.

### How Rate Limiting Works

- **Token Bucket Algorithm**: Implemented using `golang.org/x/time/rate` for precise rate control
- **Early Filtering**: Rate limiting is applied before any request processing, including hooks
- **Per-Server Limiting**: Rate limits apply to all requests to the server (not per-client)
- **HTTP 429 Responses**: Rate-limited requests receive proper HTTP 429 status codes
- **Structured Logging**: Rate limit violations are logged with client IP and endpoint information

### Configuration

Rate limiting can be configured via command-line flags or environment variables:

```bash
# Default configuration (recommended for most use cases)
./subsoxy  # 100 RPS, 200 burst, enabled

# Conservative settings (shared/public instances)
./subsoxy -rate-limit-rps 10 -rate-limit-burst 20

# Aggressive settings (high-security environments)  
./subsoxy -rate-limit-rps 1 -rate-limit-burst 5

# Development/testing (rate limiting disabled)
./subsoxy -rate-limit-enabled=false

# Environment variable configuration
export RATE_LIMIT_RPS=50
export RATE_LIMIT_BURST=100
export RATE_LIMIT_ENABLED=true
./subsoxy
```

### Rate Limiting Parameters

- **RPS (Requests Per Second)**: Maximum sustained request rate (default: 100)
- **Burst Size**: Maximum number of requests allowed in a burst (default: 200)
- **Enabled**: Whether rate limiting is active (default: true)

The burst size should typically be 1.5-2x the RPS value to allow for normal usage patterns while still providing protection against abuse.

### Testing Rate Limiting

You can test the rate limiting functionality using curl:

```bash
# Start server with strict rate limiting for testing
./subsoxy -port 8081 -rate-limit-rps 2 -rate-limit-burst 3

# Test rapid requests (4th+ requests should return HTTP 429)
for i in {1..5}; do 
  curl -s -w "Request $i - Status: %{http_code}\n" http://localhost:8081/test -o /dev/null
done
```

### Rate Limiting vs. Hooks

Rate limiting is applied **before** hook processing, which means:

- Rate-limited requests never reach hooks or the upstream server
- Built-in endpoints (like `/rest/getRandomSongs`) are protected
- Hook processing overhead is avoided for rate-limited requests
- Maximum security with minimal performance impact

### Production Recommendations

- **Web servers**: 50-100 RPS with 100-200 burst
- **API servers**: 20-50 RPS with 50-100 burst  
- **Public instances**: 5-20 RPS with 10-40 burst
- **Development**: Disable rate limiting for easier testing

## Hook System

The proxy includes a hook system that allows you to intercept requests at specific endpoints. Hooks are functions that can:

- Log or monitor specific API calls
- Block requests (return `true` to prevent forwarding)
- Allow requests to continue (return `false` to forward normally)

### Built-in Hooks

The server includes built-in hooks for:
- `/rest/ping` - Logs ping requests
- `/rest/getLicense` - Logs license requests
- `/rest/stream` - Records song start events for play tracking
- `/rest/scrobble` - Records song play/skip events and updates transition data
- `/rest/getRandomSongs` - Returns weighted shuffle of songs based on play history and preferences

### Adding Custom Hooks

```go
server.AddHook("/rest/getArtists", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    // Your custom logic here
    log.Printf("Artist list requested by %s", r.RemoteAddr)
    return false // Continue with normal proxy behavior
})
```

## Credential Management

The proxy implements secure credential handling to ensure authenticated access to the upstream Subsonic server:

### How It Works

1. **Automatic Capture**: The proxy monitors all `/rest/*` requests and extracts username/password parameters
2. **Validation**: Credentials are validated against the upstream server using a ping request
3. **Secure Storage**: Valid credentials are stored in memory with thread-safe access
4. **Background Operations**: Stored credentials are used for automated tasks like song syncing
5. **Error Handling**: Invalid credentials are handled gracefully with proper logging

### Security Features

- **No Hardcoded Credentials**: All credentials come from authenticated client requests
- **Dynamic Validation**: Credentials are validated in real-time against the upstream server
- **Timeout Protection**: Validation requests have a 10-second timeout to prevent hanging
- **Asynchronous Processing**: Credential validation doesn't block client requests
- **Automatic Cleanup**: Invalid credentials are automatically removed from storage

### Client Usage

Clients should provide credentials in their Subsonic API requests as usual:

```bash
# The proxy will automatically capture and validate these credentials
curl "http://localhost:8080/rest/ping?u=myuser&p=mypass&c=myclient&f=json"
```

The proxy transparently forwards all requests to the upstream server while maintaining valid credentials for background operations.

## Error Handling

The application implements comprehensive structured error handling with the following features:

### Error Categories

Errors are categorized for better debugging and monitoring:

- **`config`**: Configuration validation errors (invalid ports, URLs, etc.)
- **`database`**: Database connection, query, and transaction errors
- **`credentials`**: Authentication and credential validation errors
- **`server`**: Server startup, shutdown, and proxy errors
- **`network`**: Upstream server connectivity and timeout errors
- **`validation`**: Input validation and parameter errors

### Error Context

Each error includes contextual information for better debugging:

```json
{
  "category": "config",
  "code": "INVALID_PORT", 
  "message": "port must be a number",
  "context": {
    "port": "abc",
    "range": "1-65535"
  }
}
```

### Go 1.13+ Compatibility

The application uses modern Go error handling with full Go 1.13+ compatibility:

- **Error Wrapping**: Proper error chains with `Unwrap()` support
- **Error Comparison**: `Is()` method for error type comparison
- **Error Unwrapping**: `As()` method for extracting specific error types
- **Error Navigation**: Helper functions for traversing error chains

### Error Recovery

The application implements graceful error recovery:

- **Configuration errors**: Application exits with helpful error messages
- **Database errors**: Operations are retried or gracefully degraded
- **Network errors**: Automatic retry with exponential backoff
- **Credential errors**: Invalid credentials are automatically cleaned up
- **Input validation**: Invalid requests return appropriate HTTP error codes

### Logging

All errors are logged with structured context using logrus:

```bash
time="2023-12-01T10:30:00Z" level=error msg="Database connection failed" 
  error="[database:CONNECTION_FAILED] failed to open database: /path/to/db.db" 
  path="/path/to/db.db"
```

## Database Features

The server automatically creates and manages a SQLite3 database with advanced connection pooling to track song play statistics and build transition probability analysis for song sequences.

### Database Connection Pooling ✅

The application implements advanced database connection pooling for optimal performance under high load:

#### Performance Benefits
- **Connection Reuse**: Maintains a pool of database connections to avoid expensive connection creation
- **Configurable Pool Size**: Adjustable maximum open and idle connection limits
- **Connection Lifecycle Management**: Automatic rotation and cleanup of aged connections
- **Health Monitoring**: Periodic health checks to ensure connection validity
- **Thread Safety**: Safe concurrent access from multiple request handlers
- **Resource Management**: Automatic cleanup of idle and expired connections

#### Configuration Options
- **Max Open Connections**: Maximum number of concurrent database connections (default: 25)
- **Max Idle Connections**: Maximum number of idle connections to keep open (default: 5)
- **Connection Lifetime**: Maximum time a connection can be reused (default: 30 minutes)
- **Idle Timeout**: Maximum time a connection can stay idle (default: 5 minutes)
- **Health Checks**: Automatic connection health monitoring (default: enabled)

#### Pool Management
- **Background Health Checks**: Connection validation every 30 seconds
- **Connection Statistics**: Real-time monitoring of pool performance
- **Dynamic Configuration**: Runtime pool configuration updates
- **Comprehensive Logging**: Pool status and health metrics logging

#### Usage Examples
```bash
# High-performance setup for heavy load
./subsoxy -db-max-open-conns 50 -db-max-idle-conns 10 -db-conn-max-lifetime 1h

# Conservative setup for low resource usage
./subsoxy -db-max-open-conns 10 -db-max-idle-conns 2 -db-conn-max-lifetime 15m

# Environment variable configuration
export DB_MAX_OPEN_CONNS=30
export DB_MAX_IDLE_CONNS=8
export DB_CONN_MAX_LIFETIME=45m
./subsoxy
```

### Multi-Tenant Database Schema ✅ **UPDATED**

#### songs (Multi-Tenant)
- `id` (TEXT): Unique song identifier within user context
- `user_id` (TEXT): User identifier for data isolation
- `title` (TEXT): Song title
- `artist` (TEXT): Artist name
- `album` (TEXT): Album name
- `duration` (INTEGER): Song duration in seconds
- `last_played` (DATETIME): Last time the song was played by this user
- `play_count` (INTEGER): Number of times the song was played by this user
- `skip_count` (INTEGER): Number of times the song was skipped by this user
- **PRIMARY KEY**: `(id, user_id)` for per-user song isolation

#### play_events (Multi-Tenant)
- `id` (INTEGER PRIMARY KEY): Auto-incrementing event ID
- `user_id` (TEXT): User identifier for data isolation
- `song_id` (TEXT): Reference to the song within user context
- `event_type` (TEXT): Type of event (start, play, skip)
- `timestamp` (DATETIME): When the event occurred
- `previous_song` (TEXT): ID of the previously played song by this user (for transition tracking)

#### song_transitions (Multi-Tenant)
- `user_id` (TEXT): User identifier for data isolation
- `from_song_id` (TEXT): ID of the song that was playing before (within user context)
- `to_song_id` (TEXT): ID of the song that started playing (within user context)
- `play_count` (INTEGER): Number of times this transition resulted in a play for this user
- `skip_count` (INTEGER): Number of times this transition resulted in a skip for this user
- `probability` (REAL): Calculated probability of playing (vs skipping) this transition for this user
- **PRIMARY KEY**: `(user_id, from_song_id, to_song_id)` for per-user transition isolation

#### Performance Indexes
- `idx_songs_user_id`: Optimizes user-specific song queries
- `idx_play_events_user_id`: Optimizes user-specific event queries  
- `idx_song_transitions_user_id`: Optimizes user-specific transition queries

### Multi-Tenant Features ✅ **UPDATED**

- **Per-User Credential Management**: Automatically captures and validates user credentials from client requests with user isolation
- **User-Isolated Automatic Song Sync**: Fetches all songs from the Subsonic API every hour using validated credentials, with smart startup timing that waits for client requests before syncing
- **Per-User Play Tracking**: Records when songs are started, played completely, or skipped with complete user isolation
- **User-Specific Transition Probability Analysis**: Builds transition probabilities between songs for each user independently
- **Isolated Historical Data**: Maintains complete event history for analysis per user

### Multi-Tenant Data Collection

The system automatically tracks per user:
- User credentials from client requests and validates them against the upstream server with user context
- When a song starts playing (`/rest/stream` endpoint) - recorded with user ID
- When a song is marked as played or skipped (`/rest/scrobble` endpoint) - tracked per user
- Transitions between songs for building personalized recommendation data per user

### User Isolation Benefits

- **Complete Data Separation**: Each user's data is completely isolated from other users
- **Personalized Analytics**: Statistics and probabilities calculated independently per user
- **Individual Learning**: Each user's preferences learned and applied separately
- **Privacy Compliance**: No data bleeding between users ensures privacy requirements are met

## Weighted Shuffle Feature ✅ **UPDATED FOR MULTI-TENANCY & PERFORMANCE**

The `/rest/getRandomSongs` endpoint provides intelligent song shuffling using a **per-user weighted algorithm** with **memory-efficient performance optimizations** that considers multiple factors to provide personalized music recommendations for each user.

### Performance Optimizations ✅ **NEW**

The shuffle system automatically adapts to library size for optimal performance:

#### **Small Libraries (≤5,000 songs)**
- **Algorithm**: Original algorithm with complete song analysis
- **Memory Usage**: O(total_songs) - all songs loaded into memory
- **Performance**: ~5ms for 1,000 songs, ~25ms for 5,000 songs
- **Quality**: 100% of songs considered for maximum recommendation quality

#### **Large Libraries (>5,000 songs)**
- **Algorithm**: Memory-efficient reservoir sampling with batch processing
- **Memory Usage**: O(sample_size) - only representative sample in memory
- **Performance**: ~106ms for 10,000 songs, ~2.4s for 50,000 songs
- **Quality**: 3x oversampling maintains high recommendation quality
- **Batch Processing**: Processes songs in 1,000-song batches to control memory usage

#### **Performance Benefits**
- **Memory Efficiency**: ~90% reduction in memory usage for large libraries
- **Scalability**: Handles libraries with 100,000+ songs without memory exhaustion
- **Batch Database Queries**: Single query for all transition probabilities (eliminates N+1 query problem)
- **Automatic Algorithm Selection**: Seamlessly switches algorithms based on library size
- **Thread Safety**: Maintained with optimized concurrent access patterns

### How Multi-Tenant Shuffling Works

The shuffle algorithm calculates a weight for each song **per user** based on:

1. **User-Specific Time Decay**: Songs played recently by the user (within 30 days) receive lower weights to encourage variety
2. **Per-User Play/Skip Ratio**: Songs with better play-to-skip ratios for this specific user are more likely to be selected
3. **User-Specific Transition Probabilities**: Uses transition data from this user's listening history to prefer songs that historically follow well from their last played song

### Database Performance Optimizations ✅ **NEW**

- **`GetSongCount()`**: Fast song counting for intelligent algorithm selection
- **`GetSongsBatch()`**: Pagination support with LIMIT/OFFSET for memory-efficient processing
- **`GetTransitionProbabilities()`**: Batch probability queries eliminate N+1 query problems
- **Prepared Statements**: Optimized query performance with connection pooling

### Multi-Tenant Usage

```bash
# Get 50 user-specific weighted-shuffled songs (REQUIRED user parameter)
curl "http://localhost:8080/rest/getRandomSongs?u=alice&p=password&c=subsoxy&f=json"

# Different user gets different personalized recommendations
curl "http://localhost:8080/rest/getRandomSongs?u=bob&p=password&c=subsoxy&f=json"

# Get 100 user-specific weighted-shuffled songs
curl "http://localhost:8080/rest/getRandomSongs?size=100&u=alice&p=password&c=subsoxy&f=json"
```

### Multi-Tenancy Benefits

- **Personalized Recommendations**: Each user gets recommendations based on their individual listening history
- **User-Specific Repetition Reduction**: Recently played songs by each user are less likely to appear in their shuffle
- **Individual Preference Learning**: Songs each user tends to play (vs skip) are weighted higher for that user only
- **Per-User Context Awareness**: Considers what song was played previously by each user for smoother transitions
- **Individual Discovery**: New and unplayed songs get a boost per user to encourage personalized exploration
- **Complete Isolation**: User recommendations don't affect each other's shuffle algorithms

### Error Handling

- **Missing User Parameter**: Returns HTTP 400 with "Missing user parameter" error
- **Invalid Parameters**: Proper validation with descriptive error messages
- **User Context Validation**: All requests validated for user context before processing

## Development

### Project Structure

```
.
├── main.go              # Application entry point
├── config/              # Configuration management with validation
│   ├── config.go        # Configuration struct and validation logic
│   ├── config_test.go   # Configuration tests
│   └── README.md        # Configuration documentation
├── models/              # Data structures and types
│   ├── models.go        # Core data models
│   ├── models_test.go   # Model tests
│   └── README.md        # Models documentation
├── database/            # Database operations with error handling
│   ├── database.go      # Database interface and operations
│   ├── database_test.go # Database tests
│   └── README.md        # Database documentation
├── handlers/            # HTTP request handlers with validation
│   ├── handlers.go      # HTTP endpoint handlers
│   ├── handlers_test.go # Handler tests
│   └── README.md        # Handlers documentation
├── middleware/          # HTTP middleware components
│   ├── security.go      # Security headers middleware
│   ├── security_test.go # Security middleware tests
│   └── README.md        # Middleware documentation
├── server/              # Main server logic with error recovery
│   ├── server.go        # Proxy server implementation
│   ├── server_test.go   # Server tests
│   └── README.md        # Server documentation
├── credentials/         # Authentication management with timeout protection
│   ├── credentials.go   # Credential validation and storage
│   ├── credentials_test.go # Credential tests
│   └── README.md        # Credentials documentation
├── shuffle/             # Weighted shuffling algorithm
│   ├── shuffle.go       # Song shuffling logic
│   ├── shuffle_test.go  # Shuffle tests
│   └── README.md        # Shuffle documentation
├── errors/              # Structured error handling
│   ├── errors.go        # Error types and utilities
│   ├── errors_test.go   # Error handling tests
│   └── README.md        # Error handling documentation
├── go.mod               # Go module definition
├── go.sum               # Go module checksums
├── CLAUDE.md            # Development guidance
└── README.md            # This file
```

### Building and Testing

```bash
# Install dependencies
go mod tidy

# Run tests
go test ./...

# Build the application
go build -o subsoxy

# Clean up build artifacts
rm subsoxy
```

### Module Dependencies

Each module has clearly defined dependencies:

- `errors/` → No internal dependencies (foundational error handling)
- `config/` → `errors/` (for configuration validation errors)
- `models/` → No internal dependencies (pure data structures)
- `database/` → `errors/`, `models/` (database operations with structured errors)
- `credentials/` → `errors/` (credential validation with structured errors)
- `shuffle/` → `models/`, `database/` (song shuffling algorithms)
- `handlers/` → `errors/`, `shuffle/` (HTTP handlers with validation)
- `server/` → All modules (main orchestration layer)
- `main.go` → `config/`, `server/` (application entry point)

The `errors/` package provides the foundation for structured error handling throughout the application, while `models/` defines core data structures used across modules.

### External Dependencies

This application uses the following external libraries:

- **`github.com/gorilla/mux`**: HTTP router for request handling and middleware
- **`github.com/sirupsen/logrus`**: Structured logging with configurable levels and formatting
- **`github.com/mattn/go-sqlite3`**: SQLite3 database driver for song tracking and analytics
- **`golang.org/x/crypto`**: Cryptographic functions for AES-256-GCM credential encryption
- **`golang.org/x/time/rate`**: Rate limiting implementation using token bucket algorithm
- **Standard Library**: `net/http/httputil`, `crypto/aes`, `crypto/cipher`, `database/sql`, and other Go standard packages

### Performance Features

The application includes several performance optimizations:

- **Database Connection Pooling**: Advanced connection pool management with configurable limits and health monitoring
- **Memory-Efficient Shuffle Algorithms**: Automatic algorithm selection based on library size with reservoir sampling for large datasets
- **Batch Database Queries**: Optimized query patterns eliminate N+1 query problems
- **Concurrent Request Handling**: Thread-safe operations with proper synchronization
- **Rate Limiting**: Token bucket algorithm for efficient request throttling
- **Resource Management**: Automatic cleanup of connections and memory
- **Health Monitoring**: Background health checks and performance metrics

## License

MIT License