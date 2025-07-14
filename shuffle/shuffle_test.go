package shuffle

import (
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/database"
	"github.com/syeo66/subsoxy/models"
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

	service := New(db, logger)

	if service == nil {
		t.Fatal("Service should not be nil")
	}
	if service.db != db {
		t.Error("Database should be set correctly")
	}
	if service.logger != logger {
		t.Error("Logger should be set correctly")
	}
	if service.lastPlayed == nil {
		t.Error("LastPlayed map should be initialized")
	}
	if len(service.lastPlayed) != 0 {
		t.Error("LastPlayed map should be empty initially")
	}
}

func TestSetLastPlayed(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	song := &models.Song{
		ID:     "123",
		Title:  "Test Song",
		Artist: "Test Artist",
	}

	service.SetLastPlayed("testuser", song)

	if service.lastPlayed["testuser"] == nil {
		t.Error("LastPlayed should not be nil after setting")
	}
	if service.lastPlayed["testuser"].ID != "123" {
		t.Errorf("Expected last played ID '123', got '%s'", service.lastPlayed["testuser"].ID)
	}
}

func TestCalculateTimeDecayWeight(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	tests := []struct {
		name        string
		lastPlayed  time.Time
		expectedMin float64
		expectedMax float64
		description string
	}{
		{
			name:        "Never played",
			lastPlayed:  time.Time{},
			expectedMin: 2.0,
			expectedMax: 2.0,
			description: "Never played songs should get 2.0x boost",
		},
		{
			name:        "Played today",
			lastPlayed:  time.Now(),
			expectedMin: 0.1,
			expectedMax: 0.2,
			description: "Recently played songs should get low weight",
		},
		{
			name:        "Played 15 days ago",
			lastPlayed:  time.Now().AddDate(0, 0, -15),
			expectedMin: 0.5,
			expectedMax: 0.6,
			description: "Songs played 15 days ago should be mid-range",
		},
		{
			name:        "Played 30 days ago",
			lastPlayed:  time.Now().AddDate(0, 0, -30),
			expectedMin: 0.9,
			expectedMax: 1.1,
			description: "Songs played 30 days ago should get near 1.0 weight",
		},
		{
			name:        "Played 60 days ago",
			lastPlayed:  time.Now().AddDate(0, 0, -60),
			expectedMin: 1.1,
			expectedMax: 1.3,
			description: "Older songs should get higher weight",
		},
		{
			name:        "Played 365 days ago",
			lastPlayed:  time.Now().AddDate(-1, 0, 0),
			expectedMin: 1.9,
			expectedMax: 2.1,
			description: "Very old songs should get near 2.0 weight",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := service.calculateTimeDecayWeight(tt.lastPlayed)
			if weight < tt.expectedMin || weight > tt.expectedMax {
				t.Errorf("%s: expected weight between %.2f and %.2f, got %.2f",
					tt.description, tt.expectedMin, tt.expectedMax, weight)
			}
		})
	}
}

func TestCalculatePlaySkipWeight(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	tests := []struct {
		name        string
		playCount   int
		skipCount   int
		expected    float64
		description string
	}{
		{
			name:        "No history",
			playCount:   0,
			skipCount:   0,
			expected:    1.5,
			description: "Songs with no history should get 1.5x boost",
		},
		{
			name:        "Always played",
			playCount:   10,
			skipCount:   0,
			expected:    2.0,
			description: "Always played songs should get 2.0 weight",
		},
		{
			name:        "Always skipped",
			playCount:   0,
			skipCount:   10,
			expected:    0.2,
			description: "Always skipped songs should get 0.2 weight",
		},
		{
			name:        "Half played",
			playCount:   5,
			skipCount:   5,
			expected:    1.1,
			description: "Half played songs should get 1.1 weight",
		},
		{
			name:        "Mostly played",
			playCount:   8,
			skipCount:   2,
			expected:    1.64,
			description: "Mostly played songs should get high weight",
		},
		{
			name:        "Mostly skipped",
			playCount:   2,
			skipCount:   8,
			expected:    0.56,
			description: "Mostly skipped songs should get low weight",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := service.calculatePlaySkipWeight(tt.playCount, tt.skipCount)
			// Use approximate comparison for floating point values
			if weight < tt.expected-0.001 || weight > tt.expected+0.001 {
				t.Errorf("%s: expected weight %.3f, got %.3f",
					tt.description, tt.expected, weight)
			}
		})
	}
}

