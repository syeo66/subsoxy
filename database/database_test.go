package database

import "fmt"

import (
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

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
	
	err = db.StoreSongs("testuser", songs)
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
	
	err = db.StoreSongs("testuser", songs)
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
	
	err = db.StoreSongs("testuser", updatedSongs)
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
	
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Retrieve all songs
	retrievedSongs, err := db.GetAllSongs("testuser")
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
	
	songs, err := db.GetAllSongs("testuser")
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
	
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record a play event
	err = db.RecordPlayEvent("testuser", "1", "play", nil)
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
	
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record a skip event
	err = db.RecordPlayEvent("testuser", "1", "skip", nil)
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
	
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record play event with previous song
	previousSong := "1"
	err = db.RecordPlayEvent("testuser", "2", "play", &previousSong)
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
	
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record a transition (play)
	err = db.RecordTransition("testuser", "1", "2", "play")
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
	
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record a transition (skip)
	err = db.RecordTransition("testuser", "1", "2", "skip")
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
	
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Record multiple transitions
	err = db.RecordTransition("testuser", "1", "2", "play")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}
	
	err = db.RecordTransition("testuser", "1", "2", "play")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}
	
	err = db.RecordTransition("testuser", "1", "2", "skip")
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
	
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}
	
	// Test getting probability for non-existent transition (should return 0.5 and no error)
	prob, err := db.GetTransitionProbability("testuser", "1", "2")
	if err != nil {
		t.Errorf("Unexpected error for non-existent transition: %v", err)
	}
	if prob != 0.5 {
		t.Errorf("Expected default probability 0.5, got %f", prob)
	}
	
	// Record a transition and test again
	err = db.RecordTransition("testuser", "1", "2", "play")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}
	
	prob, err = db.GetTransitionProbability("testuser", "1", "2")
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
	prob, err := db.GetTransitionProbability("testuser", "nonexistent1", "nonexistent2")
	if err != nil {
		t.Errorf("Unexpected error for non-existent transition: %v", err)
	}
	if prob != 0.5 {
		t.Errorf("Expected default probability 0.5, got %f", prob)
	}

	// Test getting probability with empty strings (should return error)
	prob, err = db.GetTransitionProbability("testuser", "", "")
	if err == nil {
		t.Error("Expected error for empty song IDs")
	}
	if prob != 0.5 {
		t.Errorf("Expected default probability 0.5 on error, got %f", prob)
	}
}

// Connection Pool Tests

func TestDefaultPoolConfig(t *testing.T) {
	config := DefaultPoolConfig()
	
	if config.MaxOpenConns != 25 {
		t.Errorf("Expected MaxOpenConns 25, got %d", config.MaxOpenConns)
	}
	if config.MaxIdleConns != 5 {
		t.Errorf("Expected MaxIdleConns 5, got %d", config.MaxIdleConns)
	}
	if config.ConnMaxLifetime != 30*time.Minute {
		t.Errorf("Expected ConnMaxLifetime 30m, got %v", config.ConnMaxLifetime)
	}
	if config.ConnMaxIdleTime != 5*time.Minute {
		t.Errorf("Expected ConnMaxIdleTime 5m, got %v", config.ConnMaxIdleTime)
	}
	if !config.HealthCheck {
		t.Error("Expected HealthCheck to be true")
	}
}

func TestNewWithPool(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test_pool.db"
	defer os.Remove(dbPath)
	
	poolConfig := &ConnectionPool{
		MaxOpenConns:    10,
		MaxIdleConns:    3,
		ConnMaxLifetime: 15 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
		HealthCheck:     false, // Disable for test to avoid goroutine
	}
	
	db, err := NewWithPool(dbPath, logger, poolConfig)
	if err != nil {
		t.Fatalf("Failed to create database with pool: %v", err)
	}
	defer db.Close()
	
	if db.pool.MaxOpenConns != 10 {
		t.Errorf("Expected MaxOpenConns 10, got %d", db.pool.MaxOpenConns)
	}
	if db.pool.MaxIdleConns != 3 {
		t.Errorf("Expected MaxIdleConns 3, got %d", db.pool.MaxIdleConns)
	}
	if db.pool.ConnMaxLifetime != 15*time.Minute {
		t.Errorf("Expected ConnMaxLifetime 15m, got %v", db.pool.ConnMaxLifetime)
	}
}

