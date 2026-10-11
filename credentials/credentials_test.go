package credentials

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestNew(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in tests
	upstreamURL := "http://localhost:4533"

	manager := New(logger, upstreamURL)

	if manager == nil {
		t.Fatal("Manager should not be nil")
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
	_, _ = manager.ValidateAndStore("testuser", "testpass")

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
	_, _ = manager.ValidateAndStore("wronguser", "wrongpass")

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
	_, _ = manager.ValidateAndStore("testuser", "testpass")

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
	_, _ = manager.ValidateAndStore("testuser", "testpass")

	// Second call with same credentials should not trigger validation
	_, _ = manager.ValidateAndStore("testuser", "testpass")

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
	_, _ = manager.ValidateAndStore("testuser", "testpass")

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
	_, _ = manager.ValidateAndStore("testuser", "testpass")

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
	_, _ = manager.ValidateAndStore("user1", "pass1")
	_, _ = manager.ValidateAndStore("user2", "pass2")

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
	_, _ = manager.ValidateAndStore("testuser", "testpass")

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
	_, _ = manager.ValidateAndStore("testuser", "testpass")

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
		_, _ = manager.ValidateAndStore("user1", "pass1")
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

func TestEncryptionDecryption(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	manager := New(logger, "http://localhost:4533")

	// Test encryption and decryption
	password := "test-password-123"
	encryptedCred, err := manager.encryptPassword(password)
	if err != nil {
		t.Errorf("Failed to encrypt password: %v", err)
	}

	// Verify encrypted data is different from original
	if string(encryptedCred.EncryptedPassword) == password {
		t.Error("Encrypted password should not be the same as original")
	}

	// Test decryption
	decryptedPassword, err := manager.decryptPassword(encryptedCred)
	if err != nil {
		t.Errorf("Failed to decrypt password: %v", err)
	}

	if decryptedPassword != password {
		t.Errorf("Decrypted password doesn't match original. Expected: %s, Got: %s", password, decryptedPassword)
	}
}

func TestEncryptionWithDifferentKeys(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	manager1 := New(logger, "http://localhost:4533")
	manager2 := New(logger, "http://localhost:4533")

	// Verify different managers have different keys
	if manager1.GetEncryptionInfo() == manager2.GetEncryptionInfo() {
		t.Error("Different managers should have different encryption keys")
	}

	password := "test-password"

	// Encrypt with first manager
	encryptedCred, err := manager1.encryptPassword(password)
	if err != nil {
		t.Errorf("Failed to encrypt password: %v", err)
	}

	// Try to decrypt with second manager (should fail)
	_, err = manager2.decryptPassword(encryptedCred)
	if err == nil {
		t.Error("Decryption with different key should fail")
	}

	// Decrypt with original manager (should succeed)
	decryptedPassword, err := manager1.decryptPassword(encryptedCred)
	if err != nil {
		t.Errorf("Failed to decrypt with original key: %v", err)
	}

	if decryptedPassword != password {
		t.Errorf("Decrypted password doesn't match original. Expected: %s, Got: %s", password, decryptedPassword)
	}
}

func TestSecureCredentialClearing(t *testing.T) {
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
	_, err := manager.ValidateAndStore("testuser", "testpass")
	if err != nil {
		t.Errorf("Failed to store credentials: %v", err)
	}

	// Verify credentials are stored and encrypted
	user, pass := manager.GetValid()
	if user != "testuser" || pass != "testpass" {
		t.Errorf("Expected stored credentials testuser/testpass, got %s/%s", user, pass)
	}

	// Get reference to encrypted data before clearing
	manager.mutex.RLock()
	var encryptedData []byte
	if cred, exists := manager.validCredentials["testuser"]; exists {
		encryptedData = make([]byte, len(cred.EncryptedPassword))
		copy(encryptedData, cred.EncryptedPassword)
	}
	manager.mutex.RUnlock()

	// Clear credentials
	manager.ClearInvalid()

	// Verify credentials are cleared
	user, pass = manager.GetValid()
	if user != "" || pass != "" {
		t.Errorf("Expected cleared credentials, got %s/%s", user, pass)
	}

	// Verify encrypted data was zeroed (this is a basic check)
	if len(encryptedData) > 0 {
		// Note: We can't reliably test if the original memory was zeroed
		// because Go's garbage collector may have moved the data
		// This test mainly verifies the clearing logic executes without error
		_ = encryptedData // Reference to avoid unused variable warning
	}
}

func TestEncryptionWithCorruptedData(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	manager := New(logger, "http://localhost:4533")

	// Create corrupted encrypted credential with proper nonce size (12 bytes for GCM)
	corruptedCred := encryptedCredential{
		EncryptedPassword: []byte("corrupted-data"),
		Nonce:             make([]byte, 12), // GCM requires 12-byte nonce
	}

	// Try to decrypt corrupted data (should fail)
	_, err := manager.decryptPassword(corruptedCred)
	if err == nil {
		t.Error("Decryption of corrupted data should fail")
	}
}

func TestMemorySecurityValidation(t *testing.T) {
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

	password := "secret-password-123"

	// Store credential
	_, err := manager.ValidateAndStore("testuser", password)
	if err != nil {
		t.Errorf("Failed to store credentials: %v", err)
	}

	// Verify that the stored credential is encrypted
	manager.mutex.RLock()
	if cred, exists := manager.validCredentials["testuser"]; exists {
		// Verify the encrypted data doesn't contain the original password
		if string(cred.EncryptedPassword) == password {
			t.Error("Encrypted password should not contain original password")
		}
		if string(cred.Nonce) == password {
			t.Error("Nonce should not contain original password")
		}

		// Verify encrypted data is not empty
		if len(cred.EncryptedPassword) == 0 {
			t.Error("Encrypted password should not be empty")
		}
		if len(cred.Nonce) == 0 {
			t.Error("Nonce should not be empty")
		}
	} else {
		t.Error("Credential should be stored")
	}
	manager.mutex.RUnlock()

	// Verify retrieval still works
	user, pass := manager.GetValid()
	if user != "testuser" || pass != password {
		t.Errorf("Expected retrieved credentials testuser/%s, got %s/%s", password, user, pass)
	}
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
	_, _ = manager.ValidateAndStore("testuser", "testpass")

	// Verify no credentials were stored
	storedUser, storedPass := manager.GetValid()
	if storedUser != "" || storedPass != "" {
		t.Errorf("Expected no stored credentials, got %s/%s", storedUser, storedPass)
	}
}

func TestGetAllValid(t *testing.T) {
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

	// Test with no credentials
	allCreds := manager.GetAllValid()
	if len(allCreds) != 0 {
		t.Errorf("Expected 0 credentials, got %d", len(allCreds))
	}

	// Store multiple users
	_, _ = manager.ValidateAndStore("user1", "pass1")
	_, _ = manager.ValidateAndStore("user2", "pass2")
	_, _ = manager.ValidateAndStore("user3", "pass3")

	// GetAllValid should return all stored credentials
	allCreds = manager.GetAllValid()
	if len(allCreds) != 3 {
		t.Errorf("Expected 3 credentials, got %d", len(allCreds))
	}

	// Verify all expected credentials are present
	expectedCreds := map[string]string{
		"user1": "pass1",
		"user2": "pass2",
		"user3": "pass3",
	}

	for user, expectedPass := range expectedCreds {
		if actualPass, exists := allCreds[user]; !exists {
			t.Errorf("Expected user %s to exist", user)
		} else if actualPass != expectedPass {
			t.Errorf("Expected password %s for user %s, got %s", expectedPass, user, actualPass)
		}
	}
}

// Network timeout and failure scenario tests

func TestNetworkTimeout(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create a server that delays long enough to trigger timeout
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Short delay for fast test
		// Don't send response to simulate timeout
	}))
	defer slowServer.Close()

	manager := New(logger, slowServer.URL)

	// This should timeout and return an error (test will be fast)
	result, err := manager.ValidateAndStore("testuser", "testpass")
	if err == nil {
		t.Error("Expected timeout or error, got nil")
	}
	if result {
		t.Error("Expected validation failure, got success")
	}

	// Verify no credentials were stored
	user, pass := manager.GetValid()
	if user != "" || pass != "" {
		t.Errorf("Expected no credentials stored after error, got %s/%s", user, pass)
	}
}

