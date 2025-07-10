package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/syeo66/subsoxy/config"
	"github.com/syeo66/subsoxy/models"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "info",
		DatabasePath:      "test.db",
		RateLimitRPS:      100,
		RateLimitBurst:    200,
		RateLimitEnabled:  true,
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	if server == nil {
		t.Error("Server should not be nil")
	}
	if server.config != cfg {
		t.Error("Config should be set correctly")
	}
	if server.logger == nil {
		t.Error("Logger should not be nil")
	}
	if server.proxy == nil {
		t.Error("Proxy should not be nil")
	}
	if server.hooks == nil {
		t.Error("Hooks map should not be nil")
	}
	if server.db == nil {
		t.Error("Database should not be nil")
	}
	if server.credentials == nil {
		t.Error("Credentials manager should not be nil")
	}
	if server.handlers == nil {
		t.Error("Handlers should not be nil")
	}
	if server.shuffle == nil {
		t.Error("Shuffle service should not be nil")
	}
	if server.rateLimiter == nil {
		t.Error("Rate limiter should not be nil when enabled")
	}
}

func TestNewWithInvalidUpstreamURL(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       "://invalid-url",  // This will definitely be invalid
		LogLevel:          "info",
		DatabasePath:      "test.db",
		RateLimitRPS:      100,
		RateLimitBurst:    200,
		RateLimitEnabled:  true,
	}
	defer os.Remove("test.db")
	
	_, err := New(cfg)
	if err == nil {
		t.Error("Expected error with invalid upstream URL")
	}
	if !strings.Contains(err.Error(), "invalid upstream URL") {
		t.Errorf("Expected 'invalid upstream URL' error, got: %v", err)
	}
}

func TestNewWithInvalidLogLevel(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "invalid-level",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Should not fail with invalid log level: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	// Should default to info level when invalid level is provided
	if server.logger == nil {
		t.Error("Logger should still be initialized")
	}
}

func TestNewWithInvalidDatabase(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "info",
		DatabasePath: "/nonexistent/path/test.db",
	}
	
	_, err := New(cfg)
	if err == nil {
		t.Error("Expected error with invalid database path")
	}
	if !strings.Contains(err.Error(), "failed to initialize database") {
		t.Errorf("Expected 'failed to initialize database' error, got: %v", err)
	}
}

func TestAddHook(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "info",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	// Test adding a hook
	var hookCalled bool
	testHook := func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		hookCalled = true
		return true
	}
	
	server.AddHook("/test/endpoint", testHook)
	
	if len(server.hooks["/test/endpoint"]) != 1 {
		t.Errorf("Expected 1 hook for endpoint, got %d", len(server.hooks["/test/endpoint"]))
	}
	
	// Test adding multiple hooks to same endpoint
	var secondHookCalled bool
	secondHook := func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		secondHookCalled = true
		return false
	}
	
	server.AddHook("/test/endpoint", secondHook)
	
	if len(server.hooks["/test/endpoint"]) != 2 {
		t.Errorf("Expected 2 hooks for endpoint, got %d", len(server.hooks["/test/endpoint"]))
	}
	
	// Use the variables to avoid unused variable warnings
	_ = hookCalled
	_ = secondHookCalled
}

