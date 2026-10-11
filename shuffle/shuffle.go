package shuffle

import (
	"container/heap"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"

	"github.com/syeo66/subsoxy/database"
	"github.com/syeo66/subsoxy/models"
)

// Number of songs read from the database at a time while shuffling
const BatchSize = 1000

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
	TwoWeekReplayThreshold = 14  // Minimum days before a song can be replayed (unless no alternatives)
	MaxSkipTimeoutHours    = 1.0 // Maximum hours to wait before marking as skipped when song duration is unavailable
	ArtistRatioMinWeight   = 0.5 // Minimum weight multiplier for artists with poor play/skip ratio
	ArtistRatioMaxWeight   = 1.5 // Maximum weight multiplier for artists with good play/skip ratio
	// Bayesian prior parameters for Beta-Binomial model
	// These represent "pseudo-observations" that regularize estimates when sample size is small
	BayesianPriorAlpha = 2.0 // Prior "plays" - assumes slight tendency toward playing
	BayesianPriorBeta  = 2.0 // Prior "skips" - assumes slight tendency toward skipping
	// Range of pseudo-observations (α + β) an empirical prior carries: enough that a song or
	// artist with one or two events can't swing to an extreme weight, few enough that its own
	// history soon outweighs the prior
	MinPriorStrength = 2.0
	MaxPriorStrength = 10.0
	// AudioMuse-AI similarity weight: max multiplicative boost at similarity score 1.0.
	// Formula: 1.0 + SimilarSongMaxBoost * similarity  →  range [1.0, 1.5]
	SimilarSongMaxBoost  = 0.5
	SimilarSongsCacheTTL = 5 * time.Minute
	// Update the acoustic similarity reference every N picks instead of every pick.
	// Reduces API calls by N× while preserving the chaining effect across the queue.
	SimilarityChainStep = 5
)

// SimilarSongsFetcher fetches acoustically similar songs for a user and song,
// authenticating upstream with the user's stored credentials.
// Returns a map of song ID → similarity score (0.0–1.0), or nil when unavailable.
type SimilarSongsFetcher func(userID, songID string) (map[string]float64, error)

type similarSongsEntry struct {
	scores    map[string]float64
	fetchedAt time.Time
}

// ScrobbleInfo tracks the last scrobble for skip detection
type ScrobbleInfo struct {
	Song         *models.Song
	IsSubmission bool
	Timestamp    time.Time
}

// EmpiricalPriors holds the calculated Bayesian priors for a user
// These are derived from the user's overall listening patterns
type EmpiricalPriors struct {
	Alpha float64 // Prior for plays
	Beta  float64 // Prior for skips
}

// userPriors holds a user's priors for song and artist weights
type userPriors struct {
	song   EmpiricalPriors
	artist EmpiricalPriors
}

type Service struct {
	db                  *database.DB
	logger              *logrus.Logger
	lastPlayed          map[string]*models.Song  // Map userID to last played song
	lastScrobble        map[string]*ScrobbleInfo // Map userID to last scrobble info
	priors              map[string]*userPriors   // Map userID to calculated priors
	similarSongsFetcher SimilarSongsFetcher
	similarSongsCache   map[string]*similarSongsEntry
	inflight            singleflight.Group // Deduplicates concurrent similarity fetches for the same key
	mu                  sync.RWMutex       // Protects all maps
}

func New(db *database.DB, logger *logrus.Logger) *Service {
	return &Service{
		db:                db,
		logger:            logger,
		lastPlayed:        make(map[string]*models.Song),
		lastScrobble:      make(map[string]*ScrobbleInfo),
		priors:            make(map[string]*userPriors),
		similarSongsCache: make(map[string]*similarSongsEntry),
	}
}

// SetSimilarSongsFetcher wires up the AudioMuse-AI similarity provider.
// When set, songs acoustically similar to the last-played track receive a weight boost.
func (s *Service) SetSimilarSongsFetcher(fn SimilarSongsFetcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.similarSongsFetcher = fn
}

