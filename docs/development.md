# Development Guide

This document provides information for developers working on the Subsonic proxy server.

## Project Structure

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
├── credentials/         # Multi-mode authentication (password/token) with timeout protection
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
├── docs/                # Documentation
│   ├── architecture.md  # System architecture
│   ├── configuration.md # Configuration guide
│   ├── database.md      # Database features
│   ├── multi-tenancy.md # Multi-tenancy details
│   ├── security.md      # Security features
│   ├── weighted-shuffle.md # Shuffle algorithm
│   └── development.md   # This file
├── go.mod               # Go module definition
├── go.sum               # Go module checksums
├── CLAUDE.md            # Development guidance
└── README.md            # Main documentation
```

## Building and Testing

### Dependencies
```bash
# Install dependencies
go mod tidy
```

### Testing
```bash
# Run all tests - all tests pass with comprehensive coverage (78.4% overall)
go test ./...

# Run tests with race detection (recommended)
go test ./... -race

# Run specific test categories
go test ./database -run="ErrorHandling"  # Database error scenarios
go test ./handlers -run="BoundaryConditions"  # Input validation tests
go test ./credentials -run="Network"  # Network failure scenarios

# Run benchmarks
go test ./shuffle/... -bench=BenchmarkShuffle -benchtime=3s
go test ./shuffle/... -run=TestMemoryUsage -v
```

### Building
```bash
# Build the application
go build -o subsoxy

# Clean up build artifacts
rm subsoxy
```

## Testing Strategies

### Test Categories
- **Error Handling**: Complete database operation error scenarios with validation testing
- **Boundary Conditions**: Input validation limits, edge cases, parameter validation
- **Security Testing**: SQL injection prevention, malicious input patterns
- **Network Scenarios**: Timeouts, failures, slow responses, connection testing
- **Concurrent Access**: Thread safety and race condition prevention verification
- **Performance Testing**: Large datasets, memory efficiency, concurrent operations

### Multi-User Testing
```bash
# Test credentials and sync functionality
go test ./credentials -v -run="TestGetAllValid"
go test ./server -v -run="TestFetchAndStoreSongsMultiUser|TestSyncSongsForUserError|TestGetSortedUsernames"

# Test immediate sync with fresh credentials (clears database first)
rm -f subsoxy.db
./subsoxy -upstream https://your-server.com -port 8081 &
sleep 2
curl -s "http://localhost:8081/rest/ping?u=testuser1&p=testpass1&v=1.15.0&c=subsoxy&f=json" | jq .
# Check sync was triggered immediately
sleep 10 && sqlite3 subsoxy.db "SELECT COUNT(*) FROM songs WHERE user_id = 'testuser1';"

# Test multi-user endpoints with password authentication
curl -s "http://localhost:8081/rest/ping?u=testuser1&p=testpass1&v=1.15.0&c=subsoxy&f=json" | jq .
curl -s "http://localhost:8081/rest/getRandomSongs?u=testuser1&p=testpass1&v=1.15.0&c=subsoxy&size=5&f=json" | jq .
curl -s "http://localhost:8081/rest/scrobble?u=testuser1&p=testpass1&v=1.15.0&c=subsoxy&id=song123&submission=true"

# Test with token authentication (requires generating valid token/salt)
curl -s "http://localhost:8081/rest/ping?u=testuser1&t=generatedtoken&s=randomsalt&v=1.15.0&c=subsoxy&f=json" | jq .
curl -s "http://localhost:8081/rest/getRandomSongs?u=testuser1&t=generatedtoken&s=randomsalt&v=1.15.0&c=subsoxy&size=5&f=json" | jq .
```

### Performance Testing
```bash
# Test performance with curl
curl -s "http://localhost:8080/rest/getRandomSongs?u=user&p=pass&size=50&f=json" | jq '.["subsonic-response"].songs.song | length'
time curl -s "http://localhost:8080/rest/getRandomSongs?u=user&p=pass&size=1000&f=json" > /dev/null
```

### CORS Testing
```bash
# Test CORS headers
curl -H "Origin: http://localhost:3000" -i http://localhost:8080/rest/ping
curl -X OPTIONS -H "Origin: http://localhost:3000" -H "Access-Control-Request-Method: GET" -i http://localhost:8080/rest/ping
```

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

## Error Handling

The application implements comprehensive structured error handling with Go 1.13+ compatibility:

### Error Categories

Errors are categorized for better debugging and monitoring:

- **`config`**: Configuration validation errors (invalid ports, URLs, etc.)
- **`database`**: Database connection, query, and transaction errors
- **`credentials`**: Authentication and credential validation errors
- **`server`**: Server startup, shutdown, and proxy errors
- **`network`**: Upstream server connectivity and timeout errors
- **`validation`**: Input validation and parameter errors

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

## Code Style and Conventions

### Go Best Practices
- Follow standard Go formatting with `go fmt`
- Use descriptive variable and function names
- Include comprehensive error handling
- Write unit tests for all new functionality
- Use structured logging with context

### Security Best Practices
- Never log passwords or sensitive data
- Use `url.Values{}` for URL parameter encoding
- Follow established error handling patterns
- Test security fixes thoroughly
- Keep credentials in structured, protected storage

### Testing Best Practices
- Write tests for both happy path and error scenarios
- Include boundary condition tests
- Test concurrent access patterns
- Use table-driven tests where appropriate
- Mock external dependencies

## Contributing

1. Ensure all tests pass: `go test ./... -race`
2. Follow Go coding standards and project conventions
3. Add tests for new functionality
4. Update documentation as needed
5. Run security and performance tests for critical changes

## Maintenance Reminders

- Clean up build files and binaries after building
- Regularly review and update dependencies
- Monitor test coverage and add tests for uncovered code
- Review security implementations periodically
- Update documentation when adding new features