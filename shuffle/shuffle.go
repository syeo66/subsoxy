package shuffle

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/syeo66/subsoxy/database"
	"github.com/syeo66/subsoxy/models"
)

// Algorithm selection constants
const (
	LargeLibraryThreshold = 5000
	BatchSize             = 1000
	OversampleFactor      = 3
)

// Weight calculation constants
const (
	NeverPlayedWeight      = 2.0
	HoursPerDay            = 24.0
	TimeDecayDaysThreshold = 30
	TimeDecayMinWeight     = 0.1
	TimeDecayMaxWeight     = 0.9
	DaysPerYear            = 365.0
	UnplayedSongWeight     = 1.5
	PlayRatioMinWeight     = 0.2
	PlayRatioMaxWeight     = 1.8
	BaseTransitionWeight   = 0.5
)

type Service struct {
	db          *database.DB
	logger      *logrus.Logger
	lastPlayed  map[string]*models.Song // Map userID to last played song
	lastStarted map[string]*models.Song // Map userID to last started song
	mu          sync.RWMutex            // Protects lastPlayed and lastStarted maps
}

func New(db *database.DB, logger *logrus.Logger) *Service {
	return &Service{
		db:          db,
		logger:      logger,
		lastPlayed:  make(map[string]*models.Song),
		lastStarted: make(map[string]*models.Song),
	}
}

func (s *Service) SetLastPlayed(userID string, song *models.Song) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPlayed[userID] = song
}

// SetLastStarted records when a song starts streaming
func (s *Service) SetLastStarted(userID string, song *models.Song) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastStarted[userID] = song
}

// CheckForSkip checks if the previous started song was skipped and returns it if so
func (s *Service) CheckForSkip(userID string, newSong *models.Song) (*models.Song, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	lastStarted, hasStarted := s.lastStarted[userID]
	if !hasStarted || lastStarted == nil {
		return nil, false // No previous song to check
	}
	
	lastPlayed, hasPlayed := s.lastPlayed[userID]
	
	// If the last started song is different from the new song and wasn't played, it was skipped
	if lastStarted.ID != newSong.ID {
		if !hasPlayed || lastPlayed == nil || lastPlayed.ID != lastStarted.ID {
			return lastStarted, true // This song was skipped
		}
	}
	
	return nil, false // No skip detected
}

func (s *Service) GetWeightedShuffledSongs(userID string, count int) ([]models.Song, error) {
	// For small libraries, use the original algorithm
	totalSongs, err := s.db.GetSongCount(userID)
	if err != nil {
		return nil, err
	}

	// Switch to memory-efficient algorithm for large libraries
	if totalSongs > LargeLibraryThreshold {
		return s.getWeightedShuffledSongsOptimized(userID, count, totalSongs)
	}

	// Original algorithm for small libraries
	songs, err := s.db.GetAllSongs(userID)
	if err != nil {
		return nil, err
	}

	weightedSongs := make([]models.WeightedSong, 0, len(songs))
	for _, song := range songs {
		weight := s.calculateSongWeight(userID, song)
		weightedSongs = append(weightedSongs, models.WeightedSong{
			Song:   song,
			Weight: weight,
		})
	}

	sort.Slice(weightedSongs, func(i, j int) bool {
		return weightedSongs[i].Weight > weightedSongs[j].Weight
	})

	totalWeight := 0.0
	for _, ws := range weightedSongs {
		totalWeight += ws.Weight
	}

	result := make([]models.Song, 0, count)
	used := make(map[string]bool)

	for len(result) < count && len(result) < len(songs) {
		target := rand.Float64() * totalWeight
		current := 0.0

		for _, ws := range weightedSongs {
			if used[ws.Song.ID] {
				continue
			}
			current += ws.Weight
			if current >= target {
				result = append(result, ws.Song)
				used[ws.Song.ID] = true
				totalWeight -= ws.Weight
				break
			}
		}
	}

	return result, nil
}

