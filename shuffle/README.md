# Shuffle Module

The shuffle module implements intelligent weighted song shuffling based on listening history and preferences.

## Overview

This module provides:
- Multi-factor weighting algorithm
- Time decay calculations
- Play/skip ratio analysis
- Transition probability integration
- Configurable shuffle sizes

## Algorithm

The weighted shuffle algorithm considers three main factors:

### 1. Time Decay Weight
Reduces likelihood of recently played songs to encourage variety.

```go
func (s *Service) calculateTimeDecayWeight(lastPlayed time.Time) float64 {
    if lastPlayed.IsZero() {
        return 2.0 // Boost for never-played songs
    }
    
    daysSinceLastPlayed := time.Since(lastPlayed).Hours() / 24.0
    
    if daysSinceLastPlayed < 30 {
        return 0.1 + (daysSinceLastPlayed/30.0)*0.9 // 0.1 to 1.0
    }
    
    return 1.0 + math.Min(daysSinceLastPlayed/365.0, 1.0) // 1.0 to 2.0
}
```

### 2. Play/Skip Ratio Weight
Favors songs with better play-to-skip ratios.

```go
func (s *Service) calculatePlaySkipWeight(playCount, skipCount int) float64 {
    if playCount == 0 && skipCount == 0 {
        return 1.5 // Boost for new songs
    }
    
    totalEvents := playCount + skipCount
    if totalEvents == 0 {
        return 1.0
    }
    
    playRatio := float64(playCount) / float64(totalEvents)
    return 0.2 + (playRatio * 1.8) // 0.2 to 2.0
}
```

### 3. Transition Probability Weight
Uses transition data to prefer songs that historically follow well from the last played song.

```go
func (s *Service) calculateTransitionWeight(songID string) float64 {
    if s.lastPlayed == nil {
        return 1.0 // Neutral weight if no previous song
    }
    
    probability, err := s.db.GetTransitionProbability(s.lastPlayed.ID, songID)
    if err != nil {
        return 1.0
    }
    
    return 0.5 + probability // 0.5 to 1.5
}
```

## API

### Initialization
```go
import "github.com/syeo66/subsoxy/shuffle"

shuffleService := shuffle.New(database, logger)
```

### Getting Shuffled Songs
```go
// Get 50 weighted-shuffled songs
songs, err := shuffleService.GetWeightedShuffledSongs(50)
```

### Setting Last Played Song
```go
song := &models.Song{ID: "song123"}
shuffleService.SetLastPlayed(song)
```

## Weight Calculation

The final weight is calculated as:
```
final_weight = base_weight × time_weight × play_skip_weight × transition_weight
```

Where:
- `base_weight` = 1.0 (can be adjusted for global tuning)
- `time_weight` = 0.1 to 2.0 (lower for recently played)
- `play_skip_weight` = 0.2 to 2.0 (higher for frequently played)
- `transition_weight` = 0.5 to 1.5 (higher for good transitions)

## Selection Process

1. Calculate weights for all songs
2. Sort songs by weight (highest first)
3. Use weighted random selection to pick songs
4. Ensure no duplicates in the result set
5. Return requested number of songs

## Features

### Intelligent Recommendations
- **Variety**: Recent songs are de-prioritized
- **Preference Learning**: Frequently played songs are favored
- **Smooth Transitions**: Considers song sequence context
- **Discovery**: New songs get a boost to encourage exploration

### Performance
- Efficient weight calculation
- Single database query for all songs
- In-memory sorting and selection
- Configurable result sizes

### Debugging
- Detailed logging of weight calculations
- Per-song weight breakdown in debug mode
- Performance metrics for large libraries

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

## Usage Example

```go
// Initialize
db, _ := database.New("music.db", logger)
shuffle := shuffle.New(db, logger)

// Set context (last played song)
lastSong := &models.Song{ID: "previous-song-id"}
shuffle.SetLastPlayed(lastSong)

// Get intelligent shuffle
songs, err := shuffle.GetWeightedShuffledSongs(25)
if err != nil {
    log.Error("Failed to get shuffled songs:", err)
    return
}

// Songs are now intelligently ordered based on listening history
for _, song := range songs {
    fmt.Printf("Selected: %s - %s\n", song.Artist, song.Title)
}
```