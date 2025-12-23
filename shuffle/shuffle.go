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
	MaxSkipTimeoutHours    = 1.0 // Maximum hours to wait before marking as skipped when song duration is unavailable
	ArtistRatioMinWeight   = 0.5 // Minimum weight multiplier for artists with poor play/skip ratio
	ArtistRatioMaxWeight   = 1.5 // Maximum weight multiplier for artists with good play/skip ratio
	// Bayesian prior parameters for Beta-Binomial model
	// These represent "pseudo-observations" that regularize estimates when sample size is small
	BayesianPriorAlpha = 2.0 // Prior "plays" - assumes slight tendency toward playing
	BayesianPriorBeta  = 2.0 // Prior "skips" - assumes slight tendency toward skipping
)

// ScrobbleInfo tracks the last scrobble for skip detection
type ScrobbleInfo struct {
	Song         *models.Song
	IsSubmission bool
	Timestamp    time.Time
}

// EmpiricalPriors holds the calculated Bayesian priors for a user
// These are derived from the user's overall listening patterns
type EmpiricalPriors struct {
	Alpha float64 // Prior for plays (based on average plays per song)
	Beta  float64 // Prior for skips (based on average skips per song)
}

type Service struct {
	db                    *database.DB
	logger                *logrus.Logger
	lastPlayed            map[string]*models.Song     // Map userID to last played song
	lastScrobble          map[string]*ScrobbleInfo    // Map userID to last scrobble info
	empiricalPriors       map[string]*EmpiricalPriors // Map userID to calculated priors (song-level)
	empiricalArtistPriors map[string]*EmpiricalPriors // Map userID to calculated priors (artist-level)
	mu                    sync.RWMutex                // Protects all maps
}

func New(db *database.DB, logger *logrus.Logger) *Service {
	return &Service{
		db:                    db,
		logger:                logger,
		lastPlayed:            make(map[string]*models.Song),
		lastScrobble:          make(map[string]*ScrobbleInfo),
		empiricalPriors:       make(map[string]*EmpiricalPriors),
		empiricalArtistPriors: make(map[string]*EmpiricalPriors),
	}
}

func (s *Service) SetLastPlayed(userID string, song *models.Song) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPlayed[userID] = song
}

// InvalidateEmpiricalPriors clears the cached empirical priors for a user
// This should be called when the user's play/skip statistics change significantly
func (s *Service) InvalidateEmpiricalPriors(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.empiricalPriors, userID)
	delete(s.empiricalArtistPriors, userID)
}

// getEmpiricalPriors calculates and caches the empirical Bayesian priors for a user
// based on their overall listening patterns. This implements the empirical Bayes approach
// where priors are derived from the data itself rather than being fixed constants.
func (s *Service) getEmpiricalPriors(userID string) (alpha, beta float64) {
	// Check cache first (with read lock)
	s.mu.RLock()
	if priors, exists := s.empiricalPriors[userID]; exists {
		s.mu.RUnlock()
		return priors.Alpha, priors.Beta
	}
	s.mu.RUnlock()

	// Get user's total play/skip counts from database
	totalPlays, totalSkips, err := s.db.GetUserTotalPlaySkips(userID)
	if err != nil {
		s.logger.WithError(err).WithField("userID", userID).Debug("Failed to get user total play/skip counts, using default priors")
		return BayesianPriorAlpha, BayesianPriorBeta
	}

	// Get song count to calculate averages
	songCount, err := s.db.GetSongCount(userID)
	if err != nil || songCount == 0 {
		s.logger.WithError(err).WithField("userID", userID).Debug("Failed to get song count, using default priors")
		return BayesianPriorAlpha, BayesianPriorBeta
	}

	// Calculate average plays and skips per song as priors
	// If user has no play/skip history yet, fall back to default priors
	if totalPlays == 0 && totalSkips == 0 {
		return BayesianPriorAlpha, BayesianPriorBeta
	}

	alpha = float64(totalPlays) / float64(songCount)
	beta = float64(totalSkips) / float64(songCount)

	// Use a minimum prior strength to avoid extreme values with very sparse data
	// This ensures we have at least some regularization
	minPriorStrength := 1.0
	if alpha < minPriorStrength {
		alpha = minPriorStrength
	}
	if beta < minPriorStrength {
		beta = minPriorStrength
	}

	// Cache the calculated priors (with write lock)
	s.mu.Lock()
	s.empiricalPriors[userID] = &EmpiricalPriors{
		Alpha: alpha,
		Beta:  beta,
	}
	s.mu.Unlock()

	s.logger.WithFields(logrus.Fields{
		"userID":     userID,
		"totalPlays": totalPlays,
		"totalSkips": totalSkips,
		"songCount":  songCount,
		"alpha":      alpha,
		"beta":       beta,
	}).Debug("Calculated empirical Bayes priors for user")

	return alpha, beta
}