func TestUpdatePoolConfig(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test_update.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Test valid config update
	newConfig := &ConnectionPool{
		MaxOpenConns:    15,
		MaxIdleConns:    7,
		ConnMaxLifetime: 20 * time.Minute,
		ConnMaxIdleTime: 3 * time.Minute,
		HealthCheck:     false,
	}
	
	err = db.UpdatePoolConfig(newConfig)
	if err != nil {
		t.Errorf("Failed to update pool config: %v", err)
	}
	
	if db.pool.MaxOpenConns != 15 {
		t.Errorf("Expected MaxOpenConns 15, got %d", db.pool.MaxOpenConns)
	}
	
	// Test invalid config (max idle > max open)
	invalidConfig := &ConnectionPool{
		MaxOpenConns: 10,
		MaxIdleConns: 15, // Invalid: > MaxOpenConns
	}
	
	err = db.UpdatePoolConfig(invalidConfig)
	if err == nil {
		t.Error("Expected error for invalid pool config")
	}
	
	// Test invalid config (zero max open)
	invalidConfig2 := &ConnectionPool{
		MaxOpenConns: 0, // Invalid
		MaxIdleConns: 1,
	}
	
	err = db.UpdatePoolConfig(invalidConfig2)
	if err == nil {
		t.Error("Expected error for zero max open connections")
	}
}

func TestGetConnectionStats(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test_stats.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	stats := db.GetConnectionStats()
	
	// Basic validation that stats are being tracked
	if stats.OpenConnections < 0 {
		t.Errorf("OpenConnections should not be negative, got %d", stats.OpenConnections)
	}
	if stats.IdleConnections < 0 {
		t.Errorf("IdleConnections should not be negative, got %d", stats.IdleConnections)
	}
}

func TestPerformHealthCheck(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test_health.db"
	defer os.Remove(dbPath)
	
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Perform health check manually
	db.performHealthCheck()
	
	stats := db.GetConnectionStats()
	if stats.HealthChecks == 0 {
		t.Error("Expected health check count to be > 0")
	}
	if stats.LastHealthCheck.IsZero() {
		t.Error("Expected LastHealthCheck to be set")
	}
}

func TestConcurrentDatabaseAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	dbPath := "test_concurrent.db"
	defer os.Remove(dbPath)
	
	// Use a smaller pool for testing concurrency
	poolConfig := &ConnectionPool{
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: 1 * time.Minute,
		ConnMaxIdleTime: 10 * time.Second,
		HealthCheck:     false, // Disable to avoid interfering with test
	}
	
	db, err := NewWithPool(dbPath, logger, poolConfig)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Store test songs first
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
		{ID: "3", Title: "Song 3", Artist: "Artist", Album: "Album", Duration: 280},
	}
	
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Fatalf("Failed to store test songs: %v", err)
	}
	
	// Run concurrent operations
	var wg sync.WaitGroup
	numGoroutines := 10
	operationsPerGoroutine := 5
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			
			for j := 0; j < operationsPerGoroutine; j++ {
				// Mix of different database operations
				switch j % 4 {
				case 0:
					// Record play event
					songID := fmt.Sprintf("%d", (goroutineID%3)+1)
					err := db.RecordPlayEvent("testuser", songID, "play", nil)
					if err != nil {
						t.Errorf("Goroutine %d: RecordPlayEvent failed: %v", goroutineID, err)
					}
				case 1:
					// Get all songs
					_, err := db.GetAllSongs("testuser")
					if err != nil {
						t.Errorf("Goroutine %d: GetAllSongs failed: %v", goroutineID, err)
					}
				case 2:
					// Record transition
					fromSong := fmt.Sprintf("%d", (goroutineID%3)+1)
					toSong := fmt.Sprintf("%d", ((goroutineID+1)%3)+1)
					err := db.RecordTransition("testuser", fromSong, toSong, "play")
					if err != nil {
						t.Errorf("Goroutine %d: RecordTransition failed: %v", goroutineID, err)
					}
				case 3:
					// Get transition probability
					fromSong := fmt.Sprintf("%d", (goroutineID%3)+1)
					toSong := fmt.Sprintf("%d", ((goroutineID+1)%3)+1)
					_, err := db.GetTransitionProbability("testuser", fromSong, toSong)
					if err != nil {
						t.Errorf("Goroutine %d: GetTransitionProbability failed: %v", goroutineID, err)
					}
				}
			}
		}(i)
	}
	
	// Wait for all goroutines to complete
	wg.Wait()
	
	// Verify that operations completed successfully
	allSongs, err := db.GetAllSongs("testuser")
	if err != nil {
		t.Errorf("Failed to get songs after concurrent test: %v", err)
	}
	if len(allSongs) != 3 {
		t.Errorf("Expected 3 songs after concurrent test, got %d", len(allSongs))
	}
	
	// Check that some play events were recorded
	totalPlayCount := 0
	for _, song := range allSongs {
		totalPlayCount += song.PlayCount
	}
	if totalPlayCount == 0 {
		t.Error("Expected some play events to be recorded during concurrent test")
	}
	
	t.Logf("Concurrent test completed successfully with %d total plays", totalPlayCount)
}

