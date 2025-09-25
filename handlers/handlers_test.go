package handlers

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/database"
	"github.com/syeo66/subsoxy/models"
	"github.com/syeo66/subsoxy/shuffle"
)

func TestNew(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)

	handler := New(logger, shuffleService)

	if handler == nil {
		t.Fatal("Handler should not be nil")
	}
	if handler.logger != logger {
		t.Error("Logger should be set correctly")
	}
	if handler.shuffle != shuffleService {
		t.Error("Shuffle service should be set correctly")
	}
}

func TestHandleShuffle(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Store test songs
	testSongs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist 1", Album: "Album 1", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist 2", Album: "Album 2", Duration: 250},
		{ID: "3", Title: "Song 3", Artist: "Artist 3", Album: "Album 3", Duration: 200},
	}

	err = db.StoreSongs("testuser", testSongs)
	if err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	t.Run("Default size", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser", nil)
		w := httptest.NewRecorder()

		handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")

		if !handled {
			t.Error("HandleShuffle should return true (handled)")
		}

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		if err != nil {
			t.Errorf("Failed to decode response: %v", err)
		}

		subsonicResp, ok := response["subsonic-response"].(map[string]interface{})
		if !ok {
			t.Error("Response should contain subsonic-response")
		}

		if subsonicResp["status"] != "ok" {
			t.Errorf("Expected status 'ok', got %v", subsonicResp["status"])
		}

		if subsonicResp["version"] != "1.15.0" {
			t.Errorf("Expected version '1.15.0', got %v", subsonicResp["version"])
		}

		songs, ok := subsonicResp["songs"].(map[string]interface{})
		if !ok {
			t.Error("Response should contain songs")
		}

		songList, ok := songs["song"].([]interface{})
		if !ok {
			t.Error("Songs should contain song array")
		}

		if len(songList) != 3 {
			t.Errorf("Expected 3 songs, got %d", len(songList))
		}
	})

	t.Run("Custom size", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser&size=2", nil)
		w := httptest.NewRecorder()

		handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")

		if !handled {
			t.Error("HandleShuffle should return true (handled)")
		}

		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		if err != nil {
			t.Errorf("Failed to decode response: %v", err)
		}

		subsonicResp := response["subsonic-response"].(map[string]interface{})
		songs := subsonicResp["songs"].(map[string]interface{})
		songList := songs["song"].([]interface{})

		if len(songList) != 2 {
			t.Errorf("Expected 2 songs, got %d", len(songList))
		}
	})

	t.Run("Invalid size", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser&size=invalid", nil)
		w := httptest.NewRecorder()

		handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")

		if !handled {
			t.Error("HandleShuffle should return true (handled)")
		}

		// Should return HTTP 400 error for invalid size parameter
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Invalid size parameter") {
			t.Errorf("Expected error message about invalid size parameter, got: %s", body)
		}
	})

	t.Run("Zero size", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser&size=0", nil)
		w := httptest.NewRecorder()

		handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")

		if !handled {
			t.Error("HandleShuffle should return true (handled)")
		}

		// Should use default size since 0 is not > 0
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		if err != nil {
			t.Errorf("Failed to decode response: %v", err)
		}

		subsonicResp := response["subsonic-response"].(map[string]interface{})
		songs := subsonicResp["songs"].(map[string]interface{})
		songList := songs["song"].([]interface{})

		if len(songList) != 3 {
			t.Errorf("Expected 3 songs with zero size, got %d", len(songList))
		}
	})

	t.Run("Negative size", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser&size=-5", nil)
		w := httptest.NewRecorder()

		handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")

		if !handled {
			t.Error("HandleShuffle should return true (handled)")
		}

		// Should use default size since -5 is not > 0
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		if err != nil {
			t.Errorf("Failed to decode response: %v", err)
		}

		subsonicResp := response["subsonic-response"].(map[string]interface{})
		songs := subsonicResp["songs"].(map[string]interface{})
		songList := songs["song"].([]interface{})

		if len(songList) != 3 {
			t.Errorf("Expected 3 songs with negative size, got %d", len(songList))
		}
	})

	t.Run("Missing user parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs", nil)
		w := httptest.NewRecorder()

		handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")

		if !handled {
			t.Error("HandleShuffle should return true when user parameter is missing")
		}

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleShuffleEmpty(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser", nil)
	w := httptest.NewRecorder()

	handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")

	if !handled {
		t.Error("HandleShuffle should return true (handled)")
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	err = json.NewDecoder(w.Body).Decode(&response)
	if err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	subsonicResp := response["subsonic-response"].(map[string]interface{})
	songs := subsonicResp["songs"].(map[string]interface{})
	songList := songs["song"].([]interface{})

	if len(songList) != 0 {
		t.Errorf("Expected 0 songs from empty database, got %d", len(songList))
	}
}

func TestHandlePing(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	req := httptest.NewRequest("GET", "/rest/ping", nil)
	w := httptest.NewRecorder()

	handled := handler.HandlePing(w, req, "/rest/ping")

	if handled {
		t.Error("HandlePing should return false (not handled)")
	}

	// Should not write any response
	if w.Code != 200 {
		t.Errorf("Expected default status 200, got %d", w.Code)
	}
}

func TestHandleGetLicense(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	req := httptest.NewRequest("GET", "/rest/getLicense", nil)
	w := httptest.NewRecorder()

	handled := handler.HandleGetLicense(w, req, "/rest/getLicense")

	if handled {
		t.Error("HandleGetLicense should return false (not handled)")
	}

	// Should not write any response
	if w.Code != 200 {
		t.Errorf("Expected default status 200, got %d", w.Code)
	}
}

func TestHandleStream(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	t.Run("With song ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/stream?u=testuser&id=123", nil)
		w := httptest.NewRecorder()

		handled := handler.HandleStream(w, req, "/rest/stream")

		if handled {
			t.Error("HandleStream should return false (not handled)")
		}
	})

	t.Run("Without song ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/stream?u=testuser", nil)
		w := httptest.NewRecorder()

		handled := handler.HandleStream(w, req, "/rest/stream")

		if handled {
			t.Error("HandleStream should return false (not handled)")
		}
	})

	t.Run("With empty song ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/stream?u=testuser&id=", nil)
		w := httptest.NewRecorder()

		handled := handler.HandleStream(w, req, "/rest/stream")

		if handled {
			t.Error("HandleStream should return false (not handled)")
		}
	})
}

