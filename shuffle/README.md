# Shuffle Module

The shuffle module implements intelligent weighted song shuffling with **complete multi-tenancy support**, providing personalized recommendations based on individual user listening history and preferences.

## Overview

This module provides:
- **Per-User Multi-factor weighting algorithm** with individual preference learning
- **User-specific time decay calculations** to avoid recently played songs per user
- **Individual play/skip ratio analysis** based on each user's listening behavior
- **User-isolated transition probability integration** for personalized song sequences
- **Per-user configurable shuffle sizes** with user context validation
- **Complete user data isolation** ensuring personalized recommendations

## Multi-Tenant Algorithm ✅ **UPDATED**

The weighted shuffle algorithm considers three main factors **per user** to provide personalized recommendations:

### 1. User-Specific Time Decay Weight with 2-Week Replay Prevention ✅ **ENHANCED**
Reduces likelihood of recently played OR skipped songs **for each individual user** to encourage variety. Uses the most recent timestamp between `lastPlayed` and `lastSkipped` to accurately track when a song was presented to the listener.

```go
func (s *Service) calculateTimeDecayWeight(lastPlayed, lastSkipped time.Time) float64 {
    // Use the most recent timestamp between lastPlayed and lastSkipped
    // since both represent when the song was presented to the listener
    lastPresented := lastPlayed
    if !lastSkipped.IsZero() && (lastPlayed.IsZero() || lastSkipped.After(lastPlayed)) {
        lastPresented = lastSkipped
    }

    if lastPresented.IsZero() {
        return NeverPlayedWeight // Boost for never-played/skipped songs by this user
    }

    daysSinceLastPresented := time.Since(lastPresented).Hours() / HoursPerDay

    if daysSinceLastPresented < TimeDecayDaysThreshold {
        return TimeDecayMinWeight + (daysSinceLastPresented/TimeDecayDaysThreshold)*TimeDecayMaxWeight
    }

    return 1.0 + math.Min(daysSinceLastPresented/DaysPerYear, 1.0)
}

// 2-Week Replay Prevention - Both played AND skipped songs are filtered
// Database filtering with consistent timing prevents songs from being
// included in shuffle results for 14 days after last play OR skip
```

### 2. Per-User Play/Skip Ratio Weight with Empirical Bayesian Categorization ✅ **ENHANCED**
Favors songs with better play-to-skip ratios **for each specific user** using an **empirical Bayesian Beta-Binomial model**. Priors are **dynamically calculated from each user's overall listening patterns** for more personalized and adaptive weighting.

**Why Empirical Bayes?**
- **Adaptive Priors**: Priors adjust to each user's behavior (e.g., users who skip 80% have higher β)
- **Handles Uncertainty**: Accounts for uncertainty when sample sizes are small
- **User-Specific**: Songs with few observations get conservative estimates based on that user's tendencies
- **Converges to Truth**: Songs with many observations converge to their true play ratio
- **Prevents Extremes**: Avoids extreme weights from insufficient data (e.g., 1 play, 0 skips)

```go
// calculatePlaySkipWeight uses an empirical Bayesian approach (Beta-Binomial model)
// where priors are derived from the user's overall listening patterns
func (s *Service) calculatePlaySkipWeight(userID string, playCount, skipCount int) float64 {
    if playCount == 0 && skipCount == 0 {
        return UnplayedSongWeight // Boost for new songs for this user
    }

    // Get empirical priors based on user's overall listening patterns
    alpha, beta := s.getEmpiricalPriors(userID)

    // Calculate Bayesian posterior mean using Beta-Binomial model
    posteriorPlays := float64(playCount) + alpha
    posteriorTotal := float64(playCount+skipCount) + alpha + beta
    bayesianPlayRatio := posteriorPlays / posteriorTotal

    // Map the Bayesian ratio to weight range using proper linear interpolation
    return PlayRatioMinWeight + (bayesianPlayRatio * (PlayRatioMaxWeight - PlayRatioMinWeight))
}
```

**Empirical Bayes Example Comparisons (with default priors α=2.0, β=2.0):**

| Scenario | Simple Ratio | Empirical Bayes | Benefit |
|----------|-------------|-----------------|---------|
| 1 play, 0 skips | 2.0x (100%) | 1.16x (60%) | Less extreme for small sample |
| 1 play, 9 skips | 0.38x (10%) | 0.543x (21.4%) | More forgiving for small sample |
| 10 plays, 0 skips | 2.0x (100%) | 1.571x (85.7%) | Slight regularization |
| 100 plays, 0 skips | 2.0x (100%) | 1.770x (98.1%) | Converges to true ratio |
| 5 plays, 5 skips | 1.0x (50%) | 1.0x (50%) | Neutral weight for balanced |