func TestGetSongCount(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	userID := "testuser"
	
	// Test with no songs
	count, err := db.GetSongCount(userID)
	if err != nil {
		t.Fatalf("Failed to get song count: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 songs, got %d", count)
	}
	
	// Add some songs
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist 1", Album: "Album 1", Duration: 180},
		{ID: "2", Title: "Song 2", Artist: "Artist 2", Album: "Album 2", Duration: 200},
		{ID: "3", Title: "Song 3", Artist: "Artist 3", Album: "Album 3", Duration: 220},
	}
	
	err = db.StoreSongs(userID, songs)
	if err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	
	// Test with songs
	count, err = db.GetSongCount(userID)
	if err != nil {
		t.Fatalf("Failed to get song count: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 songs, got %d", count)
	}
	
	// Test with empty user ID
	_, err = db.GetSongCount("")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}
}

func TestGetSongsBatch(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	userID := "testuser"
	
	// Add test songs
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist 1", Album: "Album 1", Duration: 180},
		{ID: "2", Title: "Song 2", Artist: "Artist 2", Album: "Album 2", Duration: 200},
		{ID: "3", Title: "Song 3", Artist: "Artist 3", Album: "Album 3", Duration: 220},
		{ID: "4", Title: "Song 4", Artist: "Artist 4", Album: "Album 4", Duration: 240},
		{ID: "5", Title: "Song 5", Artist: "Artist 5", Album: "Album 5", Duration: 260},
	}
	
	err = db.StoreSongs(userID, songs)
	if err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	
	// Test getting first batch
	batch, err := db.GetSongsBatch(userID, 2, 0)
	if err != nil {
		t.Fatalf("Failed to get songs batch: %v", err)
	}
	if len(batch) != 2 {
		t.Errorf("Expected 2 songs in batch, got %d", len(batch))
	}
	
	// Test getting second batch
	batch, err = db.GetSongsBatch(userID, 2, 2)
	if err != nil {
		t.Fatalf("Failed to get songs batch: %v", err)
	}
	if len(batch) != 2 {
		t.Errorf("Expected 2 songs in batch, got %d", len(batch))
	}
	
	// Test getting last batch (partial)
	batch, err = db.GetSongsBatch(userID, 2, 4)
	if err != nil {
		t.Fatalf("Failed to get songs batch: %v", err)
	}
	if len(batch) != 1 {
		t.Errorf("Expected 1 song in batch, got %d", len(batch))
	}
	
	// Test with empty user ID
	_, err = db.GetSongsBatch("", 2, 0)
	if err == nil {
		t.Error("Expected error for empty user ID")
	}
	
	// Test with invalid limit
	_, err = db.GetSongsBatch(userID, 0, 0)
	if err == nil {
		t.Error("Expected error for zero limit")
	}
	
	// Test with invalid offset
	_, err = db.GetSongsBatch(userID, 2, -1)
	if err == nil {
		t.Error("Expected error for negative offset")
	}
}

