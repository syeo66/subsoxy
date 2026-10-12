package credentials

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/syeo66/subsoxy/errors"
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
	storedUser, storedPass := onlyCredential(t, manager)
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
	storedUser, storedPass := onlyCredential(t, manager)
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
	storedUser, storedPass := onlyCredential(t, manager)
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
	storedUser, storedPass := onlyCredential(t, manager)
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
	storedUser, storedPass := onlyCredential(t, manager)
	if storedUser != "" || storedPass != "" {
		t.Errorf("Expected no stored credentials, got %s/%s", storedUser, storedPass)
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
		manager.GetAllValid()
		done <- true
	}()

	// Goroutine 3: Check credential freshness
	go func() {
		manager.HasRecent("user1", time.Hour)
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
	user, pass := onlyCredential(t, manager)
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
	storedUser, storedPass := onlyCredential(t, manager)
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
	user, pass := onlyCredential(t, manager)
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
	user, pass := onlyCredential(t, manager)
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

// newPingCounter returns an upstream that accepts every ping and counts them
func newPingCounter(t *testing.T) (*httptest.Server, func() int) {
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{"status": "ok", "version": "1.15.0"},
		})
	}))
	t.Cleanup(server.Close)
	return server, func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

func TestValidateAndStoreRemembersEarlierSecrets(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	upstream, calls := newPingCounter(t)
	manager := New(logger, upstream.URL)

	// Two devices of the same user, each with its own token and salt
	for _, secret := range []string{"TOKEN:a:salt1", "TOKEN:b:salt2", "TOKEN:a:salt1", "TOKEN:b:salt2"} {
		if _, err := manager.ValidateAndStore("alice", secret); err != nil {
			t.Fatalf("ValidateAndStore(%q) failed: %v", secret, err)
		}
	}

	if got := calls(); got != 2 {
		t.Errorf("Expected 2 upstream validations, got %d", got)
	}
}

func TestValidateAndStoreRevalidatesExpiredSecret(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	upstream, calls := newPingCounter(t)
	manager := New(logger, upstream.URL)

	if isNew, _ := manager.ValidateAndStore("alice", "secret"); !isNew {
		t.Fatal("First validation should report new credentials")
	}

	key := manager.secretKey("alice", "secret")
	manager.verifiedSecrets["alice"][key] = time.Now().Add(-time.Second)

	isNew, err := manager.ValidateAndStore("alice", "secret")
	if err != nil {
		t.Fatalf("Revalidation failed: %v", err)
	}
	if isNew {
		t.Error("Revalidating a known user should not report new credentials")
	}
	if got := calls(); got != 2 {
		t.Errorf("Expected an expired secret to be validated again, got %d upstream calls", got)
	}
}

func TestVerifiedSecretsAreBoundedPerUser(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	upstream, _ := newPingCounter(t)
	manager := New(logger, upstream.URL)

	for i := 0; i < MaxVerifiedSecretsPerUser*2; i++ {
		if _, err := manager.ValidateAndStore("alice", "TOKEN:tok:salt"+strconv.Itoa(i)); err != nil {
			t.Fatalf("ValidateAndStore failed: %v", err)
		}
	}

	if got := len(manager.verifiedSecrets["alice"]); got != MaxVerifiedSecretsPerUser {
		t.Errorf("Expected %d remembered secrets, got %d", MaxVerifiedSecretsPerUser, got)
	}
	last := "TOKEN:tok:salt" + strconv.Itoa(MaxVerifiedSecretsPerUser*2-1)
	if !manager.isVerified("alice", manager.secretKey("alice", last)) {
		t.Error("The most recent secret should still be remembered")
	}
}

func TestHasRecent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	upstream, _ := newPingCounter(t)
	manager := New(logger, upstream.URL)

	if manager.HasRecent("alice", time.Hour) {
		t.Error("Unknown user should not have recent credentials")
	}

	_, _ = manager.ValidateAndStore("alice", "secret")
	if !manager.HasRecent("alice", time.Hour) {
		t.Error("Just validated credentials should be recent")
	}

	cred := manager.validCredentials["alice"]
	cred.ValidatedAt = time.Now().Add(-2 * time.Hour)
	manager.validCredentials["alice"] = cred
	if manager.HasRecent("alice", time.Hour) {
		t.Error("Credentials validated two hours ago should not be recent")
	}
}