func TestProxyHandler(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn", // Reduce log noise
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	t.Run("Request without hooks", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/some/path", nil)
		w := httptest.NewRecorder()
		
		// This will fail to connect to upstream, but that's expected in tests
		server.proxyHandler(w, req)
		
		// Should attempt to proxy (and fail with connection error)
		// We can't easily test successful proxying without a real upstream server
	})
	
	t.Run("Request with hook that handles", func(t *testing.T) {
		var hookCalled bool
		testHook := func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
			hookCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("handled by hook"))
			return true
		}
		
		server.AddHook("/test/endpoint", testHook)
		
		req := httptest.NewRequest("GET", "/test/endpoint", nil)
		w := httptest.NewRecorder()
		
		server.proxyHandler(w, req)
		
		if !hookCalled {
			t.Error("Hook should have been called")
		}
		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "handled by hook" {
			t.Errorf("Expected 'handled by hook', got '%s'", w.Body.String())
		}
	})
	
	t.Run("Request with hook that doesn't handle", func(t *testing.T) {
		var hookCalled bool
		testHook := func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
			hookCalled = true
			return false // Don't handle, continue to proxy
		}
		
		server.AddHook("/test/passthrough", testHook)
		
		req := httptest.NewRequest("GET", "/test/passthrough", nil)
		w := httptest.NewRecorder()
		
		server.proxyHandler(w, req)
		
		if !hookCalled {
			t.Error("Hook should have been called")
		}
		// Should continue to proxy (and fail with connection error)
	})
	
	t.Run("REST request with credentials", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/ping?u=testuser&p=testpass", nil)
		w := httptest.NewRecorder()
		
		server.proxyHandler(w, req)
		
		// Should extract and validate credentials (in background goroutine)
		// We can't easily test the credential validation without mocking
	})
	
	t.Run("REST request without credentials", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/ping", nil)
		w := httptest.NewRecorder()
		
		server.proxyHandler(w, req)
		
		// Should not attempt credential validation
	})
	
	t.Run("REST request with empty credentials", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/ping?u=&p=", nil)
		w := httptest.NewRecorder()
		
		server.proxyHandler(w, req)
		
		// Should not attempt credential validation with empty values
	})
}

func TestStartAndShutdown(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "0", // Use random port
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	// Test start
	err = server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	
	// Give server time to start
	time.Sleep(100 * time.Millisecond)
	
	// Test shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err = server.Shutdown(ctx)
	if err != nil {
		t.Errorf("Failed to shutdown server: %v", err)
	}
}

func TestShutdownWithoutStart(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	// Test shutdown without start (should not panic)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	
	err = server.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown should succeed even without start: %v", err)
	}
}

func TestRecordPlayEvent(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	// Store test songs first
	testSongs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
	}
	
	err = server.db.StoreSongs("testuser", testSongs)
	if err != nil {
		t.Fatalf("Failed to store test songs: %v", err)
	}
	
	t.Run("Record play event without previous song", func(t *testing.T) {
		server.RecordPlayEvent("testuser", "1", "play", nil)
		
		// Verify event was recorded (can't easily verify without exposing internals)
		// The method should not panic or return errors
	})
	
	t.Run("Record play event with previous song", func(t *testing.T) {
		previousSong := "1"
		server.RecordPlayEvent("testuser", "2", "play", &previousSong)
		
		// Should record both play event and transition
	})
	
	t.Run("Record skip event", func(t *testing.T) {
		server.RecordPlayEvent("testuser", "1", "skip", nil)
		
		// Should record skip event
	})
}

func TestSetLastPlayed(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	server.SetLastPlayed("testuser", "123")
	
	// Should set last played song in shuffle service
	// We can't easily verify this without exposing internals
}

func TestGetHandlers(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	handlers := server.GetHandlers()
	if handlers == nil {
		t.Error("Handlers should not be nil")
	}
	if handlers != server.handlers {
		t.Error("Should return the same handlers instance")
	}
}

func TestFetchAndStoreSongs(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	t.Run("Without valid credentials", func(t *testing.T) {
		// Should log warning and return early
		server.fetchAndStoreSongs()
		
		// Should not panic
	})
	
	t.Run("With mock upstream server", func(t *testing.T) {
		// Create mock upstream server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/rest/search3") {
				response := models.SubsonicResponse{
					SubsonicResponse: struct {
						Status  string `json:"status"`
						Version string `json:"version"`
						Songs   struct {
							Song []models.Song `json:"song"`
						} `json:"songs,omitempty"`
					}{
						Status:  "ok",
						Version: "1.15.0",
						Songs: struct {
							Song []models.Song `json:"song"`
						}{
							Song: []models.Song{
								{ID: "1", Title: "Test Song", Artist: "Test Artist", Album: "Test Album", Duration: 300},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			}
		}))
		defer mockServer.Close()
		
		// Update server config to use mock server
		server.config.UpstreamURL = mockServer.URL
		
		// Store valid credentials
		server.credentials.ValidateAndStore("testuser", "testpass")
		
		// This would normally fetch from upstream, but we can't easily test without
		// changing the URL or mocking the HTTP client
		server.fetchAndStoreSongs()
	})
}

