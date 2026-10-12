package middleware

import (
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/config"
)

const (
	// Development mode CSP - more permissive for local development
	DevContentSecurityPolicy = "default-src 'self' 'unsafe-inline' 'unsafe-eval'; connect-src 'self' ws: wss:; img-src 'self' data: blob:;"
	// Development mode frame options - allow same origin for development tools
	DevXFrameOptions = "SAMEORIGIN"
)

// SecurityHeaders middleware adds security headers to HTTP responses
type SecurityHeaders struct {
	config *config.Config
	logger *logrus.Logger
}

// NewSecurityHeaders creates a new security headers middleware
func NewSecurityHeaders(cfg *config.Config, logger *logrus.Logger) *SecurityHeaders {
	return &SecurityHeaders{
		config: cfg,
		logger: logger,
	}
}

// Handler returns the middleware handler function
func (s *SecurityHeaders) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip security headers if disabled
		if !s.config.SecurityHeadersEnabled {
			next.ServeHTTP(w, r)
			return
		}

		if s.config.IsDevMode() {
			s.logger.Debug("Applying development mode security headers")
			s.addDevelopmentHeaders(w)
		} else {
			s.logger.Debug("Applying production security headers")
			s.addProductionHeaders(w, r)
		}

		next.ServeHTTP(w, r)
	})
}

// addDevelopmentHeaders adds relaxed security headers for development
func (s *SecurityHeaders) addDevelopmentHeaders(w http.ResponseWriter) {
	header := w.Header()

	// X-Content-Type-Options: Always set to nosniff
	if s.config.XContentTypeOptions != "" {
		header.Set("X-Content-Type-Options", s.config.XContentTypeOptions)
	}

	// X-Frame-Options: More permissive for development
	if s.config.XFrameOptions != "" {
		header.Set("X-Frame-Options", DevXFrameOptions)
	}

	// X-XSS-Protection: Keep as configured
	if s.config.XXSSProtection != "" {
		header.Set("X-XSS-Protection", s.config.XXSSProtection)
	}

	// Content-Security-Policy: More permissive for development
	if s.config.ContentSecurityPolicy != "" {
		header.Set("Content-Security-Policy", DevContentSecurityPolicy)
	}

	// Referrer-Policy: Keep as configured
	if s.config.ReferrerPolicy != "" {
		header.Set("Referrer-Policy", s.config.ReferrerPolicy)
	}

	// Skip HSTS in development (only applies to HTTPS anyway)
	// No Strict-Transport-Security header in dev mode
}

// addProductionHeaders adds full security headers for production
func (s *SecurityHeaders) addProductionHeaders(w http.ResponseWriter, r *http.Request) {
	header := w.Header()

	// X-Content-Type-Options
	if s.config.XContentTypeOptions != "" {
		header.Set("X-Content-Type-Options", s.config.XContentTypeOptions)
	}

	// X-Frame-Options
	if s.config.XFrameOptions != "" {
		header.Set("X-Frame-Options", s.config.XFrameOptions)
	}

	// X-XSS-Protection
	if s.config.XXSSProtection != "" {
		header.Set("X-XSS-Protection", s.config.XXSSProtection)
	}

	// Strict-Transport-Security (only for HTTPS)
	if s.config.StrictTransportSecurity != "" && isHTTPS(r) {
		header.Set("Strict-Transport-Security", s.config.StrictTransportSecurity)
	}

	// Content-Security-Policy
	if s.config.ContentSecurityPolicy != "" {
		header.Set("Content-Security-Policy", s.config.ContentSecurityPolicy)
	}

	// Referrer-Policy
	if s.config.ReferrerPolicy != "" {
		header.Set("Referrer-Policy", s.config.ReferrerPolicy)
	}
}

// isHTTPS reports whether the client reached us over HTTPS, either directly or through a
// TLS-terminating reverse proxy. A spoofed X-Forwarded-Proto is harmless here, since browsers
// ignore HSTS received over plain HTTP.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
