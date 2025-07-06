package database

import (
	"database/sql"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/models"
)

func TestNew(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in tests
	
	// Test with valid database path
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	if db.conn == nil {
		t.Error("Database connection should not be nil")
	}
	if db.logger == nil {
		t.Error("Database logger should not be nil")
	}
}

func TestNewWithInvalidPath(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	// Test with invalid database path (directory that doesn't exist)
	dbPath := "/nonexistent/path/test.db"
	
	_, err := New(dbPath, logger)
	if err == nil {
		t.Error("Expected error when creating database with invalid path")
	}
}

func TestClose(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	
	err = db.Close()
	if err != nil {
		t.Errorf("Failed to close database: %v", err)
	}
	
	// Test that database is actually closed
	err = db.conn.Ping()
	if err == nil {
		t.Error("Database should be closed")
	}
}

func TestCreateTables(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Check that tables were created
	tables := []string{"songs", "play_events", "song_transitions"}
	for _, table := range tables {
		var count int
		err := db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
		if err != nil {
			t.Errorf("Failed to check table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("Table %s should exist", table)
		}
	}
	
	// Check that indexes were created
	indexes := []string{"idx_play_events_song_id", "idx_play_events_timestamp", "idx_song_transitions_from"}
	for _, index := range indexes {
		var count int
		err := db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&count)
		if err != nil {
			t.Errorf("Failed to check index %s: %v", index, err)
		}
		if count != 1 {
			t.Errorf("Index %s should exist", index)
		}
	}
}