// getWeightedShuffledSongsOptimized implements a memory-efficient shuffle algorithm
// for large song libraries using reservoir sampling and batch processing
func (s *Service) getWeightedShuffledSongsOptimized(userID string, count int, totalSongs int) ([]models.Song, error) {
	const batchSize = BatchSize
	result := make([]models.Song, 0, count)

	// Use reservoir sampling approach to avoid loading all songs
	// We'll sample more songs than needed to account for weight distribution
	oversampleFactor := OversampleFactor
	sampleSize := count * oversampleFactor
	if sampleSize > totalSongs {
		sampleSize = totalSongs
	}

	// Create reservoir for sampling
	reservoir := make([]models.Song, 0, sampleSize)

	// Process songs in batches to control memory usage
	for offset := 0; offset < totalSongs; offset += batchSize {
		batch, err := s.db.GetSongsBatch(userID, batchSize, offset)
		if err != nil {
			return nil, err
		}

		// Apply reservoir sampling to this batch
		for _, song := range batch {
			if len(reservoir) < sampleSize {
				reservoir = append(reservoir, song)
			} else {
				// Replace with probability sampleSize/totalProcessed
				randomIndex := rand.Intn(offset + len(reservoir))
				if randomIndex < sampleSize {
					reservoir[randomIndex] = song
				}
			}
		}
	}

	// Now apply weights to the sampled songs
	weightedSongs := make([]models.WeightedSong, 0, len(reservoir))

	// Get transition probabilities in batch to avoid N+1 queries
	var transitionProbabilities map[string]float64
	s.mu.RLock()
	lastPlayed, exists := s.lastPlayed[userID]
	s.mu.RUnlock()

	if exists && lastPlayed != nil {
		songIDs := make([]string, len(reservoir))
		for i, song := range reservoir {
			songIDs[i] = song.ID
		}

		var err error
		transitionProbabilities, err = s.db.GetTransitionProbabilities(userID, lastPlayed.ID, songIDs)
		if err != nil {
			s.logger.WithError(err).WithField("userID", userID).Error("Failed to get transition probabilities, using defaults")
			transitionProbabilities = make(map[string]float64)
		}
	} else {
		transitionProbabilities = make(map[string]float64)
	}

	for _, song := range reservoir {
		weight := s.calculateSongWeightWithTransition(userID, song, transitionProbabilities[song.ID])
		weightedSongs = append(weightedSongs, models.WeightedSong{
			Song:   song,
			Weight: weight,
		})
	}

	// Sort by weight for biased selection
	sort.Slice(weightedSongs, func(i, j int) bool {
		return weightedSongs[i].Weight > weightedSongs[j].Weight
	})

	// Select final songs using weighted distribution
	totalWeight := 0.0
	for _, ws := range weightedSongs {
		totalWeight += ws.Weight
	}

	used := make(map[string]bool)

	for len(result) < count && len(result) < len(weightedSongs) {
		target := rand.Float64() * totalWeight
		current := 0.0

		for _, ws := range weightedSongs {
			if used[ws.Song.ID] {
				continue
			}
			current += ws.Weight
			if current >= target {
				result = append(result, ws.Song)
				used[ws.Song.ID] = true
				totalWeight -= ws.Weight
				break
			}
		}
	}

	s.logger.WithFields(logrus.Fields{
		"userID":      userID,
		"totalSongs":  totalSongs,
		"sampleSize":  sampleSize,
		"resultCount": len(result),
		"algorithm":   "optimized",
	}).Debug("Completed optimized weighted shuffle")

	return result, nil
}

func (s *Service) calculateSongWeight(userID string, song models.Song) float64 {
	baseWeight := 1.0

	timeWeight := s.calculateTimeDecayWeight(song.LastPlayed)
	playSkipWeight := s.calculatePlaySkipWeight(song.PlayCount, song.SkipCount)
	transitionWeight := s.calculateTransitionWeight(userID, song.ID)

	finalWeight := baseWeight * timeWeight * playSkipWeight * transitionWeight

	s.logger.WithFields(logrus.Fields{
		"userID":           userID,
		"songId":           song.ID,
		"timeWeight":       timeWeight,
		"playSkipWeight":   playSkipWeight,
		"transitionWeight": transitionWeight,
		"finalWeight":      finalWeight,
	}).Debug("Calculated song weight")

	return finalWeight
}

// calculateSongWeightWithTransition calculates song weight with pre-computed transition probability
// to avoid N+1 database queries when processing batches
func (s *Service) calculateSongWeightWithTransition(userID string, song models.Song, transitionProbability float64) float64 {
	baseWeight := 1.0

	timeWeight := s.calculateTimeDecayWeight(song.LastPlayed)
	playSkipWeight := s.calculatePlaySkipWeight(song.PlayCount, song.SkipCount)

	// Use provided transition probability or default to 1.0 if not available
	transitionWeight := 1.0
	if transitionProbability > 0 {
		transitionWeight = BaseTransitionWeight + transitionProbability
	}

	finalWeight := baseWeight * timeWeight * playSkipWeight * transitionWeight

	s.logger.WithFields(logrus.Fields{
		"userID":           userID,
		"songId":           song.ID,
		"timeWeight":       timeWeight,
		"playSkipWeight":   playSkipWeight,
		"transitionWeight": transitionWeight,
		"finalWeight":      finalWeight,
	}).Debug("Calculated song weight (optimized)")

	return finalWeight
}

func (s *Service) calculateTimeDecayWeight(lastPlayed time.Time) float64 {
	if lastPlayed.IsZero() {
		return NeverPlayedWeight
	}

	daysSinceLastPlayed := time.Since(lastPlayed).Hours() / HoursPerDay

	if daysSinceLastPlayed < TimeDecayDaysThreshold {
		return TimeDecayMinWeight + (daysSinceLastPlayed/TimeDecayDaysThreshold)*TimeDecayMaxWeight
	}

	return 1.0 + math.Min(daysSinceLastPlayed/DaysPerYear, 1.0)
}

func (s *Service) calculatePlaySkipWeight(playCount, skipCount int) float64 {
	if playCount == 0 && skipCount == 0 {
		return UnplayedSongWeight
	}

	totalEvents := playCount + skipCount
	if totalEvents == 0 {
		return 1.0
	}

	playRatio := float64(playCount) / float64(totalEvents)
	return PlayRatioMinWeight + (playRatio * PlayRatioMaxWeight)
}

func (s *Service) calculateTransitionWeight(userID, songID string) float64 {
	s.mu.RLock()
	lastPlayed, exists := s.lastPlayed[userID]
	s.mu.RUnlock()

	if !exists || lastPlayed == nil {
		return 1.0
	}

	probability, err := s.db.GetTransitionProbability(userID, lastPlayed.ID, songID)
	if err != nil {
		return 1.0
	}

	return BaseTransitionWeight + probability
}