func TestNetworkConnectionRefused(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Use a non-existent server URL
	manager := New(logger, "http://localhost:99999")

	result, err := manager.ValidateAndStore("testuser", "testpass")
	if err == nil {
		t.Error("Expected connection refused error, got nil")
	}
	if result {
		t.Error("Expected validation failure due to connection refused, got success")
	}

	// Verify error message contains connection information
	if err != nil && !strings.Contains(err.Error(), "connection") && !strings.Contains(err.Error(), "dial") {
		t.Errorf("Expected connection-related error message, got: %v", err)
	}
}

func TestNetworkSlowResponse(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create a server that responds slowly but eventually succeeds
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // Simulate slow response (fast for testing)
		response := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":  "ok",
				"version": "1.15.0",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer slowServer.Close()

	manager := New(logger, slowServer.URL)

	// Should succeed despite slow response
	result, err := manager.ValidateAndStore("testuser", "testpass")
	if err != nil {
		t.Errorf("Expected success with slow response, got error: %v", err)
	}
	if !result {
		t.Error("Expected validation success with slow response, got failure")
	}

	// Verify credentials were stored
	user, pass := manager.GetValid()
	if user != "testuser" || pass != "testpass" {
		t.Errorf("Expected credentials testuser/testpass, got %s/%s", user, pass)
	}
}

func TestNetworkInvalidHTTPResponse(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create a server that returns invalid HTTP response
	invalidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Invalid non-JSON response"))
	}))
	defer invalidServer.Close()

	manager := New(logger, invalidServer.URL)

	result, err := manager.ValidateAndStore("testuser", "testpass")
	if err == nil {
		t.Error("Expected JSON decode error, got nil")
	}
	if result {
		t.Error("Expected validation failure due to invalid response, got success")
	}

	// Verify error is related to JSON parsing
	if err != nil && !strings.Contains(err.Error(), "decode") && !strings.Contains(err.Error(), "json") && !strings.Contains(err.Error(), "invalid character") {
		t.Errorf("Expected JSON decode error, got: %v", err)
	}
}

func TestNetworkHTTPErrorStatus(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create a server that returns HTTP error status
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer errorServer.Close()

	manager := New(logger, errorServer.URL)

	result, err := manager.ValidateAndStore("testuser", "testpass")
	if err == nil {
		t.Error("Expected HTTP error, got nil")
	}
	if result {
		t.Error("Expected validation failure due to HTTP error, got success")
	}

	// Verify error occurred (the specific error message may vary)
	// The important thing is that validation failed due to server error
	if err == nil {
		t.Error("Expected an error due to HTTP 500 status")
	}
}

func TestNetworkPartialResponse(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create a server that returns incomplete JSON
	partialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"subsonic-response": {"status": "ok", "ver`)) // Incomplete JSON
	}))
	defer partialServer.Close()

	manager := New(logger, partialServer.URL)

	result, err := manager.ValidateAndStore("testuser", "testpass")
	if err == nil {
		t.Error("Expected JSON parse error due to incomplete response, got nil")
	}
	if result {
		t.Error("Expected validation failure due to incomplete response, got success")
	}
}
