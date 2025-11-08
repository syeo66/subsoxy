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
	NeverPlayedWeight      = 4.0
	HoursPerDay            = 24.0
	TimeDecayDaysThreshold = 30
	TimeDecayMinWeight     = 0.1
	TimeDecayMaxWeight     = 0.9
	DaysPerYear            = 365.0
	UnplayedSongWeight     = 1.5
	PlayRatioMinWeight     = 0.2
	PlayRatioMaxWeight     = 1.8
	BaseTransitionWeight   = 0.5
	TwoWeekReplayThreshold = 14  // Minimum days before a song can be replayed (unless no alternatives)
	ArtistRatioMinWeight   = 0.5 // Minimum weight multiplier for artists with poor play/skip ratio
	ArtistRatioMaxWeight   = 1.5 // Maximum weight multiplier for artists with good play/skip ratio
)

// ScrobbleInfo tracks the last scrobble for skip detection
type ScrobbleInfo struct {
	Song         *models.Song
	IsSubmission bool
	Timestamp    time.Time
}

type Service struct {
	db           *database.DB
	logger       *logrus.Logger
	lastPlayed   map[string]*models.Song  // Map userID to last played song
	lastScrobble map[string]*ScrobbleInfo // Map userID to last scrobble info
	mu           sync.RWMutex             // Protects all maps
}

func New(db *database.DB, logger *logrus.Logger) *Service {
	return &Service{
		db:           db,
		logger:       logger,
		lastPlayed:   make(map[string]*models.Song),
		lastScrobble: make(map[string]*ScrobbleInfo),
	}
}

func (s *Service) SetLastPlayed(userID string, song *models.Song) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPlayed[userID] = song
}

// ProcessScrobble processes a scrobble event with simplified skip detection
func (s *Service) ProcessScrobble(userID, songID string, isSubmission bool, recordSkipFunc func(string, *models.Song)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if there was a previous scrobble
	lastScrobble, hadPreviousScrobble := s.lastScrobble[userID]

	// If there was a previous scrobble that wasn't a definitive play, mark it as skipped
	if hadPreviousScrobble && !lastScrobble.IsSubmission {
		recordSkipFunc(userID, lastScrobble.Song)
		s.logger.WithFields(logrus.Fields{
			"user_id": userID,
			"song_id": lastScrobble.Song.ID,
			"reason":  "followed_by_another_scrobble",
		}).Debug("Marking previous scrobble as skipped")
	}

	// Update the last scrobble info
	s.lastScrobble[userID] = &ScrobbleInfo{
		Song:         &models.Song{ID: songID},
		IsSubmission: isSubmission,
		Timestamp:    time.Now(),
	}

	s.logger.WithFields(logrus.Fields{
		"user_id":    userID,
		"song_id":    songID,
		"submission": isSubmission,
	}).Debug("Processed scrobble")
}

// GetWeightedShuffledSongs returns a shuffled list of songs based on user listening history
// with strict 2-week replay prevention. Songs played OR skipped within the last 14 days are
// strictly excluded from the results.
// Uses consistent cutoff time calculation and improved database filtering for reliability.
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

	// Calculate cutoff time once for consistency to prevent edge cases
	// from multiple time.Now() calls across components
	now := time.Now()
	twoWeeksAgo := now.AddDate(0, 0, -TwoWeekReplayThreshold)

	var eligibleSongs []models.Song
	var recentSongs []models.Song

	for _, song := range songs {
		if (song.LastPlayed.IsZero() || song.LastPlayed.Before(twoWeeksAgo)) && (song.LastSkipped.IsZero() || song.LastSkipped.Before(twoWeeksAgo)) {
			eligibleSongs = append(eligibleSongs, song)
		} else {
			recentSongs = append(recentSongs, song)
		}
	}

	s.logger.WithFields(logrus.Fields{
		"userID":         userID,
		"totalSongs":     len(songs),
		"eligibleSongs":  len(eligibleSongs),
		"recentSongs":    len(recentSongs),
		"requestedCount": count,
	}).Debug("Filtered songs by 2-week replay threshold")

	// Only use eligible songs - no fallback to recent songs
	weightedSongs := make([]models.WeightedSong, 0, len(eligibleSongs))
	for _, song := range eligibleSongs {
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

	return result, nil
}