// getSimilarSongsForUser returns a map of song ID → similarity score (0.0–1.0) for songs
// acoustically similar to songID, using a short-lived in-memory cache.
// Concurrent calls for the same key share a single in-flight HTTP request via singleflight.
func (s *Service) getSimilarSongsForUser(userID, songID string) map[string]float64 {
	cacheKey := userID + ":" + songID

	s.mu.RLock()
	fetcher := s.similarSongsFetcher
	entry, cached := s.similarSongsCache[cacheKey]
	s.mu.RUnlock()

	if fetcher == nil {
		return nil
	}

	if cached && time.Since(entry.fetchedAt) < SimilarSongsCacheTTL {
		return entry.scores
	}

	v, _, _ := s.inflight.Do(cacheKey, func() (interface{}, error) {
		scores, err := fetcher(userID, songID)
		if err != nil {
			s.logger.WithError(err).WithFields(logrus.Fields{
				"userID": userID,
				"songID": songID,
			}).Debug("Failed to fetch similar songs, continuing without similarity weights")
			return nil, nil //nolint:nilerr
		}
		if scores != nil {
			s.mu.Lock()
			s.similarSongsCache[cacheKey] = &similarSongsEntry{
				scores:    scores,
				fetchedAt: time.Now(),
			}
			s.mu.Unlock()
		}
		return scores, nil
	})

	if v == nil {
		return nil
	}
	return v.(map[string]float64)
}

// calculateSimilarityWeight returns a weight multiplier in [1.0, 1+SimilarSongMaxBoost]
// based on the song's acoustic similarity score to the last-played track.
func (s *Service) calculateSimilarityWeight(songID string, scores map[string]float64) float64 {
	if score, ok := scores[songID]; ok && score > 0 {
		return 1.0 + SimilarSongMaxBoost*score
	}
	return 1.0
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
	delete(s.priors, userID)
}

// priorsFromTotals derives Beta priors from decayed play and skip totals spread over n songs
// or artists that have any history. The prior's mean is the user's overall play ratio,
// nudged toward the default prior so a user with no skips (or no plays) yet doesn't get a
// degenerate prior. Its strength is the average number of (decayed) events per song or
// artist, kept within [MinPriorStrength, MaxPriorStrength], so it carries about as much
// evidence as a typical song or artist does. Songs never heard are left out, otherwise a
// large library would dilute the averages until every user got the same flat prior.
func priorsFromTotals(plays, skips float64, n int) EmpiricalPriors {
	total := plays + skips
	if n == 0 || total == 0 {
		return EmpiricalPriors{Alpha: BayesianPriorAlpha, Beta: BayesianPriorBeta}
	}
	playRatio := (plays + BayesianPriorAlpha) / (total + BayesianPriorAlpha + BayesianPriorBeta)
	strength := min(max(total/float64(n), MinPriorStrength), MaxPriorStrength)
	return EmpiricalPriors{Alpha: playRatio * strength, Beta: (1 - playRatio) * strength}
}

// getPriors calculates and caches the empirical Bayesian priors for a user's song and artist
// weights from their overall listening patterns, so priors adapt to each user's behavior
// (e.g. users who skip 80% of songs get a prior that expects skips).
func (s *Service) getPriors(userID string) *userPriors {
	s.mu.RLock()
	priors, exists := s.priors[userID]
	s.mu.RUnlock()
	if exists {
		return priors
	}

	totals, err := s.db.GetAdjustedTotals(userID)
	if err != nil {
		s.logger.WithError(err).WithField("userID", userID).Debug("Failed to get user adjusted totals, using default priors")
		defaults := EmpiricalPriors{Alpha: BayesianPriorAlpha, Beta: BayesianPriorBeta}
		return &userPriors{song: defaults, artist: defaults}
	}

	priors = &userPriors{
		song:   priorsFromTotals(totals.Plays, totals.Skips, totals.HeardSongs),
		artist: priorsFromTotals(totals.Plays, totals.Skips, totals.HeardArtists),
	}

	s.mu.Lock()
	s.priors[userID] = priors
	s.mu.Unlock()

	s.logger.WithFields(logrus.Fields{
		"userID":             userID,
		"totalAdjustedPlays": totals.Plays,
		"totalAdjustedSkips": totals.Skips,
		"heardSongs":         totals.HeardSongs,
		"heardArtists":       totals.HeardArtists,
		"songAlpha":          priors.song.Alpha,
		"songBeta":           priors.song.Beta,
		"artistAlpha":        priors.artist.Alpha,
		"artistBeta":         priors.artist.Beta,
	}).Debug("Calculated empirical Bayes priors for user")

	return priors
}

