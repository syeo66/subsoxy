package handlers

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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

		status, code, message := decodeSubsonicStatus(t, w)
		if status != "failed" || code != SubsonicErrorGeneric {
			t.Errorf("Expected failed response with code %d, got status %q code %d", SubsonicErrorGeneric, status, code)
		}
		if message != "Invalid size parameter" {
			t.Errorf("Expected error message about invalid size parameter, got: %q", message)
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

		status, code, _ := decodeSubsonicStatus(t, w)
		if status != "failed" || code != SubsonicErrorMissingParameter {
			t.Errorf("Expected failed response with code %d, got status %q code %d", SubsonicErrorMissingParameter, status, code)
		}
	})
}

// decodeSubsonicStatus checks that a response is a JSON Subsonic envelope sent with HTTP 200
// and returns its status and, for a failed response, the error code and message
func decodeSubsonicStatus(t *testing.T, w *httptest.ResponseRecorder) (status string, code int, message string) {
	t.Helper()

	if w.Code != http.StatusOK {
		t.Errorf("Expected HTTP status %d, got %d", http.StatusOK, w.Code)
	}

	var response struct {
		SubsonicResponse struct {
			Status string `json:"status"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"subsonic-response"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to decode response %q: %v", w.Body.String(), err)
	}

	status = response.SubsonicResponse.Status
	if response.SubsonicResponse.Error != nil {
		code = response.SubsonicResponse.Error.Code
		message = response.SubsonicResponse.Error.Message
	}
	return status, code, message
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
		var recordedAt time.Time
		var lastPlayedSongID string

		recordFunc := func(userID, songID, eventType string, at time.Time) {
			recordedSongID = songID
			recordedEventType = eventType
			recordedAt = at
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedSongID = songID
		}

		processScrobbleFunc := func(userID, client, songID string, isSubmission bool) bool {
			// Mock processing of pending songs
			return true // Allow recording
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

		if time.Since(recordedAt) > time.Minute {
			t.Errorf("Expected the play to be recorded now without a time parameter, got %v", recordedAt)
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

		recordFunc := func(userID, songID, eventType string, at time.Time) {
			recordCalled = true
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedCalled = true
		}

		processScrobbleFunc := func(userID, client, songID string, isSubmission bool) bool {
			// Mock processing of pending songs
			return true // Allow recording
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

		recordFunc := func(userID, songID, eventType string, at time.Time) {
			recordCalled = true
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedCalled = true
		}

		processScrobbleFunc := func(userID, client, songID string, isSubmission bool) bool {
			// Mock processing of pending songs
			return true // Allow recording
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

		recordFunc := func(userID, songID, eventType string, at time.Time) {
			recordCalled = true
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedCalled = true
		}

		processScrobbleFunc := func(userID, client, songID string, isSubmission bool) bool {
			// Mock processing of pending songs
			return true // Allow recording
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

		recordFunc := func(userID, songID, eventType string, at time.Time) {
			recordCalled = true
		}

		setLastPlayedFunc := func(userID, songID string) {
			lastPlayedCalled = true
		}

		processScrobbleFunc := func(userID, client, songID string, isSubmission bool) bool {
			// Mock processing of pending songs
			return true // Allow recording
		}

		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc, processScrobbleFunc)

		if handled {
			t.Error("HandleScrobble should return false (not handled)")
		}

		if !recordCalled {
			t.Error("Record function should be called without submission parameter, which defaults to true")
		}

		if !lastPlayedCalled {
			t.Error("SetLastPlayed should be called without submission parameter, which defaults to true")
		}
	})

	t.Run("Several songs with play times", func(t *testing.T) {
		first := time.Now().Add(-2 * time.Hour).Truncate(time.Millisecond)
		future := time.Now().Add(time.Hour)
		req := httptest.NewRequest("POST", "/rest/scrobble", strings.NewReader(url.Values{
			"u":          {"testuser"},
			"id":         {"a", "b", "c"},
			"time":       {strconv.FormatInt(first.UnixMilli(), 10), strconv.FormatInt(future.UnixMilli(), 10)},
			"submission": {"true"},
		}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		recorded := map[string]time.Time{}
		recordFunc := func(userID, songID, eventType string, at time.Time) {
			recorded[songID] = at
		}

		handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, func(string, string) {}, func(string, string, string, bool) bool { return true })

		if len(recorded) != 3 {
			t.Fatalf("Expected 3 recorded plays, got %d", len(recorded))
		}
		if !recorded["a"].Equal(first) {
			t.Errorf("Expected song a to be recorded at %v, got %v", first, recorded["a"])
		}
		// A time in the future is clamped to now, a missing one defaults to now
		for _, id := range []string{"b", "c"} {
			if recorded[id].After(time.Now()) || time.Since(recorded[id]) > time.Minute {
				t.Errorf("Expected song %s to be recorded now, got %v", id, recorded[id])
			}
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
		expectedStatus string
		shouldHandle   bool
	}{
		{
			name:           "Zero size",
			sizeParam:      "0",
			expectedStatus: "ok",
			shouldHandle:   true,
		},
		{
			name:           "Negative size",
			sizeParam:      "-1",
			expectedStatus: "ok", // Negative sizes are handled gracefully
			shouldHandle:   true,
		},
		{
			name:           "Very large size",
			sizeParam:      "999999",
			expectedStatus: "ok", // Very large sizes are served at the maximum
			shouldHandle:   true,
		},
		{
			name:           "Non-numeric size",
			sizeParam:      "abc",
			expectedStatus: "failed",
			shouldHandle:   true,
		},
		{
			name:           "Float size",
			sizeParam:      "10.5",
			expectedStatus: "failed",
			shouldHandle:   true,
		},
		{
			name:           "Empty size",
			sizeParam:      "",
			expectedStatus: "ok", // Should use default
			shouldHandle:   true,
		},
		{
			name:           "Size with leading zeros",
			sizeParam:      "0010",
			expectedStatus: "ok",
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

			if status, _, _ := decodeSubsonicStatus(t, w); status != tt.expectedStatus {
				t.Errorf("Expected status %q, got %q", tt.expectedStatus, status)
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
		expectedStatus string
	}{
		{
			name:           "Empty user",
			userParam:      "",
			expectedStatus: "failed",
		},
		{
			name:           "Very long username",
			userParam:      strings.Repeat("a", 1000),
			expectedStatus: "ok",
		},
		{
			name:           "Unicode username",
			userParam:      "用户名",
			expectedStatus: "ok",
		},
		{
			name:           "Special characters in username",
			userParam:      "user@domain.com",
			expectedStatus: "ok",
		},
		{
			name:           "Username with control characters",
			userParam:      "user\x01\x02",
			expectedStatus: "ok",
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

			if status, _, _ := decodeSubsonicStatus(t, w); status != tt.expectedStatus {
				t.Errorf("Expected status %q, got %q", tt.expectedStatus, status)
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

func TestHandleDebugEscapesOutputAndOmitsPassword(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	songs := []models.Song{
		{ID: `1"><b>id</b>`, Title: "<script>alert(1)</script>", Artist: "<img src=x onerror=alert(2)>", Album: "A & B", Duration: 180},
	}
	if err := db.StoreSongs("testuser", songs); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	handler := New(logger, shuffle.New(db, logger))

	req := httptest.NewRequest("GET", "/debug?u=testuser&p=hunter2", nil)
	w := httptest.NewRecorder()
	if !handler.HandleDebug(w, req, "/debug") {
		t.Fatal("HandleDebug should handle the request")
	}

	body := w.Body.String()
	for _, raw := range []string{"<script>", "<img", "<b>id</b>"} {
		if strings.Contains(body, raw) {
			t.Errorf("debug page contains unescaped %q", raw)
		}
	}
	for _, escaped := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "&lt;img src=x onerror=alert(2)&gt;", "A &amp; B"} {
		if !strings.Contains(body, escaped) {
			t.Errorf("debug page missing escaped %q", escaped)
		}
	}
	if strings.Contains(body, "hunter2") {
		t.Error("debug page must not echo the password")
	}
}