### 3. User-Isolated Transition Probability Weight
Uses **user-specific transition data** to prefer songs that historically follow well from the user's last played song. **Thread-safe access** with mutex protection.

```go
func (s *Service) calculateTransitionWeight(userID, songID string) float64 {
    s.mu.RLock()
    lastPlayed, exists := s.lastPlayed[userID]
    s.mu.RUnlock()

    if !exists || lastPlayed == nil {
        return 1.0 // Neutral weight if no previous song for this user
    }

    probability, err := s.db.GetTransitionProbability(userID, lastPlayed.ID, songID)
    if err != nil {
        return 1.0
    }

    return BaseTransitionWeight + probability // Based on user's transition history
}
```

### 4. Per-User Artist Valuation Weight ✅ **NEW**
Applies a weight multiplier based on the user's historical preference for the song's artist. Artists that the user tends to play (vs skip) get higher weights.

```go
func (s *Service) calculateArtistWeight(userID, artist string) float64 {
    stats, err := s.db.GetArtistStats(userID, artist)
    if err != nil || (stats.PlayCount == 0 && stats.SkipCount == 0) {
        return 1.0 // Neutral weight for new/unknown artists
    }

    // Map ratio [0.0, 1.0] to weight range [0.5, 1.5]
    // ratio=0.0 (all skips) -> 0.5x weight
    // ratio=0.5 (balanced) -> 1.0x weight
    // ratio=1.0 (all plays) -> 1.5x weight
    return ArtistRatioMinWeight + (stats.Ratio * (ArtistRatioMaxWeight - ArtistRatioMinWeight))
}
```

## Multi-Tenant API ✅ **UPDATED**

### Initialization
```go
import "github.com/syeo66/subsoxy/shuffle"

shuffleService := shuffle.New(database, logger)
```

### Getting User-Specific Shuffled Songs
```go
// Get 50 weighted-shuffled songs for a specific user (REQUIRED user parameter)
userID := "alice"
songs, err := shuffleService.GetWeightedShuffledSongs(userID, 50)

// Different users get completely different personalized recommendations
bobSongs, err := shuffleService.GetWeightedShuffledSongs("bob", 50)
```

### Setting User-Specific Last Played Song and Time-Based Skip Detection (Thread-Safe) ✅ **ENHANCED**
```go
// Set last played song for a specific user - thread-safe
userID := "alice"
song := &models.Song{ID: "song123"}
shuffleService.SetLastPlayed(userID, song) // Protected by mutex

// Process scrobble with time-based skip detection (scrobble-only logic with duration checking)
recordSkipFunc := func(userID string, song *models.Song) {
    // Record skip event in database
    fmt.Printf("Song %s was skipped by %s\n", song.ID, userID)
}

// Time-based skip detection:
// 1. Non-submission scrobble followed by another scrobble = skip previous (IF time < 2x song duration)
shuffleService.ProcessScrobble(userID, "song456", false, recordSkipFunc) // submission=false
shuffleService.ProcessScrobble(userID, "song789", false, recordSkipFunc) // song456 marked as skip (if < 2x duration)

// 2. Extended pause handling: If hours pass (> 2x song duration), previous song NOT marked as skip
// This prevents false skips when users pause playback for extended periods

// 3. Submission scrobble = definitive play
shuffleService.ProcessScrobble(userID, "song789", true, recordSkipFunc) // song789 marked as play

// 4. Fallback behavior: When song duration is unavailable (0), falls back to always marking as skipped

// Multiple goroutines can safely access different users concurrently
go shuffleService.SetLastPlayed("alice", songA)
go shuffleService.ProcessScrobble("bob", "songB", true, recordSkipFunc) // Safe concurrent access
```

## Multi-Tenant Weight Calculation ✅ **UPDATED**

The final weight is calculated **per user** as:
```
final_weight = base_weight × user_time_weight × user_play_skip_weight × user_transition_weight × artist_weight
```

Where:
- `base_weight` = 1.0 (can be adjusted for global tuning)
- `user_time_weight` = 0.1 to 2.0 (lower for recently played by this user)
- `user_play_skip_weight` = 0.2 to 2.0 (higher for frequently played by this user)
- `user_transition_weight` = 0.5 to 1.5 (higher for good transitions for this user)
- `artist_weight` = 0.5 to 1.5 (higher for artists this user tends to play) ✅ **NEW**