// getEmpiricalArtistPriors calculates and caches the empirical Bayesian priors for artist weights
// based on the user's overall artist-level listening patterns. Similar to getEmpiricalPriors but
// operates at the artist level rather than song level.
func (s *Service) getEmpiricalArtistPriors(userID string) (alpha, beta float64) {
	// Check cache first (with read lock)
	s.mu.RLock()
	if priors, exists := s.empiricalArtistPriors[userID]; exists {
		s.mu.RUnlock()
		return priors.Alpha, priors.Beta
	}
	s.mu.RUnlock()

	// Get user's total play/skip counts from artist_stats table
	totalPlays, totalSkips, err := s.db.GetUserTotalArtistPlaySkips(userID)
	if err != nil {
		s.logger.WithError(err).WithField("userID", userID).Debug("Failed to get user total artist play/skip counts, using default priors")
		return BayesianPriorAlpha, BayesianPriorBeta
	}

	// Get artist count to calculate averages
	artistCount, err := s.db.GetArtistCount(userID)
	if err != nil || artistCount == 0 {
		s.logger.WithError(err).WithField("userID", userID).Debug("Failed to get artist count, using default priors")
		return BayesianPriorAlpha, BayesianPriorBeta
	}

	// Calculate average plays and skips per artist as priors
	// If user has no play/skip history yet, fall back to default priors
	if totalPlays == 0 && totalSkips == 0 {
		return BayesianPriorAlpha, BayesianPriorBeta
	}

	alpha = float64(totalPlays) / float64(artistCount)
	beta = float64(totalSkips) / float64(artistCount)

	// Use a minimum prior strength to avoid extreme values with very sparse data
	// This ensures we have at least some regularization
	minPriorStrength := 1.0
	if alpha < minPriorStrength {
		alpha = minPriorStrength
	}
	if beta < minPriorStrength {
		beta = minPriorStrength
	}

	// Cache the calculated priors (with write lock)
	s.mu.Lock()
	s.empiricalArtistPriors[userID] = &EmpiricalPriors{
		Alpha: alpha,
		Beta:  beta,
	}
	s.mu.Unlock()

	s.logger.WithFields(logrus.Fields{
		"userID":      userID,
		"totalPlays":  totalPlays,
		"totalSkips":  totalSkips,
		"artistCount": artistCount,
		"alpha":       alpha,
		"beta":        beta,
	}).Debug("Calculated empirical Bayes priors for artist weights")

	return alpha, beta
}