func TestGet(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	upstream, _ := newPingCounter(t)
	manager := New(logger, upstream.URL)

	_, _ = manager.ValidateAndStore("alice", "secret")
	_, _ = manager.ValidateAndStore("bob", "hunter2")

	if password, ok := manager.Get("alice"); !ok || password != "secret" {
		t.Errorf("Get(alice) = %q, %v; want secret, true", password, ok)
	}
	if _, ok := manager.Get("carol"); ok {
		t.Error("Get should report unknown users as missing")
	}
}

// onlyCredential returns the single stored credential, or empty strings when none is stored
func onlyCredential(t *testing.T, cm *Manager) (string, string) {
	t.Helper()
	all := cm.GetAllValid()
	if len(all) > 1 {
		t.Fatalf("Expected at most one stored credential, got %d", len(all))
	}
	for username, password := range all {
		return username, password
	}
	return "", ""
}

func TestGetAllValidEmpty(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	manager := New(logger, "http://localhost:4533")

	if all := manager.GetAllValid(); len(all) != 0 {
		t.Errorf("Expected no credentials, got %d", len(all))
	}
}

func TestGetAllValidMultipleUsers(t *testing.T) {
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

	_, _ = manager.ValidateAndStore("user1", "pass1")
	_, _ = manager.ValidateAndStore("user2", "pass2")

	all := manager.GetAllValid()
	if len(all) != 2 || all["user1"] != "pass1" || all["user2"] != "pass2" {
		t.Errorf("Expected user1/pass1 and user2/pass2, got %v", all)
	}
}

// newAuthUpstream counts pings and accepts only the password "right". With serverError set,
// it answers HTTP 500 instead.
func newAuthUpstream(t *testing.T, serverError bool) (*httptest.Server, func() int) {
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		if serverError {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		status := "failed"
		if r.URL.Query().Get("p") == "right" {
			status = "ok"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{"status": status, "version": "1.15.0"},
		})
	}))
	t.Cleanup(server.Close)
	return server, func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

func TestValidateAndStoreRemembersRejectedSecret(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	upstream, calls := newAuthUpstream(t, false)
	manager := New(logger, upstream.URL)

	for i := 0; i < 3; i++ {
		_, err := manager.ValidateAndStore("alice", "wrong")
		if !errors.IsCode(err, errors.ErrInvalidCredentials.Code) {
			t.Fatalf("Attempt %d: expected invalid credentials error, got %v", i+1, err)
		}
	}
	if got := calls(); got != 1 {
		t.Errorf("Expected a rejected secret to be checked upstream once, got %d calls", got)
	}

	// The rejection is tied to that secret, so the right password still gets through
	if isNew, err := manager.ValidateAndStore("alice", "right"); err != nil || !isNew {
		t.Errorf("Expected the correct password to be accepted as new, got isNew=%v err=%v", isNew, err)
	}
}

func TestValidateAndStoreRevalidatesExpiredRejection(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	upstream, calls := newAuthUpstream(t, false)
	manager := New(logger, upstream.URL)

	manager.ValidateAndStore("alice", "wrong")
	manager.rejectedSecrets[manager.secretKey("alice", "wrong")] = time.Now().Add(-time.Second)
	manager.ValidateAndStore("alice", "wrong")

	if got := calls(); got != 2 {
		t.Errorf("Expected an expired rejection to be checked upstream again, got %d calls", got)
	}
}

func TestValidateAndStoreDoesNotRememberUpstreamFailures(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	upstream, calls := newAuthUpstream(t, true)
	manager := New(logger, upstream.URL)

	manager.ValidateAndStore("alice", "right")
	manager.ValidateAndStore("alice", "right")

	if got := calls(); got != 2 {
		t.Errorf("Expected upstream errors not to be cached as rejections, got %d calls", got)
	}
	if len(manager.rejectedSecrets) != 0 {
		t.Errorf("Expected no remembered rejections, got %d", len(manager.rejectedSecrets))
	}
}

func TestRejectedSecretsAreBounded(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	manager := New(logger, "http://localhost:4533")

	now := time.Now()
	for i := 0; i < MaxRejectedSecrets*2; i++ {
		manager.rememberRejected(manager.secretKey("user"+strconv.Itoa(i), "wrong"), now.Add(time.Duration(i)*time.Millisecond))
	}

	if got := len(manager.rejectedSecrets); got != MaxRejectedSecrets {
		t.Errorf("Expected %d remembered rejections, got %d", MaxRejectedSecrets, got)
	}
	last := manager.secretKey("user"+strconv.Itoa(MaxRejectedSecrets*2-1), "wrong")
	if !manager.isRejected(last) {
		t.Error("The most recent rejection should still be remembered")
	}
}