func TestSyncSongsLifecycle(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	
	// Sync songs should be running in background
	time.Sleep(50 * time.Millisecond)
	
	// Shutdown should stop the sync goroutine
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	err = server.Shutdown(ctx)
	if err != nil {
		t.Errorf("Failed to shutdown server: %v", err)
	}
}

func TestProxyHandlerCredentialExtraction(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	tests := []struct {
		name        string
		url         string
		expectValid bool
	}{
		{
			name:        "Valid credentials",
			url:         "/rest/ping?u=testuser&p=testpass&v=1.15.0&c=testclient",
			expectValid: true,
		},
		{
			name:        "Missing username",
			url:         "/rest/ping?p=testpass&v=1.15.0&c=testclient",
			expectValid: false,
		},
		{
			name:        "Missing password",
			url:         "/rest/ping?u=testuser&v=1.15.0&c=testclient",
			expectValid: false,
		},
		{
			name:        "Empty username",
			url:         "/rest/ping?u=&p=testpass&v=1.15.0&c=testclient",
			expectValid: false,
		},
		{
			name:        "Empty password",
			url:         "/rest/ping?u=testuser&p=&v=1.15.0&c=testclient",
			expectValid: false,
		},
		{
			name:        "Non-REST endpoint",
			url:         "/other/endpoint?u=testuser&p=testpass",
			expectValid: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			
			server.proxyHandler(w, req)
			
			// We can't easily verify if credentials were extracted without exposing internals
			// The test mainly ensures no panics occur
		})
	}
}

func TestExtractCredentials(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	tests := []struct {
		name             string
		setupRequest     func() *http.Request
		expectedUsername string
		expectedPassword string
	}{
		{
			name: "URL query parameters",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/rest/ping?u=testuser&p=testpass", nil)
				return req
			},
			expectedUsername: "testuser",
			expectedPassword: "testpass",
		},
		{
			name: "POST form data",
			setupRequest: func() *http.Request {
				form := url.Values{}
				form.Add("u", "formuser")
				form.Add("p", "formpass")
				req := httptest.NewRequest("POST", "/rest/ping", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return req
			},
			expectedUsername: "formuser",
			expectedPassword: "formpass",
		},
		{
			name: "Authorization header Basic Auth",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/rest/ping", nil)
				req.SetBasicAuth("basicuser", "basicpass")
				return req
			},
			expectedUsername: "basicuser",
			expectedPassword: "basicpass",
		},
		{
			name: "X-Subsonic headers",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/rest/ping", nil)
				req.Header.Set("X-Subsonic-Username", "headeruser")
				req.Header.Set("X-Subsonic-Password", "headerpass")
				return req
			},
			expectedUsername: "headeruser",
			expectedPassword: "headerpass",
		},
		{
			name: "No credentials",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/rest/ping", nil)
				return req
			},
			expectedUsername: "",
			expectedPassword: "",
		},
		{
			name: "URL parameters take precedence over headers",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/rest/ping?u=urluser&p=urlpass", nil)
				req.SetBasicAuth("basicuser", "basicpass")
				req.Header.Set("X-Subsonic-Username", "headeruser")
				req.Header.Set("X-Subsonic-Password", "headerpass")
				return req
			},
			expectedUsername: "urluser",
			expectedPassword: "urlpass",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupRequest()
			username, password := server.extractCredentials(req)
			
			if username != tt.expectedUsername {
				t.Errorf("Expected username %q, got %q", tt.expectedUsername, username)
			}
			if password != tt.expectedPassword {
				t.Errorf("Expected password %q, got %q", tt.expectedPassword, password)
			}
		})
	}
}

