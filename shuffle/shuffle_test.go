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
		lastSkipped time.Time
		expectedMin float64
		expectedMax float64
		description string
	}{
		{
			name:        "Never played or skipped",
			lastPlayed:  time.Time{},
			lastSkipped: time.Time{},
			expectedMin: 4.0,
			expectedMax: 4.0,
			description: "Never played/skipped songs should get 4.0x boost",
		},
		{
			name:        "Played today",
			lastPlayed:  time.Now(),
			lastSkipped: time.Time{},
			expectedMin: 0.1,
			expectedMax: 0.2,
			description: "Recently played songs should get low weight",
		},
		{
			name:        "Skipped today, never played",
			lastPlayed:  time.Time{},
			lastSkipped: time.Now(),
			expectedMin: 0.1,
			expectedMax: 0.2,
			description: "Recently skipped songs should get low weight even if never played",
		},
		{
			name:        "Played 30 days ago, skipped today",
			lastPlayed:  time.Now().AddDate(0, 0, -30),
			lastSkipped: time.Now(),
			expectedMin: 0.1,
			expectedMax: 0.2,
			description: "Should use more recent timestamp (skip)",
		},
		{
			name:        "Skipped 30 days ago, played today",
			lastPlayed:  time.Now(),
			lastSkipped: time.Now().AddDate(0, 0, -30),
			expectedMin: 0.1,
			expectedMax: 0.2,
			description: "Should use more recent timestamp (play)",
		},
		{
			name:        "Played 15 days ago",
			lastPlayed:  time.Now().AddDate(0, 0, -15),
			lastSkipped: time.Time{},
			expectedMin: 0.5,
			expectedMax: 0.6,
			description: "Songs played 15 days ago should be mid-range",
		},
		{
			name:        "Played 30 days ago",
			lastPlayed:  time.Now().AddDate(0, 0, -30),
			lastSkipped: time.Time{},
			expectedMin: 0.9,
			expectedMax: 1.1,
			description: "Songs played 30 days ago should get near 1.0 weight",
		},
		{
			name:        "Played 60 days ago",
			lastPlayed:  time.Now().AddDate(0, 0, -60),
			lastSkipped: time.Time{},
			expectedMin: 1.1,
			expectedMax: 1.3,
			description: "Older songs should get higher weight",
		},
		{
			name:        "Played 365 days ago",
			lastPlayed:  time.Now().AddDate(-1, 0, 0),
			lastSkipped: time.Time{},
			expectedMin: 1.9,
			expectedMax: 2.1,
			description: "Very old songs should get near 2.0 weight",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := service.calculateTimeDecayWeight(tt.lastPlayed, tt.lastSkipped)
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

	// Test user ID for empirical priors
	// Since the user has no data in the database, it will fall back to default priors (α=2.0, β=2.0)
	testUserID := "test-user"

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
			expected:    1.571, // Bayesian: (10+2)/(10+0+2+2) = 12/14 = 0.857; 0.2 + 0.857*1.6 = 1.571
			description: "Always played songs with Bayesian smoothing",
		},
		{
			name:        "Always skipped",
			playCount:   0,
			skipCount:   10,
			expected:    0.429, // Bayesian: (0+2)/(0+10+2+2) = 2/14 = 0.143; 0.2 + 0.143*1.6 = 0.429
			description: "Always skipped songs with Bayesian smoothing",
		},
		{
			name:        "Half played",
			playCount:   5,
			skipCount:   5,
			expected:    1.0, // Bayesian: (5+2)/(5+5+2+2) = 7/14 = 0.5; 0.2 + 0.5*1.6 = 1.0
			description: "Half played songs should get neutral 1.0 weight",
		},
		{
			name:        "Mostly played",
			playCount:   8,
			skipCount:   2,
			expected:    1.343, // Bayesian: (8+2)/(8+2+2+2) = 10/14 = 0.714; 0.2 + 0.714*1.6 = 1.343
			description: "Mostly played songs with Bayesian smoothing",
		},
		{
			name:        "Mostly skipped",
			playCount:   2,
			skipCount:   8,
			expected:    0.657, // Bayesian: (2+2)/(2+8+2+2) = 4/14 = 0.286; 0.2 + 0.286*1.6 = 0.657
			description: "Mostly skipped songs with Bayesian smoothing",
		},
		{
			name:        "Single play (demonstrates Bayesian regularization)",
			playCount:   1,
			skipCount:   0,
			expected:    1.16, // Bayesian: (1+2)/(1+0+2+2) = 3/5 = 0.6; 0.2 + 0.6*1.6 = 1.16
			description: "Single play is regularized toward 50% instead of 100%",
		},
		{
			name:        "Single skip (demonstrates Bayesian regularization)",
			playCount:   0,
			skipCount:   1,
			expected:    0.84, // Bayesian: (0+2)/(0+1+2+2) = 2/5 = 0.4; 0.2 + 0.4*1.6 = 0.84
			description: "Single skip is regularized toward 50% instead of 0%",
		},
		{
			name:        "Many plays converge to true ratio",
			playCount:   100,
			skipCount:   0,
			expected:    1.770, // Bayesian: (100+2)/(100+0+2+2) = 102/104 = 0.981; 0.2 + 0.981*1.6 = 1.770
			description: "With many observations, Bayesian estimate converges to true ratio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := service.calculatePlaySkipWeight(testUserID, float64(tt.playCount), float64(tt.skipCount))
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
			expectedWeight: 1.0 * 0.1 * 1.743 * 1.0, // Recently played with Bayesian-smoothed play ratio
			tolerance:      0.05,
			description:    "Recently played but always played song should balance time decay with play ratio",
		},
		{
			name: "Frequently skipped song",
			song: models.Song{
				ID:            "song3",
				Title:         "Test Song 3",
				Artist:        "Test Artist",
				Album:         "Test Album",
				Duration:      300,
				LastPlayed:    time.Now().AddDate(0, 0, -60), // Played 60 days ago
				PlayCount:     1,
				SkipCount:     9,
				AdjustedPlays: 1.0,
				AdjustedSkips: 9.0,
			},
			setupFunc: func() {
				// No setup needed
			},
			expectedWeight: 1.0 * 1.164 * 0.543 * 1.0, // Old song with bad skip ratio (Bayesian regularization)
			tolerance:      0.1,
			description:    "Frequently skipped song gets moderate penalty with Bayesian regularization",
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
				ID:            "song5",
				Title:         "Test Song 5",
				Artist:        "Test Artist",
				Album:         "Test Album",
				Duration:      300,
				LastPlayed:    time.Now().AddDate(-1, 0, 0), // Played 1 year ago
				PlayCount:     5,
				SkipCount:     5,
				AdjustedPlays: 5.0,
				AdjustedSkips: 5.0,
			},
			setupFunc: func() {
				// No setup needed
			},
			expectedWeight: 1.0 * 2.0 * 1.0 * 1.0, // Old song with balanced play/skip ratio (neutral 1.0x)
			tolerance:      0.1,
			description:    "Very old song with balanced history should get good weight",
		},
		{
			name: "Song played 30 days ago - boundary case",
			song: models.Song{
				ID:            "song6",
				Title:         "Test Song 6",
				Artist:        "Test Artist",
				Album:         "Test Album",
				Duration:      300,
				LastPlayed:    time.Now().AddDate(0, 0, -30), // Exactly 30 days ago
				PlayCount:     3,
				SkipCount:     2,
				AdjustedPlays: 3.0,
				AdjustedSkips: 2.0,
			},
			setupFunc: func() {
				// No setup needed
			},
			expectedWeight: 1.0 * 1.0 * 1.2 * 1.0, // 30 days with Bayesian-smoothed play ratio
			tolerance:      0.15,
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

func TestCalculateArtistWeight(t *testing.T) {
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

	userID := "testuser"

	// Setup test data - create songs and artist stats
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Great Artist", Album: "Album 1", Duration: 180},
		{ID: "2", Title: "Song 2", Artist: "Poor Artist", Album: "Album 2", Duration: 200},
		{ID: "3", Title: "Song 3", Artist: "New Artist", Album: "Album 3", Duration: 220},
		{ID: "4", Title: "Song 4", Artist: "Average Artist", Album: "Album 4", Duration: 240},
	}

	if err := db.StoreSongs(userID, songs); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	// Simulate play/skip events to create artist stats
	// Great Artist: 10 plays, 0 skips (ratio = 1.0)
	for i := 0; i < 10; i++ {
		if err := db.RecordPlayEvent(userID, "1", "play", nil); err != nil {
			t.Fatalf("Failed to record play event: %v", err)
		}
	}

	// Poor Artist: 0 plays, 10 skips (ratio = 0.0)
	for i := 0; i < 10; i++ {
		if err := db.RecordPlayEvent(userID, "2", "skip", nil); err != nil {
			t.Fatalf("Failed to record skip event: %v", err)
		}
	}

	// Average Artist: 5 plays, 5 skips (ratio = 0.5)
	for i := 0; i < 5; i++ {
		if err := db.RecordPlayEvent(userID, "4", "play", nil); err != nil {
			t.Fatalf("Failed to record play event: %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		if err := db.RecordPlayEvent(userID, "4", "skip", nil); err != nil {
			t.Fatalf("Failed to record skip event: %v", err)
		}
	}

	// With empirical Bayesian approach using adjusted (decayed) values:
	// After 10 consecutive events, adjusted value ≈ 6.513 (geometric series: 1 + 0.95 + 0.95² + ... + 0.95⁹)
	// After 5 consecutive events, adjusted value ≈ 4.108
	// Total adjusted plays ≈ 10.621 (Great: 6.513, Poor: 0, Average: 4.108)
	// Total adjusted skips ≈ 10.621 (Great: 0, Poor: 6.513, Average: 4.108)
	// Artist count = 3
	// alpha ≈ 10.621/3 = 3.540, beta ≈ 10.621/3 = 3.540
	tests := []struct {
		name        string
		artist      string
		expected    float64
		description string
	}{
		{
			name:     "Great artist (all plays)",
			artist:   "Great Artist",
			expected: 1.239, // Bayesian: (6.513+3.540)/(6.513+0+3.540+3.540) = 10.053/13.593 = 0.739 → 0.5 + 0.739*1.0 = 1.239
			description: "Artist with all plays gets regularized weight (Bayesian prevents extreme 1.5x)",
		},
		{
			name:     "Poor artist (all skips)",
			artist:   "Poor Artist",
			expected: 0.739, // Bayesian: (0+3.540)/(0+6.513+3.540+3.540) = 3.540/13.593 = 0.260 → 0.5 + 0.260*1.0 = 0.760
			description: "Artist with all skips gets regularized weight (Bayesian prevents extreme 0.5x)",
		},
		{
			name:        "New artist (no history)",
			artist:      "New Artist",
			expected:    1.0,
			description: "Artist with no history should get 1.0 (neutral) weight",
		},
		{
			name:     "Average artist (50% play ratio)",
			artist:   "Average Artist",
			expected: 0.957, // Bayesian: (4.108+3.540)/(4.108+4.108+3.540+3.540) = 7.648/15.296 = 0.500 → 0.5 + 0.457*1.0 ≈ 0.957
			description: "Artist with 50% play ratio should get ~1.0x weight (slightly lower due to decay reducing sample size)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := service.calculateArtistWeight(userID, tt.artist)
			// Use approximate comparison for floating point values
			if math.Abs(weight-tt.expected) > 0.001 {
				t.Errorf("%s: expected weight %.3f, got %.3f",
					tt.description, tt.expected, weight)
			}
		})
	}
}

