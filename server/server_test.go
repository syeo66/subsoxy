package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
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
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	if server == nil {
		t.Fatal("Server should not be nil")
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
		UpstreamURL:       "://invalid-url", // This will definitely be invalid
		LogLevel:          "info",
		DatabasePath:      "test.db",
		RateLimitRPS:      100,
		RateLimitBurst:    200,
		RateLimitEnabled:  true,
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "invalid-level",
		DatabasePath:      "test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "info",
		DatabasePath:      "/nonexistent/path/test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "info",
		DatabasePath:      "test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn", // Reduce log noise
		DatabasePath:      "test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		ProxyPort:         "0", // Use random port
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
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

		// Should record the play event with its previous song
	})

	t.Run("Record skip event", func(t *testing.T) {
		server.RecordPlayEvent("testuser", "1", "skip", nil)

		// Should record skip event
	})
}

// A skip is recorded through the server's real callback, which calls back into the
// shuffle service. This used to deadlock because the shuffle lock was still held.
func TestProcessScrobbleRecordsSkipWithoutDeadlock(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test_scrobble_skip.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test_scrobble_skip.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	testSongs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
	}
	if err := server.db.StoreSongs("testuser", testSongs); err != nil {
		t.Fatalf("Failed to store test songs: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.ProcessScrobble("testuser", "", "1", false) // now playing song 1
		server.ProcessScrobble("testuser", "", "2", false) // song 1 skipped
		server.ProcessScrobble("testuser", "", "1", false) // song 2 skipped
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ProcessScrobble deadlocked while recording a skip")
	}

	all, err := server.db.GetAllSongs("testuser")
	if err != nil {
		t.Fatalf("Failed to load songs: %v", err)
	}
	for _, song := range all {
		if song.SkipCount != 1 {
			t.Errorf("Song %s: expected 1 skip, got %d", song.ID, song.SkipCount)
		}
	}
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
						MusicFolders struct {
							MusicFolder []models.MusicFolder `json:"musicFolder"`
						} `json:"musicFolders,omitempty"`
						Indexes struct {
							Index []models.Index `json:"index"`
						} `json:"indexes,omitempty"`
						Directory struct {
							Child []models.Song `json:"child"`
						} `json:"directory,omitempty"`
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
		_, _ = server.credentials.ValidateAndStore(context.Background(), "testuser", "testpass")

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
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
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
				UpstreamURL:  "://invalid-url", // This is definitely invalid
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
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		CredentialWorkers: config.DefaultCredentialWorkers,
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

func TestRateLimitingIsPerClient(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test.db",
		RateLimitRPS:      1,
		RateLimitBurst:    1,
		RateLimitEnabled:  true,
		TrustedProxies:    []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	server.AddHook("/test/hook", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		w.WriteHeader(http.StatusOK)
		return true
	})

	send := func(client string) int {
		req := httptest.NewRequest("GET", "/test/hook", nil)
		req.RemoteAddr = "10.0.0.2:40000" // The reverse proxy
		req.Header.Set("X-Forwarded-For", client)
		w := httptest.NewRecorder()
		server.proxyHandler(w, req)
		return w.Code
	}

	if code := send("198.51.100.1"); code != http.StatusOK {
		t.Fatalf("First client's request: expected 200, got %d", code)
	}
	if code := send("198.51.100.1"); code != http.StatusTooManyRequests {
		t.Errorf("First client's second request: expected 429, got %d", code)
	}
	if code := send("198.51.100.2"); code != http.StatusOK {
		t.Errorf("Second client behind the same proxy: expected 200, got %d", code)
	}
}