func TestHandleScrobble(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	t.Run("Play event", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/scrobble?u=testuser&id=123&submission=true", nil)
		w := httptest.NewRecorder()

		var recordedSongID string
		var recordedEventType string
		var recordedPreviousSong *string
		var lastPlayedSongID string

		recordFunc := func(userID, songID, eventType string, previousSong *string) {
			recordedSongID = songID
			recordedEventType = eventType
			recordedPreviousSong = previousSong
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedSongID = songID
		}

		processScrobbleFunc := func(userID, songID string, isSubmission bool) {
			// Mock processing of pending songs
		}

		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc, processScrobbleFunc)

		if handled {
			t.Error("HandleScrobble should return false (not handled)")
		}

		if recordedSongID != "123" {
			t.Errorf("Expected recorded song ID '123', got '%s'", recordedSongID)
		}

		if recordedEventType != "play" {
			t.Errorf("Expected recorded event type 'play', got '%s'", recordedEventType)
		}

		if recordedPreviousSong != nil {
			t.Errorf("Expected no previous song, got %v", recordedPreviousSong)
		}

		if lastPlayedSongID != "123" {
			t.Errorf("Expected last played song ID '123', got '%s'", lastPlayedSongID)
		}
	})

	t.Run("Song ended without play threshold (submission=false)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/scrobble?u=testuser&id=456&submission=false", nil)
		w := httptest.NewRecorder()

		var recordCalled bool
		var lastPlayedCalled bool

		recordFunc := func(userID, songID, eventType string, previousSong *string) {
			recordCalled = true
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedCalled = true
		}

		processScrobbleFunc := func(userID, songID string, isSubmission bool) {
			// Mock processing of pending songs
		}

		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc, processScrobbleFunc)

		if handled {
			t.Error("HandleScrobble should return false (not handled)")
		}

		if recordCalled {
			t.Error("Record function should not be called for submission=false (song ended but didn't meet play threshold)")
		}

		if lastPlayedCalled {
			t.Error("SetLastPlayed should not be called for submission=false")
		}
	})

	t.Run("Without song ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/scrobble?u=testuser&submission=true", nil)
		w := httptest.NewRecorder()

		var recordCalled bool
		var lastPlayedCalled bool

		recordFunc := func(userID, songID, eventType string, previousSong *string) {
			recordCalled = true
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedCalled = true
		}

		processScrobbleFunc := func(userID, songID string, isSubmission bool) {
			// Mock processing of pending songs
		}

		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc, processScrobbleFunc)

		if handled {
			t.Error("HandleScrobble should return false (not handled)")
		}

		if recordCalled {
			t.Error("Record function should not be called without song ID")
		}

		if lastPlayedCalled {
			t.Error("SetLastPlayed should not be called without song ID")
		}
	})

	t.Run("With empty song ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/scrobble?u=testuser&id=&submission=true", nil)
		w := httptest.NewRecorder()

		var recordCalled bool
		var lastPlayedCalled bool

		recordFunc := func(userID, songID, eventType string, previousSong *string) {
			recordCalled = true
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedCalled = true
		}

		processScrobbleFunc := func(userID, songID string, isSubmission bool) {
			// Mock processing of pending songs
		}

		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc, processScrobbleFunc)

		if handled {
			t.Error("HandleScrobble should return false (not handled)")
		}

		if recordCalled {
			t.Error("Record function should not be called with empty song ID")
		}

		if lastPlayedCalled {
			t.Error("SetLastPlayed should not be called with empty song ID")
		}
	})

	t.Run("Without submission parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/scrobble?u=testuser&id=789", nil)
		w := httptest.NewRecorder()

		var recordCalled bool
		var lastPlayedCalled bool

		recordFunc := func(userID, songID, eventType string, previousSong *string) {
			recordCalled = true
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedCalled = true
		}

		processScrobbleFunc := func(userID, songID string, isSubmission bool) {
			// Mock processing of pending songs
		}

		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc, processScrobbleFunc)

		if handled {
			t.Error("HandleScrobble should return false (not handled)")
		}

		if recordCalled {
			t.Error("Record function should not be called without submission parameter (song ended but didn't meet play threshold)")
		}

		if lastPlayedCalled {
			t.Error("SetLastPlayed should not be called without submission parameter")
		}
	})
}

