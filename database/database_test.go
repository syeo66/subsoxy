package database

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"strings"
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

func TestNewConfiguresSQLiteForConcurrency(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	db, err := New(t.TempDir()+"/test.db", logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	var journalMode string
	if err := db.conn.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("Failed to read journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("Expected journal_mode wal, got %q", journalMode)
	}

	var busyTimeout int
	if err := db.conn.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("Failed to read busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("Expected busy_timeout 5000, got %d", busyTimeout)
	}
}

func TestSQLiteDSN(t *testing.T) {
	if got := sqliteDSN("subsoxy.db"); got != "subsoxy.db?"+sqliteConnParams {
		t.Errorf("Unexpected DSN for plain path: %q", got)
	}
	if got := sqliteDSN("file:subsoxy.db?cache=shared"); got != "file:subsoxy.db?cache=shared&"+sqliteConnParams {
		t.Errorf("Unexpected DSN for path with parameters: %q", got)
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
	tables := []string{"songs", "play_events"}
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
	indexes := []string{"idx_play_events_song_id", "idx_play_events_timestamp"}
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

	// song_transitions is no longer part of the schema
	var count int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name LIKE '%song_transitions%'").Scan(&count); err != nil {
		t.Fatalf("Failed to check for song_transitions: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected no song_transitions table or indexes, found %d objects", count)
	}
}

func TestDropSongTransitionsTableMigration(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_drop_transitions.db"
	defer os.Remove(dbPath)

	// Simulate a database created by a version that still tracked transitions
	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	legacyQueries := []string{
		`CREATE TABLE song_transitions (
			user_id TEXT NOT NULL,
			from_song_id TEXT NOT NULL,
			to_song_id TEXT NOT NULL,
			play_count INTEGER DEFAULT 0,
			skip_count INTEGER DEFAULT 0,
			probability REAL DEFAULT 0.0,
			PRIMARY KEY (user_id, from_song_id, to_song_id)
		)`,
		`CREATE INDEX idx_song_transitions_user_id ON song_transitions(user_id)`,
		`CREATE INDEX idx_song_transitions_from ON song_transitions(from_song_id)`,
		`INSERT INTO song_transitions (user_id, from_song_id, to_song_id, play_count) VALUES ('testuser', '1', '2', 3)`,
		`CREATE TABLE song_transitions_backup AS SELECT * FROM song_transitions`,
	}
	for _, query := range legacyQueries {
		if _, err := db.conn.Exec(query); err != nil {
			t.Fatalf("Failed to set up legacy schema: %v", err)
		}
	}
	if err := db.StoreSongs("testuser", []models.Song{{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300}}); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	db.Close()

	// Reopening runs the migrations, twice to verify they are idempotent
	for i := 0; i < 2; i++ {
		db, err = New(dbPath, logger)
		if err != nil {
			t.Fatalf("Failed to reopen database (run %d): %v", i+1, err)
		}

		var count int
		if err := db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name LIKE '%song_transitions%'").Scan(&count); err != nil {
			t.Fatalf("Failed to check for song_transitions: %v", err)
		}
		if count != 0 {
			t.Errorf("Run %d: expected song_transitions table, backup and indexes to be dropped, found %d objects", i+1, count)
		}

		// Other data must survive the migration
		songs, err := db.GetAllSongs("testuser")
		if err != nil {
			t.Fatalf("Failed to get songs: %v", err)
		}
		if len(songs) != 1 {
			t.Errorf("Run %d: expected 1 song after migration, got %d", i+1, len(songs))
		}
		db.Close()
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

	// Perform health check manually; it only pings and logs, so it must not disturb the database
	db.performHealthCheck()

	if _, err := db.GetAllSongs("testuser"); err != nil {
		t.Errorf("Database unusable after health check: %v", err)
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
					// Record skip event
					songID := fmt.Sprintf("%d", ((goroutineID+1)%3)+1)
					err := db.RecordPlayEvent("testuser", songID, "skip", nil)
					if err != nil {
						t.Errorf("Goroutine %d: RecordPlayEvent (skip) failed: %v", goroutineID, err)
					}
				case 3:
					// Get existing song IDs
					_, err := db.GetExistingSongIDs("testuser")
					if err != nil {
						t.Errorf("Goroutine %d: GetExistingSongIDs failed: %v", goroutineID, err)
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

// Note: The existing TestConcurrentDatabaseAccess already provides comprehensive
// concurrent testing. Additional concurrent tests were removed to avoid complexity
// with table creation in the test environment.

func TestGetSongsBatchFiltered(t *testing.T) {
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

	// None of the songs has been played, so every song is older than any cutoff
	cutoff := time.Now()

	// Page through by the last id of each batch
	var got []string
	afterID := ""
	for _, want := range []int{2, 2, 1, 0} {
		batch, err := db.GetSongsBatchFiltered(userID, 2, afterID, cutoff)
		if err != nil {
			t.Fatalf("Failed to get songs batch: %v", err)
		}
		if len(batch) != want {
			t.Fatalf("Expected %d songs in batch after %q, got %d", want, afterID, len(batch))
		}
		for _, song := range batch {
			got = append(got, song.ID)
		}
		if len(batch) > 0 {
			afterID = batch[len(batch)-1].ID
		}
	}
	if fmt.Sprint(got) != "[1 2 3 4 5]" {
		t.Errorf("Expected every song once in id order, got %v", got)
	}

	// Test with empty user ID
	_, err = db.GetSongsBatchFiltered("", 2, "", cutoff)
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	// Test with invalid limit
	_, err = db.GetSongsBatchFiltered(userID, 0, "", cutoff)
	if err == nil {
		t.Error("Expected error for zero limit")
	}
}

// A song added before the current page during a sync must not shift later pages, which
// paging by offset would do, returning a song twice
func TestGetSongsBatchFilteredStableUnderInsert(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	userID := "testuser"
	songs := []models.Song{
		{ID: "b", Title: "B", Artist: "A", Album: "A", Duration: 180},
		{ID: "c", Title: "C", Artist: "A", Album: "A", Duration: 180},
		{ID: "d", Title: "D", Artist: "A", Album: "A", Duration: 180},
	}
	if err := db.StoreSongs(userID, songs); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	cutoff := time.Now()

	first, err := db.GetSongsBatchFiltered(userID, 2, "", cutoff)
	if err != nil || len(first) != 2 {
		t.Fatalf("Expected 2 songs in first batch, got %d (err %v)", len(first), err)
	}

	if err := db.StoreSongs(userID, []models.Song{{ID: "a", Title: "A", Artist: "A", Album: "A", Duration: 180}}); err != nil {
		t.Fatalf("Failed to store song: %v", err)
	}

	second, err := db.GetSongsBatchFiltered(userID, 2, first[len(first)-1].ID, cutoff)
	if err != nil {
		t.Fatalf("Failed to get songs batch: %v", err)
	}
	if len(second) != 1 || second[0].ID != "d" {
		t.Errorf("Expected only song d in second batch, got %v", second)
	}
}

func TestSongsUserIDIndexAvoidsSort(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	rows, err := db.conn.Query(`EXPLAIN QUERY PLAN SELECT id FROM songs WHERE user_id = ? AND id > ? ORDER BY id LIMIT 10`, "u", "")
	if err != nil {
		t.Fatalf("Failed to explain query: %v", err)
	}
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("Failed to scan plan: %v", err)
		}
		plan = append(plan, detail)
	}
	joined := strings.Join(plan, "; ")
	if !strings.Contains(joined, "idx_songs_user_id_id") || strings.Contains(joined, "TEMP B-TREE") {
		t.Errorf("Expected an index range scan without sorting, got plan: %s", joined)
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

func TestGetExistingSongIDs(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	userID := "testuser"
	songs := []models.Song{
		{ID: "song1", Title: "Test Song 1", Artist: "Test Artist", Album: "Test Album", Duration: 180},
		{ID: "song2", Title: "Test Song 2", Artist: "Test Artist", Album: "Test Album", Duration: 200},
		{ID: "song3", Title: "Test Song 3", Artist: "Test Artist", Album: "Test Album", Duration: 220},
	}

	// Store initial songs
	err = db.StoreSongs(userID, songs)
	if err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	// Test getting existing song IDs
	existingIDs, err := db.GetExistingSongIDs(userID)
	if err != nil {
		t.Fatalf("Failed to get existing song IDs: %v", err)
	}

	// Verify all songs are present
	if len(existingIDs) != 3 {
		t.Errorf("Expected 3 existing song IDs, got %d", len(existingIDs))
	}

	expectedIDs := []string{"song1", "song2", "song3"}
	for _, id := range expectedIDs {
		if !existingIDs[id] {
			t.Errorf("Expected song ID %s to exist", id)
		}
	}

	// Test with empty user ID
	_, err = db.GetExistingSongIDs("")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	// Test with non-existent user
	emptyIDs, err := db.GetExistingSongIDs("nonexistent")
	if err != nil {
		t.Fatalf("Failed to get existing song IDs for non-existent user: %v", err)
	}
	if len(emptyIDs) != 0 {
		t.Errorf("Expected 0 song IDs for non-existent user, got %d", len(emptyIDs))
	}
}

func TestDeleteSongs(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	userID := "testuser"
	songs := []models.Song{
		{ID: "song1", Title: "Test Song 1", Artist: "Test Artist", Album: "Test Album", Duration: 180},
		{ID: "song2", Title: "Test Song 2", Artist: "Test Artist", Album: "Test Album", Duration: 200},
		{ID: "song3", Title: "Test Song 3", Artist: "Test Artist", Album: "Test Album", Duration: 220},
		{ID: "song4", Title: "Test Song 4", Artist: "Test Artist", Album: "Test Album", Duration: 240},
	}

	// Store initial songs
	err = db.StoreSongs(userID, songs)
	if err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	// Record some play events to test preservation of historical data
	err = db.RecordPlayEvent(userID, "song1", "play", nil)
	if err != nil {
		t.Fatalf("Failed to record play event: %v", err)
	}
	err = db.RecordPlayEvent(userID, "song2", "skip", nil)
	if err != nil {
		t.Fatalf("Failed to record skip event: %v", err)
	}

	// Delete songs 2 and 3
	songsToDelete := []string{"song2", "song3"}
	err = db.DeleteSongs(userID, songsToDelete)
	if err != nil {
		t.Fatalf("Failed to delete songs: %v", err)
	}

	// Verify songs are deleted
	existingIDs, err := db.GetExistingSongIDs(userID)
	if err != nil {
		t.Fatalf("Failed to get existing song IDs: %v", err)
	}

	if len(existingIDs) != 2 {
		t.Errorf("Expected 2 remaining songs, got %d", len(existingIDs))
	}

	if !existingIDs["song1"] {
		t.Error("Expected song1 to still exist")
	}
	if !existingIDs["song4"] {
		t.Error("Expected song4 to still exist")
	}
	if existingIDs["song2"] {
		t.Error("Expected song2 to be deleted")
	}
	if existingIDs["song3"] {
		t.Error("Expected song3 to be deleted")
	}

	// Verify play events are preserved (historical data)
	allSongs, err := db.GetAllSongs(userID)
	if err != nil {
		t.Fatalf("Failed to get all songs: %v", err)
	}

	var song1 *models.Song
	for i := range allSongs {
		if allSongs[i].ID == "song1" {
			song1 = &allSongs[i]
			break
		}
	}

	if song1 == nil {
		t.Fatal("song1 not found")
	}

	// song1 should still have its play count
	if song1.PlayCount != 1 {
		t.Errorf("Expected song1 to have play count 1, got %d", song1.PlayCount)
	}

	// Test edge cases
	// Test with empty user ID
	err = db.DeleteSongs("", []string{"song1"})
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	// Test with empty song list (should not error)
	err = db.DeleteSongs(userID, []string{})
	if err != nil {
		t.Errorf("Unexpected error for empty song list: %v", err)
	}

	// Test with non-existent songs (should not error)
	err = db.DeleteSongs(userID, []string{"nonexistent1", "nonexistent2"})
	if err != nil {
		t.Errorf("Unexpected error for non-existent songs: %v", err)
	}
}

func TestDifferentialSyncWorkflow(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	userID := "testuser"

	// Initial sync with 4 songs
	initialSongs := []models.Song{
		{ID: "song1", Title: "Test Song 1", Artist: "Test Artist", Album: "Test Album", Duration: 180},
		{ID: "song2", Title: "Test Song 2", Artist: "Test Artist", Album: "Test Album", Duration: 200},
		{ID: "song3", Title: "Test Song 3", Artist: "Test Artist", Album: "Test Album", Duration: 220},
		{ID: "song4", Title: "Test Song 4", Artist: "Test Artist", Album: "Test Album", Duration: 240},
	}

	err = db.StoreSongs(userID, initialSongs)
	if err != nil {
		t.Fatalf("Failed to store initial songs: %v", err)
	}

	// Record some user activity
	err = db.RecordPlayEvent(userID, "song1", "play", nil)
	if err != nil {
		t.Fatalf("Failed to record play event: %v", err)
	}
	err = db.RecordPlayEvent(userID, "song2", "skip", nil)
	if err != nil {
		t.Fatalf("Failed to record skip event: %v", err)
	}

	// Simulate differential sync: songs 2 and 3 removed, song 5 added, song 1 and 4 remain
	upstreamSongs := []models.Song{
		{ID: "song1", Title: "Test Song 1 Updated", Artist: "Test Artist", Album: "Test Album", Duration: 180}, // Updated title
		{ID: "song4", Title: "Test Song 4", Artist: "Test Artist", Album: "Test Album", Duration: 240},         // Unchanged
		{ID: "song5", Title: "Test Song 5", Artist: "Test Artist", Album: "Test Album", Duration: 260},         // New song
	}

	// Step 1: Get existing song IDs
	existingIDs, err := db.GetExistingSongIDs(userID)
	if err != nil {
		t.Fatalf("Failed to get existing song IDs: %v", err)
	}

	// Step 2: Determine songs to delete
	upstreamIDs := make(map[string]bool)
	for _, song := range upstreamSongs {
		upstreamIDs[song.ID] = true
	}

	var songsToDelete []string
	for existingID := range existingIDs {
		if !upstreamIDs[existingID] {
			songsToDelete = append(songsToDelete, existingID)
		}
	}

	// Step 3: Delete removed songs
	if len(songsToDelete) > 0 {
		err = db.DeleteSongs(userID, songsToDelete)
		if err != nil {
			t.Fatalf("Failed to delete songs: %v", err)
		}
	}

	// Step 4: Store/update current songs
	err = db.StoreSongs(userID, upstreamSongs)
	if err != nil {
		t.Fatalf("Failed to store upstream songs: %v", err)
	}

	// Verify final state
	finalSongs, err := db.GetAllSongs(userID)
	if err != nil {
		t.Fatalf("Failed to get final songs: %v", err)
	}

	if len(finalSongs) != 3 {
		t.Errorf("Expected 3 final songs, got %d", len(finalSongs))
	}

	// Verify specific songs
	songMap := make(map[string]models.Song)
	for _, song := range finalSongs {
		songMap[song.ID] = song
	}

	// song1 should exist with preserved play count and updated title
	if song1, exists := songMap["song1"]; !exists {
		t.Error("Expected song1 to exist")
	} else {
		if song1.PlayCount != 1 {
			t.Errorf("Expected song1 to have preserved play count 1, got %d", song1.PlayCount)
		}
		if song1.Title != "Test Song 1 Updated" {
			t.Errorf("Expected song1 to have updated title, got %s", song1.Title)
		}
	}

	// song4 should exist unchanged
	if _, exists := songMap["song4"]; !exists {
		t.Error("Expected song4 to exist")
	}

	// song5 should exist as new song
	if song5, exists := songMap["song5"]; !exists {
		t.Error("Expected song5 to exist")
	} else {
		if song5.PlayCount != 0 {
			t.Errorf("Expected song5 to have play count 0, got %d", song5.PlayCount)
		}
	}

	// song2 and song3 should not exist
	if _, exists := songMap["song2"]; exists {
		t.Error("Expected song2 to be deleted")
	}
	if _, exists := songMap["song3"]; exists {
		t.Error("Expected song3 to be deleted")
	}

	// Verify deletion counts
	expectedDeleted := []string{"song2", "song3"}
	if len(songsToDelete) != len(expectedDeleted) {
		t.Errorf("Expected %d songs to be deleted, got %d", len(expectedDeleted), len(songsToDelete))
	}
}

// Error handling tests for database operations

func TestStoreSongsErrorHandling(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Test with empty user ID
	songs := []models.Song{
		{ID: "1", Title: "Test Song", Artist: "Test Artist", Album: "Test Album", Duration: 180},
	}
	err = db.StoreSongs("", songs)
	if err == nil {
		t.Error("Expected error for empty user ID")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("Expected validation error, got: %v", err)
	}

	// Test with empty songs list (should not error)
	err = db.StoreSongs("testuser", []models.Song{})
	if err != nil {
		t.Errorf("Unexpected error for empty songs list: %v", err)
	}

	// Test with very long song data to test potential constraints
	longString := strings.Repeat("a", 10000)
	longSongs := []models.Song{
		{ID: "1", Title: longString, Artist: longString, Album: longString, Duration: 180},
	}
	err = db.StoreSongs("testuser", longSongs)
	if err != nil {
		t.Errorf("Unexpected error for long song data: %v", err)
	}
}

func TestRecordPlayEventErrorHandling(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Test with empty user ID
	err = db.RecordPlayEvent("", "song1", "play", nil)
	if err == nil {
		t.Error("Expected error for empty user ID")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("Expected validation error, got: %v", err)
	}

	// Test with empty song ID
	err = db.RecordPlayEvent("testuser", "", "play", nil)
	if err == nil {
		t.Error("Expected error for empty song ID")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("Expected validation error, got: %v", err)
	}

	// Test with empty event type
	err = db.RecordPlayEvent("testuser", "song1", "", nil)
	if err == nil {
		t.Error("Expected error for empty event type")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("Expected validation error, got: %v", err)
	}

	// Test with invalid event type (should still work as DB doesn't validate content)
	err = db.RecordPlayEvent("testuser", "song1", "invalid_type", nil)
	if err != nil {
		t.Errorf("Unexpected error for invalid event type: %v", err)
	}

	// Test with very long string values
	longString := strings.Repeat("a", 10000)
	err = db.RecordPlayEvent(longString, longString, "play", &longString)
	if err != nil {
		t.Errorf("Unexpected error for long string values: %v", err)
	}
}

func TestGetAllSongsErrorHandling(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Test with empty user ID
	_, err = db.GetAllSongs("")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("Expected validation error, got: %v", err)
	}

	// Test with non-existent user (should return empty slice, not error)
	songs, err := db.GetAllSongs("nonexistent_user")
	if err != nil {
		t.Errorf("Unexpected error for non-existent user: %v", err)
	}
	if len(songs) != 0 {
		t.Errorf("Expected empty songs list for non-existent user, got %d songs", len(songs))
	}

	// Test with very long user ID
	longUserID := strings.Repeat("a", 10000)
	songs, err = db.GetAllSongs(longUserID)
	if err != nil {
		t.Errorf("Unexpected error for long user ID: %v", err)
	}
	if len(songs) != 0 {
		t.Errorf("Expected empty songs list for long user ID, got %d songs", len(songs))
	}
}

func TestDatabaseCloseErrorHandling(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	db, err := New(":memory:", logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Close once (should work)
	err = db.Close()
	if err != nil {
		t.Errorf("Unexpected error on first close: %v", err)
	}

	// Close again (should not error due to idempotent shutdown channel handling)
	err = db.Close()
	if err != nil {
		t.Errorf("Unexpected error on second close: %v", err)
	}

	// Try to use database after close (should fail)
	_, err = db.GetAllSongs("testuser")
	if err == nil {
		t.Error("Expected error when using database after close")
	}
}

func TestMigrationErrorHandling(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Test with invalid database path
	_, err := New("/invalid/path/that/does/not/exist/test.db", logger)
	if err == nil {
		t.Error("Expected error for invalid database path")
	}

	// Test with read-only directory (if we can create one)
	// This is platform-specific and might not work in all test environments
	// so we'll skip this test if we can't create the conditions
}

func TestSQLInjectionResistance(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Test with potential SQL injection in user ID
	maliciousUserID := "'; DROP TABLE songs; --"

	// These operations should not cause SQL injection
	_, err = db.GetAllSongs(maliciousUserID)
	if err != nil && !strings.Contains(err.Error(), "validation") {
		t.Errorf("Unexpected error type for malicious user ID: %v", err)
	}

	_, err = db.GetExistingSongIDs(maliciousUserID)
	if err != nil && !strings.Contains(err.Error(), "validation") {
		t.Errorf("Unexpected error type for malicious user ID in GetExistingSongIDs: %v", err)
	}

	// Test with potential SQL injection in song ID
	maliciousSongID := "'; DROP TABLE songs; --"
	err = db.RecordPlayEvent("testuser", maliciousSongID, "play", nil)
	if err != nil {
		t.Errorf("Unexpected error for malicious song ID: %v", err)
	}

	// Verify tables still exist after potential injection attempts
	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='songs'").Scan(&count)
	if err != nil {
		t.Errorf("Failed to check if songs table exists: %v", err)
	}
	if count != 1 {
		t.Error("Songs table should still exist after injection attempts")
	}
}

func TestGetAllArtistAdjustedStats(t *testing.T) {
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
		{ID: "1", Title: "Song 1", Artist: "Artist A", Album: "Album", Duration: 180},
		{ID: "2", Title: "Song 2", Artist: "Artist A", Album: "Album", Duration: 180},
		{ID: "3", Title: "Song 3", Artist: "Artist B", Album: "Album", Duration: 180},
	}
	if err := db.StoreSongs("user1", songs); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	if err := db.StoreSongs("user2", songs); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	for _, ev := range []struct{ user, song, event string }{
		{"user1", "1", "play"},
		{"user1", "2", "play"},
		{"user1", "3", "skip"},
		{"user2", "1", "skip"},
	} {
		if err := db.RecordPlayEvent(ev.user, ev.song, ev.event, nil); err != nil {
			t.Fatalf("Failed to record event: %v", err)
		}
	}

	stats, err := db.GetAllArtistAdjustedStats("user1")
	if err != nil {
		t.Fatalf("GetAllArtistAdjustedStats failed: %v", err)
	}

	want := map[string]AdjustedStats{
		"Artist A": {Plays: 2.0, Skips: 0.0},
		"Artist B": {Plays: 0.0, Skips: 1.0},
	}
	if len(stats) != len(want) {
		t.Fatalf("got %d artists, want %d: %v", len(stats), len(want), stats)
	}
	// The counters decay over time, so allow for the moments that pass during the test
	for artist, w := range want {
		got := stats[artist]
		if math.Abs(got.Plays-w.Plays) > 1e-6 || math.Abs(got.Skips-w.Skips) > 1e-6 {
			t.Errorf("stats[%q] = %+v, want %+v", artist, got, w)
		}
	}

	empty, err := db.GetAllArtistAdjustedStats("nobody")
	if err != nil {
		t.Fatalf("GetAllArtistAdjustedStats failed for unknown user: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected no stats for unknown user, got %v", empty)
	}
}

func TestAdjustedCountersDecayOverTime(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_decay.db"
	defer os.Remove(dbPath)

	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	if err := db.StoreSongs("user1", []models.Song{{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 180}}); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	now := time.Now()
	oneHalfLifeAgo := now.Add(-AdjustedHalfLife)
	twoHalfLivesAgo := now.Add(-2 * AdjustedHalfLife)

	getSong := func() models.Song {
		t.Helper()
		songs, err := db.GetAllSongs("user1")
		if err != nil || len(songs) != 1 {
			t.Fatalf("GetAllSongs failed: %v (%d songs)", err, len(songs))
		}
		return songs[0]
	}

	// A play one half-life ago is worth half a play now
	if err := db.RecordPlayEventAt("user1", "1", "play", nil, oneHalfLifeAgo); err != nil {
		t.Fatalf("Failed to record play: %v", err)
	}
	if got := getSong().AdjustedPlays; math.Abs(got-0.5) > 1e-3 {
		t.Errorf("AdjustedPlays after a play one half-life ago = %.4f, want 0.5", got)
	}

	// A skip now counts fully
	if err := db.RecordPlayEventAt("user1", "1", "skip", nil, now); err != nil {
		t.Fatalf("Failed to record skip: %v", err)
	}
	song := getSong()
	if math.Abs(song.AdjustedPlays-0.5) > 1e-3 || math.Abs(song.AdjustedSkips-1) > 1e-3 {
		t.Errorf("Adjusted counters = %.4f plays, %.4f skips, want 0.5 and 1", song.AdjustedPlays, song.AdjustedSkips)
	}

	// A play delivered late counts for what it's worth today, and doesn't move last_played back
	if err := db.RecordPlayEventAt("user1", "1", "play", nil, twoHalfLivesAgo); err != nil {
		t.Fatalf("Failed to record late play: %v", err)
	}
	song = getSong()
	if math.Abs(song.AdjustedPlays-0.75) > 1e-3 {
		t.Errorf("AdjustedPlays after a late play = %.4f, want 0.75", song.AdjustedPlays)
	}
	if song.PlayCount != 2 {
		t.Errorf("PlayCount = %d, want 2", song.PlayCount)
	}
	if song.LastPlayed.Sub(oneHalfLifeAgo).Abs() > time.Second {
		t.Errorf("LastPlayed = %v, want %v", song.LastPlayed, oneHalfLifeAgo)
	}

	// A sync keeps the history
	if err := db.StoreSongs("user1", []models.Song{{ID: "1", Title: "Renamed", Artist: "Artist", Album: "Album", Duration: 180}}); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	song = getSong()
	if song.Title != "Renamed" || math.Abs(song.AdjustedPlays-0.75) > 1e-3 || song.PlayCount != 2 {
		t.Errorf("After sync: title %q, %.4f adjusted plays, %d plays; want Renamed, 0.75, 2", song.Title, song.AdjustedPlays, song.PlayCount)
	}

	totals, err := db.GetAdjustedTotals("user1")
	if err != nil {
		t.Fatalf("GetAdjustedTotals failed: %v", err)
	}
	if totals.HeardSongs != 1 || totals.HeardArtists != 1 || math.Abs(totals.Plays-0.75) > 1e-3 || math.Abs(totals.Skips-1) > 1e-3 {
		t.Errorf("GetAdjustedTotals = %+v, want 0.75 plays, 1 skip, 1 song, 1 artist", totals)
	}
}

func TestAdjustedAtMigration(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_adjusted_at.db"
	defer os.Remove(dbPath)

	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	if err := db.StoreSongs("user1", []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 180},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 180},
	}); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	// Recreate a database from before adjusted_at: counters last updated one half-life ago
	lastEvent := time.Now().UTC().Add(-AdjustedHalfLife).Format("2006-01-02 15:04:05")
	for _, q := range []string{
		`ALTER TABLE songs DROP COLUMN adjusted_at`,
		`UPDATE songs SET adjusted_plays = 2, last_played = '` + lastEvent + `' WHERE id = '1'`,
	} {
		if _, err := db.conn.Exec(q); err != nil {
			t.Fatalf("Failed to set up old schema: %v", err)
		}
	}
	db.Close()

	db, err = New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer db.Close()

	all, err := db.GetAllSongs("user1")
	if err != nil {
		t.Fatalf("GetAllSongs failed: %v", err)
	}
	for _, song := range all {
		want := 0.0
		if song.ID == "1" {
			want = 1.0
		}
		if math.Abs(song.AdjustedPlays-want) > 1e-3 {
			t.Errorf("Song %s AdjustedPlays after migration = %.4f, want %.1f", song.ID, song.AdjustedPlays, want)
		}
	}
}