func TestFetchAndStoreSongsMultiUser(t *testing.T) {
	os.Remove("test_multiuser.db")

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test_multiuser.db",
		RateLimitRPS:      100,
		RateLimitBurst:    200,
		RateLimitEnabled:  false,
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		} else if strings.Contains(r.URL.Path, "/rest/getMusicFolders") {
			// Music folders endpoint
			response := map[string]interface{}{
				"subsonic-response": map[string]interface{}{
					"status":  "ok",
					"version": "1.15.0",
					"musicFolders": map[string]interface{}{
						"musicFolder": []models.MusicFolder{
							{ID: "1", Name: "Music"},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		} else if strings.Contains(r.URL.Path, "/rest/getIndexes") {
			// Indexes endpoint - return different artists per user
			var artists []models.Artist
			switch username {
			case "user1":
				artists = []models.Artist{
					{ID: "artist1", Name: "Artist 1"},
				}
			case "user2":
				artists = []models.Artist{
					{ID: "artist2", Name: "Artist 2"},
				}
			case "user3":
				artists = []models.Artist{
					{ID: "artist3", Name: "Artist 3"},
				}
			}

			response := map[string]interface{}{
				"subsonic-response": map[string]interface{}{
					"status":  "ok",
					"version": "1.15.0",
					"indexes": map[string]interface{}{
						"index": []models.Index{
							{Name: "A", Artists: artists},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		} else if strings.Contains(r.URL.Path, "/rest/getMusicDirectory") {
			// Music directory endpoint - return albums and songs
			id := r.URL.Query().Get("id")

			// Handle artist directories (return albums)
			if strings.HasPrefix(id, "artist") {
				var albums []models.Song
				switch id {
				case "artist1":
					albums = []models.Song{
						{ID: "album1", Title: "Album 1 User 1", Artist: "Artist 1", IsDir: true},
					}
				case "artist2":
					albums = []models.Song{
						{ID: "album2", Title: "Album 1 User 2", Artist: "Artist 2", IsDir: true},
					}
				case "artist3":
					albums = []models.Song{
						{ID: "album3", Title: "Album 1 User 3", Artist: "Artist 3", IsDir: true},
					}
				}

				response := map[string]interface{}{
					"subsonic-response": map[string]interface{}{
						"status":  "ok",
						"version": "1.15.0",
						"directory": map[string]interface{}{
							"child": albums,
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			} else if strings.HasPrefix(id, "album") {
				// Handle album directories (return songs)
				var songs []models.Song
				switch id {
				case "album1":
					songs = []models.Song{
						{ID: "1", Title: "Song 1 User 1", Artist: "Artist 1", IsDir: false},
						{ID: "2", Title: "Song 2 User 1", Artist: "Artist 1", IsDir: false},
					}
				case "album2":
					songs = []models.Song{
						{ID: "3", Title: "Song 1 User 2", Artist: "Artist 2", IsDir: false},
						{ID: "4", Title: "Song 2 User 2", Artist: "Artist 2", IsDir: false},
					}
				case "album3":
					songs = []models.Song{
						{ID: "5", Title: "Song 1 User 3", Artist: "Artist 3", IsDir: false},
						{ID: "6", Title: "Song 2 User 3", Artist: "Artist 3", IsDir: false},
					}
				}

				response := map[string]interface{}{
					"subsonic-response": map[string]interface{}{
						"status":  "ok",
						"version": "1.15.0",
						"directory": map[string]interface{}{
							"child": songs,
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
	_, err = server.credentials.ValidateAndStore(context.Background(), "user1", "pass1")
	if err != nil {
		t.Errorf("Failed to validate user1: %v", err)
	}
	_, err = server.credentials.ValidateAndStore(context.Background(), "user2", "pass2")
	if err != nil {
		t.Errorf("Failed to validate user2: %v", err)
	}
	_, err = server.credentials.ValidateAndStore(context.Background(), "user3", "pass3")
	if err != nil {
		t.Errorf("Failed to validate user3: %v", err)
	}

	// Trigger direct sync for each user to avoid delays
	err = server.syncSongsForUser("user1", "pass1")
	if err != nil {
		t.Errorf("Failed to sync songs for user1: %v", err)
	}
	err = server.syncSongsForUser("user2", "pass2")
	if err != nil {
		t.Errorf("Failed to sync songs for user2: %v", err)
	}
	err = server.syncSongsForUser("user3", "pass3")
	if err != nil {
		t.Errorf("Failed to sync songs for user3: %v", err)
	}

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
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test_sync_error.db",
		RateLimitRPS:      100,
		RateLimitBurst:    200,
		RateLimitEnabled:  false,
		CredentialWorkers: config.DefaultCredentialWorkers,
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
		} else if strings.Contains(r.URL.Path, "/rest/getMusicFolders") {
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
				response := map[string]interface{}{
					"subsonic-response": map[string]interface{}{
						"status":  "ok",
						"version": "1.15.0",
						"musicFolders": map[string]interface{}{
							"musicFolder": []models.MusicFolder{
								{ID: "1", Name: "Music"},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			}
		} else if strings.Contains(r.URL.Path, "/rest/getIndexes") {
			// Only user1 should reach this point (user2 fails at getMusicFolders)
			response := map[string]interface{}{
				"subsonic-response": map[string]interface{}{
					"status":  "ok",
					"version": "1.15.0",
					"indexes": map[string]interface{}{
						"index": []models.Index{
							{Name: "A", Artists: []models.Artist{
								{ID: "artist1", Name: "Artist 1"},
							}},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		} else if strings.Contains(r.URL.Path, "/rest/getMusicDirectory") {
			// Only user1 should reach this point (user2 fails at getMusicFolders)
			id := r.URL.Query().Get("id")

			if strings.HasPrefix(id, "artist") {
				// Return album for artist1
				response := map[string]interface{}{
					"subsonic-response": map[string]interface{}{
						"status":  "ok",
						"version": "1.15.0",
						"directory": map[string]interface{}{
							"child": []models.Song{
								{ID: "album1", Title: "Album 1", Artist: "Artist 1", IsDir: true},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			} else if strings.HasPrefix(id, "album") {
				// Return song for album1
				response := map[string]interface{}{
					"subsonic-response": map[string]interface{}{
						"status":  "ok",
						"version": "1.15.0",
						"directory": map[string]interface{}{
							"child": []models.Song{
								{ID: "1", Title: "Song 1", Artist: "Artist 1", IsDir: false},
							},
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
	_, err = server.credentials.ValidateAndStore(context.Background(), "user1", "pass1")
	if err != nil {
		t.Errorf("Failed to validate user1: %v", err)
	}
	_, err = server.credentials.ValidateAndStore(context.Background(), "user2", "pass2")
	if err != nil {
		t.Errorf("Failed to validate user2: %v", err)
	}

	// Trigger direct sync for user1 only (user2 should fail)
	err = server.syncSongsForUser("user1", "pass1")
	if err != nil {
		t.Errorf("Failed to sync songs for user1: %v", err)
	}
	err = server.syncSongsForUser("user2", "pass2")
	if err == nil {
		t.Error("Expected sync to fail for user2, but it succeeded")
	}

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

// CORS functionality tests

func TestSetCORSHeaders(t *testing.T) {
	tests := []struct {
		name                string
		corsConfig          config.Config
		requestOrigin       string
		expectedOrigin      string
		expectedMethods     string
		expectedHeaders     string
		expectedCredentials string
		expectedMaxAge      string
	}{
		{
			name: "Wildcard origin",
			corsConfig: config.Config{
				CORSEnabled:          true,
				CORSAllowOrigins:     []string{"*"},
				CORSAllowMethods:     []string{"GET", "POST"},
				CORSAllowHeaders:     []string{"Content-Type", "Authorization"},
				CORSAllowCredentials: false,
			},
			requestOrigin:       "https://example.com",
			expectedOrigin:      "*",
			expectedMethods:     "GET, POST",
			expectedHeaders:     "Content-Type, Authorization",
			expectedCredentials: "",
			expectedMaxAge:      "86400",
		},
		{
			name: "Specific allowed origin",
			corsConfig: config.Config{
				CORSEnabled:          true,
				CORSAllowOrigins:     []string{"https://example.com", "https://test.com"},
				CORSAllowMethods:     []string{"GET", "POST", "PUT"},
				CORSAllowHeaders:     []string{"Content-Type"},
				CORSAllowCredentials: true,
			},
			requestOrigin:       "https://example.com",
			expectedOrigin:      "https://example.com",
			expectedMethods:     "GET, POST, PUT",
			expectedHeaders:     "Content-Type",
			expectedCredentials: "true",
			expectedMaxAge:      "86400",
		},
		{
			name: "Origin not in allowed list",
			corsConfig: config.Config{
				CORSEnabled:          true,
				CORSAllowOrigins:     []string{"https://example.com"},
				CORSAllowMethods:     []string{"GET"},
				CORSAllowHeaders:     []string{"Content-Type"},
				CORSAllowCredentials: false,
			},
			requestOrigin:       "https://malicious.com",
			expectedOrigin:      "",
			expectedMethods:     "GET",
			expectedHeaders:     "Content-Type",
			expectedCredentials: "",
			expectedMaxAge:      "86400",
		},
		{
			name: "Empty CORS configuration",
			corsConfig: config.Config{
				CORSEnabled:          true,
				CORSAllowOrigins:     []string{},
				CORSAllowMethods:     []string{},
				CORSAllowHeaders:     []string{},
				CORSAllowCredentials: false,
			},
			requestOrigin:       "https://example.com",
			expectedOrigin:      "",
			expectedMethods:     "",
			expectedHeaders:     "",
			expectedCredentials: "",
			expectedMaxAge:      "86400",
		},
		{
			name: "No origin header in request",
			corsConfig: config.Config{
				CORSEnabled:          true,
				CORSAllowOrigins:     []string{"https://example.com"},
				CORSAllowMethods:     []string{"GET"},
				CORSAllowHeaders:     []string{"Content-Type"},
				CORSAllowCredentials: false,
			},
			requestOrigin:       "",
			expectedOrigin:      "",
			expectedMethods:     "GET",
			expectedHeaders:     "Content-Type",
			expectedCredentials: "",
			expectedMaxAge:      "86400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary database for testing
			cfg := &tt.corsConfig
			cfg.DatabasePath = "test_cors.db"
			cfg.ProxyPort = "8080"
			cfg.UpstreamURL = "http://localhost:4533"
			cfg.LogLevel = "info"
			defer os.Remove("test_cors.db")

			server, err := New(cfg)
			if err != nil {
				t.Fatalf("Failed to create server: %v", err)
			}
			defer server.Shutdown(context.Background())

			// Create a test request
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}

			// Create a response recorder
			w := httptest.NewRecorder()

			// Call setCORSHeaders
			server.setCORSHeaders(w, req)

			// Check the response headers
			if tt.expectedOrigin != "" {
				if got := w.Header().Get("Access-Control-Allow-Origin"); got != tt.expectedOrigin {
					t.Errorf("Access-Control-Allow-Origin = %v, expected %v", got, tt.expectedOrigin)
				}
			} else {
				if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
					t.Errorf("Access-Control-Allow-Origin should be empty, got %v", got)
				}
			}

			if tt.expectedMethods != "" {
				if got := w.Header().Get("Access-Control-Allow-Methods"); got != tt.expectedMethods {
					t.Errorf("Access-Control-Allow-Methods = %v, expected %v", got, tt.expectedMethods)
				}
			}

			if tt.expectedHeaders != "" {
				if got := w.Header().Get("Access-Control-Allow-Headers"); got != tt.expectedHeaders {
					t.Errorf("Access-Control-Allow-Headers = %v, expected %v", got, tt.expectedHeaders)
				}
			}

			if tt.expectedCredentials != "" {
				if got := w.Header().Get("Access-Control-Allow-Credentials"); got != tt.expectedCredentials {
					t.Errorf("Access-Control-Allow-Credentials = %v, expected %v", got, tt.expectedCredentials)
				}
			} else {
				if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
					t.Errorf("Access-Control-Allow-Credentials should be empty, got %v", got)
				}
			}

			if got := w.Header().Get("Access-Control-Max-Age"); got != tt.expectedMaxAge {
				t.Errorf("Access-Control-Max-Age = %v, expected %v", got, tt.expectedMaxAge)
			}
		})
	}
}

func TestRecordPlayEventRefreshesPriors(t *testing.T) {
	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "warn",
		DatabasePath:      "test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	testSongs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist A", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist B", Album: "Album", Duration: 250},
		{ID: "3", Title: "Song 3", Artist: "Artist C", Album: "Album", Duration: 250},
	}
	if err := server.db.StoreSongs("testuser", testSongs); err != nil {
		t.Fatalf("Failed to store test songs: %v", err)
	}

	playSkipWeight := func() float64 {
		songs, err := server.db.GetAllSongs("testuser")
		if err != nil {
			t.Fatalf("Failed to get songs: %v", err)
		}
		for _, song := range songs {
			if song.ID == "1" {
				_, w, _, _ := server.shuffle.GetWeightComponents("testuser", song, nil, nil)
				return w
			}
		}
		t.Fatal("song 1 not found")
		return 0
	}

	server.RecordPlayEvent("testuser", "1", "play", nil)
	before := playSkipWeight() // computes and caches the priors

	// Skips on other songs raise the user's skip prior, which changes song 1's weight
	for i := 0; i < 10; i++ {
		server.RecordPlayEvent("testuser", "2", "skip", nil)
		server.RecordPlayEvent("testuser", "3", "skip", nil)
	}
	after := playSkipWeight()

	if before == after {
		t.Errorf("play/skip weight stayed %.4f after new events; cached priors were not refreshed", before)
	}
}

func TestAuthenticatedHooks(t *testing.T) {
	var mu sync.Mutex
	forwarded := 0 // getRandomSongs requests that reached upstream
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/rest/ping" {
			if r.URL.Path == "/rest/getRandomSongs" {
				mu.Lock()
				forwarded++
				mu.Unlock()
			}
			w.Write([]byte("upstream"))
			return
		}
		status := "failed"
		if q.Get("u") == "alice" && (q.Get("p") == "secret" || (q.Get("t") == "goodtoken" && q.Get("s") == "salt")) {
			status = "ok"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{"status": status, "version": "1.15.0"},
		})
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_auth_hooks.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test_auth_hooks.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	hookRan := false
	server.AddAuthenticatedHook("/rest/getRandomSongs", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		hookRan = true
		w.Write([]byte("local"))
		return true
	})

	tests := []struct {
		name      string
		query     string
		basicAuth []string
		wantLocal bool
		post      bool // Send the query as a form-encoded POST body instead
	}{
		{"valid password", "u=alice&p=secret", nil, true, false},
		{"valid token", "u=alice&t=goodtoken&s=salt", nil, true, false},
		{"no credentials", "u=alice", nil, false, false},
		{"wrong password", "u=alice&p=wrong", nil, false, false},
		{"wrong token", "u=alice&t=badtoken&s=salt", nil, false, false},
		{"credentials for another user", "u=bob", []string{"alice", "secret"}, false, false},
		{"valid password in POST body", "u=alice&p=secret", nil, true, true},
		{"valid token in POST body", "u=alice&t=goodtoken&s=salt", nil, true, true},
		{"wrong password in POST body", "u=alice&p=wrong", nil, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hookRan = false
			mu.Lock()
			forwarded = 0
			mu.Unlock()

			req := httptest.NewRequest("GET", "/rest/getRandomSongs?"+tt.query, nil)
			if tt.post {
				req = httptest.NewRequest("POST", "/rest/getRandomSongs", strings.NewReader(tt.query))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			if tt.basicAuth != nil {
				req.SetBasicAuth(tt.basicAuth[0], tt.basicAuth[1])
			}
			w := httptest.NewRecorder()
			server.proxyHandler(w, req)

			if hookRan != tt.wantLocal {
				t.Errorf("hook ran = %v, want %v", hookRan, tt.wantLocal)
			}
			wantBody := "upstream"
			if tt.wantLocal {
				wantBody = "local"
			}
			if w.Body.String() != wantBody {
				t.Errorf("body = %q, want %q", w.Body.String(), wantBody)
			}
			mu.Lock()
			gotForwarded := forwarded
			mu.Unlock()
			wantForwarded := 1
			if tt.wantLocal {
				wantForwarded = 0
			}
			if gotForwarded != wantForwarded {
				t.Errorf("forwarded upstream %d times, want %d", gotForwarded, wantForwarded)
			}
		})
	}
}

func TestViewSuffixRunsHooks(t *testing.T) {
	var mu sync.Mutex
	var forwardedPaths []string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/rest/ping") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"subsonic-response": map[string]interface{}{"status": "ok", "version": "1.15.0"},
			})
			return
		}
		// Valid credentials start a background library sync; keep its calls out of the forwarded paths
		if r.URL.Path == "/rest/search3" || r.URL.Path == "/rest/getMusicFolders" {
			w.Write([]byte("upstream"))
			return
		}
		mu.Lock()
		forwardedPaths = append(forwardedPaths, r.URL.Path)
		mu.Unlock()
		w.Write([]byte("upstream"))
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_view_suffix.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test_view_suffix.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	var hookEndpoint string
	server.AddAuthenticatedHook("/rest/getRandomSongs", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		hookEndpoint = endpoint
		w.Write([]byte("local"))
		return true
	})

	w := httptest.NewRecorder()
	server.proxyHandler(w, httptest.NewRequest("GET", "/rest/getRandomSongs.view?u=alice&p=secret", nil))
	if w.Body.String() != "local" {
		t.Errorf("body = %q, want the hook to answer a .view request", w.Body.String())
	}
	if hookEndpoint != "/rest/getRandomSongs" {
		t.Errorf("hook endpoint = %q, want it without the .view suffix", hookEndpoint)
	}

	// Requests without a hook are forwarded with the path the client sent
	w = httptest.NewRecorder()
	server.proxyHandler(w, httptest.NewRequest("GET", "/rest/getAlbumList2.view?u=alice&p=secret", nil))
	mu.Lock()
	defer mu.Unlock()
	if len(forwardedPaths) != 1 || forwardedPaths[0] != "/rest/getAlbumList2.view" {
		t.Errorf("forwarded paths = %v, want [/rest/getAlbumList2.view]", forwardedPaths)
	}
}

func TestShutdownAbortsRunningSync(t *testing.T) {
	syncStarted := make(chan struct{})
	var once sync.Once
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/ping" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"subsonic-response": map[string]interface{}{"status": "ok", "version": "1.15.0"},
			})
			return
		}
		if r.URL.Path == "/rest/search3" {
			// Hang like a slow upstream until the proxy gives up on the request
			once.Do(func() { close(syncStarted) })
			<-r.Context().Done()
			return
		}
		w.Write([]byte("upstream"))
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_shutdown_sync.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test_shutdown_sync.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// New credentials start a library sync in the background
	server.proxyHandler(httptest.NewRecorder(), httptest.NewRequest("GET", "/rest/getAlbumList2?u=alice&p=secret", nil))

	select {
	case <-syncStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("library sync did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Shutdown took %v, want the running sync to be aborted", elapsed)
	}
	if ctx.Err() != nil {
		t.Error("Shutdown hit its timeout instead of waiting for the sync to finish")
	}
}

func TestAuthenticatedHookCapturesCredentials(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{"status": "ok", "version": "1.15.0"},
		})
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_auth_capture.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test_auth_capture.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	server.AddAuthenticatedHook("/rest/scrobble", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return false
	})

	req := httptest.NewRequest("GET", "/rest/scrobble?u=alice&p=secret&id=song1", nil)
	server.proxyHandler(httptest.NewRecorder(), req)

	if _, ok := server.credentials.GetAllValid()["alice"]; !ok {
		t.Error("verified credentials should be stored for background sync")
	}
}

func TestProxyForwardsPostFormBody(t *testing.T) {
	var mu sync.Mutex
	var forwardedBody string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/getSong" {
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			forwardedBody = string(body)
			mu.Unlock()
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{"status": "ok", "version": "1.15.0"},
		})
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_post_form.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test_post_form.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	form := "u=alice&p=secret&id=song1"
	req := httptest.NewRequest("POST", "/rest/getSong", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.proxyHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	mu.Lock()
	defer mu.Unlock()
	if forwardedBody != form {
		t.Errorf("upstream received body %q, want %q", forwardedBody, form)
	}
}

func TestPostScrobbleRecordsPlay(t *testing.T) {
	var mu sync.Mutex
	var forwardedBody string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/scrobble" {
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			forwardedBody = string(body)
			mu.Unlock()
		}
		status := "failed"
		if r.URL.Path == "/rest/ping" || r.URL.Path == "/rest/scrobble" {
			status = "ok"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{"status": status, "version": "1.15.0"},
		})
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_post_scrobble.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove("test_post_scrobble.db")

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	if err := server.db.StoreSongs("alice", []models.Song{{ID: "song1", Title: "Song", Artist: "Artist", Album: "Album", Duration: 180}}); err != nil {
		t.Fatalf("Failed to store song: %v", err)
	}

	handlers := server.GetHandlers()
	server.AddAuthenticatedHook("/rest/scrobble", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return handlers.HandleScrobble(w, r, endpoint, server.RecordPlayEventAt, server.SetLastPlayed, server.ProcessScrobble)
	})

	form := "u=alice&p=secret&id=song1&submission=true"
	req := httptest.NewRequest("POST", "/rest/scrobble", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.proxyHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	mu.Lock()
	if forwardedBody != form {
		t.Errorf("upstream received body %q, want %q", forwardedBody, form)
	}
	mu.Unlock()

	songs, err := server.db.GetAllSongs("alice")
	if err != nil {
		t.Fatalf("Failed to get songs: %v", err)
	}
	if len(songs) != 1 || songs[0].PlayCount != 1 {
		t.Errorf("songs = %+v, want song1 with play count 1", songs)
	}
}

func TestSyncSongsForUserIncompleteFetchKeepsSongs(t *testing.T) {
	var mu sync.Mutex
	albumIDs := []string{"album1", "album2"}
	failAlbum2 := false

	subsonicOK := func(w http.ResponseWriter, key string, value interface{}) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{"status": "ok", "version": "1.15.0", key: value},
		})
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/rest/getMusicFolders":
			subsonicOK(w, "musicFolders", map[string]interface{}{
				"musicFolder": []models.MusicFolder{{ID: "1", Name: "Music"}},
			})
		case "/rest/getIndexes":
			subsonicOK(w, "indexes", map[string]interface{}{
				"index": []models.Index{{Name: "A", Artists: []models.Artist{{ID: "artist1", Name: "Artist 1"}}}},
			})
		case "/rest/getMusicDirectory":
			var children []models.Song
			switch id := r.URL.Query().Get("id"); id {
			case "artist1":
				for _, albumID := range albumIDs {
					children = append(children, models.Song{ID: albumID, Title: albumID, IsDir: true})
				}
			case "album1":
				children = []models.Song{
					{ID: "song1", Title: "Song 1", Artist: "Artist 1", Album: "album1"},
					{ID: "song2", Title: "Song 2", Artist: "Artist 1", Album: "album1"},
				}
			case "album2":
				if failAlbum2 {
					http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
					return
				}
				children = []models.Song{{ID: "song3", Title: "Song 3", Artist: "Artist 1", Album: "album2"}}
			}
			subsonicOK(w, "directory", map[string]interface{}{"child": children})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_sync_incomplete.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	os.Remove(cfg.DatabasePath)
	defer os.Remove(cfg.DatabasePath)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	songsByID := func() map[string]models.Song {
		t.Helper()
		songs, err := server.db.GetAllSongs("user1")
		if err != nil {
			t.Fatalf("Failed to get songs: %v", err)
		}
		byID := make(map[string]models.Song, len(songs))
		for _, song := range songs {
			byID[song.ID] = song
		}
		return byID
	}

	if err := server.syncSongsForUser("user1", "pass1"); err != nil {
		t.Fatalf("Initial sync failed: %v", err)
	}
	if got := len(songsByID()); got != 3 {
		t.Fatalf("Expected 3 songs after initial sync, got %d", got)
	}
	if err := server.db.RecordPlayEvent("user1", "song3", "play", nil); err != nil {
		t.Fatalf("Failed to record play: %v", err)
	}

	// A failed album fetch must not be mistaken for the album being removed upstream
	mu.Lock()
	failAlbum2 = true
	mu.Unlock()
	if err := server.syncSongsForUser("user1", "pass1"); err != nil {
		t.Fatalf("Incomplete sync failed: %v", err)
	}
	song3, ok := songsByID()["song3"]
	if !ok {
		t.Fatal("song3 was deleted after an incomplete library fetch")
	}
	if song3.PlayCount != 1 {
		t.Errorf("Expected song3 play count 1 to survive incomplete sync, got %d", song3.PlayCount)
	}

	// Once the fetch is complete again, songs really removed upstream are deleted
	mu.Lock()
	failAlbum2 = false
	albumIDs = []string{"album1"}
	mu.Unlock()
	if err := server.syncSongsForUser("user1", "pass1"); err != nil {
		t.Fatalf("Complete sync failed: %v", err)
	}
	songs := songsByID()
	if _, ok := songs["song3"]; ok {
		t.Error("song3 should be deleted after a complete sync that no longer lists it")
	}
	if len(songs) != 2 {
		t.Errorf("Expected 2 songs after complete sync, got %d", len(songs))
	}
}

func TestProxySetsForwardedHost(t *testing.T) {
	var gotForwardedHost, gotHost string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotForwardedHost = r.Header.Get("X-Forwarded-Host")
		gotHost = r.Host
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_forwarded_host.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove(cfg.DatabasePath)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "http://music.example.com/rest/getAlbum", nil)
	server.proxyHandler(httptest.NewRecorder(), req)

	if gotForwardedHost != "music.example.com" {
		t.Errorf("Expected X-Forwarded-Host %q, got %q", "music.example.com", gotForwardedHost)
	}
	upstreamURL, _ := url.Parse(mockServer.URL)
	if gotHost != upstreamURL.Host {
		t.Errorf("Expected upstream Host %q, got %q", upstreamURL.Host, gotHost)
	}
}

func TestShutdownLetsInFlightRequestsUseDatabase(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find free port: %v", err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()

	cfg := &config.Config{
		ProxyPort:         port,
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "error",
		DatabasePath:      "test_shutdown_order.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	defer os.Remove(cfg.DatabasePath)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	dbErr := make(chan error, 1)
	server.AddHook("/slow", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		close(entered)
		<-release
		dbErr <- server.db.StoreSongs("user1", []models.Song{{ID: "song1", Title: "Song 1", Artist: "Artist 1", Album: "Album 1"}})
		w.WriteHeader(http.StatusOK)
		return true
	})

	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	go func() {
		for i := 0; i < 50; i++ {
			resp, err := http.Get("http://127.0.0.1:" + port + "/slow")
			if err == nil {
				resp.Body.Close()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Request never reached the hook")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(ctx) }()

	// Give Shutdown time to get as far as it can while the request is still running
	time.Sleep(100 * time.Millisecond)
	close(release)

	if err := <-dbErr; err != nil {
		t.Errorf("In-flight request could not use the database during shutdown: %v", err)
	}
	if err := <-shutdownDone; err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

// upstreamRecorder is a mock upstream that accepts every ping and records the path and
// user of each request
type upstreamRecorder struct {
	mu       sync.Mutex
	requests []string // "path user"
}

func (u *upstreamRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.requests = append(u.requests, r.URL.Path+" "+r.URL.Query().Get("u"))
	u.mu.Unlock()
	if r.URL.Path == "/rest/ping" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{"status": "ok", "version": "1.15.0"},
		})
		return
	}
	w.Write([]byte("upstream"))
}

func (u *upstreamRecorder) count(request string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for _, r := range u.requests {
		if r == request {
			n++
		}
	}
	return n
}

func newCaptureTestServer(t *testing.T, upstream *upstreamRecorder, workers int) *ProxyServer {
	t.Helper()
	mockServer := httptest.NewServer(upstream)
	t.Cleanup(mockServer.Close)

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      t.TempDir() + "/capture.db",
		CredentialWorkers: workers,
	}
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	t.Cleanup(func() { server.Shutdown(context.Background()) })
	return server
}

func TestForwardedRequestDoesNotWaitForCredentialWorkers(t *testing.T) {
	upstream := &upstreamRecorder{}
	server := newCaptureTestServer(t, upstream, 1)

	// Occupy the only worker
	server.credentialWorkers <- struct{}{}
	defer func() { <-server.credentialWorkers }()

	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest("GET", "/rest/getCoverArt?u=alice&p=secret&id=1", nil)
		server.proxyHandler(httptest.NewRecorder(), req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Forwarded request blocked waiting for a credential worker")
	}
	if got := upstream.count("/rest/getCoverArt alice"); got != 1 {
		t.Errorf("Expected request to be forwarded once, got %d", got)
	}
	if got := upstream.count("/rest/ping alice"); got != 0 {
		t.Errorf("Expected no validation while workers are busy, got %d", got)
	}
}

func TestForwardedRequestsSkipValidationForRecentCredentials(t *testing.T) {
	upstream := &upstreamRecorder{}
	server := newCaptureTestServer(t, upstream, config.DefaultCredentialWorkers)

	// A token client sends a new salt with every request
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/rest/getCoverArt?u=alice&t=tok&s=salt"+strconv.Itoa(i)+"&id=1", nil)
		server.proxyHandler(httptest.NewRecorder(), req)
		server.credentialWg.Wait()
	}

	if got := upstream.count("/rest/ping alice"); got != 1 {
		t.Errorf("Expected 1 credential validation, got %d", got)
	}
}

func TestNewCredentialsSyncOnlyThatUser(t *testing.T) {
	upstream := &upstreamRecorder{}
	server := newCaptureTestServer(t, upstream, config.DefaultCredentialWorkers)

	if _, err := server.credentials.ValidateAndStore(context.Background(), "bob", "hunter2"); err != nil {
		t.Fatalf("Failed to store bob's credentials: %v", err)
	}

	server.AddAuthenticatedHook("/rest/scrobble", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return false
	})
	req := httptest.NewRequest("GET", "/rest/scrobble?u=alice&p=secret&id=1", nil)
	server.proxyHandler(httptest.NewRecorder(), req)
	server.credentialWg.Wait()

	if got := upstream.count("/rest/getMusicFolders alice"); got != 1 {
		t.Errorf("Expected alice to be synced once, got %d", got)
	}
	if got := upstream.count("/rest/getMusicFolders bob"); got != 0 {
		t.Errorf("Expected bob not to be synced, got %d", got)
	}
}

func TestSyncSkippedWhileUserSyncRunning(t *testing.T) {
	upstream := &upstreamRecorder{}
	server := newCaptureTestServer(t, upstream, config.DefaultCredentialWorkers)

	server.syncingUsers.Store("alice", struct{}{})
	server.syncUserWithPassword("alice", "secret")
	if got := upstream.count("/rest/getMusicFolders alice"); got != 0 {
		t.Errorf("Expected no second sync while one is running, got %d", got)
	}

	server.syncingUsers.Delete("alice")
	server.syncUserWithPassword("alice", "secret")
	if got := upstream.count("/rest/getMusicFolders alice"); got != 1 {
		t.Errorf("Expected sync to run once the earlier one finished, got %d", got)
	}
	if _, running := server.syncingUsers.Load("alice"); running {
		t.Error("Sync should be marked finished")
	}
}

func TestSyncSongsForUserUsesSearch3(t *testing.T) {
	const librarySize = 2*LibraryPageSize + 3

	library := make([]models.Song, librarySize)
	for i := range library {
		library[i] = models.Song{ID: fmt.Sprintf("song%d", i), Title: "Song", Artist: "Artist", Album: "Album", Duration: 180}
	}

	var mu sync.Mutex
	var ignoreOffset, emptySearch bool
	calls := map[string]int{}

	subsonicOK := func(w http.ResponseWriter, key string, value interface{}) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{"status": "ok", "version": "1.15.0", key: value},
		})
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls[r.URL.Path]++
		q := r.URL.Query()

		switch r.URL.Path {
		case "/rest/search3":
			if !q.Has("query") || q.Get("query") != "" {
				t.Errorf("search3 should be called with an empty query, got %q", q.Get("query"))
			}
			count, _ := strconv.Atoi(q.Get("songCount"))
			offset, _ := strconv.Atoi(q.Get("songOffset"))
			if ignoreOffset {
				offset = 0
			}
			var page []models.Song
			if !emptySearch && offset < len(library) {
				page = library[offset:min(offset+count, len(library))]
			}
			subsonicOK(w, "searchResult3", map[string]interface{}{"song": page})
		case "/rest/getMusicFolders":
			subsonicOK(w, "musicFolders", map[string]interface{}{
				"musicFolder": []models.MusicFolder{{ID: "1", Name: "Music"}},
			})
		case "/rest/getIndexes":
			subsonicOK(w, "indexes", map[string]interface{}{
				"index": []models.Index{{Name: "A", Artists: []models.Artist{{ID: "artist1", Name: "Artist"}}}},
			})
		case "/rest/getMusicDirectory":
			var children []models.Song
			switch q.Get("id") {
			case "artist1":
				children = []models.Song{{ID: "album1", IsDir: true}}
			case "album1":
				children = library[:2]
			}
			subsonicOK(w, "directory", map[string]interface{}{"child": children})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_sync_search3.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	os.Remove(cfg.DatabasePath)
	defer os.Remove(cfg.DatabasePath)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	sync := func(name string) (songCount int, searchCalls int, folderCalls int) {
		t.Helper()
		mu.Lock()
		clear(calls)
		mu.Unlock()
		if err := server.syncSongsForUser("user1", "pass1"); err != nil {
			t.Fatalf("%s: sync failed: %v", name, err)
		}
		songIDs, err := server.db.GetExistingSongIDs("user1")
		if err != nil {
			t.Fatalf("%s: failed to count songs: %v", name, err)
		}
		count := len(songIDs)
		mu.Lock()
		defer mu.Unlock()
		return count, calls["/rest/search3"], calls["/rest/getMusicFolders"]
	}

	// The whole library comes from three search3 pages, without walking folders
	if count, searches, folders := sync("search3"); count != librarySize || searches != 3 || folders != 0 {
		t.Errorf("search3 sync: %d songs, %d search3 calls, %d folder walks; want %d, 3, 0", count, searches, folders, librarySize)
	}

	// The walks below find far fewer songs than the search stored. Treat that removal as
	// confirmed, so these syncs apply what they fetched.
	server.pendingMassDeletions.Store("user1", time.Now().Add(-MassDeletionConfirmDelay))

	// A server that ignores the offset would page forever; the folder walk takes over instead
	mu.Lock()
	ignoreOffset = true
	mu.Unlock()
	if count, searches, folders := sync("ignored offset"); count != 2 || searches != 2 || folders != 1 {
		t.Errorf("ignored offset: %d songs, %d search3 calls, %d folder walks; want 2, 2, 1", count, searches, folders)
	}

	// An empty search result isn't trusted to mean an empty library
	mu.Lock()
	ignoreOffset = false
	emptySearch = true
	mu.Unlock()
	if count, searches, folders := sync("empty search"); count != 2 || searches != 1 || folders != 1 {
		t.Errorf("empty search: %d songs, %d search3 calls, %d folder walks; want 2, 1, 1", count, searches, folders)
	}
}

func TestSyncSongsForUserHoldsBackMassDeletion(t *testing.T) {
	var mu sync.Mutex
	var library []models.Song
	for i := range 10 {
		library = append(library, models.Song{ID: fmt.Sprintf("song%02d", i), Title: "Song", Artist: "Artist", Album: "Album", Duration: 180})
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/search3" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		var page []models.Song
		if r.URL.Query().Get("songOffset") == "0" {
			page = library
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok", "version": "1.15.0", "searchResult3": map[string]interface{}{"song": page},
			},
		})
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_sync_mass_deletion.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	os.Remove(cfg.DatabasePath)
	defer os.Remove(cfg.DatabasePath)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	syncAndCount := func(name string) int {
		t.Helper()
		if err := server.syncSongsForUser("user1", "pass1"); err != nil {
			t.Fatalf("%s: sync failed: %v", name, err)
		}
		ids, err := server.db.GetExistingSongIDs("user1")
		if err != nil {
			t.Fatalf("%s: failed to count songs: %v", name, err)
		}
		return len(ids)
	}

	if count := syncAndCount("initial"); count != 10 {
		t.Fatalf("initial: %d songs, want 10", count)
	}

	// Removing a few songs is applied right away
	mu.Lock()
	library = library[:8]
	mu.Unlock()
	if count := syncAndCount("small removal"); count != 8 {
		t.Errorf("small removal: %d songs, want 8", count)
	}

	// Removing most of the library is held back, also by a sync soon after
	mu.Lock()
	library = library[:2]
	mu.Unlock()
	if count := syncAndCount("mass removal"); count != 8 {
		t.Errorf("mass removal: %d songs, want all 8 kept", count)
	}
	if count := syncAndCount("mass removal again"); count != 8 {
		t.Errorf("mass removal again: %d songs, want all 8 kept until the confirm delay passed", count)
	}

	// A sync after the confirm delay that still sees the removal applies it
	server.pendingMassDeletions.Store("user1", time.Now().Add(-MassDeletionConfirmDelay))
	if count := syncAndCount("confirmed"); count != 2 {
		t.Errorf("confirmed: %d songs, want 2", count)
	}

	// A sync that finds the library back to normal cancels a pending removal
	server.pendingMassDeletions.Store("user1", time.Now())
	mu.Lock()
	library = append(library, models.Song{ID: "new1", Title: "New", Artist: "Artist", Album: "Album", Duration: 180})
	mu.Unlock()
	syncAndCount("recovered")
	if _, pending := server.pendingMassDeletions.Load("user1"); pending {
		t.Error("recovered: a sync without a mass removal should clear the pending one")
	}
}

func TestSyncSongsForUserWritesOnlyChangedSongs(t *testing.T) {
	var mu sync.Mutex
	library := []models.Song{
		{ID: "song1", Title: "One", Artist: "Artist", Album: "Album", Duration: 180},
		{ID: "song2", Title: "Two", Artist: "Artist", Album: "Album", Duration: 200},
		{ID: "song3", Title: "Three", Artist: "Artist", Album: "Album", Duration: 220},
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/search3" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		var page []models.Song
		if r.URL.Query().Get("songOffset") == "0" {
			page = library
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok", "version": "1.15.0", "searchResult3": map[string]interface{}{"song": page},
			},
		})
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		ProxyPort:         "8080",
		UpstreamURL:       mockServer.URL,
		LogLevel:          "error",
		DatabasePath:      "test_sync_changed_only.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}
	os.Remove(cfg.DatabasePath)
	defer os.Remove(cfg.DatabasePath)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	if err := server.syncSongsForUser("user1", "pass1"); err != nil {
		t.Fatalf("Initial sync failed: %v", err)
	}

	// Count every row the next sync writes, through a second connection to the same file
	conn, err := sql.Open("sqlite3", cfg.DatabasePath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer conn.Close()
	for _, stmt := range []string{
		`CREATE TABLE song_writes (id TEXT)`,
		`CREATE TRIGGER count_song_inserts AFTER INSERT ON songs BEGIN INSERT INTO song_writes VALUES (NEW.id); END`,
		`CREATE TRIGGER count_song_updates AFTER UPDATE ON songs BEGIN INSERT INTO song_writes VALUES (NEW.id); END`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("Failed to set up write counting: %v", err)
		}
	}

	mu.Lock()
	library[1].Title = "Two (Remastered)"
	library = append(library, models.Song{ID: "song4", Title: "Four", Artist: "Artist", Album: "Album", Duration: 240})
	mu.Unlock()

	if err := server.syncSongsForUser("user1", "pass1"); err != nil {
		t.Fatalf("Second sync failed: %v", err)
	}

	rows, err := conn.Query(`SELECT id FROM song_writes ORDER BY id`)
	if err != nil {
		t.Fatalf("Failed to read writes: %v", err)
	}
	defer rows.Close()
	var written []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		written = append(written, id)
	}
	if want := []string{"song2", "song4"}; !slices.Equal(written, want) {
		t.Errorf("Sync wrote %v, want only the changed and new songs %v", written, want)
	}

	songs, err := server.db.GetSongsByIDs("user1", []string{"song2"})
	if err != nil {
		t.Fatalf("Failed to get song: %v", err)
	}
	if songs["song2"].Title != "Two (Remastered)" {
		t.Errorf("Expected the changed title to be stored, got %q", songs["song2"].Title)
	}
}

func TestStartReportsPortInUse(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to reserve a port: %v", err)
	}
	defer listener.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)

	cfg := &config.Config{
		ProxyPort:         port,
		UpstreamURL:       "http://localhost:4533",
		LogLevel:          "panic",
		DatabasePath:      t.TempDir() + "/test.db",
		CredentialWorkers: config.DefaultCredentialWorkers,
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if err := server.Start(); err == nil {
		t.Error("Expected Start to fail when the port is already in use")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("Failed to shut down after a failed start: %v", err)
	}
}
