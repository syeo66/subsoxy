# Configuration

This document provides comprehensive information about configuring the Subsonic proxy server.

## Configuration Overview

Configuration can be set via command-line flags or environment variables. Command-line flags take precedence over environment variables. All configuration parameters are validated at startup with helpful error messages.

## Command-line Flags

### Server Configuration
- `-port string`: Proxy server port, must be 1-65535 (default: 8080)
- `-upstream string`: Upstream Subsonic server URL, must be valid HTTP/HTTPS URL (default: http://localhost:4533)
- `-log-level string`: Log level - debug, info, warn, error (default: info)
- `-debug-mode`: Enable interactive debug endpoint with HTML UI for visualizing song weights and clickable transition analysis (default: false)

### Database Configuration
- `-db-path string`: SQLite database file path, directories will be created if needed (default: subsoxy.db)
- `-db-max-open-conns int`: Maximum number of open database connections (default: 25)
- `-db-max-idle-conns int`: Maximum number of idle database connections (default: 5)
- `-db-conn-max-lifetime duration`: Maximum connection lifetime (default: 30m)
- `-db-conn-max-idle-time duration`: Maximum connection idle time (default: 5m)
- `-db-health-check`: Enable database health checks (default: true)

### Performance Configuration
- `-credential-workers int`: Maximum concurrent credential validation workers (default: 100)

### Rate Limiting Configuration
- `-rate-limit-rps int`: Rate limit requests per second (default: 100)
- `-rate-limit-burst int`: Rate limit burst size (default: 200)
- `-rate-limit-enabled`: Enable rate limiting (default: true)

### CORS Configuration
- `-cors-enabled`: Enable CORS headers (default: true)
- `-cors-allow-origins string`: CORS allowed origins, comma-separated (default: "*")
- `-cors-allow-methods string`: CORS allowed methods, comma-separated (default: "GET,POST,PUT,DELETE,OPTIONS")
- `-cors-allow-headers string`: CORS allowed headers, comma-separated (default: "Content-Type,Authorization,X-Requested-With")
- `-cors-allow-credentials`: CORS allow credentials (default: false)

### Security Headers Configuration
- `-security-headers-enabled`: Enable security headers (default: true)
- `-security-dev-mode`: Enable development mode (relaxed security headers for localhost) (default: false)
- `-x-content-type-options string`: X-Content-Type-Options header value (default: nosniff)
- `-x-frame-options string`: X-Frame-Options header value (default: DENY)
- `-x-xss-protection string`: X-XSS-Protection header value (default: 1; mode=block)
- `-strict-transport-security string`: Strict-Transport-Security header value (default: max-age=31536000; includeSubDomains)
- `-content-security-policy string`: Content-Security-Policy header value (default: default-src 'self'; script-src 'self'; object-src 'none';)
- `-referrer-policy string`: Referrer-Policy header value (default: strict-origin-when-cross-origin)

## Environment Variables

### Server Configuration
- `PORT`: Proxy server port (1-65535)
- `UPSTREAM_URL`: Upstream Subsonic server URL (HTTP/HTTPS)
- `LOG_LEVEL`: Log level (debug, info, warn, error)
- `DEBUG`: Enable interactive debug endpoint with HTML UI for visualizing song weights and clickable transition analysis (true/false, default: false)

### Database Configuration
- `DB_PATH`: SQLite database file path
- `DB_MAX_OPEN_CONNS`: Maximum number of open database connections (default: 25)
- `DB_MAX_IDLE_CONNS`: Maximum number of idle database connections (default: 5)
- `DB_CONN_MAX_LIFETIME`: Maximum connection lifetime (default: 30m)
- `DB_CONN_MAX_IDLE_TIME`: Maximum connection idle time (default: 5m)
- `DB_HEALTH_CHECK`: Enable database health checks (default: true)

### Performance Configuration
- `CREDENTIAL_WORKERS`: Maximum concurrent credential validation workers (default: 100)

### Rate Limiting Configuration
- `RATE_LIMIT_RPS`: Rate limit requests per second (default: 100)
- `RATE_LIMIT_BURST`: Rate limit burst size (default: 200)
- `RATE_LIMIT_ENABLED`: Enable rate limiting (default: true)

### CORS Configuration
- `CORS_ENABLED`: Enable CORS headers (default: true)
- `CORS_ALLOW_ORIGINS`: CORS allowed origins, comma-separated (default: "*")
- `CORS_ALLOW_METHODS`: CORS allowed methods, comma-separated (default: "GET,POST,PUT,DELETE,OPTIONS")
- `CORS_ALLOW_HEADERS`: CORS allowed headers, comma-separated (default: "Content-Type,Authorization,X-Requested-With")
- `CORS_ALLOW_CREDENTIALS`: CORS allow credentials (default: false)

### Security Headers Configuration
- `SECURITY_HEADERS_ENABLED`: Enable security headers (default: true)
- `SECURITY_DEV_MODE`: Enable development mode (default: false)
- `X_CONTENT_TYPE_OPTIONS`: X-Content-Type-Options header value (default: nosniff)
- `X_FRAME_OPTIONS`: X-Frame-Options header value (default: DENY)
- `X_XSS_PROTECTION`: X-XSS-Protection header value (default: 1; mode=block)
- `STRICT_TRANSPORT_SECURITY`: Strict-Transport-Security header value (default: max-age=31536000; includeSubDomains)
- `CONTENT_SECURITY_POLICY`: Content-Security-Policy header value (default: default-src 'self'; script-src 'self'; object-src 'none';)
- `REFERRER_POLICY`: Referrer-Policy header value (default: strict-origin-when-cross-origin)

## Configuration Validation

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
- **Credential Workers**: Must be at least 1 worker
- **CORS Origins**: Cannot be empty when CORS is enabled
- **CORS Methods**: Must be valid HTTP methods (GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH)
- **CORS Headers**: Can be empty (optional)

If any configuration is invalid, the application will exit with a detailed error message explaining what needs to be fixed.

## Configuration Examples

### Basic Usage
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

# Enable debug endpoint with HTML UI
./subsoxy -debug-mode
# OR
DEBUG=1 ./subsoxy
```

### Rate Limiting Examples
```bash
# Moderate rate limiting
./subsoxy -rate-limit-rps 50 -rate-limit-burst 100

# Strict rate limiting  
./subsoxy -rate-limit-rps 10 -rate-limit-burst 20

# Disable rate limiting
./subsoxy -rate-limit-enabled=false
```

### Database Connection Pool Examples
```bash
# High-performance setup for heavy load
./subsoxy -db-max-open-conns 50 -db-max-idle-conns 10 -db-conn-max-lifetime 1h

# Conservative setup for low resource usage
./subsoxy -db-max-open-conns 10 -db-max-idle-conns 2 -db-conn-max-lifetime 15m

# Disable health checks
./subsoxy -db-health-check=false
```

### Performance Tuning Examples
```bash
# High-load setup with increased worker pool
./subsoxy -credential-workers 200 -rate-limit-rps 200

# Conservative setup for low resource environments
./subsoxy -credential-workers 25 -rate-limit-rps 50

# Default balanced setup (recommended)
./subsoxy  # Uses 100 credential workers by default
```

### CORS Configuration Examples
```bash
# Specific origins
./subsoxy -cors-allow-origins "http://localhost:3000,https://myapp.com"

# Enable credentials
./subsoxy -cors-allow-credentials=true

# Disable CORS entirely
./subsoxy -cors-enabled=false

# Restrict methods
./subsoxy -cors-allow-methods "GET,POST,OPTIONS"
```

### Security Headers Examples
```bash
# Default configuration (recommended)
./subsoxy  # Security headers enabled with smart detection

# Force development mode
./subsoxy -security-dev-mode=true

# Custom security headers
./subsoxy -x-frame-options="SAMEORIGIN" -content-security-policy="default-src 'self' data:"

# Disable security headers (API-only usage)
./subsoxy -security-headers-enabled=false
```

### Environment Variable Configuration
```bash
# Using environment variables
PORT=9090 UPSTREAM_URL=http://my-subsonic-server:4533 LOG_LEVEL=debug \
DB_PATH=/path/to/music.db RATE_LIMIT_RPS=50 DB_MAX_OPEN_CONNS=30 \
CORS_ALLOW_ORIGINS="http://localhost:3000" ./subsoxy

# Environment variable configuration for CORS
export CORS_ENABLED=true
export CORS_ALLOW_ORIGINS="http://localhost:3000,https://app.example.com"
export CORS_ALLOW_CREDENTIALS=true
./subsoxy

# Environment variable configuration for security headers
export SECURITY_HEADERS_ENABLED=true
export SECURITY_DEV_MODE=false
export X_FRAME_OPTIONS="DENY"
./subsoxy

# Enable debug endpoint with environment variable
export DEBUG=1
./subsoxy
```

## Production Recommendations

### Performance & Resource Management
- **Credential Workers**: Controls goroutine pool size for credential validation
  - **High-traffic servers**: 200-500 workers (handles many simultaneous users)
  - **Standard servers**: 100 workers (default, handles most scenarios)
  - **Low-resource environments**: 25-50 workers (limited memory/CPU)
  - **Impact**: Prevents resource exhaustion under high load while maintaining responsiveness

### Rate Limiting
- **Web servers**: 50-100 RPS with 100-200 burst
- **API servers**: 20-50 RPS with 50-100 burst
- **Public instances**: 5-20 RPS with 10-40 burst
- **Development**: Disable rate limiting for easier testing

### CORS
- **Development**: Use wildcard "*" for origins during development
- **Production**: Specify exact origins for security: `-cors-allow-origins "https://myapp.com"`
- **Credentials**: Only enable when necessary and with specific origins
- **Methods**: Restrict to only needed HTTP methods for tighter security

### Security Headers
- **Default Settings**: Security headers are enabled by default with secure settings
- **Development**: Automatic detection provides relaxed headers for localhost
- **Production**: Strict headers are automatically applied for external requests
- **Custom CSP**: Customize Content Security Policy based on your application needs
- **HTTPS**: Use HTTPS in production to enable HSTS protection