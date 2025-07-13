# Security

This document details the comprehensive security measures implemented to protect credentials, data, and network communications.

## Credential Security ✅ **ENHANCED WITH TOKEN SUPPORT**

### Authentication Modes ✅ **NEW**
- **Password-based Authentication**: Traditional `u` + `p` parameter authentication
- **Token-based Authentication**: Modern `u` + `t` + `s` parameter authentication (recommended)
- **Multi-Format Support**: Supports URL parameters, POST form data, and Authorization headers
- **Client Compatibility**: Full support for modern Subsonic clients (Symfonium, DSub, etc.)

### Security Features
- **AES-256-GCM Encryption**: All credentials (passwords/tokens) encrypted in memory using industry-standard authenticated encryption
- **Memory Protection**: Credentials never stored in plain text, protecting against memory dumps and process inspection
- **Unique Instance Keys**: Each server instance generates random 32-byte encryption keys for isolation
- **Secure Memory Management**: Encrypted data securely zeroed before deallocation
- **Forward Security**: New encryption keys generated on each server restart
- **No Credential Logging**: Passwords and tokens never exposed in server logs, debug output, or error messages
- **Secure URL Encoding**: All credentials properly encoded using `url.Values{}` to prevent logging vulnerabilities
- **Dynamic Validation**: Credentials validated against upstream Subsonic server with timeout protection
- **Token Validation**: Real-time validation of token-based authentication against upstream server
- **Thread-Safe Storage**: Valid encrypted credentials stored in memory with mutex protection
- **Automatic Cleanup**: Invalid credentials automatically and securely removed from storage
- **No Hardcoded Credentials**: All credentials come from authenticated client requests
- **Client Compatibility**: Works seamlessly with modern Subsonic clients using token authentication

### Operational Features
- **Dynamic Capture**: Auto-captures credentials from client requests (both auth modes)
- **Upstream Validation**: Validates against Subsonic server via `/rest/ping` endpoint
- **Thread-Safe Storage**: Mutex-protected encrypted credential storage
- **Background Operations**: Uses encrypted credentials for automated tasks
- **Multi-Client Support**: Handles different authentication methods from various clients

## Network Security

- **Timeout Protection**: All network requests have configurable timeouts to prevent hanging connections
- **Upstream Validation**: All requests to upstream servers are validated before forwarding
- **Error Context**: Network errors provide context without exposing sensitive information

## Input Validation & Security ✅

### Security Features
- **Log Injection Prevention**: All user inputs sanitized to remove control characters before logging
- **Input Length Limits**: Maximum lengths enforced for song IDs (255), usernames (100), and general inputs (1000)
- **Song ID Validation**: Format and length validation for all song identifiers
- **Control Character Filtering**: Removes newlines, carriage returns, tabs, and escape sequences
- **DoS Protection**: Input truncation prevents memory exhaustion attacks
- **Parameter Validation**: All API parameters validated with structured error responses
- **Database Protection**: SQLite database operations use prepared statements to prevent injection
- **Structured Errors**: Error handling provides context while protecting sensitive information
- **Graceful Degradation**: System continues operating even when individual components fail

### Implementation
- **handlers/**: Song ID validation and sanitization before processing
- **server/**: Username length validation and endpoint sanitization
- **HTTP 400 responses**: For invalid inputs with structured error messages

### Benefits
- Log injection prevention
- DoS attack protection
- Data integrity validation
- Clean, parseable audit logs

## Rate Limiting ✅

### DoS Protection
- **DoS Protection**: Comprehensive rate limiting using token bucket algorithm to prevent abuse
- **Configurable Limits**: Adjustable requests per second (RPS) and burst size for different environments
- **Early Filtering**: Rate limiting applied before request processing to maximize security
- **HTTP 429 Responses**: Clean error responses for rate-limited requests with proper logging
- **Hook Protection**: All endpoints including built-in hooks are protected from rapid requests
- **Flexible Configuration**: Can be disabled for development or tuned for production environments

### Configuration
- **RPS**: Maximum requests per second (default: 100)
- **Burst Size**: Maximum burst requests (default: 200)
- **Enabled**: Toggle rate limiting (default: true)

### Behavior
- Applied before hook processing
- Returns HTTP 429 when limits exceeded
- Logs violations with client IP and endpoint

## Security Headers Middleware ✅ **NEW**

Advanced security headers middleware with intelligent development mode detection to protect against common web vulnerabilities.

### Security Features
- **Production Security Headers**: Full protection with strict Content Security Policy, frame options, and HSTS
- **Development Mode**: Relaxed headers for localhost development with automatic detection
- **IPv6 Support**: Proper localhost detection for IPv6 addresses (`::1`, `[::1]:port`)
- **Configurable Headers**: All security headers can be customized or disabled
- **HTTPS Detection**: HSTS only applied when running on HTTPS

### Supported Security Headers
- **X-Content-Type-Options**: Prevents MIME type sniffing (always `nosniff`)
- **X-Frame-Options**: Protects against clickjacking (`DENY` in production, `SAMEORIGIN` in dev)
- **X-XSS-Protection**: XSS filtering (`1; mode=block`)
- **Strict-Transport-Security**: HTTPS enforcement (HTTPS only)
- **Content-Security-Policy**: Script and resource restrictions
- **Referrer-Policy**: Controls referrer information leakage

### Development vs Production Headers

**Development Mode (Relaxed)**:
- CSP: `default-src 'self' 'unsafe-inline' 'unsafe-eval'; connect-src 'self' ws: wss:; img-src 'self' data: blob:;`
- X-Frame-Options: `SAMEORIGIN`
- No HSTS header (development safety)

**Production Mode (Strict)**:
- CSP: `default-src 'self'; script-src 'self'; object-src 'none';`
- X-Frame-Options: `DENY`
- HSTS: `max-age=31536000; includeSubDomains` (HTTPS only)

## Security Best Practices

- **Minimal Exposure**: Only necessary information is logged or exposed in error messages
- **Secure Defaults**: All security-sensitive configurations use secure default values
- **Comprehensive Testing**: Security features are thoroughly tested with unit tests
- **Regular Updates**: Security implementations follow Go best practices and are regularly reviewed

## Recent Security Improvements

- **Background Sync Authentication Fix**: ✅ **CRITICAL FIX** - Fixed token authentication in background song sync for modern Subsonic clients
- **Input Validation & Sanitization**: ✅ **IMPLEMENTED** - Comprehensive protection against log injection, control character attacks, and DoS attempts
- **Rate Limiting**: ✅ **IMPLEMENTED** - Complete DoS protection with configurable token bucket rate limiting
- **Password Logging Fix**: ✅ **RESOLVED** - Eliminated password exposure in server logs during song synchronization  
- **Secure Authentication**: Enhanced credential validation with proper error handling
- **Network Security**: Improved timeout handling and error context