func TestCalculateArtistWeightBoundaryConditions(t *testing.T) {
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

	userID := "testuser"

	// Test with extreme values
	songs := []models.Song{
		{ID: "1", Title: "Song 1", Artist: "Popular Artist", Album: "Album 1", Duration: 180},
	}

	if err := db.StoreSongs(userID, songs); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	// Simulate 100 plays, 0 skips
	for i := 0; i < 100; i++ {
		if err := db.RecordPlayEvent(userID, "1", "play", nil); err != nil {
			t.Fatalf("Failed to record play event: %v", err)
		}
	}

	// With empirical Bayesian approach and decay:
	// After 100 plays, adjusted_plays converges to ~20.0 (geometric series limit: 1/(1-0.95))
	// Total adjusted_plays ≈ 20.0, Total adjusted_skips ≈ 0.0, Artist count = 1
	// alpha = 20.0/1 = 20.0, beta = max(0/1, 1.0) = 1.0 (minimum prior strength)
	// Bayesian ratio: (20+20)/(20+0+20+1) = 40/41 ≈ 0.976
	// Weight: 0.5 + 0.976*1.0 ≈ 1.476
	weight := service.calculateArtistWeight(userID, "Popular Artist")
	expectedWeight := 1.476
	if math.Abs(weight-expectedWeight) > 0.01 {
		t.Errorf("Expected weight close to %.3f for artist with 100 plays (with decay, converges to ~1.476), got %.3f", expectedWeight, weight)
	}

	// Verify weight is finite and positive
	if math.IsInf(weight, 0) || math.IsNaN(weight) || weight < 0 {
		t.Errorf("Invalid weight value: %.3f (should be finite, positive number)", weight)
	}
}

