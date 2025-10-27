package shuffle

import (
	"math"
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
			expectedMin: 4.0,
			expectedMax: 4.0,
			description: "Never played songs should get 4.0x boost",
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

	tests := []struct {
		name           string
		song           models.Song
		setupFunc      func()
		expectedWeight float64
		tolerance      float64
		description    string
	}{
		{
			name: "Never played song with no history",
			song: models.Song{
				ID:         "song1",
				Title:      "Test Song 1",
				Artist:     "Test Artist",
				Album:      "Test Album",
				Duration:   300,
				LastPlayed: time.Time{}, // Never played
				PlayCount:  0,
				SkipCount:  0,
			},
			setupFunc: func() {
				// No setup needed
			},
			expectedWeight: 1.0 * 4.0 * 1.5 * 1.0, // baseWeight * timeWeight * playSkipWeight * transitionWeight
			tolerance:      0.001,
			description:    "Never played song with no history should get maximum weight boost",
		},
		{
			name: "Recently played song with high play ratio",
			song: models.Song{
				ID:         "song2",
				Title:      "Test Song 2",
				Artist:     "Test Artist",
				Album:      "Test Album",
				Duration:   300,
				LastPlayed: time.Now().Add(-1 * time.Hour), // Played 1 hour ago
				PlayCount:  10,
				SkipCount:  0,
			},
			setupFunc: func() {
				// No setup needed
			},
			expectedWeight: 1.0 * 0.1 * 2.0 * 1.0, // Recently played should have low time weight but high play/skip weight
			tolerance:      0.05,                   // Allow some tolerance for time calculations
			description:    "Recently played but always played song should balance time decay with play ratio",
		},
		{
			name: "Frequently skipped song",
			song: models.Song{
				ID:         "song3",
				Title:      "Test Song 3",
				Artist:     "Test Artist",
				Album:      "Test Album",
				Duration:   300,
				LastPlayed: time.Now().AddDate(0, 0, -60), // Played 60 days ago
				PlayCount:  1,
				SkipCount:  9,
			},
			setupFunc: func() {
				// No setup needed
			},
			expectedWeight: 1.0 * 1.164 * 0.38 * 1.0, // Old song with bad skip ratio
			tolerance:      0.1,
			description:    "Frequently skipped song should get low weight despite age",
		},
		{
			name: "Song with transition history",
			song: models.Song{
				ID:         "song4",
				Title:      "Test Song 4",
				Artist:     "Test Artist",
				Album:      "Test Album",
				Duration:   300,
				LastPlayed: time.Time{}, // Never played
				PlayCount:  0,
				SkipCount:  0,
			},
			setupFunc: func() {
				// Store songs in database
				testSongs := []models.Song{
					{ID: "prev_song", Title: "Previous Song", Artist: "Artist", Album: "Album", Duration: 300},
					{ID: "song4", Title: "Test Song 4", Artist: "Test Artist", Album: "Test Album", Duration: 300},
				}
				err := db.StoreSongs("testuser", testSongs)
				if err != nil {
					t.Errorf("Failed to store songs: %v", err)
				}

				// Set last played song
				service.SetLastPlayed("testuser", &testSongs[0])

				// Record a transition with high probability
				err = db.RecordTransition("testuser", "prev_song", "song4", "play")
				if err != nil {
					t.Errorf("Failed to record transition: %v", err)
				}
			},
			expectedWeight: 1.0 * 4.0 * 1.5 * 1.5, // Never played song with good transition history
			tolerance:      0.001,
			description:    "Song with strong transition history should get boosted weight",
		},
		{
			name: "Old song with mixed history",
			song: models.Song{
				ID:         "song5",
				Title:      "Test Song 5",
				Artist:     "Test Artist",
				Album:      "Test Album",
				Duration:   300,
				LastPlayed: time.Now().AddDate(-1, 0, 0), // Played 1 year ago
				PlayCount:  5,
				SkipCount:  5,
			},
			setupFunc: func() {
				// No setup needed
			},
			expectedWeight: 1.0 * 2.0 * 1.1 * 1.0, // Old song with balanced play/skip ratio
			tolerance:      0.1,
			description:    "Very old song with balanced history should get good weight",
		},
		{
			name: "Song played 30 days ago - boundary case",
			song: models.Song{
				ID:         "song6",
				Title:      "Test Song 6",
				Artist:     "Test Artist",
				Album:      "Test Album",
				Duration:   300,
				LastPlayed: time.Now().AddDate(0, 0, -30), // Exactly 30 days ago
				PlayCount:  3,
				SkipCount:  2,
			},
			setupFunc: func() {
				// No setup needed
			},
			expectedWeight: 1.0 * 1.0 * 1.28 * 1.0, // 30 days should be at the boundary
			tolerance:      0.15, // Increased tolerance for time boundary calculations
			description:    "Song at 30-day boundary should transition from decay to age boost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean state for each test
			service.lastPlayed = make(map[string]*models.Song)

			// Run setup if provided
			if tt.setupFunc != nil {
				tt.setupFunc()
			}

			weight := service.calculateSongWeight("testuser", tt.song)

			if weight < tt.expectedWeight-tt.tolerance || weight > tt.expectedWeight+tt.tolerance {
				t.Errorf("%s: expected weight %.3f ± %.3f, got %.3f",
					tt.description, tt.expectedWeight, tt.tolerance, weight)
			}

			// Verify weight is positive
			if weight <= 0 {
				t.Errorf("Weight should always be positive, got %.3f", weight)
			}
		})
	}
}