## Multi-Tenant Selection Process ✅ **UPDATED**

1. **User Context Validation**: Ensure valid user ID provided
2. **User-Specific Song Retrieval**: Get all songs for the specific user
3. **Per-User Weight Calculation**: Calculate weights based on user's individual data
4. **User-Isolated Sorting**: Sort songs by weight based on user's preferences
5. **Weighted Random Selection**: Use user-specific weights to pick songs
6. **Duplicate Prevention**: Ensure no duplicates in the user's result set
7. **User-Specific Results**: Return requested number of songs for the user

## Multi-Tenant Features ✅ **UPDATED**

### Personalized Intelligent Recommendations
- **Individual Variety**: Recent songs are de-prioritized per user
- **Personal Preference Learning**: Frequently played songs by each user are favored for that user
- **User-Specific Smooth Transitions**: Considers each user's song sequence context
- **Personalized Discovery**: New songs get a boost to encourage exploration for each user
- **Complete User Isolation**: One user's preferences don't affect another's recommendations

### Multi-Tenant Performance
- **User-Specific Efficient Weight Calculation**: Optimized for per-user data
- **Isolated Database Queries**: Single query per user for all their songs
- **Per-User In-Memory Processing**: Sorting and selection isolated by user
- **User Context Validation**: Input validation ensures proper user isolation
- **Scalable Architecture**: Supports unlimited users with optimal performance

### Multi-Tenant Debugging
- **User-Specific Logging**: Detailed logging of weight calculations per user
- **Per-User Weight Breakdown**: Debug mode shows calculations for each user
- **User Performance Metrics**: Performance metrics tracked per user for large libraries
- **User Context Tracking**: All logs include user context for proper isolation

## Configuration

The algorithm uses several well-defined constants for maintainable and tunable behavior:

```go
// Algorithm selection constants
const (
    LargeLibraryThreshold = 5000
    BatchSize             = 1000
    OversampleFactor      = 3
)

// Weight calculation constants
const (
    NeverPlayedWeight       = 2.0
    HoursPerDay            = 24.0
    TimeDecayDaysThreshold = 30
    TimeDecayMinWeight     = 0.1
    TimeDecayMaxWeight     = 0.9
    DaysPerYear            = 365.0
    UnplayedSongWeight     = 1.5
    PlayRatioMinWeight     = 0.2
    PlayRatioMaxWeight     = 1.8
    BaseTransitionWeight   = 0.5
    MaxTransitionWeight    = 1.5  // Maximum transition weight
    ArtistRatioMinWeight   = 0.5  // Minimum weight for unpopular artists ✅ **NEW**
    ArtistRatioMaxWeight   = 1.5  // Maximum weight for popular artists ✅ **NEW**
    // Bayesian prior parameters for Beta-Binomial model ✅ **NEW**
    BayesianPriorAlpha     = 2.0  // Prior "plays" - assumes slight tendency toward playing
    BayesianPriorBeta      = 2.0  // Prior "skips" - assumes slight tendency toward skipping
)
```

## Multi-Tenant Usage Example ✅ **UPDATED**

```go
// Initialize
db, _ := database.New("music.db", logger)
shuffle := shuffle.New(db, logger)

// User-specific operations
userID := "alice"

// Set user-specific context (last played song)
lastSong := &models.Song{ID: "previous-song-id"}
shuffle.SetLastPlayed(userID, lastSong)

// Get user-specific intelligent shuffle
songs, err := shuffle.GetWeightedShuffledSongs(userID, 25)
if err != nil {
    log.Error("Failed to get shuffled songs for user:", userID, err)
    return
}

// Songs are now intelligently ordered based on this user's listening history
for _, song := range songs {
    fmt.Printf("Selected for %s: %s - %s\n", userID, song.Artist, song.Title)
}

// Different user gets completely different recommendations
bobSongs, err := shuffle.GetWeightedShuffledSongs("bob", 25)
if err != nil {
    log.Error("Failed to get shuffled songs for bob:", err)
    return
}

// Bob's recommendations are based on his individual preferences
for _, song := range bobSongs {
    fmt.Printf("Selected for bob: %s - %s\n", song.Artist, song.Title)
}
```

## Multi-Tenant Benefits

