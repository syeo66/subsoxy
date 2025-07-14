package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/config"
)

func TestNewSecurityHeaders(t *testing.T) {
	cfg := &config.Config{
		SecurityHeadersEnabled: true,
	}
	logger := logrus.New()

	middleware := NewSecurityHeaders(cfg, logger)

	if middleware == nil {
		t.Fatal("Expected middleware to be created")
	}

	if middleware.config != cfg {
		t.Error("Expected config to be set")
	}

	if middleware.logger != logger {
		t.Error("Expected logger to be set")
	}
}

func TestSecurityHeadersDisabled(t *testing.T) {
	cfg := &config.Config{
		SecurityHeadersEnabled: false,
	}
	logger := logrus.New()

	middleware := NewSecurityHeaders(cfg, logger)

	// Create a test handler that sets a custom header
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Test-Header", "test-value")
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	wrappedHandler := middleware.Handler(testHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify no security headers were added
	headers := rec.Header()
	if headers.Get("X-Content-Type-Options") != "" {
		t.Error("Expected no X-Content-Type-Options header when security headers disabled")
	}
	if headers.Get("X-Frame-Options") != "" {
		t.Error("Expected no X-Frame-Options header when security headers disabled")
	}
	if headers.Get("X-XSS-Protection") != "" {
		t.Error("Expected no X-XSS-Protection header when security headers disabled")
	}

	// Verify test handler was called
	if headers.Get("Test-Header") != "test-value" {
		t.Error("Expected test handler to be called")
	}
}

func TestSecurityHeadersProductionMode(t *testing.T) {
	cfg := &config.Config{
		SecurityHeadersEnabled:  true,
		SecurityDevMode:         false,
		ProxyPort:               "9090", // Not localhost default
		XContentTypeOptions:     "nosniff",
		XFrameOptions:           "DENY",
		XXSSProtection:          "1; mode=block",
		StrictTransportSecurity: "max-age=31536000; includeSubDomains",
		ContentSecurityPolicy:   "default-src 'self'",
		ReferrerPolicy:          "strict-origin-when-cross-origin",
	}
	logger := logrus.New()

	middleware := NewSecurityHeaders(cfg, logger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware.Handler(testHandler)

	// Test with external host
	req := httptest.NewRequest("GET", "/test", nil)
	req.Host = "example.com:9090"
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	headers := rec.Header()

	// Verify production security headers
	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Expected X-Content-Type-Options: nosniff, got: %s", headers.Get("X-Content-Type-Options"))
	}
	if headers.Get("X-Frame-Options") != "DENY" {
		t.Errorf("Expected X-Frame-Options: DENY, got: %s", headers.Get("X-Frame-Options"))
	}
	if headers.Get("X-XSS-Protection") != "1; mode=block" {
		t.Errorf("Expected X-XSS-Protection: 1; mode=block, got: %s", headers.Get("X-XSS-Protection"))
	}
	if headers.Get("Content-Security-Policy") != "default-src 'self'" {
		t.Errorf("Expected CSP: default-src 'self', got: %s", headers.Get("Content-Security-Policy"))
	}
	if headers.Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Errorf("Expected Referrer-Policy: strict-origin-when-cross-origin, got: %s", headers.Get("Referrer-Policy"))
	}

	// HSTS should not be set for HTTP
	if headers.Get("Strict-Transport-Security") != "" {
		t.Error("Expected no HSTS header for HTTP")
	}
}

func TestSecurityHeadersDevelopmentMode(t *testing.T) {
	tests := []struct {
		name          string
		host          string
		remoteAddr    string
		devModeConfig bool
		expectDevMode bool
	}{
		{
			name:          "Localhost host",
			host:          "localhost:8080",
			remoteAddr:    "127.0.0.1:12345",
			devModeConfig: false,
			expectDevMode: true,
		},
		{
			name:          "127.0.0.1 host",
			host:          "127.0.0.1:8080",
			remoteAddr:    "127.0.0.1:12345",
			devModeConfig: false,
			expectDevMode: true,
		},
		{
			name:          "::1 host",
			host:          "[::1]:8080",
			remoteAddr:    "[::1]:12345",
			devModeConfig: false,
			expectDevMode: true,
		},
		{
			name:          "Config dev mode enabled",
			host:          "example.com:9090",
			remoteAddr:    "192.168.1.1:12345",
			devModeConfig: true,
			expectDevMode: true,
		},
		{
			name:          "Production mode",
			host:          "example.com:9090",
			remoteAddr:    "192.168.1.1:12345",
			devModeConfig: false,
			expectDevMode: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use port 8080 for tests that should detect dev mode via default port,
			// and port 9090 for tests that should not
			testPort := "9090"
			if tt.expectDevMode && !tt.devModeConfig {
				testPort = "8080" // Use default port for localhost/default port detection
			}

			cfg := &config.Config{
				SecurityHeadersEnabled: true,
				SecurityDevMode:        tt.devModeConfig,
				ProxyPort:              testPort,
				XContentTypeOptions:    "nosniff",
				XFrameOptions:          "DENY",
				XXSSProtection:         "1; mode=block",
				ContentSecurityPolicy:  "default-src 'self'",
				ReferrerPolicy:         "strict-origin-when-cross-origin",
			}
			logger := logrus.New()

			middleware := NewSecurityHeaders(cfg, logger)

			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrappedHandler := middleware.Handler(testHandler)

			req := httptest.NewRequest("GET", "/test", nil)
			req.Host = tt.host
			req.RemoteAddr = tt.remoteAddr
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			headers := rec.Header()

			if tt.expectDevMode {
				// Development mode: relaxed CSP and frame options
				if headers.Get("Content-Security-Policy") != DevContentSecurityPolicy {
					t.Errorf("Expected dev CSP, got: %s", headers.Get("Content-Security-Policy"))
				}
				if headers.Get("X-Frame-Options") != DevXFrameOptions {
					t.Errorf("Expected dev X-Frame-Options: %s, got: %s", DevXFrameOptions, headers.Get("X-Frame-Options"))
				}
				// HSTS should not be set in dev mode
				if headers.Get("Strict-Transport-Security") != "" {
					t.Error("Expected no HSTS header in development mode")
				}
			} else {
				// Production mode: strict headers
				if headers.Get("Content-Security-Policy") != "default-src 'self'" {
					t.Errorf("Expected production CSP, got: %s", headers.Get("Content-Security-Policy"))
				}
				if headers.Get("X-Frame-Options") != "DENY" {
					t.Errorf("Expected production X-Frame-Options: DENY, got: %s", headers.Get("X-Frame-Options"))
				}
			}

			// These should be the same in both modes
			if headers.Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("Expected X-Content-Type-Options: nosniff, got: %s", headers.Get("X-Content-Type-Options"))
			}
			if headers.Get("X-XSS-Protection") != "1; mode=block" {
				t.Errorf("Expected X-XSS-Protection: 1; mode=block, got: %s", headers.Get("X-XSS-Protection"))
			}
		})
	}
}