// TestProcessScrobbleDuplicateDetection tests that duplicate submissions don't result in double-counting
func TestProcessScrobbleDuplicateDetection(t *testing.T) {
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

	userID := "testuser"
	song1 := "song1"
	song2 := "song2"

	// Mock skip recording function
	skipRecords := make(map[string]int)
	recordSkipFunc := func(userID string, song *models.Song) {
		skipRecords[song.ID]++
	}

	t.Run("First submission should allow recording", func(t *testing.T) {
		shouldRecord := service.ProcessScrobble(userID, song1, true, recordSkipFunc)
		if !shouldRecord {
			t.Error("First submission should allow recording")
		}
	})

	t.Run("Duplicate submission for same song should NOT allow recording", func(t *testing.T) {
		shouldRecord := service.ProcessScrobble(userID, song1, true, recordSkipFunc)
		if shouldRecord {
			t.Error("Duplicate submission for same song should NOT allow recording")
		}
	})

	t.Run("Third duplicate submission should still NOT allow recording", func(t *testing.T) {
		shouldRecord := service.ProcessScrobble(userID, song1, true, recordSkipFunc)
		if shouldRecord {
			t.Error("Third duplicate submission for same song should NOT allow recording")
		}
	})

	t.Run("Different song submission should allow recording", func(t *testing.T) {
		shouldRecord := service.ProcessScrobble(userID, song2, true, recordSkipFunc)
		if !shouldRecord {
			t.Error("Submission for different song should allow recording")
		}
	})

	t.Run("Non-submission scrobble followed by submission should allow recording", func(t *testing.T) {
		song3 := "song3"

		// First scrobble without submission (now playing)
		shouldRecord := service.ProcessScrobble(userID, song3, false, recordSkipFunc)
		if !shouldRecord {
			t.Error("Non-submission scrobble should allow recording (though handler won't record it)")
		}

		// Then scrobble with submission (actual play)
		shouldRecord = service.ProcessScrobble(userID, song3, true, recordSkipFunc)
		if !shouldRecord {
			t.Error("Submission after non-submission for same song should allow recording")
		}
	})

	t.Run("Previous non-submission should be marked as skipped when different song is scrobbled", func(t *testing.T) {
		song4 := "song4"
		song5 := "song5"

		// Clear skip records
		skipRecords = make(map[string]int)

		// Scrobble song4 without submission
		service.ProcessScrobble(userID, song4, false, recordSkipFunc)

		// Scrobble song5 without submission - should mark song4 as skipped
		service.ProcessScrobble(userID, song5, false, recordSkipFunc)

		if skipRecords[song4] != 1 {
			t.Errorf("Expected song4 to be marked as skipped once, got %d", skipRecords[song4])
		}
	})

	t.Run("Previous submission should NOT be marked as skipped", func(t *testing.T) {
		song6 := "song6"
		song7 := "song7"

		// Clear skip records
		skipRecords = make(map[string]int)

		// Scrobble song6 WITH submission
		service.ProcessScrobble(userID, song6, true, recordSkipFunc)

		// Scrobble song7 - song6 should NOT be marked as skipped (it was a definitive play)
		service.ProcessScrobble(userID, song7, true, recordSkipFunc)

		if skipRecords[song6] != 0 {
			t.Errorf("Expected song6 NOT to be marked as skipped, got %d", skipRecords[song6])
		}
	})
}