func TestStoreSongs(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	songs := []models.Song{
		{
			ID:       "1",
			Title:    "Test Song 1",
			Artist:   "Test Artist 1",
			Album:    "Test Album 1",
			Duration: 300,
		},
		{
			ID:       "2",
			Title:    "Test Song 2",
			Artist:   "Test Artist 2",
			Album:    "Test Album 2",
			Duration: 250,
		},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Verify songs were stored
	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM songs").Scan(&count)
	if err != nil {
		t.Errorf("Failed to count songs: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 songs, got %d", count)
	}
}

func TestStoreSongsReplace(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// First, store a song
	songs := []models.Song{
		{
			ID:       "1",
			Title:    "Original Title",
			Artist:   "Original Artist",
			Album:    "Original Album",
			Duration: 300,
		},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Update the song with new information
	updatedSongs := []models.Song{
		{
			ID:       "1",
			Title:    "Updated Title",
			Artist:   "Updated Artist",
			Album:    "Updated Album",
			Duration: 350,
		},
	}
	
	err = db.StoreSongs(updatedSongs)
	if err != nil {
		t.Errorf("Failed to update songs: %v", err)
	}
	
	// Verify song was updated
	var title string
	err = db.conn.QueryRow("SELECT title FROM songs WHERE id = ?", "1").Scan(&title)
	if err != nil {
		t.Errorf("Failed to query updated song: %v", err)
	}
	if title != "Updated Title" {
		t.Errorf("Expected 'Updated Title', got '%s'", title)
	}
}

func TestGetAllSongs(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Store test songs
	songs := []models.Song{
		{
			ID:       "1",
			Title:    "Test Song 1",
			Artist:   "Test Artist 1",
			Album:    "Test Album 1",
			Duration: 300,
		},
		{
			ID:       "2",
			Title:    "Test Song 2",
			Artist:   "Test Artist 2",
			Album:    "Test Album 2",
			Duration: 250,
		},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Retrieve all songs
	retrievedSongs, err := db.GetAllSongs()
	if err != nil {
		t.Errorf("Failed to get all songs: %v", err)
	}
	
	if len(retrievedSongs) != 2 {
		t.Errorf("Expected 2 songs, got %d", len(retrievedSongs))
	}
	
	// Verify song data
	for i, song := range retrievedSongs {
		if song.ID != songs[i].ID {
			t.Errorf("Song %d ID mismatch: expected %s, got %s", i, songs[i].ID, song.ID)
		}
		if song.Title != songs[i].Title {
			t.Errorf("Song %d Title mismatch: expected %s, got %s", i, songs[i].Title, song.Title)
		}
		if song.PlayCount != 0 {
			t.Errorf("Song %d PlayCount should be 0, got %d", i, song.PlayCount)
		}
		if song.SkipCount != 0 {
			t.Errorf("Song %d SkipCount should be 0, got %d", i, song.SkipCount)
		}
	}
}

func TestGetAllSongsEmpty(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	songs, err := db.GetAllSongs()
	if err != nil {
		t.Errorf("Failed to get all songs: %v", err)
	}
	
	if len(songs) != 0 {
		t.Errorf("Expected 0 songs, got %d", len(songs))
	}
}

func TestRecordPlayEvent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Store a test song first
	songs := []models.Song{
		{
			ID:       "1",
			Title:    "Test Song",
			Artist:   "Test Artist",
			Album:    "Test Album",
			Duration: 300,
		},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record a play event
	err = db.RecordPlayEvent("1", "play", nil)
	if err != nil {
		t.Errorf("Failed to record play event: %v", err)
	}
	
	// Verify event was recorded
	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM play_events WHERE song_id = ? AND event_type = ?", "1", "play").Scan(&count)
	if err != nil {
		t.Errorf("Failed to count play events: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 play event, got %d", count)
	}
	
	// Verify play count was updated
	var playCount int
	err = db.conn.QueryRow("SELECT play_count FROM songs WHERE id = ?", "1").Scan(&playCount)
	if err != nil {
		t.Errorf("Failed to get play count: %v", err)
	}
	if playCount != 1 {
		t.Errorf("Expected play count 1, got %d", playCount)
	}
}

func TestRecordSkipEvent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Store a test song first
	songs := []models.Song{
		{
			ID:       "1",
			Title:    "Test Song",
			Artist:   "Test Artist",
			Album:    "Test Album",
			Duration: 300,
		},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record a skip event
	err = db.RecordPlayEvent("1", "skip", nil)
	if err != nil {
		t.Errorf("Failed to record skip event: %v", err)
	}
	
	// Verify skip count was updated
	var skipCount int
	err = db.conn.QueryRow("SELECT skip_count FROM songs WHERE id = ?", "1").Scan(&skipCount)
	if err != nil {
		t.Errorf("Failed to get skip count: %v", err)
	}
	if skipCount != 1 {
		t.Errorf("Expected skip count 1, got %d", skipCount)
	}
}

func TestRecordPlayEventWithPreviousSong(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Store test songs
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record play event with previous song
	previousSong := "1"
	err = db.RecordPlayEvent("2", "play", &previousSong)
	if err != nil {
		t.Errorf("Failed to record play event with previous song: %v", err)
	}
	
	// Verify previous song was recorded
	var recordedPreviousSong sql.NullString
	err = db.conn.QueryRow("SELECT previous_song FROM play_events WHERE song_id = ?", "2").Scan(&recordedPreviousSong)
	if err != nil {
		t.Errorf("Failed to get previous song: %v", err)
	}
	if !recordedPreviousSong.Valid || recordedPreviousSong.String != "1" {
		t.Errorf("Expected previous song '1', got %v", recordedPreviousSong)
	}
}

func TestRecordTransition(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Store test songs
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record a transition (play)
	err = db.RecordTransition("1", "2", "play")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}
	
	// Verify transition was recorded
	var playCount int
	err = db.conn.QueryRow("SELECT play_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?", "1", "2").Scan(&playCount)
	if err != nil {
		t.Errorf("Failed to get transition play count: %v", err)
	}
	if playCount != 1 {
		t.Errorf("Expected transition play count 1, got %d", playCount)
	}
}

func TestRecordTransitionSkip(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Store test songs
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record a transition (skip)
	err = db.RecordTransition("1", "2", "skip")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}
	
	// Verify skip count was recorded
	var skipCount int
	err = db.conn.QueryRow("SELECT skip_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?", "1", "2").Scan(&skipCount)
	if err != nil {
		t.Errorf("Failed to get transition skip count: %v", err)
	}
	if skipCount != 1 {
		t.Errorf("Expected transition skip count 1, got %d", skipCount)
	}
}

func TestUpdateTransitionProbabilities(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Store test songs
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record multiple transitions
	err = db.RecordTransition("1", "2", "play")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}
	
	err = db.RecordTransition("1", "2", "play")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}
	
	err = db.RecordTransition("1", "2", "skip")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}
	
	// Verify probability calculation (2 plays, 1 skip = 2/3 = 0.6667)
	var probability float64
	err = db.conn.QueryRow("SELECT probability FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?", "1", "2").Scan(&probability)
	if err != nil {
		t.Errorf("Failed to get transition probability: %v", err)
	}
	
	expected := float64(2) / float64(3)
	if probability < expected-0.01 || probability > expected+0.01 {
		t.Errorf("Expected transition probability %.4f, got %.4f", expected, probability)
	}
}

func TestGetTransitionProbability(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Store test songs
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
	}
	
	err = db.StoreSongs(songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Test getting probability for non-existent transition (should return 0.5 and no error)
	prob, err := db.GetTransitionProbability("1", "2")
	if err != nil {
		t.Errorf("Unexpected error for non-existent transition: %v", err)
	}
	if prob != 0.5 {
		t.Errorf("Expected default probability 0.5, got %f", prob)
	}
	
	// Record a transition and test again
	err = db.RecordTransition("1", "2", "play")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}
	
	prob, err = db.GetTransitionProbability("1", "2")
	if err != nil {
		t.Errorf("Failed to get transition probability: %v", err)
	}
	if prob != 1.0 {
		t.Errorf("Expected probability 1.0, got %f", prob)
	}
}

func TestGetTransitionProbabilityNonExistent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Test getting probability for non-existent songs (should return 0.5 and no error)
	prob, err := db.GetTransitionProbability("nonexistent1", "nonexistent2")
	if err != nil {
		t.Errorf("Unexpected error for non-existent transition: %v", err)
	}
	if prob != 0.5 {
		t.Errorf("Expected default probability 0.5, got %f", prob)
	}

	// Test getting probability with empty strings (should return error)
	prob, err = db.GetTransitionProbability("", "")
	if err == nil {
		t.Error("Expected error for empty song IDs")
	}
	if prob != 0.5 {
		t.Errorf("Expected default probability 0.5 on error, got %f", prob)
	}
}