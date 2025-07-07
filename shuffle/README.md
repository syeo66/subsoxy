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

### 1. User-Specific Time Decay Weight
Reduces likelihood of recently played songs **for each individual user** to encourage variety.

```go
func (s *Service) calculateTimeDecayWeight(lastPlayed time.Time) float64 {
    if lastPlayed.IsZero() {
        return 2.0 // Boost for never-played songs by this user
    }
    
    daysSinceLastPlayed := time.Since(lastPlayed).Hours() / 24.0
    
    if daysSinceLastPlayed < 30 {
        return 0.1 + (daysSinceLastPlayed/30.0)*0.9 // 0.1 to 1.0
    }
    
    return 1.0 + math.Min(daysSinceLastPlayed/365.0, 1.0) // 1.0 to 2.0
}
```

### 2. Per-User Play/Skip Ratio Weight
Favors songs with better play-to-skip ratios **for each specific user**.

```go
func (s *Service) calculatePlaySkipWeight(playCount, skipCount int) float64 {
    if playCount == 0 && skipCount == 0 {
        return 1.5 // Boost for new songs for this user
    }
    
    totalEvents := playCount + skipCount
    if totalEvents == 0 {
        return 1.0
    }
    
    playRatio := float64(playCount) / float64(totalEvents)
    return 0.2 + (playRatio * 1.8) // 0.2 to 2.0 based on user's behavior
}
```

### 3. User-Isolated Transition Probability Weight
Uses **user-specific transition data** to prefer songs that historically follow well from the user's last played song.

```go
func (s *Service) calculateTransitionWeight(userID, songID string) float64 {
    if s.lastPlayed[userID] == nil {
        return 1.0 // Neutral weight if no previous song for this user
    }
    
    probability, err := s.db.GetTransitionProbability(userID, s.lastPlayed[userID].ID, songID)
    if err != nil {
        return 1.0
    }
    
    return 0.5 + probability // 0.5 to 1.5 based on user's transition history
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

### Setting User-Specific Last Played Song
```go
// Set last played song for a specific user
userID := "alice"
song := &models.Song{ID: "song123"}
shuffleService.SetLastPlayed(userID, song)

// Each user's last played song is tracked independently
shuffleService.SetLastPlayed("bob", &models.Song{ID: "song456"})
```

## Multi-Tenant Weight Calculation ✅ **UPDATED**

The final weight is calculated **per user** as:
```
final_weight = base_weight × user_time_weight × user_play_skip_weight × user_transition_weight
```

Where:
- `base_weight` = 1.0 (can be adjusted for global tuning)
- `user_time_weight` = 0.1 to 2.0 (lower for recently played by this user)
- `user_play_skip_weight` = 0.2 to 2.0 (higher for frequently played by this user)
- `user_transition_weight` = 0.5 to 1.5 (higher for good transitions for this user)

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

The algorithm uses several constants that can be tuned:

```go
const (
    RecentDaysThreshold = 30.0    // Days to consider "recent"
    NeverPlayedBoost   = 2.0      // Weight boost for new songs
    NewSongBoost       = 1.5      // Play/skip weight for new songs
    MinPlaySkipWeight  = 0.2      // Minimum play/skip weight
    MaxPlaySkipWeight  = 2.0      // Maximum play/skip weight
    MinTransitionWeight = 0.5     // Minimum transition weight
    MaxTransitionWeight = 1.5     // Maximum transition weight
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