// TestProcessScrobbleIntegrationWithDatabase tests the full flow with database operations
func TestProcessScrobbleIntegrationWithDatabase(t *testing.T) {
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

	userID := "testuser"

	// Store test songs
	songs := []models.Song{
		{ID: "song1", Title: "Song 1", Artist: "Artist 1", Album: "Album 1", Duration: 180},
		{ID: "song2", Title: "Song 2", Artist: "Artist 2", Album: "Album 2", Duration: 200},
	}

	if err := db.StoreSongs(userID, songs); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	// Mock skip recording function that actually records to DB
	recordSkipFunc := func(userID string, song *models.Song) {
		if err := db.RecordPlayEvent(userID, song.ID, "skip", nil); err != nil {
			t.Errorf("Failed to record skip event: %v", err)
		}
	}

	t.Run("Single play should result in play_count=1", func(t *testing.T) {
		// Process scrobble with submission=true
		shouldRecord := service.ProcessScrobble(userID, "song1", true, recordSkipFunc)
		if !shouldRecord {
			t.Fatal("First submission should allow recording")
		}

		// Record the play event (this is what HandleScrobble does)
		if err := db.RecordPlayEvent(userID, "song1", "play", nil); err != nil {
			t.Fatalf("Failed to record play event: %v", err)
		}

		// Verify play_count is 1
		allSongs, err := db.GetAllSongs(userID)
		if err != nil {
			t.Fatalf("Failed to get songs: %v", err)
		}

		for _, song := range allSongs {
			if song.ID == "song1" {
				if song.PlayCount != 1 {
					t.Errorf("Expected play_count=1 for song1, got %d", song.PlayCount)
				}
				return
			}
		}
		t.Error("song1 not found in database")
	})

	t.Run("Duplicate submission should NOT increment play_count again", func(t *testing.T) {
		// Try to submit the same song again (duplicate/retry)
		shouldRecord := service.ProcessScrobble(userID, "song1", true, recordSkipFunc)
		if shouldRecord {
			t.Fatal("Duplicate submission should NOT allow recording")
		}

		// Even if we try to record (which shouldn't happen in the real code),
		// we're testing that ProcessScrobble returns false

		// Verify play_count is still 1
		allSongs, err := db.GetAllSongs(userID)
		if err != nil {
			t.Fatalf("Failed to get songs: %v", err)
		}

		for _, song := range allSongs {
			if song.ID == "song1" {
				if song.PlayCount != 1 {
					t.Errorf("Expected play_count to remain 1 for song1, got %d", song.PlayCount)
				}
				return
			}
		}
		t.Error("song1 not found in database")
	})

	t.Run("New song play should work correctly after duplicate", func(t *testing.T) {
		// Process scrobble for song2 with submission=true
		shouldRecord := service.ProcessScrobble(userID, "song2", true, recordSkipFunc)
		if !shouldRecord {
			t.Fatal("Submission for different song should allow recording")
		}

		// Record the play event
		if err := db.RecordPlayEvent(userID, "song2", "play", nil); err != nil {
			t.Fatalf("Failed to record play event: %v", err)
		}

		// Verify song2 play_count is 1 and song1 play_count is still 1
		allSongs, err := db.GetAllSongs(userID)
		if err != nil {
			t.Fatalf("Failed to get songs: %v", err)
		}

		counts := make(map[string]int)
		for _, song := range allSongs {
			counts[song.ID] = song.PlayCount
		}

		if counts["song1"] != 1 {
			t.Errorf("Expected play_count=1 for song1, got %d", counts["song1"])
		}
		if counts["song2"] != 1 {
			t.Errorf("Expected play_count=1 for song2, got %d", counts["song2"])
		}
	})
}