func TestSecurityHeadersHTTPS(t *testing.T) {
	// Test HSTS header is set for HTTPS
	cfg := &config.Config{
		SecurityHeadersEnabled:  true,
		SecurityDevMode:         false,
		ProxyPort:               "443", // HTTPS port
		UpstreamURL:             "https://example.com",
		StrictTransportSecurity: "max-age=31536000; includeSubDomains",
		XContentTypeOptions:     "nosniff",
	}
	logger := logrus.New()

	middleware := NewSecurityHeaders(cfg, logger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware.Handler(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Host = "example.com:443"
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	headers := rec.Header()

	// HSTS should be set for HTTPS
	if headers.Get("Strict-Transport-Security") != "max-age=31536000; includeSubDomains" {
		t.Errorf("Expected HSTS header for HTTPS, got: %s", headers.Get("Strict-Transport-Security"))
	}
}

func TestSecurityHeadersEmptyValues(t *testing.T) {
	// Test that empty config values don't set headers
	cfg := &config.Config{
		SecurityHeadersEnabled: true,
		SecurityDevMode:        false,
		ProxyPort:              "9090",
		// All header values empty
		XContentTypeOptions:     "",
		XFrameOptions:           "",
		XXSSProtection:          "",
		StrictTransportSecurity: "",
		ContentSecurityPolicy:   "",
		ReferrerPolicy:          "",
	}
	logger := logrus.New()

	middleware := NewSecurityHeaders(cfg, logger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware.Handler(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Host = "example.com:9090"
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	headers := rec.Header()

	// No headers should be set when config values are empty
	if headers.Get("X-Content-Type-Options") != "" {
		t.Error("Expected no X-Content-Type-Options header when config is empty")
	}
	if headers.Get("X-Frame-Options") != "" {
		t.Error("Expected no X-Frame-Options header when config is empty")
	}
	if headers.Get("X-XSS-Protection") != "" {
		t.Error("Expected no X-XSS-Protection header when config is empty")
	}
	if headers.Get("Strict-Transport-Security") != "" {
		t.Error("Expected no HSTS header when config is empty")
	}
	if headers.Get("Content-Security-Policy") != "" {
		t.Error("Expected no CSP header when config is empty")
	}
	if headers.Get("Referrer-Policy") != "" {
		t.Error("Expected no Referrer-Policy header when config is empty")
	}
}

func TestIsDevModeRequest(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		remoteAddr string
		configDev  bool
		expected   bool
	}{
		{
			name:       "localhost host",
			host:       "localhost:8080",
			remoteAddr: "127.0.0.1:12345",
			configDev:  false,
			expected:   true,
		},
		{
			name:       "127.0.0.1 host",
			host:       "127.0.0.1:8080",
			remoteAddr: "127.0.0.1:12345",
			configDev:  false,
			expected:   true,
		},
		{
			name:       "::1 IPv6 localhost",
			host:       "[::1]:8080",
			remoteAddr: "[::1]:12345",
			configDev:  false,
			expected:   true,
		},
		{
			name:       "0.0.0.0 host",
			host:       "0.0.0.0:8080",
			remoteAddr: "192.168.1.1:12345",
			configDev:  false,
			expected:   true,
		},
		{
			name:       "config dev mode enabled",
			host:       "example.com:9090",
			remoteAddr: "192.168.1.1:12345",
			configDev:  true,
			expected:   true,
		},
		{
			name:       "production host and config",
			host:       "example.com:9090",
			remoteAddr: "192.168.1.1:12345",
			configDev:  false,
			expected:   false,
		},
		{
			name:       "remote addr localhost",
			host:       "example.com:9090",
			remoteAddr: "127.0.0.1:12345",
			configDev:  false,
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use port 8080 for tests that should detect dev mode via localhost,
			// and port 9090 for tests that should not
			testPort := "9090"
			if tt.expected && !tt.configDev {
				testPort = "8080" // Use default port for localhost detection
			}

			cfg := &config.Config{
				SecurityDevMode: tt.configDev,
				ProxyPort:       testPort,
			}
			logger := logrus.New()

			middleware := NewSecurityHeaders(cfg, logger)

			req := httptest.NewRequest("GET", "/test", nil)
			req.Host = tt.host
			req.RemoteAddr = tt.remoteAddr

			result := middleware.isDevModeRequest(req)
			if result != tt.expected {
				t.Errorf("Expected isDevModeRequest to return %v, got %v", tt.expected, result)
			}
		})
	}
}
