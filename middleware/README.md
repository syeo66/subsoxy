# Middleware Package

The middleware package provides HTTP middleware components for the Subsonic API proxy server, including security headers and other request/response processing functionality.

## Components

### Security Headers Middleware

The security headers middleware (`security.go`) provides advanced protection against common web vulnerabilities with intelligent development mode detection.

#### Features

- **Automatic Environment Detection**: Distinguishes between development and production environments
- **Comprehensive Security Headers**: X-Content-Type-Options, X-Frame-Options, X-XSS-Protection, CSP, HSTS, Referrer-Policy
- **Development Mode**: Relaxed headers for localhost development
- **Production Mode**: Strict security headers for production deployments
- **IPv6 Support**: Proper localhost detection for IPv6 addresses
- **Configurable**: All headers can be customized or disabled

#### Development vs Production Headers

**Development Mode (Automatic Detection)**:
- Triggers: localhost requests, default port 8080, or explicit flag
- CSP: Relaxed policy allowing inline scripts and styles
- X-Frame-Options: SAMEORIGIN (allows dev tools)
- No HSTS (safer for development)

**Production Mode**:
- Triggers: Non-localhost requests on non-default ports
- CSP: Strict policy restricting script sources
- X-Frame-Options: DENY (maximum protection)
- HSTS: Enabled for HTTPS (max-age=31536000; includeSubDomains)

#### Usage

```go
// Create security headers middleware
securityMiddleware := middleware.NewSecurityHeaders(config, logger)

// Apply to Gorilla Mux router
router.Use(securityMiddleware.Handler)
```

#### Configuration

All security headers can be configured via command-line flags or environment variables:

```bash
# Enable/disable security headers
-security-headers-enabled=true
-security-dev-mode=false

# Customize individual headers
-x-content-type-options="nosniff"
-x-frame-options="DENY"
-x-xss-protection="1; mode=block"
-strict-transport-security="max-age=31536000; includeSubDomains"
-content-security-policy="default-src 'self'; script-src 'self'; object-src 'none';"
-referrer-policy="strict-origin-when-cross-origin"
```

#### Testing

The middleware includes comprehensive tests covering:
- Development vs production mode detection
- IPv6 localhost detection
- Custom header configuration
- Disabled mode
- HTTPS detection for HSTS
- Edge cases and error scenarios

Run tests with:
```bash
go test ./middleware -v
```

#### Security Benefits

- **XSS Protection**: Prevents cross-site scripting attacks
- **Clickjacking Protection**: Prevents UI redressing attacks
- **MIME Sniffing Protection**: Prevents MIME type confusion attacks
- **HTTPS Enforcement**: Ensures secure connections in production
- **Information Leakage Prevention**: Controls referrer information sharing
- **Content Source Control**: Restricts script and resource loading

#### Development Benefits

- **Zero Configuration**: Works securely out of the box
- **Smart Detection**: Automatically adapts to development environments
- **Tool Compatibility**: Relaxed headers don't break development tools
- **Easy Testing**: Simple curl commands to verify functionality
- **Flexible Configuration**: All aspects can be customized as needed