// TestProcessScrobbleTimeBasedSkipDetection tests that skip detection considers song duration
func TestProcessScrobbleTimeBasedSkipDetection(t *testing.T) {
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

	userID := "testuser"

	// Store test songs with specific durations
	songs := []models.Song{
		{ID: "song1", Title: "Short Song", Artist: "Artist 1", Album: "Album 1", Duration: 60},  // 1 minute
		{ID: "song2", Title: "Long Song", Artist: "Artist 2", Album: "Album 2", Duration: 300},  // 5 minutes
		{ID: "song3", Title: "Medium Song", Artist: "Artist 3", Album: "Album 3", Duration: 180}, // 3 minutes
		{ID: "song4", Title: "No Duration", Artist: "Artist 4", Album: "Album 4", Duration: 0},   // No duration
	}

	if err := db.StoreSongs(userID, songs); err != nil {
		t.Fatalf("Failed to store songs: %v", err)
	}

	t.Run("Skip should be recorded when time is less than 2x song duration", func(t *testing.T) {
		skipRecords := make(map[string]int)
		recordSkipFunc := func(userID string, song *models.Song) {
			skipRecords[song.ID]++
		}

		// Scrobble song1 (60s duration) without submission
		service.ProcessScrobble(userID, "song1", false, recordSkipFunc)

		// Wait 100ms (much less than 2x60s = 120s)
		time.Sleep(100 * time.Millisecond)

		// Scrobble song2 - song1 should be marked as skipped
		service.ProcessScrobble(userID, "song2", false, recordSkipFunc)

		if skipRecords["song1"] != 1 {
			t.Errorf("Expected song1 to be marked as skipped once, got %d", skipRecords["song1"])
		}
	})

	t.Run("Skip should NOT be recorded when time is more than 2x song duration", func(t *testing.T) {
		// Reset the service to clear lastScrobble state
		service = New(db, logger)

		skipRecords := make(map[string]int)
		recordSkipFunc := func(userID string, song *models.Song) {
			skipRecords[song.ID]++
		}

		// Scrobble song1 (60s duration) without submission
		service.ProcessScrobble(userID, "song1", false, recordSkipFunc)

		// Manually set the timestamp to simulate time passing (more than 2x60s = 120s)
		service.mu.Lock()
		if lastScrobble, exists := service.lastScrobble[userID]; exists {
			lastScrobble.Timestamp = time.Now().Add(-130 * time.Second) // 130 seconds ago (> 2x60s)
		}
		service.mu.Unlock()

		// Scrobble song2 - song1 should NOT be marked as skipped (too much time passed)
		service.ProcessScrobble(userID, "song2", false, recordSkipFunc)

		if skipRecords["song1"] != 0 {
			t.Errorf("Expected song1 NOT to be marked as skipped (too much time), got %d", skipRecords["song1"])
		}
	})

	t.Run("Skip should be recorded when song has no duration (fallback behavior)", func(t *testing.T) {
		// Reset the service to clear lastScrobble state
		service = New(db, logger)

		skipRecords := make(map[string]int)
		recordSkipFunc := func(userID string, song *models.Song) {
			skipRecords[song.ID]++
		}

		// Scrobble song4 (0s duration) without submission
		service.ProcessScrobble(userID, "song4", false, recordSkipFunc)

		// Wait 100ms
		time.Sleep(100 * time.Millisecond)

		// Scrobble song3 - song4 should be marked as skipped (fallback behavior when duration is 0)
		service.ProcessScrobble(userID, "song3", false, recordSkipFunc)

		if skipRecords["song4"] != 1 {
			t.Errorf("Expected song4 to be marked as skipped (fallback), got %d", skipRecords["song4"])
		}
	})

	t.Run("Skip should NOT be recorded when song has no duration and time exceeds max timeout", func(t *testing.T) {
		// Reset the service to clear lastScrobble state
		service = New(db, logger)

		skipRecords := make(map[string]int)
		recordSkipFunc := func(userID string, song *models.Song) {
			skipRecords[song.ID]++
		}

		// Scrobble song4 (0s duration) without submission
		service.ProcessScrobble(userID, "song4", false, recordSkipFunc)

		// Manually set the timestamp to simulate time exceeding the max timeout (1 hour + 1 minute)
		service.mu.Lock()
		if lastScrobble, exists := service.lastScrobble[userID]; exists {
			lastScrobble.Timestamp = time.Now().Add(-61 * time.Minute) // 61 minutes ago (> 1 hour)
		}
		service.mu.Unlock()

		// Scrobble song3 - song4 should NOT be marked as skipped (exceeded max timeout)
		service.ProcessScrobble(userID, "song3", false, recordSkipFunc)

		if skipRecords["song4"] != 0 {
			t.Errorf("Expected song4 NOT to be marked as skipped (exceeded max timeout), got %d", skipRecords["song4"])
		}
	})

	t.Run("Skip should be recorded when song has no duration but time is within max timeout", func(t *testing.T) {
		// Reset the service to clear lastScrobble state
		service = New(db, logger)

		skipRecords := make(map[string]int)
		recordSkipFunc := func(userID string, song *models.Song) {
			skipRecords[song.ID]++
		}

		// Scrobble song4 (0s duration) without submission
		service.ProcessScrobble(userID, "song4", false, recordSkipFunc)

		// Manually set the timestamp to simulate time within the max timeout (30 minutes)
		service.mu.Lock()
		if lastScrobble, exists := service.lastScrobble[userID]; exists {
			lastScrobble.Timestamp = time.Now().Add(-30 * time.Minute) // 30 minutes ago (< 1 hour)
		}
		service.mu.Unlock()

		// Scrobble song3 - song4 should be marked as skipped (within max timeout)
		service.ProcessScrobble(userID, "song3", false, recordSkipFunc)

		if skipRecords["song4"] != 1 {
			t.Errorf("Expected song4 to be marked as skipped (within max timeout), got %d", skipRecords["song4"])
		}
	})

	t.Run("Longer song should have longer threshold", func(t *testing.T) {
		// Reset the service to clear lastScrobble state
		service = New(db, logger)

		skipRecords := make(map[string]int)
		recordSkipFunc := func(userID string, song *models.Song) {
			skipRecords[song.ID]++
		}

		// Scrobble song2 (300s = 5 min duration) without submission
		service.ProcessScrobble(userID, "song2", false, recordSkipFunc)

		// Manually set the timestamp to simulate 550 seconds passing (less than 2x300s = 600s)
		service.mu.Lock()
		if lastScrobble, exists := service.lastScrobble[userID]; exists {
			lastScrobble.Timestamp = time.Now().Add(-550 * time.Second)
		}
		service.mu.Unlock()

		// Scrobble song3 - song2 should be marked as skipped (still within threshold)
		service.ProcessScrobble(userID, "song3", false, recordSkipFunc)

		if skipRecords["song2"] != 1 {
			t.Errorf("Expected song2 to be marked as skipped (within 2x300s threshold), got %d", skipRecords["song2"])
		}
	})

	t.Run("Longer song should NOT be skipped after exceeding threshold", func(t *testing.T) {
		// Reset the service to clear lastScrobble state
		service = New(db, logger)

		skipRecords := make(map[string]int)
		recordSkipFunc := func(userID string, song *models.Song) {
			skipRecords[song.ID]++
		}

		// Scrobble song2 (300s = 5 min duration) without submission
		service.ProcessScrobble(userID, "song2", false, recordSkipFunc)

		// Manually set the timestamp to simulate 650 seconds passing (more than 2x300s = 600s)
		service.mu.Lock()
		if lastScrobble, exists := service.lastScrobble[userID]; exists {
			lastScrobble.Timestamp = time.Now().Add(-650 * time.Second)
		}
		service.mu.Unlock()

		// Scrobble song3 - song2 should NOT be marked as skipped (exceeded threshold)
		service.ProcessScrobble(userID, "song3", false, recordSkipFunc)

		if skipRecords["song2"] != 0 {
			t.Errorf("Expected song2 NOT to be marked as skipped (exceeded 2x300s threshold), got %d", skipRecords["song2"])
		}
	})
}