// ProcessScrobble processes a scrobble event with simplified skip detection
// Returns true if a play event should be recorded, false if it's a duplicate submission
func (s *Service) ProcessScrobble(userID, songID string, isSubmission bool, recordSkipFunc func(string, *models.Song)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Fetch current song details to get duration
	currentSongs, err := s.db.GetSongsByIDs(userID, []string{songID})
	var currentSong *models.Song
	if err == nil && len(currentSongs) > 0 {
		if song, exists := currentSongs[songID]; exists {
			currentSong = &song
		}
	}

	// If we can't get the song details, create a minimal song with just the ID
	if currentSong == nil {
		currentSong = &models.Song{ID: songID}
	}

	// Check if there was a previous scrobble
	lastScrobble, hadPreviousScrobble := s.lastScrobble[userID]

	// Check if this is a duplicate submission for the same song
	// This prevents double-counting when clients retry or send multiple submission=true requests
	if hadPreviousScrobble && lastScrobble.IsSubmission && isSubmission && lastScrobble.Song.ID == songID {
		s.logger.WithFields(logrus.Fields{
			"user_id": userID,
			"song_id": songID,
			"reason":  "duplicate_submission",
		}).Debug("Ignoring duplicate submission for same song")
		return false // Don't record another play event
	}

	// If there was a previous scrobble that wasn't a definitive play, mark it as skipped
	// BUT only if it's a different song (same song being scrobbled again should just update status)
	// AND only if the time between scrobbles is less than 2x the song duration (when duration is available)
	if hadPreviousScrobble && !lastScrobble.IsSubmission && lastScrobble.Song.ID != songID {
		timeSinceLastScrobble := time.Since(lastScrobble.Timestamp)
		songDuration := time.Duration(lastScrobble.Song.Duration) * time.Second
		maxSkipTime := songDuration * 2

		// Determine if we should mark as skipped based on timing
		// If song duration is not available (0), use MaxSkipTimeoutHours as fallback
		// Otherwise, only mark as skipped if the time since last scrobble is reasonable (less than 2x song duration)
		// If more time has passed, the user likely paused or stopped playback
		shouldMarkAsSkipped := false
		effectiveMaxTime := maxSkipTime

		if songDuration == 0 {
			// When duration is unavailable, use the maximum timeout (e.g., 1 hour)
			effectiveMaxTime = time.Duration(MaxSkipTimeoutHours * float64(time.Hour))
			shouldMarkAsSkipped = timeSinceLastScrobble <= effectiveMaxTime
		} else {
			// When duration is available, use 2x duration as threshold
			shouldMarkAsSkipped = timeSinceLastScrobble <= maxSkipTime
		}

		if shouldMarkAsSkipped {
			recordSkipFunc(userID, lastScrobble.Song)
			s.logger.WithFields(logrus.Fields{
				"user_id":                userID,
				"song_id":                lastScrobble.Song.ID,
				"reason":                 "followed_by_another_scrobble",
				"time_since_scrobble":    timeSinceLastScrobble,
				"song_duration":          songDuration,
				"max_skip_time":          maxSkipTime,
				"effective_max_time":     effectiveMaxTime,
				"duration_unavailable":   songDuration == 0,
			}).Debug("Marking previous scrobble as skipped")
		} else {
			s.logger.WithFields(logrus.Fields{
				"user_id":                userID,
				"song_id":                lastScrobble.Song.ID,
				"reason":                 "too_much_time_passed",
				"time_since_scrobble":    timeSinceLastScrobble,
				"song_duration":          songDuration,
				"max_skip_time":          maxSkipTime,
				"effective_max_time":     effectiveMaxTime,
			}).Debug("Not marking as skipped - too much time passed since last scrobble")
		}
	}

	// Update the last scrobble info
	s.lastScrobble[userID] = &ScrobbleInfo{
		Song:         currentSong,
		IsSubmission: isSubmission,
		Timestamp:    time.Now(),
	}

	s.logger.WithFields(logrus.Fields{
		"user_id":    userID,
		"song_id":    songID,
		"submission": isSubmission,
	}).Debug("Processed scrobble")

	return true // OK to record play event
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

	timeWeight := s.calculateTimeDecayWeight(song.LastPlayed, song.LastSkipped)
	playSkipWeight := s.calculatePlaySkipWeight(userID, song.PlayCount, song.SkipCount)
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

	timeWeight := s.calculateTimeDecayWeight(song.LastPlayed, song.LastSkipped)
	playSkipWeight := s.calculatePlaySkipWeight(userID, song.PlayCount, song.SkipCount)
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

func (s *Service) calculateTimeDecayWeight(lastPlayed, lastSkipped time.Time) float64 {
	// Use the most recent timestamp between lastPlayed and lastSkipped
	// since both represent when the song was presented to the listener
	lastPresented := lastPlayed
	if !lastSkipped.IsZero() && (lastPlayed.IsZero() || lastSkipped.After(lastPlayed)) {
		lastPresented = lastSkipped
	}

	if lastPresented.IsZero() {
		return NeverPlayedWeight
	}

	daysSinceLastPresented := time.Since(lastPresented).Hours() / HoursPerDay

	if daysSinceLastPresented < TimeDecayDaysThreshold {
		return TimeDecayMinWeight + (daysSinceLastPresented/TimeDecayDaysThreshold)*TimeDecayMaxWeight
	}

	return 1.0 + math.Min(daysSinceLastPresented/DaysPerYear, 1.0)
}

// calculatePlaySkipWeight uses an empirical Bayesian approach (Beta-Binomial model) to calculate
// the weight based on play/skip history. This approach is more robust than simple ratios
// because it accounts for uncertainty when sample sizes are small.
//
// The Bayesian posterior mean is: (playCount + α) / (playCount + skipCount + α + β)
// where α and β are empirical priors derived from the user's overall listening patterns.
//
// Benefits:
// - Priors adapt to each user's behavior (e.g., users who skip 80% have higher β)
// - Songs with few observations get conservative estimates based on user tendencies
// - Songs with many observations converge to their true play ratio
// - Prevents extreme weights from small sample sizes (e.g., 1 play, 0 skips)
func (s *Service) calculatePlaySkipWeight(userID string, playCount, skipCount int) float64 {
	if playCount == 0 && skipCount == 0 {
		return UnplayedSongWeight
	}

	// Get empirical priors based on user's overall listening patterns
	alpha, beta := s.getEmpiricalPriors(userID)

	// Calculate Bayesian posterior mean using Beta-Binomial model
	// This gives us a regularized estimate of the play ratio
	posteriorPlays := float64(playCount) + alpha
	posteriorTotal := float64(playCount+skipCount) + alpha + beta
	bayesianPlayRatio := posteriorPlays / posteriorTotal

	// Map the Bayesian ratio to the weight range [PlayRatioMinWeight, PlayRatioMaxWeight]
	// Using proper linear interpolation: min + (ratio * (max - min))
	return PlayRatioMinWeight + (bayesianPlayRatio * (PlayRatioMaxWeight - PlayRatioMinWeight))
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

// calculateArtistWeight uses an empirical Bayesian approach (Beta-Binomial model) to calculate
// the artist weight based on play/skip history at the artist level. This is analogous to
// calculatePlaySkipWeight but operates on artist-level statistics rather than song-level.
//
// The Bayesian posterior mean is: (playCount + α) / (playCount + skipCount + α + β)
// where α and β are empirical priors derived from the user's overall artist listening patterns.
//
// Benefits:
// - Priors adapt to each user's artist preferences
// - Artists with few observations get conservative estimates based on user tendencies
// - Artists with many observations converge to their true play ratio
// - Prevents extreme weights from small sample sizes (e.g., 1 play, 0 skips for a new artist)
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

	// Get empirical priors based on user's overall artist listening patterns
	alpha, beta := s.getEmpiricalArtistPriors(userID)

	// Calculate Bayesian posterior mean using Beta-Binomial model
	// This gives us a regularized estimate of the artist play ratio
	posteriorPlays := float64(stats.PlayCount) + alpha
	posteriorTotal := float64(stats.PlayCount+stats.SkipCount) + alpha + beta
	bayesianArtistRatio := posteriorPlays / posteriorTotal

	// Map the Bayesian ratio to the weight range [ArtistRatioMinWeight, ArtistRatioMaxWeight]
	// Using proper linear interpolation: min + (ratio * (max - min))
	// ratio=0 (all skips) -> 0.5x weight
	// ratio=0.5 (equal) -> 1.0x weight
	// ratio=1.0 (all plays) -> 1.5x weight
	artistWeight := ArtistRatioMinWeight + (bayesianArtistRatio * (ArtistRatioMaxWeight - ArtistRatioMinWeight))

	s.logger.WithFields(logrus.Fields{
		"user_id":             userID,
		"artist":              artist,
		"play_count":          stats.PlayCount,
		"skip_count":          stats.SkipCount,
		"alpha":               alpha,
		"beta":                beta,
		"bayesian_ratio":      bayesianArtistRatio,
		"artist_weight":       artistWeight,
	}).Debug("Calculated artist weight (Bayesian)")

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
	timeWeight = s.calculateTimeDecayWeight(song.LastPlayed, song.LastSkipped)
	playSkipWeight = s.calculatePlaySkipWeight(userID, song.PlayCount, song.SkipCount)
	transitionWeight = s.calculateTransitionWeight(userID, song.ID)
	artistWeight = s.calculateArtistWeight(userID, song.Artist)
	return
}