func TestHookExecution(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	// Test multiple hooks, where first returns false and second returns true
	var firstHookCalled, secondHookCalled bool
	
	firstHook := func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		firstHookCalled = true
		return false // Continue to next hook
	}
	
	secondHook := func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		secondHookCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("handled"))
		return true // Stop processing
	}
	
	server.AddHook("/test/multiple", firstHook)
	server.AddHook("/test/multiple", secondHook)
	
	req := httptest.NewRequest("GET", "/test/multiple", nil)
	w := httptest.NewRecorder()
	
	server.proxyHandler(w, req)
	
	if !firstHookCalled {
		t.Error("First hook should have been called")
	}
	if !secondHookCalled {
		t.Error("Second hook should have been called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "handled" {
		t.Errorf("Expected 'handled', got '%s'", w.Body.String())
	}
}

func TestErrorHandling(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "warn",
		DatabasePath: "test.db",
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	// Test that methods handle errors gracefully
	server.RecordPlayEvent("testuser", "nonexistent", "play", nil)
	server.SetLastPlayed("testuser", "nonexistent")
	
	// Should not panic
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *config.Config
		expectError bool
		errorStr    string
	}{
		{
			name: "Valid config",
			config: &config.Config{
				ProxyPort:    "8080",
				UpstreamURL:  "http://localhost:4533",
				LogLevel:     "info",
				DatabasePath: "test.db",
			},
			expectError: false,
		},
		{
			name: "Invalid upstream URL",
			config: &config.Config{
				ProxyPort:    "8080",
				UpstreamURL:  "://invalid-url",  // This is definitely invalid
				LogLevel:     "info",
				DatabasePath: "test.db",
			},
			expectError: true,
			errorStr:    "invalid upstream URL",
		},
		{
			name: "Invalid database path",
			config: &config.Config{
				ProxyPort:    "8080",
				UpstreamURL:  "http://localhost:4533",
				LogLevel:     "info",
				DatabasePath: "/nonexistent/path/test.db",
			},
			expectError: true,
			errorStr:    "failed to initialize database",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "Valid config" {
				defer os.Remove("test.db")
			}
			
			server, err := New(tt.config)
			
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorStr) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorStr, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else {
					defer server.Shutdown(context.Background())
				}
			}
		})
	}
}

func TestRateLimitingEnabled(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test.db",
		RateLimitRPS:      2,
		RateLimitBurst:    3,
		RateLimitEnabled:  true,
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	if server.rateLimiter == nil {
		t.Error("Rate limiter should not be nil when enabled")
	}
	
	// Test that first few requests are allowed
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		
		server.proxyHandler(w, req)
		
		// Should not be rate limited (will fail proxy connection, but that's expected)
		if w.Code == http.StatusTooManyRequests {
			t.Errorf("Request %d should not be rate limited", i+1)
		}
	}
	
	// Test that additional requests are rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	
	server.proxyHandler(w, req)
	
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Rate limit exceeded") {
		t.Errorf("Expected 'Rate limit exceeded' message, got: %s", w.Body.String())
	}
}

func TestRateLimitingDisabled(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test.db",
		RateLimitRPS:      1,
		RateLimitBurst:    1,
		RateLimitEnabled:  false,
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	if server.rateLimiter != nil {
		t.Error("Rate limiter should be nil when disabled")
	}
	
	// Test that many requests are allowed when rate limiting is disabled
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		
		server.proxyHandler(w, req)
		
		// Should never be rate limited
		if w.Code == http.StatusTooManyRequests {
			t.Errorf("Request %d should not be rate limited when rate limiting is disabled", i+1)
		}
	}
}

func TestRateLimitingWithHooks(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test.db",
		RateLimitRPS:      1,
		RateLimitBurst:    1,
		RateLimitEnabled:  true,
	}
	defer os.Remove("test.db")
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	// Add a hook that handles the request
	var hookCalled bool
	testHook := func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		hookCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("handled by hook"))
		return true
	}
	
	server.AddHook("/test/hook", testHook)
	
	// First request should be allowed and handled by hook
	req := httptest.NewRequest("GET", "/test/hook", nil)
	w := httptest.NewRecorder()
	
	server.proxyHandler(w, req)
	
	if !hookCalled {
		t.Error("Hook should have been called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "handled by hook" {
		t.Errorf("Expected 'handled by hook', got '%s'", w.Body.String())
	}
	
	// Second request should be rate limited before hook is called
	hookCalled = false
	req = httptest.NewRequest("GET", "/test/hook", nil)
	w = httptest.NewRecorder()
	
	server.proxyHandler(w, req)
	
	if hookCalled {
		t.Error("Hook should not have been called when rate limited")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", w.Code)
	}
}