func TestCalculateTransitionWeight(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	// Test with no last played song
	weight := service.calculateTransitionWeight("testuser", "123")
	if weight != 1.0 {
		t.Errorf("Expected weight 1.0 when no last played song, got %.2f", weight)
	}

	// Store test songs
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
	}

	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}

	// Set last played song
	service.SetLastPlayed("testuser", &songs[0])

	// Test with no transition data (should return 1.0)
	weight = service.calculateTransitionWeight("testuser", "2")
	if weight != 1.0 {
		t.Errorf("Expected weight 1.0 when no transition data, got %.2f", weight)
	}

	// Record a transition
	err = db.RecordTransition("testuser", "1", "2", "play")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}

	// Test with transition data
	weight = service.calculateTransitionWeight("testuser", "2")
	expected := 0.5 + 1.0 // 0.5 base + 1.0 probability
	if weight != expected {
		t.Errorf("Expected weight %.2f with transition data, got %.2f", expected, weight)
	}
}

func TestCalculateSongWeight(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	song := models.Song{
		ID:         "123",
		Title:      "Test Song",
		Artist:     "Test Artist",
		Album:      "Test Album",
		Duration:   300,
		LastPlayed: time.Time{}, // Never played
		PlayCount:  0,
		SkipCount:  0,
	}

	weight := service.calculateSongWeight("testuser", song)

	// Never played song with no history should get:
	// baseWeight(1.0) * timeWeight(2.0) * playSkipWeight(1.5) * transitionWeight(1.0) = 3.0
	expected := 1.0 * 2.0 * 1.5 * 1.0
	if weight != expected {
		t.Errorf("Expected weight %.2f for never played song, got %.2f", expected, weight)
	}
}

func TestGetWeightedShuffledSongs(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	// Test with empty database
	songs, err := service.GetWeightedShuffledSongs("testuser", 10)
	if err != nil {
		t.Errorf("Failed to get shuffled songs: %v", err)
	}
	if len(songs) != 0 {
		t.Errorf("Expected 0 songs from empty database, got %d", len(songs))
	}

	// Store test songs
	testSongs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
		{ID: "3", Title: "Song 3", Artist: "Artist", Album: "Album", Duration: 200},
		{ID: "4", Title: "Song 4", Artist: "Artist", Album: "Album", Duration: 180},
		{ID: "5", Title: "Song 5", Artist: "Artist", Album: "Album", Duration: 220},
	}

	err = db.StoreSongs("testuser", testSongs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}

	// Test requesting more songs than available
	songs, err = service.GetWeightedShuffledSongs("testuser", 10)
	if err != nil {
		t.Errorf("Failed to get shuffled songs: %v", err)
	}
	if len(songs) != 5 {
		t.Errorf("Expected 5 songs when requesting 10 from 5 available, got %d", len(songs))
	}

	// Test requesting fewer songs than available
	songs, err = service.GetWeightedShuffledSongs("testuser", 3)
	if err != nil {
		t.Errorf("Failed to get shuffled songs: %v", err)
	}
	if len(songs) != 3 {
		t.Errorf("Expected 3 songs, got %d", len(songs))
	}

	// Verify no duplicates
	usedIDs := make(map[string]bool)
	for _, song := range songs {
		if usedIDs[song.ID] {
			t.Errorf("Duplicate song ID found: %s", song.ID)
		}
		usedIDs[song.ID] = true
	}
}

func TestGetWeightedShuffledSongsWithHistory(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	// Store test songs
	testSongs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
		{ID: "3", Title: "Song 3", Artist: "Artist", Album: "Album", Duration: 200},
	}

	err = db.StoreSongs("testuser", testSongs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}

	// Add some play history to make weights different
	err = db.RecordPlayEvent("testuser", "1", "play", nil)
	if err != nil {
		t.Errorf("Failed to record play event: %v", err)
	}

	err = db.RecordPlayEvent("testuser", "2", "skip", nil)
	if err != nil {
		t.Errorf("Failed to record skip event: %v", err)
	}

	// Test that songs are returned (we can't easily test randomness, but we can verify functionality)
	songs, err := service.GetWeightedShuffledSongs("testuser", 2)
	if err != nil {
		t.Errorf("Failed to get shuffled songs: %v", err)
	}
	if len(songs) != 2 {
		t.Errorf("Expected 2 songs, got %d", len(songs))
	}
}