func TestCalculateSongWeightBoundaryConditions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_boundary.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	tests := []struct {
		name        string
		song        models.Song
		description string
	}{
		{
			name: "Extreme play count",
			song: models.Song{
				ID:         "extreme_play",
				LastPlayed: time.Time{},
				PlayCount:  1000000,
				SkipCount:  0,
			},
			description: "Song with extreme play count should not cause overflow",
		},
		{
			name: "Extreme skip count",
			song: models.Song{
				ID:         "extreme_skip",
				LastPlayed: time.Time{},
				PlayCount:  0,
				SkipCount:  1000000,
			},
			description: "Song with extreme skip count should not cause underflow",
		},
		{
			name: "Very recent play",
			song: models.Song{
				ID:         "very_recent",
				LastPlayed: time.Now().Add(-1 * time.Minute),
				PlayCount:  1,
				SkipCount:  0,
			},
			description: "Very recently played song should have minimal but positive weight",
		},
		{
			name: "Ancient play date",
			song: models.Song{
				ID:         "ancient",
				LastPlayed: time.Now().AddDate(-10, 0, 0), // 10 years ago
				PlayCount:  1,
				SkipCount:  0,
			},
			description: "Very old song should have bounded maximum weight",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := service.calculateSongWeight("testuser", tt.song)

			// Verify weight is finite and positive
			if weight <= 0 || !isFinite(weight) {
				t.Errorf("%s: weight should be finite and positive, got %.6f", tt.description, weight)
			}

			// Verify weight is within reasonable bounds (shouldn't be too extreme)
			if weight > 100.0 {
				t.Errorf("%s: weight seems unreasonably high: %.6f", tt.description, weight)
			}
		})
	}
}

func TestCalculateSongWeightWithTransition(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	dbPath := "test_transition.db"
	defer os.Remove(dbPath)

	db, err := database.New(dbPath, logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)

	song := models.Song{
		ID:         "test_song",
		LastPlayed: time.Time{},
		PlayCount:  0,
		SkipCount:  0,
	}

	tests := []struct {
		name                  string
		transitionProbability float64
		expectedWeight        float64
		description           string
	}{
		{
			name:                  "No transition data",
			transitionProbability: 0.0,
			expectedWeight:        1.0 * 4.0 * 1.5 * 1.0, // Default transition weight is 1.0
			description:           "Song with no transition data should use default weight",
		},
		{
			name:                  "Low transition probability",
			transitionProbability: 0.2,
			expectedWeight:        1.0 * 4.0 * 1.5 * (0.5 + 0.2), // BaseTransitionWeight + probability
			description:           "Song with low transition probability should get modest boost",
		},
		{
			name:                  "High transition probability",
			transitionProbability: 0.8,
			expectedWeight:        1.0 * 4.0 * 1.5 * (0.5 + 0.8), // BaseTransitionWeight + probability
			description:           "Song with high transition probability should get significant boost",
		},
		{
			name:                  "Perfect transition probability",
			transitionProbability: 1.0,
			expectedWeight:        1.0 * 4.0 * 1.5 * (0.5 + 1.0), // BaseTransitionWeight + probability
			description:           "Song always follows previous song should get maximum transition boost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := service.calculateSongWeightWithTransition("testuser", song, tt.transitionProbability)

			tolerance := 0.001
			if weight < tt.expectedWeight-tolerance || weight > tt.expectedWeight+tolerance {
				t.Errorf("%s: expected weight %.3f, got %.3f",
					tt.description, tt.expectedWeight, weight)
			}
		})
	}
}

// Helper function to check if a float64 is finite
func isFinite(f float64) bool {
	return !math.IsInf(f, 0) && !math.IsNaN(f)
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
	// Note: Only 1 song should be eligible because the other 2 have recent play/skip events
	// within the 2-week replay prevention window
	songs, err := service.GetWeightedShuffledSongs("testuser", 2)
	if err != nil {
		t.Errorf("Failed to get shuffled songs: %v", err)
	}
	// Should return only 1 song (song "3") because songs "1" and "2" were recently played/skipped
	if len(songs) != 1 {
		t.Errorf("Expected 1 song (due to 2-week replay prevention), got %d", len(songs))
	}
	// Verify it's song "3" (the only eligible one)
	if len(songs) == 1 && songs[0].ID != "3" {
		t.Errorf("Expected song '3', got '%s'", songs[0].ID)
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