func TestFetchAndStoreSongsMultiUser(t *testing.T) {
	os.Remove("test_multiuser.db")
	
	cfg := &config.Config{
		ProxyPort:       "8080",
		UpstreamURL:     "http://localhost:4533",
		LogLevel:        "warn",
		DatabasePath:    "test_multiuser.db",
		RateLimitRPS:    100,
		RateLimitBurst:  200,
		RateLimitEnabled: false,
	}
	defer os.Remove("test_multiuser.db")
	
	// Create mock upstream server that returns different songs for different users
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := r.URL.Query().Get("u")
		
		if strings.Contains(r.URL.Path, "/rest/ping") {
			// Ping endpoint - return success
			response := map[string]interface{}{
				"subsonic-response": map[string]interface{}{
					"status":  "ok",
					"version": "1.15.0",
				},
			}
			json.NewEncoder(w).Encode(response)
		} else if strings.Contains(r.URL.Path, "/rest/search3") {
			// Search endpoint - return different songs per user
			var songs []models.Song
			switch username {
			case "user1":
				songs = []models.Song{
					{ID: "1", Title: "Song 1 User 1", Artist: "Artist 1"},
					{ID: "2", Title: "Song 2 User 1", Artist: "Artist 1"},
				}
			case "user2":
				songs = []models.Song{
					{ID: "3", Title: "Song 1 User 2", Artist: "Artist 2"},
					{ID: "4", Title: "Song 2 User 2", Artist: "Artist 2"},
				}
			case "user3":
				songs = []models.Song{
					{ID: "5", Title: "Song 1 User 3", Artist: "Artist 3"},
					{ID: "6", Title: "Song 2 User 3", Artist: "Artist 3"},
				}
			}
			
			response := map[string]interface{}{
				"subsonic-response": map[string]interface{}{
					"status":  "ok",
					"version": "1.15.0",
					"songs": map[string]interface{}{
						"song": songs,
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer mockServer.Close()
	
	cfg.UpstreamURL = mockServer.URL
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	// Add multiple users with valid credentials
	err = server.credentials.ValidateAndStore("user1", "pass1")
	if err != nil {
		t.Errorf("Failed to validate user1: %v", err)
	}
	err = server.credentials.ValidateAndStore("user2", "pass2")
	if err != nil {
		t.Errorf("Failed to validate user2: %v", err)
	}
	err = server.credentials.ValidateAndStore("user3", "pass3")
	if err != nil {
		t.Errorf("Failed to validate user3: %v", err)
	}
	
	// Trigger multi-user sync
	server.fetchAndStoreSongs()
	
	// Verify songs were stored for each user
	user1Songs, err := server.db.GetAllSongs("user1")
	if err != nil {
		t.Errorf("Failed to get songs for user1: %v", err)
	}
	if len(user1Songs) != 2 {
		t.Errorf("Expected 2 songs for user1, got %d", len(user1Songs))
	}
	
	user2Songs, err := server.db.GetAllSongs("user2")
	if err != nil {
		t.Errorf("Failed to get songs for user2: %v", err)
	}
	if len(user2Songs) != 2 {
		t.Errorf("Expected 2 songs for user2, got %d", len(user2Songs))
	}
	
	user3Songs, err := server.db.GetAllSongs("user3")
	if err != nil {
		t.Errorf("Failed to get songs for user3: %v", err)
	}
	if len(user3Songs) != 2 {
		t.Errorf("Expected 2 songs for user3, got %d", len(user3Songs))
	}
	
	// Verify songs are different for each user
	if len(user1Songs) > 0 && len(user2Songs) > 0 {
		if user1Songs[0].ID == user2Songs[0].ID {
			t.Error("User1 and User2 should have different songs")
		}
	}
}

func TestSyncSongsForUserError(t *testing.T) {
	os.Remove("test_sync_error.db")
	
	cfg := &config.Config{
		ProxyPort:       "8080",
		UpstreamURL:     "http://localhost:4533",
		LogLevel:        "warn",
		DatabasePath:    "test_sync_error.db",
		RateLimitRPS:    100,
		RateLimitBurst:  200,
		RateLimitEnabled: false,
	}
	defer os.Remove("test_sync_error.db")
	
	// Create mock upstream server that returns error for user2
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := r.URL.Query().Get("u")
		
		if strings.Contains(r.URL.Path, "/rest/ping") {
			// Ping endpoint - return success
			response := map[string]interface{}{
				"subsonic-response": map[string]interface{}{
					"status":  "ok",
					"version": "1.15.0",
				},
			}
			json.NewEncoder(w).Encode(response)
		} else if strings.Contains(r.URL.Path, "/rest/search3") {
			if username == "user2" {
				// Return error for user2
				response := map[string]interface{}{
					"subsonic-response": map[string]interface{}{
						"status": "failed",
						"error": map[string]interface{}{
							"code":    "10",
							"message": "Required parameter is missing",
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			} else {
				// Return success for other users
				songs := []models.Song{
					{ID: "1", Title: "Song 1", Artist: "Artist 1"},
				}
				response := map[string]interface{}{
					"subsonic-response": map[string]interface{}{
						"status":  "ok",
						"version": "1.15.0",
						"songs": map[string]interface{}{
							"song": songs,
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			}
		}
	}))
	defer mockServer.Close()
	
	cfg.UpstreamURL = mockServer.URL
	
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())
	
	// Add multiple users with valid credentials
	err = server.credentials.ValidateAndStore("user1", "pass1")
	if err != nil {
		t.Errorf("Failed to validate user1: %v", err)
	}
	err = server.credentials.ValidateAndStore("user2", "pass2")
	if err != nil {
		t.Errorf("Failed to validate user2: %v", err)
	}
	
	// Trigger multi-user sync
	server.fetchAndStoreSongs()
	
	// Verify songs were stored for user1 but not user2
	user1Songs, err := server.db.GetAllSongs("user1")
	if err != nil {
		t.Errorf("Failed to get songs for user1: %v", err)
	}
	if len(user1Songs) != 1 {
		t.Errorf("Expected 1 song for user1, got %d", len(user1Songs))
	}
	
	user2Songs, err := server.db.GetAllSongs("user2")
	if err != nil {
		t.Errorf("Failed to get songs for user2: %v", err)
	}
	if len(user2Songs) != 0 {
		t.Errorf("Expected 0 songs for user2 (sync failed), got %d", len(user2Songs))
	}
}

func TestGetSortedUsernames(t *testing.T) {
	testCases := []struct {
		name     string
		input    map[string]string
		expected []string
	}{
		{
			name:     "Empty map",
			input:    map[string]string{},
			expected: []string{},
		},
		{
			name: "Single user",
			input: map[string]string{
				"user1": "pass1",
			},
			expected: []string{"user1"},
		},
		{
			name: "Multiple users",
			input: map[string]string{
				"user3": "pass3",
				"user1": "pass1",
				"user2": "pass2",
			},
			expected: []string{"user1", "user2", "user3"},
		},
		{
			name: "Users with special characters",
			input: map[string]string{
				"user_z": "pass_z",
				"user_a": "pass_a",
				"user_m": "pass_m",
			},
			expected: []string{"user_a", "user_m", "user_z"},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := getSortedUsernames(tc.input)
			
			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d usernames, got %d", len(tc.expected), len(result))
			}
			
			for i, expected := range tc.expected {
				if i >= len(result) || result[i] != expected {
					t.Errorf("Expected username at index %d to be %s, got %s", i, expected, result[i])
				}
			}
		})
	}
}