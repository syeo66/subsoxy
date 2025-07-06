package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
		t.Error("Handler should not be nil")
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
	
	err = db.StoreSongs(testSongs)
	if err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	
	shuffleService := shuffle.New(db, logger)
	handler := New(logger, shuffleService)
	
	t.Run("Default size", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs", nil)
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
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?size=2", nil)
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
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?size=invalid", nil)
		w := httptest.NewRecorder()
		
		handled := handler.HandleShuffle(w, req, "/rest/getRandomSongs")
		
		if !handled {
			t.Error("HandleShuffle should return true (handled)")
		}
		
		// Should use default size of 50, but only return 3 songs (all available)
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		if err != nil {
			t.Errorf("Failed to decode response: %v", err)
		}
		
		subsonicResp := response["subsonic-response"].(map[string]interface{})
		songs := subsonicResp["songs"].(map[string]interface{})
		songList := songs["song"].([]interface{})
		
		if len(songList) != 3 {
			t.Errorf("Expected 3 songs with invalid size, got %d", len(songList))
		}
	})
	
	t.Run("Zero size", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?size=0", nil)
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
		req := httptest.NewRequest("GET", "/rest/getRandomSongs?size=-5", nil)
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
	
	req := httptest.NewRequest("GET", "/rest/getRandomSongs", nil)
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
		req := httptest.NewRequest("GET", "/rest/stream?id=123", nil)
		w := httptest.NewRecorder()
		
		var recordedSongID string
		var recordedEventType string
		var recordedPreviousSong *string
		
		recordFunc := func(songID, eventType string, previousSong *string) {
			recordedSongID = songID
			recordedEventType = eventType
			recordedPreviousSong = previousSong
		}
		
		handled := handler.HandleStream(w, req, "/rest/stream", recordFunc)
		
		if handled {
			t.Error("HandleStream should return false (not handled)")
		}
		
		if recordedSongID != "123" {
			t.Errorf("Expected recorded song ID '123', got '%s'", recordedSongID)
		}
		
		if recordedEventType != "start" {
			t.Errorf("Expected recorded event type 'start', got '%s'", recordedEventType)
		}
		
		if recordedPreviousSong != nil {
			t.Errorf("Expected no previous song, got %v", recordedPreviousSong)
		}
	})
	
	t.Run("Without song ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/stream", nil)
		w := httptest.NewRecorder()
		
		var recordCalled bool
		recordFunc := func(songID, eventType string, previousSong *string) {
			recordCalled = true
		}
		
		handled := handler.HandleStream(w, req, "/rest/stream", recordFunc)
		
		if handled {
			t.Error("HandleStream should return false (not handled)")
		}
		
		if recordCalled {
			t.Error("Record function should not be called without song ID")
		}
	})
	
	t.Run("With empty song ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/stream?id=", nil)
		w := httptest.NewRecorder()
		
		var recordCalled bool
		recordFunc := func(songID, eventType string, previousSong *string) {
			recordCalled = true
		}
		
		handled := handler.HandleStream(w, req, "/rest/stream", recordFunc)
		
		if handled {
			t.Error("HandleStream should return false (not handled)")
		}
		
		if recordCalled {
			t.Error("Record function should not be called with empty song ID")
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
		req := httptest.NewRequest("GET", "/rest/scrobble?id=123&submission=true", nil)
		w := httptest.NewRecorder()
		
		var recordedSongID string
		var recordedEventType string
		var recordedPreviousSong *string
		var lastPlayedSongID string
		
		recordFunc := func(songID, eventType string, previousSong *string) {
			recordedSongID = songID
			recordedEventType = eventType
			recordedPreviousSong = previousSong
		}
		
		setLastPlayedFunc := func(songID string) {
			lastPlayedSongID = songID
		}
		
		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc)
		
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
	
	t.Run("Skip event", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/scrobble?id=456&submission=false", nil)
		w := httptest.NewRecorder()
		
		var recordedSongID string
		var recordedEventType string
		var recordedPreviousSong *string
		var lastPlayedCalled bool
		
		recordFunc := func(songID, eventType string, previousSong *string) {
			recordedSongID = songID
			recordedEventType = eventType
			recordedPreviousSong = previousSong
		}
		
		setLastPlayedFunc := func(songID string) {
			lastPlayedCalled = true
		}
		
		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc)
		
		if handled {
			t.Error("HandleScrobble should return false (not handled)")
		}
		
		if recordedSongID != "456" {
			t.Errorf("Expected recorded song ID '456', got '%s'", recordedSongID)
		}
		
		if recordedEventType != "skip" {
			t.Errorf("Expected recorded event type 'skip', got '%s'", recordedEventType)
		}
		
		if recordedPreviousSong != nil {
			t.Errorf("Expected no previous song, got %v", recordedPreviousSong)
		}
		
		if lastPlayedCalled {
			t.Error("SetLastPlayed should not be called for skip events")
		}
	})
	
	t.Run("Without song ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/rest/scrobble?submission=true", nil)
		w := httptest.NewRecorder()
		
		var recordCalled bool
		var lastPlayedCalled bool
		
		recordFunc := func(songID, eventType string, previousSong *string) {
			recordCalled = true
		}
		
		setLastPlayedFunc := func(songID string) {
			lastPlayedCalled = true
		}
		
		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc)
		
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
		req := httptest.NewRequest("GET", "/rest/scrobble?id=&submission=true", nil)
		w := httptest.NewRecorder()
		
		var recordCalled bool
		var lastPlayedCalled bool
		
		recordFunc := func(songID, eventType string, previousSong *string) {
			recordCalled = true
		}
		
		setLastPlayedFunc := func(songID string) {
			lastPlayedCalled = true
		}
		
		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc)
		
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
		req := httptest.NewRequest("GET", "/rest/scrobble?id=789", nil)
		w := httptest.NewRecorder()
		
		var recordedSongID string
		var recordedEventType string
		var lastPlayedCalled bool
		
		recordFunc := func(songID, eventType string, previousSong *string) {
			recordedSongID = songID
			recordedEventType = eventType
		}
		
		setLastPlayedFunc := func(songID string) {
			lastPlayedCalled = true
		}
		
		handled := handler.HandleScrobble(w, req, "/rest/scrobble", recordFunc, setLastPlayedFunc)
		
		if handled {
			t.Error("HandleScrobble should return false (not handled)")
		}
		
		if recordedSongID != "789" {
			t.Errorf("Expected recorded song ID '789', got '%s'", recordedSongID)
		}
		
		if recordedEventType != "skip" {
			t.Errorf("Expected recorded event type 'skip', got '%s'", recordedEventType)
		}
		
		if lastPlayedCalled {
			t.Error("SetLastPlayed should not be called for skip events")
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
	
	req := httptest.NewRequest("GET", "/rest/getRandomSongs", nil)
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