func TestDropArtistStatsMigration(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_drop_artist_stats.db"
	defer os.Remove(dbPath)

	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Recreate the artist_stats table that older versions kept
	for _, q := range []string{
		`CREATE TABLE artist_stats (user_id TEXT NOT NULL, artist TEXT NOT NULL, play_count INTEGER DEFAULT 0,
			skip_count INTEGER DEFAULT 0, ratio REAL DEFAULT 0.5, PRIMARY KEY (user_id, artist))`,
		`CREATE INDEX idx_artist_stats_user_id ON artist_stats(user_id)`,
		`INSERT INTO artist_stats (user_id, artist, play_count) VALUES ('user1', 'Artist', 3)`,
	} {
		if _, err := db.conn.Exec(q); err != nil {
			t.Fatalf("Failed to set up old schema: %v", err)
		}
	}
	db.Close()

	db, err = New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer db.Close()

	var count int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('artist_stats', 'idx_artist_stats_user_id')`).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for artist_stats: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected artist_stats and its index to be dropped, found %d objects", count)
	}
}

func TestStoreSongsFailsOnInsertError(t *testing.T) {
	db, err := New(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	if _, err := db.conn.Exec(`CREATE TRIGGER reject_bad_song BEFORE INSERT ON songs
		WHEN NEW.id = 'bad' BEGIN SELECT RAISE(ABORT, 'rejected'); END`); err != nil {
		t.Fatalf("Failed to create trigger: %v", err)
	}

	songs := []models.Song{
		{ID: "good", Title: "Good", Artist: "Artist", Album: "Album", Duration: 180},
		{ID: "bad", Title: "Bad", Artist: "Artist", Album: "Album", Duration: 180},
	}
	if err := db.StoreSongs("testuser", songs); err == nil {
		t.Fatal("Expected an error when a song can't be stored")
	}

	// The batch is rolled back, so a failed sync leaves nothing half-written
	ids, err := db.GetExistingSongIDs("testuser")
	if err != nil {
		t.Fatalf("Failed to get song IDs: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("Expected no songs stored after a failed batch, got %d", len(ids))
	}
}

func TestStoreSongsGenreAndYear(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	db, err := New(t.TempDir()+"/test.db", logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	song := models.Song{ID: "1", Title: "t", Artist: "a", Album: "b", Duration: 10, Genre: "Rock", Year: 1985}
	if err := db.StoreSongs("user", []models.Song{song}); err != nil {
		t.Fatalf("Failed to store song: %v", err)
	}

	// A later sync updates them like the rest of the metadata
	song.Genre, song.Year = "Pop", 1986
	if err := db.StoreSongs("user", []models.Song{song}); err != nil {
		t.Fatalf("Failed to update song: %v", err)
	}

	songs, err := db.GetAllSongs("user")
	if err != nil {
		t.Fatalf("Failed to get songs: %v", err)
	}
	if len(songs) != 1 || songs[0].Genre != "Pop" || songs[0].Year != 1986 {
		t.Errorf("Expected genre Pop and year 1986, got %+v", songs)
	}
}

func TestClientMetadataMigration(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_client_metadata.db"
	defer os.Remove(dbPath)

	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	if err := db.StoreSongs("user1", []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 180},
	}); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	// Recreate a database from before the client metadata columns
	for _, column := range []string{"album_id", "artist_id", "track", "disc_number", "suffix", "content_type", "bit_rate", "size"} {
		if _, err := db.conn.Exec(`ALTER TABLE songs DROP COLUMN ` + column); err != nil {
			t.Fatalf("Failed to set up old schema: %v", err)
		}
	}
	db.Close()

	db, err = New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer db.Close()

	// Existing songs read back with empty metadata until the next sync fills it in
	all, err := db.GetAllSongs("user1")
	if err != nil || len(all) != 1 {
		t.Fatalf("GetAllSongs after migration = %v, %v; want one song", all, err)
	}
	if all[0].AlbumID != "" || all[0].Track != 0 {
		t.Errorf("Expected empty metadata for a migrated song, got %+v", all[0])
	}

	song := models.Song{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 180,
		AlbumID: "al-1", ArtistID: "ar-1", Track: 2, DiscNumber: 1, Suffix: "mp3", ContentType: "audio/mpeg", BitRate: 320, Size: 4096}
	if err := db.StoreSongs("user1", []models.Song{song}); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}
	all, err = db.GetAllSongs("user1")
	if err != nil || len(all) != 1 {
		t.Fatalf("GetAllSongs = %v, %v; want one song", all, err)
	}
	got := all[0]
	if got.AlbumID != song.AlbumID || got.ArtistID != song.ArtistID || got.Track != song.Track ||
		got.DiscNumber != song.DiscNumber || got.Suffix != song.Suffix || got.ContentType != song.ContentType ||
		got.BitRate != song.BitRate || got.Size != song.Size {
		t.Errorf("Stored metadata = %+v, want %+v", got, song)
	}
}
