# Config Module

The config module provides centralized configuration management for the Subsonic proxy server with comprehensive validation and error handling.

## Overview

This module handles:
- Command-line flag parsing with validation
- Environment variable support with fallback
- Comprehensive configuration validation
- Structured error handling with helpful messages
- Default value management
- Automatic directory creation for database paths

## Usage

```go
import "github.com/syeo66/subsoxy/config"

cfg, err := config.New()
if err != nil {
    log.Fatalf("Configuration error: %v", err)
}
// Configuration is now validated and ready to use
```

## Configuration Options

| Flag | Environment Variable | Default | Validation | Description |
|------|---------------------|---------|------------|-------------|
| `-port` | `PORT` | `8080` | 1-65535 | Proxy server port |
| `-upstream` | `UPSTREAM_URL` | `http://localhost:4533` | Valid HTTP/HTTPS URL | Upstream Subsonic server URL |
| `-log-level` | `LOG_LEVEL` | `info` | debug/info/warn/error | Log level (case-insensitive) |
| `-db-path` | `DB_PATH` | `subsoxy.db` | Valid file path | SQLite database file path |
| `-rate-limit-rps` | `RATE_LIMIT_RPS` | `100` | ≥1 | Rate limit requests per second |
| `-rate-limit-burst` | `RATE_LIMIT_BURST` | `200` | ≥1, ≥RPS | Rate limit burst size |
| `-rate-limit-enabled` | `RATE_LIMIT_ENABLED` | `true` | true/false | Enable rate limiting |

## Validation Details

### Port Validation
- Must be a valid integer
- Must be in range 1-65535
- Error example: `[config:INVALID_PORT] port must be a number`

### Upstream URL Validation
- Must be a valid URL
- Must have HTTP or HTTPS scheme
- Must include a host component
- Error example: `[config:INVALID_UPSTREAM_URL] invalid upstream URL format`

### Log Level Validation
- Must be one of: debug, info, warn, error
- Case-insensitive matching
- Error example: `[config:INVALID_LOG_LEVEL] invalid log level`

### Database Path Validation
- Cannot be empty
- Parent directories are created automatically if they don't exist
- Error example: `[config:INVALID_DATABASE_PATH] cannot create database directory`

## Examples

```bash
# Using command-line flags
./subsoxy -port 9090 -upstream http://my-server:4533 -log-level debug

# Using environment variables
PORT=9090 UPSTREAM_URL=http://my-server:4533 LOG_LEVEL=debug ./subsoxy

# Mixed usage (flags override environment variables)
PORT=8080 ./subsoxy -port 9090  # Will use port 9090
```

## Error Handling

The configuration module uses structured error handling from the `errors` package:

```go
// Example error with context
[config:INVALID_PORT] port must be a number
Context: {
  "port": "abc",
  "range": "1-65535"
}
```

### Common Error Scenarios

1. **Invalid Port**: Non-numeric or out-of-range port values
2. **Invalid URL**: Malformed URLs or unsupported schemes
3. **Invalid Log Level**: Unsupported log level strings
4. **Database Path Issues**: Permission problems or invalid paths

### Error Recovery

Configuration errors are fatal and cause the application to exit with a descriptive error message. This ensures the application doesn't start with invalid configuration.

## Implementation Details

- Command-line flags take precedence over environment variables
- The `New()` function calls `flag.Parse()` automatically and validates all parameters
- All configuration is validated at startup before the application continues
- Validation failures return structured errors with helpful context
- Parent directories for database paths are created automatically
- URL schemes are validated to ensure proper proxy configuration

## Testing

The module includes comprehensive tests covering:
- Environment variable handling
- Flag parsing integration
- Validation logic for all parameters
- Error condition handling
- Edge cases and invalid inputs

Run tests with:
```bash
go test ./config/...
```