func TestHandleShuffleXMLError(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_xml_error.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	handler := New(logger, shuffle.New(db, logger))

	req := httptest.NewRequest("GET", "/rest/getRandomSongs?f=xml", nil)
	w := httptest.NewRecorder()

	handler.HandleShuffle(w, req, "/rest/getRandomSongs")

	if w.Code != http.StatusOK {
		t.Errorf("Expected HTTP status %d, got %d", http.StatusOK, w.Code)
	}
	if contentType := w.Header().Get("Content-Type"); contentType != "application/xml" {
		t.Errorf("Expected Content-Type application/xml, got %s", contentType)
	}

	var xmlResponse models.XMLSubsonicResponse
	if err := xml.Unmarshal(w.Body.Bytes(), &xmlResponse); err != nil {
		t.Fatalf("Failed to parse XML response: %v", err)
	}
	if xmlResponse.Status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", xmlResponse.Status)
	}
	if xmlResponse.Songs != nil {
		t.Error("Expected no songs element in a failed response")
	}
	if xmlResponse.Error == nil || xmlResponse.Error.Code != SubsonicErrorMissingParameter {
		t.Errorf("Expected error code %d, got %+v", SubsonicErrorMissingParameter, xmlResponse.Error)
	}
}

func TestHandleShuffleFilters(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	db, err := database.New(t.TempDir()+"/test.db", logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	err = db.StoreSongs("testuser", []models.Song{
		{ID: "rock85", Title: "a", Artist: "x", Album: "y", Genre: "Rock", Year: 1985},
		{ID: "rock95", Title: "b", Artist: "x", Album: "y", Genre: "Rock", Year: 1995},
		{ID: "jazz85", Title: "c", Artist: "x", Album: "y", Genre: "Jazz", Year: 1985},
	})
	if err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	handler := New(logger, shuffle.New(db, logger))

	// shuffle returns the song IDs of a successful response, or the error message of a failed one
	shuffle := func(t *testing.T, query string) (bool, []string, string) {
		t.Helper()
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser&"+query, nil)
		w := httptest.NewRecorder()
		if !handler.HandleShuffle(w, req, "/rest/getRandomSongs") {
			return false, nil, ""
		}

		var response struct {
			SubsonicResponse struct {
				Status string `json:"status"`
				Songs  struct {
					Song []models.Song `json:"song"`
				} `json:"songs"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"subsonic-response"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		var ids []string
		for _, song := range response.SubsonicResponse.Songs.Song {
			ids = append(ids, song.ID)
		}
		slices.Sort(ids)
		return true, ids, response.SubsonicResponse.Error.Message
	}

	tests := []struct {
		name    string
		query   string
		want    []string
		wantErr string
	}{
		{"genre", "genre=rock", []string{"rock85", "rock95"}, ""},
		{"year range", "fromYear=1980&toYear=1989", []string{"jazz85", "rock85"}, ""},
		{"reversed year range", "fromYear=1989&toYear=1980", []string{"jazz85", "rock85"}, ""},
		{"genre and year", "genre=Rock&fromYear=1990", []string{"rock95"}, ""},
		{"invalid fromYear", "fromYear=eighties", nil, "Invalid fromYear parameter"},
		{"invalid toYear", "toYear=x", nil, "Invalid toYear parameter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handled, ids, errMsg := shuffle(t, tt.query)
			if !handled {
				t.Fatal("Expected the request to be handled locally")
			}
			if errMsg != tt.wantErr {
				t.Errorf("Expected error %q, got %q", tt.wantErr, errMsg)
			}
			if !slices.Equal(ids, tt.want) {
				t.Errorf("Expected songs %v, got %v", tt.want, ids)
			}
		})
	}

	t.Run("music folder is forwarded", func(t *testing.T) {
		if handled, _, _ := shuffle(t, "musicFolderId=1"); handled {
			t.Error("Expected a music folder request to be forwarded to upstream")
		}
	})
}

// TestHandleShuffleResponseFields checks that songs are returned with the Subsonic Child
// fields clients use, and without the play statistics kept for weighting
func TestHandleShuffleResponseFields(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_response_fields.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	if err := db.StoreSongs("testuser", []models.Song{{
		ID: "1", Title: "Song 1", Artist: "Artist 1", Album: "Album 1", Duration: 300,
		AlbumID: "al-1", ArtistID: "ar-1", Track: 3, DiscNumber: 1,
		Suffix: "flac", ContentType: "audio/flac", BitRate: 900, Size: 12345678,
	}}); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	if err := db.RecordPlayEvent("testuser", "1", "play"); err != nil {
		t.Fatalf("Failed to record play: %v", err)
	}

	handler := New(logger, shuffle.New(db, logger))

	t.Run("JSON", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser&f=json", nil)
		w := httptest.NewRecorder()
		handler.HandleShuffle(w, req, "/rest/getRandomSongs")

		var response struct {
			SubsonicResponse struct {
				Songs struct {
					Song []map[string]any `json:"song"`
				} `json:"songs"`
			} `json:"subsonic-response"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if len(response.SubsonicResponse.Songs.Song) != 1 {
			t.Fatalf("Expected 1 song, got %d", len(response.SubsonicResponse.Songs.Song))
		}
		song := response.SubsonicResponse.Songs.Song[0]

		want := map[string]any{
			"id": "1", "isDir": false, "title": "Song 1", "albumId": "al-1", "artistId": "ar-1",
			"track": 3.0, "discNumber": 1.0, "suffix": "flac", "contentType": "audio/flac",
			"bitRate": 900.0, "size": 12345678.0, "duration": 300.0, "type": "music",
		}
		for key, value := range want {
			if song[key] != value {
				t.Errorf("Field %s = %v, want %v", key, song[key], value)
			}
		}
		for _, key := range []string{"lastPlayed", "lastSkipped", "playCount", "skipCount", "adjustedPlays", "adjustedSkips", "name"} {
			if _, present := song[key]; present {
				t.Errorf("Internal field %s should not be in the response", key)
			}
		}
	})

	t.Run("XML", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?u=testuser&f=xml", nil)
		w := httptest.NewRecorder()
		handler.HandleShuffle(w, req, "/rest/getRandomSongs")

		body := w.Body.String()
		for _, attr := range []string{`albumId="al-1"`, `suffix="flac"`, `type="music"`, `isDir="false"`} {
			if !strings.Contains(body, attr) {
				t.Errorf("Expected %s in XML response: %s", attr, body)
			}
		}
		if strings.Contains(body, "lastPlayed") || strings.Contains(body, "adjustedPlays") {
			t.Errorf("Internal fields should not be in the XML response: %s", body)
		}
	})
}