- **Complete User Isolation**: Each user's recommendations are based solely on their own listening history
- **Personalized Experience**: Individual users receive recommendations tailored to their preferences
- **Privacy Compliance**: No data bleeding between users ensures privacy requirements are met
- **Scalable Architecture**: Supports unlimited users with efficient per-user processing
- **Individual Learning**: Each user's preferences are learned and applied independently
- **Thread Safety**: ✅ **NEW** - Concurrent access from multiple users is fully protected with mutex locks
- **Race Condition Free**: ✅ **NEW** - No data corruption under high concurrent load (verified with Go race detector)
- **Artist Preference Learning**: ✅ **NEW** - Learns and applies artist-level preferences per user for better recommendations

## Thread Safety Implementation ✅ **NEW**

The shuffle service now includes comprehensive thread safety:

### Mutex Protection
- **RWMutex**: Uses `sync.RWMutex` for optimal read/write performance
- **Write Protection**: `SetLastPlayed()` and `SetLastStarted()` use exclusive locks (`Lock()/Unlock()`)  
- **Read Protection**: `CheckForSkip()` and `calculateTransitionWeight()` use shared locks (`RLock()/RUnlock()`)
- **Concurrent Users**: Multiple users can safely access the service simultaneously

### Time-Based Skip Detection Methods ✅ **ENHANCED**
- **ProcessScrobble**: Implements intelligent scrobble-only skip detection with time-based validation
  - Fetches song details to get duration for accurate skip detection
  - Only marks as skipped if time between scrobbles < 2x previous song duration
  - Prevents false skips from extended pauses or playback interruptions
  - Falls back to always marking as skipped when song duration is unavailable (0)
- **SetLastPlayed**: Records when a song is successfully played (only definitive plays)
- **No Stream Tracking**: Stream events no longer influence skip detection
- **Extended Pause Handling**: Automatically detects and handles long pauses (> 2x song duration)
- **Cleaner Implementation**: Focuses on user scrobble behavior with intelligent time validation
- **Thread-Safe**: All methods use appropriate mutex protection for concurrent access

### Testing ✅ **ENHANCED**

#### Comprehensive Test Coverage
- **Core Algorithm Tests**: `TestCalculateSongWeight()` with 6 scenarios covering all weight calculation paths
- **Boundary Condition Tests**: `TestCalculateSongWeightBoundaryConditions()` with extreme values and edge cases
- **Transition Weight Tests**: `TestCalculateSongWeightWithTransition()` with pre-computed probabilities
- **Component Tests**: Individual tests for time decay, play/skip ratio, and transition weights
- **Integration Tests**: Full shuffle workflow testing with various library sizes
- **Time-Based Skip Detection Tests**: ✅ **NEW** - `TestProcessScrobbleTimeBasedSkipDetection()` with 6 scenarios:
  - Skip recorded when time < 2x song duration
  - Skip NOT recorded when time > 2x song duration
  - Fallback behavior when song has no duration (0)
  - Fallback persists even after long time when duration is 0
  - Longer songs have longer threshold (5 min song = 10 min threshold)
  - Longer songs not skipped after exceeding their threshold

#### Test Scenarios
- **Never played songs**: Validates maximum weight boost (2.0 × 1.5 × 1.0 = 3.0)
- **Recently played songs**: Tests time decay with high play ratios
- **Frequently skipped songs**: Validates low weights despite song age
- **Transition history**: Tests weight boost from previous song relationships
- **Mixed play/skip history**: Validates balanced weight calculations
- **Boundary cases**: 30-day threshold testing for time decay transitions
- **Extreme values**: Million-count plays/skips, ancient dates, very recent plays
- **Finite validation**: Ensures all weights are positive and finite

#### Concurrent Access Testing
- **Concurrent Test**: `TestConcurrentAccess()` with 100 goroutines × 10 iterations
- **Race Detection**: Verified with `go test -race` - no race conditions detected
- **Load Testing**: Tested with 10 simultaneous users via curl - no issues
- **Thread Safety**: All weight calculation functions tested under concurrent access

#### Test Quality Metrics
- **Coverage**: High test coverage across all weight calculation algorithms
- **Validation**: All weights verified as positive, finite, and within expected ranges
- **Error Handling**: Boundary conditions tested for robustness
- **Performance**: Algorithm efficiency validated with timing assertions

### Performance
- **Read Optimization**: Multiple readers can access `lastPlayed` data simultaneously
- **Write Synchronization**: Only write operations block other access
- **Lock Granularity**: Fine-grained locking minimizes contention
- **Zero Overhead**: No performance impact when accessed by single user