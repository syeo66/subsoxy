package credentials

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestNew(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in tests
	upstreamURL := "http://localhost:4533"
	
	manager := New(logger, upstreamURL)
	
	if manager == nil {
		t.Error("Manager should not be nil")
	}
	if manager.logger != logger {
		t.Error("Logger should be set correctly")
	}
	if manager.upstreamURL != upstreamURL {
		t.Error("Upstream URL should be set correctly")
	}
	if manager.validCredentials == nil {
		t.Error("Valid credentials map should be initialized")
	}
}

func TestValidateAndStoreValidCredentials(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	// Create a mock upstream server that returns valid response
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":  "ok",
				"version": "1.15.0",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// Test valid credentials
	manager.ValidateAndStore("testuser", "testpass")
	
	// Verify credentials were stored
	storedUser, storedPass := manager.GetValid()
	if storedUser != "testuser" || storedPass != "testpass" {
		t.Errorf("Expected stored credentials testuser/testpass, got %s/%s", storedUser, storedPass)
	}
}

func TestValidateAndStoreInvalidCredentials(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	// Create a mock upstream server that returns invalid response
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "failed",
				"error": map[string]interface{}{
					"code":    40,
					"message": "Wrong username or password",
				},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// Test invalid credentials
	manager.ValidateAndStore("wronguser", "wrongpass")
	
	// Verify no credentials were stored
	storedUser, storedPass := manager.GetValid()
	if storedUser != "" || storedPass != "" {
		t.Errorf("Expected no stored credentials, got %s/%s", storedUser, storedPass)
	}
}

func TestValidateAndStoreServerError(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	// Create a mock upstream server that returns 500 error
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// Test server error
	manager.ValidateAndStore("testuser", "testpass")
	
	// Verify no credentials were stored
	storedUser, storedPass := manager.GetValid()
	if storedUser != "" || storedPass != "" {
		t.Errorf("Expected no stored credentials, got %s/%s", storedUser, storedPass)
	}
}

func TestValidateAndStoreAlreadyStored(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	callCount := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		response := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":  "ok",
				"version": "1.15.0",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// First call should validate and store
	manager.ValidateAndStore("testuser", "testpass")
	
	// Second call with same credentials should not trigger validation
	manager.ValidateAndStore("testuser", "testpass")
	
	// Should only have made one HTTP call
	if callCount != 1 {
		t.Errorf("Expected 1 HTTP call, got %d", callCount)
	}
}

func TestValidateAndStoreInvalidJSON(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	// Create a mock upstream server that returns invalid JSON
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid json"))
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// Test invalid JSON response
	manager.ValidateAndStore("testuser", "testpass")
	
	// Verify no credentials were stored
	storedUser, storedPass := manager.GetValid()
	if storedUser != "" || storedPass != "" {
		t.Errorf("Expected no stored credentials, got %s/%s", storedUser, storedPass)
	}
}

func TestValidateAndStoreUnreachableServer(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	// Use an unreachable server URL
	manager := New(logger, "http://localhost:99999")
	
	// Test connection failure
	manager.ValidateAndStore("testuser", "testpass")
	
	// Verify no credentials were stored
	storedUser, storedPass := manager.GetValid()
	if storedUser != "" || storedPass != "" {
		t.Errorf("Expected no stored credentials, got %s/%s", storedUser, storedPass)
	}
}

func TestGetValidEmpty(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	manager := New(logger, "http://localhost:4533")
	
	// Test getting valid credentials when none are stored
	user, pass := manager.GetValid()
	if user != "" || pass != "" {
		t.Errorf("Expected empty credentials, got %s/%s", user, pass)
	}
}

func TestGetValidMultipleUsers(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":  "ok",
				"version": "1.15.0",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// Store multiple users
	manager.ValidateAndStore("user1", "pass1")
	manager.ValidateAndStore("user2", "pass2")
	
	// GetValid should return one of them (implementation returns first found)
	user, pass := manager.GetValid()
	if user == "" || pass == "" {
		t.Error("Expected non-empty credentials")
	}
	
	// Should be one of the stored pairs
	if !((user == "user1" && pass == "pass1") || (user == "user2" && pass == "pass2")) {
		t.Errorf("Expected user1/pass1 or user2/pass2, got %s/%s", user, pass)
	}
}

func TestClearInvalid(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":  "ok",
				"version": "1.15.0",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// Store credentials
	manager.ValidateAndStore("testuser", "testpass")
	
	// Verify credentials are stored
	user, pass := manager.GetValid()
	if user != "testuser" || pass != "testpass" {
		t.Errorf("Expected stored credentials testuser/testpass, got %s/%s", user, pass)
	}
	
	// Clear invalid credentials
	manager.ClearInvalid()
	
	// Verify credentials are cleared
	user, pass = manager.GetValid()
	if user != "" || pass != "" {
		t.Errorf("Expected cleared credentials, got %s/%s", user, pass)
	}
}

func TestClearInvalidEmpty(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	manager := New(logger, "http://localhost:4533")
	
	// Clear when no credentials are stored (should not panic)
	manager.ClearInvalid()
	
	// Verify still empty
	user, pass := manager.GetValid()
	if user != "" || pass != "" {
		t.Errorf("Expected empty credentials, got %s/%s", user, pass)
	}
}

func TestValidateURLFormat(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	var capturedURL string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		response := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":  "ok",
				"version": "1.15.0",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// Test URL format
	manager.ValidateAndStore("testuser", "testpass")
	
	expectedURL := "/rest/ping?c=subsoxy&f=json&p=testpass&u=testuser&v=1.15.0"
	if capturedURL != expectedURL {
		t.Errorf("Expected URL %s, got %s", expectedURL, capturedURL)
	}
}

func TestConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":  "ok",
				"version": "1.15.0",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// Test concurrent operations
	done := make(chan bool, 3)
	
	// Goroutine 1: Store credentials
	go func() {
		manager.ValidateAndStore("user1", "pass1")
		done <- true
	}()
	
	// Goroutine 2: Get credentials
	go func() {
		manager.GetValid()
		done <- true
	}()
	
	// Goroutine 3: Clear credentials
	go func() {
		manager.ClearInvalid()
		done <- true
	}()
	
	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		<-done
	}
	
	// Test should complete without deadlock or panic
}

func TestValidateInvalidResponseFormat(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	// Create mock server with invalid response structure
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"invalid-response": map[string]interface{}{
				"status": "ok",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()
	
	manager := New(logger, mockServer.URL)
	
	// Test invalid response format
	manager.ValidateAndStore("testuser", "testpass")
	
	// Verify no credentials were stored
	storedUser, storedPass := manager.GetValid()
	if storedUser != "" || storedPass != "" {
		t.Errorf("Expected no stored credentials, got %s/%s", storedUser, storedPass)
	}
}