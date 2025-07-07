# Subsonic API Proxy Server

A Go-based proxy server that relays requests to a Subsonic API server with configurable endpoint hooks for monitoring and interception. Includes SQLite3 database functionality for tracking played songs and building transition probability analysis.

## Architecture

This application uses a modular architecture with the following components:

- **`config/`**: Configuration management with comprehensive validation and environment variable support
- **`models/`**: Data structures and type definitions
- **`database/`**: SQLite3 database operations with structured error handling and schema management
- **`handlers/`**: HTTP request handlers for different Subsonic API endpoints with input validation
- **`server/`**: Main proxy server logic and lifecycle management with error recovery
- **`credentials/`**: Secure authentication and credential validation with timeout protection
- **`shuffle/`**: Weighted song shuffling algorithm with intelligent preference learning
- **`errors/`**: Structured error handling with categorization and context
- **`main.go`**: Entry point that wires all modules together

## Features

- **Reverse Proxy**: Forwards all requests to upstream Subsonic server with health monitoring
- **Hook System**: Intercept and process requests at any endpoint with comprehensive error handling
- **Credential Management**: Secure credential handling with dynamic validation, timeout protection, and thread-safe storage
- **Song Tracking**: SQLite3 database tracks played songs with play/skip statistics and comprehensive validation
- **Transition Probability Analysis**: Builds transition probabilities between songs for intelligent recommendations
- **Weighted Shuffle**: Intelligent song shuffling based on play history, preferences, and transition probabilities
- **Automatic Sync**: Fetches and updates song library from Subsonic API with error recovery and authentication
- **Rate Limiting**: Configurable DoS protection using token bucket algorithm with intelligent request throttling
- **Structured Error Handling**: Comprehensive error categorization, context, and logging for better debugging
- **Input Validation**: Thorough validation of all configuration parameters and API inputs
- **Logging**: Structured logging with configurable levels and error context
- **Configuration**: Command-line flags and environment variables with validation and helpful error messages

## Security

This application implements comprehensive security measures to protect credentials, data, and network communications:

### Credential Security ✅

- **No Password Logging**: Passwords are never exposed in server logs, debug output, or error messages
- **Secure URL Encoding**: All credentials are properly encoded using `url.Values{}` to prevent logging vulnerabilities
- **Dynamic Validation**: Credentials are validated against the upstream Subsonic server with timeout protection
- **Thread-Safe Storage**: Valid credentials are stored in memory with mutex protection
- **Automatic Cleanup**: Invalid credentials are automatically removed from storage
- **No Hardcoded Credentials**: All credentials come from authenticated client requests

### Network Security

- **Timeout Protection**: All network requests have configurable timeouts to prevent hanging connections
- **Upstream Validation**: All requests to upstream servers are validated before forwarding
- **Error Context**: Network errors provide context without exposing sensitive information

### Data Security

- **Input Validation**: All configuration parameters and API inputs are thoroughly validated
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

- **Rate Limiting**: ✅ **IMPLEMENTED** - Complete DoS protection with configurable token bucket rate limiting
- **Password Logging Fix**: ✅ **RESOLVED** - Eliminated password exposure in server logs during song synchronization  
- **Secure Authentication**: Enhanced credential validation with proper error handling
- **Network Security**: Improved timeout handling and error context

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

#### Environment variables
- `PORT`: Proxy server port (1-65535)
- `UPSTREAM_URL`: Upstream Subsonic server URL (HTTP/HTTPS)
- `LOG_LEVEL`: Log level (debug, info, warn, error)
- `DB_PATH`: SQLite database file path
- `RATE_LIMIT_RPS`: Rate limit requests per second (default: 100)
- `RATE_LIMIT_BURST`: Rate limit burst size (default: 200)
- `RATE_LIMIT_ENABLED`: Enable rate limiting (default: true)

#### Configuration Validation

The application validates all configuration parameters at startup:
- **Port**: Must be a valid number between 1 and 65535
- **Upstream URL**: Must be a valid HTTP or HTTPS URL with a host
- **Log Level**: Must be one of: debug, info, warn, error (case-insensitive)
- **Database Path**: Parent directories will be created automatically if they don't exist
- **Rate Limit RPS**: Must be at least 1 request per second
- **Rate Limit Burst**: Must be at least 1 and greater than or equal to RPS

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

# Using environment variables
PORT=9090 UPSTREAM_URL=http://my-subsonic-server:4533 LOG_LEVEL=debug DB_PATH=/path/to/music.db RATE_LIMIT_RPS=50 ./subsoxy
```

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

The server automatically creates and manages a SQLite3 database to track song play statistics and build transition probability analysis for song sequences.

### Database Schema

#### songs
- `id` (TEXT PRIMARY KEY): Unique song identifier
- `title` (TEXT): Song title
- `artist` (TEXT): Artist name
- `album` (TEXT): Album name
- `duration` (INTEGER): Song duration in seconds
- `last_played` (DATETIME): Last time the song was played
- `play_count` (INTEGER): Number of times the song was played
- `skip_count` (INTEGER): Number of times the song was skipped

#### play_events
- `id` (INTEGER PRIMARY KEY): Auto-incrementing event ID
- `song_id` (TEXT): Reference to the song
- `event_type` (TEXT): Type of event (start, play, skip)
- `timestamp` (DATETIME): When the event occurred
- `previous_song` (TEXT): ID of the previously played song (for transition tracking)

#### song_transitions
- `from_song_id` (TEXT): ID of the song that was playing before
- `to_song_id` (TEXT): ID of the song that started playing
- `play_count` (INTEGER): Number of times this transition resulted in a play
- `skip_count` (INTEGER): Number of times this transition resulted in a skip
- `probability` (REAL): Calculated probability of playing (vs skipping) this transition

### Features

- **Credential Management**: Automatically captures and validates user credentials from client requests
- **Automatic Song Sync**: Fetches all songs from the Subsonic API every hour using validated credentials
- **Play Tracking**: Records when songs are started, played completely, or skipped
- **Transition Probability Analysis**: Builds transition probabilities between songs
- **Historical Data**: Maintains complete event history for analysis

### Data Collection

The system automatically tracks:
- User credentials from client requests and validates them against the upstream server
- When a song starts playing (`/rest/stream` endpoint)
- When a song is marked as played or skipped (`/rest/scrobble` endpoint)
- Transitions between songs for building recommendation data

## Weighted Shuffle Feature

The `/rest/getRandomSongs` endpoint provides intelligent song shuffling using a weighted algorithm that considers multiple factors to provide better music recommendations.

### How It Works

The shuffle algorithm calculates a weight for each song based on:

1. **Time Decay**: Songs played recently (within 30 days) receive lower weights to encourage variety
2. **Play/Skip Ratio**: Songs with better play-to-skip ratios are more likely to be selected
3. **Transition Probabilities**: Uses transition data to prefer songs that historically follow well from the last played song

### Usage

```bash
# Get 50 weighted-shuffled songs (default)
curl "http://localhost:8080/rest/getRandomSongs?u=admin&p=admin&c=subsoxy&f=json"

# Get 100 weighted-shuffled songs
curl "http://localhost:8080/rest/getRandomSongs?size=100&u=admin&p=admin&c=subsoxy&f=json"
```

### Benefits

- **Reduces repetition**: Recently played songs are less likely to appear
- **Learns preferences**: Songs you tend to play (vs skip) are weighted higher
- **Context-aware**: Considers what song was played previously for smoother transitions
- **Balances discovery**: New and unplayed songs get a boost to encourage exploration

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

## License

MIT License