func TestHandleShuffleContentType(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser", nil)
	w := httptest.NewRecorder()

	handler.HandleShuffle(w, req, "/rest/getRandomSongs")

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}
}

func TestHandleShuffleWithURLParams(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	// Test with multiple query parameters
	reqURL := "/rest/getRandomSongs?size=10&v=1.15.0&u=testuser&p=testpass&c=testclient&f=json"
	req := httptest.NewRequest("GET", reqURL, nil)
	w := httptest.NewRecorder()

	handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")

	if !handled {
		t.Error("HandleShuffle should return true (handled)")
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// Input validation boundary condition tests

func TestValidateSongIDBoundaryConditions(t *testing.T) {
	tests := []struct {
		name     string
		songID   string
		expected bool
	}{
		{
			name:     "Empty string",
			songID:   "",
			expected: false,
		},
		{
			name:     "Single character",
			songID:   "a",
			expected: true,
		},
		{
			name:     "Max length (255 chars)",
			songID:   strings.Repeat("a", 255),
			expected: true,
		},
		{
			name:     "Over max length (256 chars)",
			songID:   strings.Repeat("a", 256),
			expected: false,
		},
		{
			name:     "Much over max length (1000 chars)",
			songID:   strings.Repeat("a", 1000),
			expected: false,
		},
		{
			name:     "Special characters",
			songID:   "song-123_abc.mp3",
			expected: true,
		},
		{
			name:     "Unicode characters",
			songID:   "歌曲-123",
			expected: true,
		},
		{
			name:     "Control characters",
			songID:   "song\x01\x02\x03",
			expected: true, // ValidateSongID doesn't filter control chars, only length
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateSongID(tt.songID) == nil
			if result != tt.expected {
				t.Errorf("ValidateSongID(%q) = %v, expected %v", tt.songID, result, tt.expected)
			}
		})
	}
}

func TestSanitizeForLoggingBoundaryConditions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Normal text",
			input:    "normal text",
			expected: "normal text",
		},
		{
			name:     "All control characters",
			input:    "\x00\x01\x02\x1F\x7F",
			expected: "",
		},
		{
			name:     "Mixed control and normal",
			input:    "hello\x00world\x01test",
			expected: "helloworldtest",
		},
		{
			name:     "Newline and tab",
			input:    "line1\nline2\ttest",
			expected: "line1line2test",
		},
		{
			name:     "Very long string with control chars",
			input:    strings.Repeat("a", 500) + "\x01\x02" + strings.Repeat("b", 500),
			expected: strings.Repeat("a", 500) + strings.Repeat("b", 500),
		},
		{
			name:     "Unicode characters (should be preserved)",
			input:    "测试文本🎵",
			expected: "测试文本🎵",
		},
		{
			name:     "Only whitespace (should be preserved)",
			input:    "   \t   ",
			expected: "      ", // tabs converted to spaces, regular spaces preserved
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeForLogging(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeForLogging(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShuffleSizeBoundaryConditions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_boundary.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	tests := []struct {
		name           string
		sizeParam      string
		expectedStatus int
		shouldHandle   bool
	}{
		{
			name:           "Zero size",
			sizeParam:      "0",
			expectedStatus: http.StatusOK,
			shouldHandle:   true,
		},
		{
			name:           "Negative size",
			sizeParam:      "-1",
			expectedStatus: http.StatusOK, // Negative sizes are handled gracefully
			shouldHandle:   true,
		},
		{
			name:           "Very large size",
			sizeParam:      "999999",
			expectedStatus: http.StatusBadRequest, // Very large sizes are rejected
			shouldHandle:   true,
		},
		{
			name:           "Non-numeric size",
			sizeParam:      "abc",
			expectedStatus: http.StatusBadRequest,
			shouldHandle:   true,
		},
		{
			name:           "Float size",
			sizeParam:      "10.5",
			expectedStatus: http.StatusBadRequest,
			shouldHandle:   true,
		},
		{
			name:           "Empty size",
			sizeParam:      "",
			expectedStatus: http.StatusOK, // Should use default
			shouldHandle:   true,
		},
		{
			name:           "Size with leading zeros",
			sizeParam:      "0010",
			expectedStatus: http.StatusOK,
			shouldHandle:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL := "/rest/getRandomSongs?u=testuser&p=testpass"
			if tt.sizeParam != "" {
				reqURL += "&size=" + tt.sizeParam
			}

			req := httptest.NewRequest("GET", reqURL, nil)
			w := httptest.NewRecorder()

			handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")

			if handled != tt.shouldHandle {
				t.Errorf("Expected handled=%v, got %v", tt.shouldHandle, handled)
			}

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestHandlerUserParameterBoundaryConditions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_user_boundary.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	tests := []struct {
		name           string
		userParam      string
		expectedStatus int
	}{
		{
			name:           "Empty user",
			userParam:      "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Very long username",
			userParam:      strings.Repeat("a", 1000),
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Unicode username",
			userParam:      "用户名",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Special characters in username",
			userParam:      "user@domain.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Username with control characters",
			userParam:      "user\x01\x02",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL := "/rest/getRandomSongs?p=testpass&size=10"
			if tt.userParam != "" {
				reqURL += "&u=" + url.QueryEscape(tt.userParam)
			}

			req := httptest.NewRequest("GET", reqURL, nil)
			w := httptest.NewRecorder()

			handler.HandleShuffle(w, req, "/rest/getRandomSongs")

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestHandleShuffleXMLFormat(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_xml.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Add test songs to the database
	testSongs := []models.Song{
		{ID: "1", Title: "Test Song 1", Artist: "Test Artist 1", Album: "Test Album 1", Duration: 180},
		{ID: "2", Title: "Test Song 2", Artist: "Test Artist 2", Album: "Test Album 2", Duration: 240},
	}

	for _, song := range testSongs {
		err := db.StoreSongs("testuser", []models.Song{song})
		if err != nil {
			t.Fatalf("Failed to store test song: %v", err)
		}
	}

	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)

	tests := []struct {
		name           string
		formatParam    string
		expectedType   string
		expectedStatus int
	}{
		{
			name:           "JSON format (default)",
			formatParam:    "",
			expectedType:   "application/json",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "JSON format (explicit)",
			formatParam:    "json",
			expectedType:   "application/json",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "XML format",
			formatParam:    "xml",
			expectedType:   "application/xml",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL := "/rest/getRandomSongs?u=testuser&size=2"
			if tt.formatParam != "" {
				reqURL += "&f=" + tt.formatParam
			}

			req := httptest.NewRequest("GET", reqURL, nil)
			w := httptest.NewRecorder()

			handler.HandleShuffle(w, req, "/rest/getRandomSongs")

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != tt.expectedType {
				t.Errorf("Expected Content-Type %s, got %s", tt.expectedType, contentType)
			}

			// Verify response can be parsed correctly
			if tt.formatParam == "xml" {
				var xmlResponse models.XMLSubsonicResponse
				err := xml.Unmarshal(w.Body.Bytes(), &xmlResponse)
				if err != nil {
					t.Errorf("Failed to parse XML response: %v", err)
				}

				if xmlResponse.Status != "ok" {
					t.Errorf("Expected status 'ok', got '%s'", xmlResponse.Status)
				}

				if xmlResponse.Version != "1.15.0" {
					t.Errorf("Expected version '1.15.0', got '%s'", xmlResponse.Version)
				}

				if xmlResponse.Songs == nil {
					t.Error("Expected songs element to be present")
				}
			} else {
				// Test JSON response
				var jsonResponse map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &jsonResponse)
				if err != nil {
					t.Errorf("Failed to parse JSON response: %v", err)
				}

				subsonicResponse, ok := jsonResponse["subsonic-response"].(map[string]interface{})
				if !ok {
					t.Error("Expected subsonic-response object")
				}

				if status, ok := subsonicResponse["status"].(string); !ok || status != "ok" {
					t.Errorf("Expected status 'ok', got '%v'", subsonicResponse["status"])
				}
			}
		})
	}
}