// ProcessScrobble processes a scrobble event with simplified skip detection
// Returns true if a play event should be recorded, false if it's a duplicate submission
func (s *Service) ProcessScrobble(userID, songID string, isSubmission bool, recordSkipFunc func(string, *models.Song)) bool {
	// recordSkipFunc runs after s.mu is released: it writes to the database and may call
	// back into the service (e.g. InvalidateEmpiricalPriors), which takes s.mu itself.
	skippedSong, shouldRecord := s.updateScrobbleState(userID, songID, isSubmission)
	if skippedSong != nil {
		recordSkipFunc(userID, skippedSong)
	}
	return shouldRecord
}

// updateScrobbleState updates the user's last scrobble under s.mu. It returns the previous
// song if it should be recorded as skipped, and whether a play event should be recorded.
func (s *Service) updateScrobbleState(userID, songID string, isSubmission bool) (*models.Song, bool) {
	// Fetch current song details to get duration (before locking, as it queries the database)
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

	s.mu.Lock()
	defer s.mu.Unlock()

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
		return nil, false // Don't record another play event
	}

	var skippedSong *models.Song

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
			skippedSong = lastScrobble.Song
			s.logger.WithFields(logrus.Fields{
				"user_id":              userID,
				"song_id":              lastScrobble.Song.ID,
				"reason":               "followed_by_another_scrobble",
				"time_since_scrobble":  timeSinceLastScrobble,
				"song_duration":        songDuration,
				"max_skip_time":        maxSkipTime,
				"effective_max_time":   effectiveMaxTime,
				"duration_unavailable": songDuration == 0,
			}).Debug("Marking previous scrobble as skipped")
		} else {
			s.logger.WithFields(logrus.Fields{
				"user_id":             userID,
				"song_id":             lastScrobble.Song.ID,
				"reason":              "too_much_time_passed",
				"time_since_scrobble": timeSinceLastScrobble,
				"song_duration":       songDuration,
				"max_skip_time":       maxSkipTime,
				"effective_max_time":  effectiveMaxTime,
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

	return skippedSong, true // OK to record play event
}

// racer is one song in the exponential race that orders the shuffle. Each song has a clock
// that runs at the song's current weight and fires once it has used up a random Exp(1)
// budget, so the next clock to fire belongs to a song picked with probability proportional
// to its weight. The clocks are memoryless, so a song's rate can change partway through the
// race, when the similarity reference moves, without biasing later picks.
type racer struct {
	song      models.Song
	weight    float64 // Base weight: time × play/skip × artist
	rate      float64 // Base weight × current similarity factor
	remaining float64 // Budget left as of since
	since     float64
	fireAt    float64
	index     int // Position in the heap, -1 once picked
}

func (r *racer) setRate(now, rate float64) {
	r.remaining -= r.rate * (now - r.since)
	r.since = now
	r.rate = rate
	r.fireAt = now + r.remaining/rate
}

// raceQueue is a heap of racers ordered by earliest firing time, or by latest when
// latestFirst is set (used to keep the earliest songs while streaming the library).
type raceQueue struct {
	items       []*racer
	latestFirst bool
}

func (q *raceQueue) Len() int { return len(q.items) }

func (q *raceQueue) Less(i, j int) bool {
	if q.latestFirst {
		return q.items[i].fireAt > q.items[j].fireAt
	}
	return q.items[i].fireAt < q.items[j].fireAt
}

func (q *raceQueue) Swap(i, j int) {
	q.items[i], q.items[j] = q.items[j], q.items[i]
	q.items[i].index = i
	q.items[j].index = j
}

func (q *raceQueue) Push(x interface{}) {
	r := x.(*racer)
	r.index = len(q.items)
	q.items = append(q.items, r)
}

func (q *raceQueue) Pop() interface{} {
	last := len(q.items) - 1
	r := q.items[last]
	q.items = q.items[:last]
	r.index = -1
	return r
}

// GetWeightedShuffledSongs returns up to count songs, each picked with probability
// proportional to its weight among the songs not picked yet. Songs played or skipped within
// the last TwoWeekReplayThreshold days are strictly excluded.
//
// The picks are run as an exponential race (see racer). That lets the library be streamed in
// batches while keeping only count songs in memory, without losing any weighting: every
// other song's clock fires later at its base weight, so it can only be picked if the
// similarity boost speeds it up, and those songs are loaded when they become similar.
func (s *Service) GetWeightedShuffledSongs(userID string, count int) ([]models.Song, error) {
	if count <= 0 {
		return []models.Song{}, nil
	}

	// Calculate cutoff time once so every query filters against the same moment
	cutoffTime := time.Now().AddDate(0, 0, -TwoWeekReplayThreshold)

	// Start the similarity chain from the last-played track, fetching it in the
	// background while the library is scanned.
	s.mu.RLock()
	lastPlayed := s.lastPlayed[userID]
	s.mu.RUnlock()
	referenceID := ""
	if lastPlayed != nil {
		referenceID = lastPlayed.ID
	}
	if referenceID != "" {
		go s.getSimilarSongsForUser(userID, referenceID)
	}

	artistStats := s.LoadArtistStats(userID)

	// Keep the count songs whose clocks fire first at base weight
	pool := &raceQueue{latestFirst: true}
	eligible := 0
	for offset := 0; ; offset += BatchSize {
		batch, err := s.db.GetSongsBatchFiltered(userID, BatchSize, offset, cutoffTime)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}

		for _, song := range batch {
			weight := s.calculateBaseWeight(userID, song, artistStats)
			if weight <= 0 {
				continue
			}
			eligible++

			budget := rand.ExpFloat64()
			r := &racer{song: song, weight: weight, rate: weight, remaining: budget, fireAt: budget / weight}
			if pool.Len() < count {
				heap.Push(pool, r)
			} else if r.fireAt < pool.items[0].fireAt {
				r.index = 0
				pool.items[0] = r
				heap.Fix(pool, 0)
			}
		}
	}

	// Every eligible song left out of the pool fires after horizon at its base weight
	horizon := math.Inf(1)
	if eligible > pool.Len() {
		horizon = pool.items[0].fireAt
	}

	race := &raceQueue{items: pool.items}
	heap.Init(race)

	racers := make(map[string]*racer, race.Len()) // Songs still in the race
	seen := make(map[string]bool, race.Len())     // Songs already in the race or looked up
	for _, r := range race.items {
		racers[r.song.ID] = r
		seen[r.song.ID] = true
	}

	now := 0.0
	var similarSongs map[string]float64

	// setSimilarSongs moves the similarity boost to a new set of songs at the current time
	setSimilarSongs := func(next map[string]float64) {
		if !math.IsInf(horizon, 1) {
			var missing []string
			for id := range next {
				if !seen[id] {
					seen[id] = true
					missing = append(missing, id)
				}
			}

			songs, err := s.db.GetEligibleSongsByIDs(userID, missing, cutoffTime)
			if err != nil {
				s.logger.WithError(err).WithField("userID", userID).Warn("Failed to load similar songs, continuing without them")
			}
			for _, song := range songs {
				weight := s.calculateBaseWeight(userID, song, artistStats)
				if weight <= 0 {
					continue
				}
				// The song's clock hasn't fired by horizon at base weight, so by memorylessness
				// its budget left now is what it would use until horizon plus a fresh Exp(1).
				remaining := weight*(horizon-now) + rand.ExpFloat64()
				r := &racer{song: song, weight: weight, rate: weight, remaining: remaining, since: now, fireAt: now + remaining/weight}
				heap.Push(race, r)
				racers[song.ID] = r
			}
		}

		for _, ids := range []map[string]float64{similarSongs, next} {
			for id := range ids {
				if r, ok := racers[id]; ok {
					r.setRate(now, r.weight*s.calculateSimilarityWeight(id, next))
					heap.Fix(race, r.index)
				}
			}
		}
		similarSongs = next
	}

	if referenceID != "" {
		setSimilarSongs(s.getSimilarSongsForUser(userID, referenceID))
	}

	result := make([]models.Song, 0, min(count, race.Len()))
	for len(result) < count && race.Len() > 0 {
		r := heap.Pop(race).(*racer)
		delete(racers, r.song.ID)
		now = r.fireAt
		result = append(result, r.song)

		// Refresh the similarity reference every SimilarityChainStep picks instead of every
		// pick. This reduces API calls by that factor while preserving acoustic flow.
		if len(result)%SimilarityChainStep == 0 && len(result) < count {
			setSimilarSongs(s.getSimilarSongsForUser(userID, r.song.ID))
		}
	}

	s.logger.WithFields(logrus.Fields{
		"userID":         userID,
		"eligibleSongs":  eligible,
		"requestedCount": count,
		"resultCount":    len(result),
	}).Debug("Completed weighted shuffle with 2-week replay prevention")

	return result, nil
}

// calculateBaseWeight returns a song's weight without the similarity factor, which changes
// as the shuffle progresses.
func (s *Service) calculateBaseWeight(userID string, song models.Song, artistStats ArtistStats) float64 {
	timeWeight := s.calculateTimeDecayWeight(song.LastPlayed, song.LastSkipped)
	playSkipWeight := s.calculatePlaySkipWeight(userID, song.AdjustedPlays, song.AdjustedSkips)
	artistWeight := s.calculateArtistWeight(userID, song.Artist, artistStats)
	return timeWeight * playSkipWeight * artistWeight
}

// calculateSongWeight returns a song's full weight, including the similarity factor.
// It runs for every song in the library, so it doesn't log per song; the debug view shows
// the individual weight components instead.
func (s *Service) calculateSongWeight(userID string, song models.Song, artistStats ArtistStats, similarSongs map[string]float64) float64 {
	return s.calculateBaseWeight(userID, song, artistStats) * s.calculateSimilarityWeight(song.ID, similarSongs)
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
// the weight based on play/skip history with exponential decay. This approach is more robust than
// simple ratios because it accounts for uncertainty when sample sizes are small and emphasizes
// recent behavior over historical data.
//
// The Bayesian posterior mean is: (adjustedPlays + α) / (adjustedPlays + adjustedSkips + α + β)
// where α and β are empirical priors derived from the user's overall listening patterns.
//
// Benefits:
// - Priors adapt to each user's behavior (e.g., users who skip 80% have higher β)
// - Songs with few observations get conservative estimates based on user tendencies
// - Songs with many observations converge to their true play ratio
// - Prevents extreme weights from small sample sizes (e.g., 1 play, 0 skips)
// - Recent plays/skips have more influence: the counters halve every database.AdjustedHalfLife
func (s *Service) calculatePlaySkipWeight(userID string, adjustedPlays, adjustedSkips float64) float64 {
	if adjustedPlays == 0.0 && adjustedSkips == 0.0 {
		return UnplayedSongWeight
	}

	// Get empirical priors based on user's overall listening patterns (using adjusted totals)
	priors := s.getPriors(userID).song
	alpha, beta := priors.Alpha, priors.Beta

	// Calculate Bayesian posterior mean using Beta-Binomial model with adjusted values
	// This gives us a regularized estimate of the play ratio weighted toward recent behavior
	posteriorPlays := adjustedPlays + alpha
	posteriorTotal := adjustedPlays + adjustedSkips + alpha + beta
	bayesianPlayRatio := posteriorPlays / posteriorTotal

	// Map the Bayesian ratio to the weight range [PlayRatioMinWeight, PlayRatioMaxWeight]
	// Using proper linear interpolation: min + (ratio * (max - min))
	return PlayRatioMinWeight + (bayesianPlayRatio * (PlayRatioMaxWeight - PlayRatioMinWeight))
}

// calculateArtistWeight uses an empirical Bayesian approach (Beta-Binomial model) to calculate
// the artist weight based on play/skip history at the artist level with exponential decay.
// This is analogous to calculatePlaySkipWeight but operates on artist-level statistics aggregated
// from song-level adjusted values, emphasizing recent behavior over historical data.
//
// The Bayesian posterior mean is: (adjustedPlays + α) / (adjustedPlays + adjustedSkips + α + β)
// where α and β are empirical priors derived from the user's overall artist listening patterns.
//
// Benefits:
// - Priors adapt to each user's artist preferences
// - Artists with few observations get conservative estimates based on user tendencies
// - Artists with many observations converge to their true play ratio
// - Prevents extreme weights from small sample sizes (e.g., 1 play, 0 skips for a new artist)
// - Recent plays/skips have more influence: the counters halve every database.AdjustedHalfLife
func (s *Service) calculateArtistWeight(userID, artist string, artistStats ArtistStats) float64 {
	// Adjusted stats are summed from the songs table (song-level decay applied)
	stats := artistStats[artist]
	adjustedPlays, adjustedSkips := stats.Plays, stats.Skips

	// If the artist has no play/skip history, use neutral weight
	if adjustedPlays == 0.0 && adjustedSkips == 0.0 {
		return 1.0
	}

	// Get empirical priors based on user's overall artist listening patterns (using adjusted totals)
	priors := s.getPriors(userID).artist
	alpha, beta := priors.Alpha, priors.Beta

	// Calculate Bayesian posterior mean using Beta-Binomial model with adjusted values
	// This gives us a regularized estimate of the artist play ratio weighted toward recent behavior
	posteriorPlays := adjustedPlays + alpha
	posteriorTotal := adjustedPlays + adjustedSkips + alpha + beta
	bayesianArtistRatio := posteriorPlays / posteriorTotal

	// Map the Bayesian ratio to the weight range [ArtistRatioMinWeight, ArtistRatioMaxWeight]
	// Using proper linear interpolation: min + (ratio * (max - min))
	// ratio=0 (all skips) -> 0.5x weight
	// ratio=0.5 (equal) -> 1.0x weight
	// ratio=1.0 (all plays) -> 1.5x weight
	return ArtistRatioMinWeight + (bayesianArtistRatio * (ArtistRatioMaxWeight - ArtistRatioMinWeight))
}

// ArtistStats maps artist name to the artist's adjusted play/skip totals.
type ArtistStats map[string]database.AdjustedStats

// LoadArtistStats fetches the adjusted play/skip totals of all of the user's artists in
// one query. On error it returns nil, which gives every artist the neutral weight.
func (s *Service) LoadArtistStats(userID string) ArtistStats {
	stats, err := s.db.GetAllArtistAdjustedStats(userID)
	if err != nil {
		s.logger.WithError(err).WithField("user_id", userID).Debug("Failed to get artist adjusted stats, using default weights")
		return nil
	}
	return stats
}

// GetAllSongsWithWeights returns all songs for a user with their calculated weights
// This is primarily used for debugging purposes to visualize weight calculations
func (s *Service) GetAllSongsWithWeights(userID string) ([]models.WeightedSong, error) {
	songs, err := s.db.GetAllSongs(userID)
	if err != nil {
		return nil, err
	}

	artistStats := s.LoadArtistStats(userID)
	weightedSongs := make([]models.WeightedSong, 0, len(songs))
	for _, song := range songs {
		weight := s.calculateSongWeight(userID, song, artistStats, nil)
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

// GetWeightComponents returns individual weight components for debugging.
// artistStats comes from LoadArtistStats; similarSongs may be nil when no last-played
// context is available.
func (s *Service) GetWeightComponents(userID string, song models.Song, artistStats ArtistStats, similarSongs map[string]float64) (timeWeight, playSkipWeight, artistWeight, similarityWeight float64) {
	timeWeight = s.calculateTimeDecayWeight(song.LastPlayed, song.LastSkipped)
	playSkipWeight = s.calculatePlaySkipWeight(userID, song.AdjustedPlays, song.AdjustedSkips)
	artistWeight = s.calculateArtistWeight(userID, song.Artist, artistStats)
	similarityWeight = s.calculateSimilarityWeight(song.ID, similarSongs)
	return
}

// GetLastPlayedSong returns the last played song for a user (used by the debug view).
func (s *Service) GetLastPlayedSong(userID string) *models.Song {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastPlayed[userID]
}

// GetSimilarSongsForDebug fetches similar songs for the debug view (song ID → score).
func (s *Service) GetSimilarSongsForDebug(userID, songID string) map[string]float64 {
	return s.getSimilarSongsForUser(userID, songID)
}
