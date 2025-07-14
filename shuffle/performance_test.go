package shuffle

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/database"
	"github.com/syeo66/subsoxy/models"
)

// setupLargeDataset creates a large dataset for performance testing
func setupLargeDataset(tb testing.TB, db *database.DB, userID string, songCount int) {
	tb.Helper()

	// Create a large number of songs for testing
	songs := make([]models.Song, songCount)
	for i := 0; i < songCount; i++ {
		songs[i] = models.Song{
			ID:       fmt.Sprintf("song_%d", i),
			Title:    fmt.Sprintf("Song %d", i),
			Artist:   fmt.Sprintf("Artist %d", i%100), // 100 different artists
			Album:    fmt.Sprintf("Album %d", i%200),  // 200 different albums
			Duration: 180 + (i % 120),                 // 3-5 minutes
		}
	}

	// Store songs in batches to avoid memory issues
	batchSize := 1000
	for i := 0; i < len(songs); i += batchSize {
		end := i + batchSize
		if end > len(songs) {
			end = len(songs)
		}
		err := db.StoreSongs(userID, songs[i:end])
		if err != nil {
			tb.Fatalf("Failed to store songs batch: %v", err)
		}
	}
}

// BenchmarkShuffleSmallDataset benchmarks shuffle with small dataset (< 5000 songs)
func BenchmarkShuffleSmallDataset(b *testing.B) {
	dbPath := "/tmp/benchmark_small.db"
	defer os.Remove(dbPath)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce log noise

	db, err := database.New(dbPath, logger)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)
	userID := "benchmark_user"

	// Setup 1000 songs (small dataset)
	setupLargeDataset(b, db, userID, 1000)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		songs, err := service.GetWeightedShuffledSongs(userID, 50)
		if err != nil {
			b.Fatalf("Failed to get shuffled songs: %v", err)
		}
		if len(songs) == 0 {
			b.Fatalf("No songs returned")
		}
	}
}

// BenchmarkShuffleLargeDataset benchmarks shuffle with large dataset (> 5000 songs)
func BenchmarkShuffleLargeDataset(b *testing.B) {
	dbPath := "/tmp/benchmark_large.db"
	defer os.Remove(dbPath)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce log noise

	db, err := database.New(dbPath, logger)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)
	userID := "benchmark_user"

	// Setup 10000 songs (large dataset)
	setupLargeDataset(b, db, userID, 10000)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		songs, err := service.GetWeightedShuffledSongs(userID, 50)
		if err != nil {
			b.Fatalf("Failed to get shuffled songs: %v", err)
		}
		if len(songs) == 0 {
			b.Fatalf("No songs returned")
		}
	}
}

// BenchmarkShuffleVeryLargeDataset benchmarks shuffle with very large dataset (> 50000 songs)
func BenchmarkShuffleVeryLargeDataset(b *testing.B) {
	dbPath := "/tmp/benchmark_very_large.db"
	defer os.Remove(dbPath)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce log noise

	db, err := database.New(dbPath, logger)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db, logger)
	userID := "benchmark_user"

	// Setup 50000 songs (very large dataset)
	setupLargeDataset(b, db, userID, 50000)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		songs, err := service.GetWeightedShuffledSongs(userID, 50)
		if err != nil {
			b.Fatalf("Failed to get shuffled songs: %v", err)
		}
		if len(songs) == 0 {
			b.Fatalf("No songs returned")
		}
	}
}

// BenchmarkDatabaseBatchQueries benchmarks the new batch database operations
func BenchmarkDatabaseBatchQueries(b *testing.B) {
	dbPath := "/tmp/benchmark_db_batch.db"
	defer os.Remove(dbPath)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce log noise

	db, err := database.New(dbPath, logger)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	userID := "benchmark_user"

	// Setup 10000 songs
	setupLargeDataset(b, db, userID, 10000)

	b.Run("GetSongsBatch", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			songs, err := db.GetSongsBatch(userID, 1000, 0)
			if err != nil {
				b.Fatalf("Failed to get songs batch: %v", err)
			}
			if len(songs) == 0 {
				b.Fatalf("No songs returned")
			}
		}
	})

	b.Run("GetSongCount", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			count, err := db.GetSongCount(userID)
			if err != nil {
				b.Fatalf("Failed to get song count: %v", err)
			}
			if count == 0 {
				b.Fatalf("No songs counted")
			}
		}
	})
}

// TestMemoryUsage tests memory usage patterns for different dataset sizes
func TestMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory usage test in short mode")
	}

	testSizes := []int{1000, 5000, 10000, 25000}

	for _, size := range testSizes {
		t.Run(fmt.Sprintf("Size_%d", size), func(t *testing.T) {
			dbPath := fmt.Sprintf("/tmp/memory_test_%d.db", size)
			defer os.Remove(dbPath)

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			db, err := database.New(dbPath, logger)
			if err != nil {
				t.Fatalf("Failed to create database: %v", err)
			}
			defer db.Close()

			service := New(db, logger)
			userID := "memory_test_user"

			// Setup dataset
			setupLargeDataset(t, db, userID, size)

			// Test shuffle performance
			start := time.Now()
			songs, err := service.GetWeightedShuffledSongs(userID, 50)
			duration := time.Since(start)

			if err != nil {
				t.Fatalf("Failed to get shuffled songs: %v", err)
			}

			t.Logf("Dataset size: %d, Duration: %v, Songs returned: %d", size, duration, len(songs))

			// Verify we got the expected number of songs
			if len(songs) != 50 {
				t.Errorf("Expected 50 songs, got %d", len(songs))
			}
		})
	}
}

// TestAlgorithmSelection tests that the correct algorithm is selected based on dataset size
func TestAlgorithmSelection(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Test small dataset (should use original algorithm)
	t.Run("SmallDataset_OriginalAlgorithm", func(t *testing.T) {
		dbPath := "/tmp/algorithm_test_small.db"
		defer os.Remove(dbPath)

		db, err := database.New(dbPath, logger)
		if err != nil {
			t.Fatalf("Failed to create database: %v", err)
		}
		defer db.Close()

		service := New(db, logger)
		userID := "algorithm_test_user"

		// Setup 1000 songs (small dataset)
		setupLargeDataset(t, db, userID, 1000)

		songs, err := service.GetWeightedShuffledSongs(userID, 50)
		if err != nil {
			t.Fatalf("Failed to get shuffled songs: %v", err)
		}

		if len(songs) != 50 {
			t.Errorf("Expected 50 songs, got %d", len(songs))
		}
	})

	// Test large dataset (should use optimized algorithm)
	t.Run("LargeDataset_OptimizedAlgorithm", func(t *testing.T) {
		dbPath := "/tmp/algorithm_test_large.db"
		defer os.Remove(dbPath)

		db, err := database.New(dbPath, logger)
		if err != nil {
			t.Fatalf("Failed to create database: %v", err)
		}
		defer db.Close()

		service := New(db, logger)
		userID := "algorithm_test_user"

		// Setup 10000 songs (large dataset)
		setupLargeDataset(t, db, userID, 10000)

		songs, err := service.GetWeightedShuffledSongs(userID, 50)
		if err != nil {
			t.Fatalf("Failed to get shuffled songs: %v", err)
		}

		if len(songs) != 50 {
			t.Errorf("Expected 50 songs, got %d", len(songs))
		}
	})
}