// getWeightedShuffledSongsOptimized implements a memory-efficient shuffle algorithm
// for large song libraries using reservoir sampling and batch processing with strict 2-week
// replay prevention. Filters at the database level for optimal memory usage.
// Uses consistent cutoff time passed to database methods for timing consistency.
func (s *Service) getWeightedShuffledSongsOptimized(userID string, count int, totalSongs int) ([]models.Song, error) {
	const batchSize = BatchSize
	result := make([]models.Song, 0, count)

	// Calculate cutoff time once for consistency to prevent edge cases
	// from multiple time.Now() calls across database methods
	now := time.Now()
	cutoffTime := now.AddDate(0, 0, -TwoWeekReplayThreshold)

	// First try to get songs that haven't been played within 2 weeks
	eligibleSongs, err := s.db.GetSongCountFiltered(userID, cutoffTime)
	if err != nil {
		return nil, err
	}

	// Always use filtered songs - no fallback to recent songs
	songsToSampleFrom := eligibleSongs
	useFiltered := true

	s.logger.WithFields(logrus.Fields{
		"userID":         userID,
		"eligibleSongs":  eligibleSongs,
		"totalSongs":     totalSongs,
		"requestedCount": count,
	}).Debug("Using filtered songs (not played within 2 weeks)")

	// Use reservoir sampling approach to avoid loading all songs
	// We'll sample more songs than needed to account for weight distribution
	oversampleFactor := OversampleFactor
	sampleSize := count * oversampleFactor
	if sampleSize > songsToSampleFrom {
		sampleSize = songsToSampleFrom
	}

	// Create reservoir for sampling
	reservoir := make([]models.Song, 0, sampleSize)

	// Track total number of songs processed (not database offset)
	totalProcessed := 0

	// Process songs in batches to control memory usage
	for offset := 0; offset < songsToSampleFrom; offset += batchSize {
		var batch []models.Song
		var err error

		if useFiltered {
			batch, err = s.db.GetSongsBatchFiltered(userID, batchSize, offset, cutoffTime)
		} else {
			batch, err = s.db.GetSongsBatch(userID, batchSize, offset)
		}

		if err != nil {
			return nil, err
		}

		// Apply reservoir sampling to this batch
		for _, song := range batch {
			totalProcessed++
			if len(reservoir) < sampleSize {
				reservoir = append(reservoir, song)
			} else {
				// Replace with probability sampleSize/totalProcessed
				randomIndex := rand.Intn(totalProcessed)
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

	algorithmType := "optimized"
	if useFiltered {
		algorithmType = "optimized-filtered"
	}

	s.logger.WithFields(logrus.Fields{
		"userID":        userID,
		"totalSongs":    totalSongs,
		"eligibleSongs": eligibleSongs,
		"sampleSize":    sampleSize,
		"resultCount":   len(result),
		"algorithm":     algorithmType,
		"useFiltered":   useFiltered,
	}).Debug("Completed optimized weighted shuffle with 2-week replay prevention")

	return result, nil
}

func (s *Service) calculateSongWeight(userID string, song models.Song) float64 {
	baseWeight := 1.0

	timeWeight := s.calculateTimeDecayWeight(song.LastPlayed)
	playSkipWeight := s.calculatePlaySkipWeight(song.PlayCount, song.SkipCount)
	transitionWeight := s.calculateTransitionWeight(userID, song.ID)
	artistWeight := s.calculateArtistWeight(userID, song.Artist)

	finalWeight := baseWeight * timeWeight * playSkipWeight * transitionWeight * artistWeight

	s.logger.WithFields(logrus.Fields{
		"userID":           userID,
		"songId":           song.ID,
		"timeWeight":       timeWeight,
		"playSkipWeight":   playSkipWeight,
		"transitionWeight": transitionWeight,
		"artistWeight":     artistWeight,
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
	artistWeight := s.calculateArtistWeight(userID, song.Artist)

	// Use provided transition probability or default to 1.0 if not available
	transitionWeight := 1.0
	if transitionProbability > 0 {
		transitionWeight = BaseTransitionWeight + transitionProbability
	}

	finalWeight := baseWeight * timeWeight * playSkipWeight * transitionWeight * artistWeight

	s.logger.WithFields(logrus.Fields{
		"userID":           userID,
		"songId":           song.ID,
		"timeWeight":       timeWeight,
		"playSkipWeight":   playSkipWeight,
		"transitionWeight": transitionWeight,
		"artistWeight":     artistWeight,
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

func (s *Service) calculateArtistWeight(userID, artist string) float64 {
	stats, err := s.db.GetArtistStats(userID, artist)
	if err != nil {
		s.logger.WithError(err).WithFields(logrus.Fields{
			"user_id": userID,
			"artist":  artist,
		}).Debug("Failed to get artist stats, using default weight")
		return 1.0
	}

	// If the artist has no play/skip history, use neutral weight
	if stats.PlayCount == 0 && stats.SkipCount == 0 {
		return 1.0
	}

	// Use the pre-calculated ratio from the database
	// Map ratio [0.0, 1.0] to weight range [ArtistRatioMinWeight, ArtistRatioMaxWeight]
	// ratio=0 (all skips) -> 0.5x weight
	// ratio=0.5 (equal) -> 1.0x weight
	// ratio=1.0 (all plays) -> 1.5x weight
	artistWeight := ArtistRatioMinWeight + (stats.Ratio * (ArtistRatioMaxWeight - ArtistRatioMinWeight))

	s.logger.WithFields(logrus.Fields{
		"user_id":       userID,
		"artist":        artist,
		"play_count":    stats.PlayCount,
		"skip_count":    stats.SkipCount,
		"ratio":         stats.Ratio,
		"artist_weight": artistWeight,
	}).Debug("Calculated artist weight")

	return artistWeight
}

// GetAllSongsWithWeights returns all songs for a user with their calculated weights
// This is primarily used for debugging purposes to visualize weight calculations
func (s *Service) GetAllSongsWithWeights(userID string) ([]models.WeightedSong, error) {
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

	// Sort by weight descending
	sort.Slice(weightedSongs, func(i, j int) bool {
		return weightedSongs[i].Weight > weightedSongs[j].Weight
	})

	return weightedSongs, nil
}

// GetWeightComponents returns individual weight components for debugging
func (s *Service) GetWeightComponents(userID string, song models.Song) (timeWeight, playSkipWeight, transitionWeight, artistWeight float64) {
	timeWeight = s.calculateTimeDecayWeight(song.LastPlayed)
	playSkipWeight = s.calculatePlaySkipWeight(song.PlayCount, song.SkipCount)
	transitionWeight = s.calculateTransitionWeight(userID, song.ID)
	artistWeight = s.calculateArtistWeight(userID, song.Artist)
	return
}