func TestGetWeightedShuffledSongsWithTransitions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	// Store test songs
	testSongs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
		{ID: "3", Title: "Song 3", Artist: "Artist", Album: "Album", Duration: 200},
	}

	err = db.StoreSongs("testuser", testSongs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}

	// Set last played song
	service.SetLastPlayed("testuser", &testSongs[0])

	// Record transitions
	err = db.RecordTransition("testuser", "1", "2", "play")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}

	err = db.RecordTransition("testuser", "1", "3", "skip")
	if err != nil {
		t.Errorf("Failed to record transition: %v", err)
	}

	// Test that songs are returned with transition weighting
	songs, err := service.GetWeightedShuffledSongs("testuser", 2)
	if err != nil {
		t.Errorf("Failed to get shuffled songs: %v", err)
	}
	if len(songs) != 2 {
		t.Errorf("Expected 2 songs, got %d", len(songs))
	}
}

func TestGetWeightedShuffledSongsConsistency(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	// Store test songs
	testSongs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Artist", Album: "Album", Duration: 300},
		{ID: "2", Title: "Song 2", Artist: "Artist", Album: "Album", Duration: 250},
		{ID: "3", Title: "Song 3", Artist: "Artist", Album: "Album", Duration: 200},
	}

	err = db.StoreSongs("testuser", testSongs)
	if err != nil {
		t.Errorf("Failed to store songs: %v", err)
	}

	// Test multiple calls return valid results
	for i := 0; i < 10; i++ {
		songs, err := service.GetWeightedShuffledSongs("testuser", 2)
		if err != nil {
			t.Errorf("Failed to get shuffled songs on iteration %d: %v", i, err)
		}
		if len(songs) != 2 {
			t.Errorf("Expected 2 songs on iteration %d, got %d", i, len(songs))
		}

		// Verify no duplicates in single call
		usedIDs := make(map[string]bool)
		for _, song := range songs {
			if usedIDs[song.ID] {
				t.Errorf("Duplicate song ID found on iteration %d: %s", i, song.ID)
			}
			usedIDs[song.ID] = true
		}
	}
}

func TestEdgeCases(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	// Test requesting 0 songs
	songs, err := service.GetWeightedShuffledSongs("testuser", 0)
	if err != nil {
		t.Errorf("Failed to get 0 shuffled songs: %v", err)
	}
	if len(songs) != 0 {
		t.Errorf("Expected 0 songs when requesting 0, got %d", len(songs))
	}

	// Test requesting large number of songs (should not panic)
	songs, err = service.GetWeightedShuffledSongs("testuser", 1000000)
	if err != nil {
		t.Errorf("Failed to get large number of shuffled songs: %v", err)
	}
	// Should return all available songs (not panic)
	if len(songs) == 0 {
		t.Log("No songs returned for large request - this is expected if no songs exist")
	}
}

func TestConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_concurrent.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	// Create test songs
	songs := []models.Song{
		{ID: "song1", Title: "Song 1", Artist: "Artist 1"},
		{ID: "song2", Title: "Song 2", Artist: "Artist 2"},
		{ID: "song3", Title: "Song 3", Artist: "Artist 3"},
	}

	// Store songs in database
	err = db.StoreSongs("testuser", songs)
	if err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	// Test concurrent access to SetLastPlayed and calculateTransitionWeight
	const numGoroutines = 100
	const numIterations = 10

	// Start multiple goroutines that concurrently access lastPlayed map
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			userID := "testuser"
			for j := 0; j < numIterations; j++ {
				// Concurrent SetLastPlayed calls
				songIndex := (goroutineID + j) % len(songs)
				service.SetLastPlayed(userID, &songs[songIndex])

				// Concurrent calculateTransitionWeight calls (reads lastPlayed)
				weight := service.calculateTransitionWeight(userID, songs[songIndex].ID)
				if weight < 0 {
					t.Errorf("Invalid weight: %f", weight)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify that the service is still functional after concurrent access
	shuffledSongs, err := service.GetWeightedShuffledSongs("testuser", 3)
	if err != nil {
		t.Errorf("Failed to get shuffled songs after concurrent access: %v", err)
	}

	if len(shuffledSongs) == 0 {
		t.Error("Expected shuffled songs after concurrent access")
	}
}