func TestGetTransitionProbabilities(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	userID := "testuser"
	fromSongID := "song1"
	toSongIDs := []string{"song2", "song3", "song4"}
	
	// Add some songs first
	songs := []models.Song{
		{ID: "song1", Title: "Song 1", Artist: "Artist 1", Album: "Album 1", Duration: 180},
		{ID: "song2", Title: "Song 2", Artist: "Artist 2", Album: "Album 2", Duration: 200},
		{ID: "song3", Title: "Song 3", Artist: "Artist 3", Album: "Album 3", Duration: 220},
		{ID: "song4", Title: "Song 4", Artist: "Artist 4", Album: "Album 4", Duration: 240},
	}
	
	err = db.StoreSongs(userID, songs)
	if err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	
	// Record some transitions
	err = db.RecordTransition(userID, fromSongID, "song2", "play")
	if err != nil {
		t.Fatalf("Failed to record transition: %v", err)
	}
	
	err = db.RecordTransition(userID, fromSongID, "song3", "skip")
	if err != nil {
		t.Fatalf("Failed to record transition: %v", err)
	}
	
	// Update probabilities - this is done automatically by RecordTransition
	// so we just need to trigger it by recording multiple transitions
	err = db.RecordTransition(userID, fromSongID, "song2", "play")
	if err != nil {
		t.Fatalf("Failed to record second transition: %v", err)
	}
	
	// Test getting batch probabilities
	probabilities, err := db.GetTransitionProbabilities(userID, fromSongID, toSongIDs)
	if err != nil {
		t.Fatalf("Failed to get transition probabilities: %v", err)
	}
	
	if len(probabilities) != 3 {
		t.Errorf("Expected 3 probabilities, got %d", len(probabilities))
	}
	
	// Check that we got probabilities for all requested songs
	for _, toSongID := range toSongIDs {
		if _, exists := probabilities[toSongID]; !exists {
			t.Errorf("Missing probability for song %s", toSongID)
		}
	}
	
	// song2 should have been played, so probability > 0.5
	if probabilities["song2"] <= 0.5 {
		t.Errorf("Expected probability > 0.5 for song2, got %f", probabilities["song2"])
	}
	
	// song3 should have been skipped, so probability < 0.5
	if probabilities["song3"] >= 0.5 {
		t.Errorf("Expected probability < 0.5 for song3, got %f", probabilities["song3"])
	}
	
	// song4 should have default probability of 0.5
	if probabilities["song4"] != 0.5 {
		t.Errorf("Expected probability 0.5 for song4, got %f", probabilities["song4"])
	}
	
	// Test with empty user ID
	_, err = db.GetTransitionProbabilities("", fromSongID, toSongIDs)
	if err == nil {
		t.Error("Expected error for empty user ID")
	}
	
	// Test with empty from song ID
	_, err = db.GetTransitionProbabilities(userID, "", toSongIDs)
	if err == nil {
		t.Error("Expected error for empty from song ID")
	}
	
	// Test with empty to song IDs
	probabilities, err = db.GetTransitionProbabilities(userID, fromSongID, []string{})
	if err != nil {
		t.Fatalf("Failed to get transition probabilities for empty list: %v", err)
	}
	if len(probabilities) != 0 {
		t.Errorf("Expected empty probabilities map, got %d entries", len(probabilities))
	}
}

func TestHealthCheckShutdown(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	// Create database with health check enabled
	poolConfig := &ConnectionPool{
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
		HealthCheck:     true,
	}
	
	db, err := NewWithPool(":memory:", logger, poolConfig)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	
	// Give the health check goroutine time to start
	time.Sleep(100 * time.Millisecond)
	
	// Verify shutdown channel is initialized
	if db.shutdownChan == nil {
		t.Error("Shutdown channel should be initialized")
	}
	
	// Close the database - this should signal the health check goroutine to stop
	err = db.Close()
	if err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}
	
	// Verify shutdown channel is closed
	select {
	case <-db.shutdownChan:
		// Channel is closed, which is expected
	default:
		t.Error("Shutdown channel should be closed after database close")
	}
	
	// Give time for goroutine to exit
	time.Sleep(200 * time.Millisecond)
	
	// Verify that the health check goroutine has stopped by checking that 
	// no new health checks are performed (this is implicit - if the goroutine
	// was still running, it would continue updating statistics)
	
	// Test that calling Close() multiple times doesn't panic
	err = db.Close()
	if err != nil {
		t.Fatalf("Second close should not return error: %v", err)
	}
}

func TestHealthCheckDisabled(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	// Create database with health check disabled
	poolConfig := &ConnectionPool{
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
		HealthCheck:     false,
	}
	
	db, err := NewWithPool(":memory:", logger, poolConfig)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	
	// Verify shutdown channel is still initialized even when health check is disabled
	if db.shutdownChan == nil {
		t.Error("Shutdown channel should be initialized even when health check is disabled")
	}
	
	// Close should work normally
	err = db.Close()
	if err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}
}