# Models Module

The models module defines all data structures and types used throughout the application.

## Overview

This module provides:
- Core data structures for songs, events, and transitions
- HTTP request/response types
- Function type definitions
- JSON marshaling tags

## Data Structures

### Song
Represents a song in the music library.

```go
type Song struct {
    ID          string    `json:"id"`
    Title       string    `json:"title"`
    Artist      string    `json:"artist"`
    Album       string    `json:"album"`
    Duration    int       `json:"duration"`
    LastPlayed  time.Time `json:"lastPlayed"`
    PlayCount   int       `json:"playCount"`
    SkipCount   int       `json:"skipCount"`
    IsDir       bool      `json:"isDir"`       // Indicates if this is a directory (album)
    Name        string    `json:"name"`        // Alternative name field for directories
}
```

### PlayEvent
Records when songs are played, skipped, or started.

```go
type PlayEvent struct {
    ID          int       `json:"id"`
    SongID      string    `json:"songId"`
    EventType   string    `json:"eventType"` // "play", "skip", "start"
    Timestamp   time.Time `json:"timestamp"`
    PreviousSong *string  `json:"previousSong,omitempty"`
}
```

### SongTransition
Tracks transition probabilities between songs.

```go
type SongTransition struct {
    FromSongID string  `json:"fromSongId"`
    ToSongID   string  `json:"toSongId"`
    PlayCount  int     `json:"playCount"`
    SkipCount  int     `json:"skipCount"`
    Probability float64 `json:"probability"`
}
```

### WeightedSong
Used in the weighted shuffle algorithm.

```go
type WeightedSong struct {
    Song   Song    `json:"song"`
    Weight float64 `json:"weight"`
}
```

### SubsonicResponse
Standard Subsonic API response structure.

```go
type SubsonicResponse struct {
    SubsonicResponse struct {
        Status  string `json:"status"`
        Version string `json:"version"`
        Songs   struct {
            Song []Song `json:"song"`
        } `json:"songs,omitempty"`
        MusicFolders struct {
            MusicFolder []MusicFolder `json:"musicFolder"`
        } `json:"musicFolders,omitempty"`
        Indexes struct {
            Index []Index `json:"index"`
        } `json:"indexes,omitempty"`
        Directory struct {
            Child []Song `json:"child"`
        } `json:"directory,omitempty"`
    } `json:"subsonic-response"`
}
```

### MusicFolder
Represents a music folder in the Subsonic API.

```go
type MusicFolder struct {
    ID   interface{} `json:"id"`
    Name string      `json:"name"`
}
```

### Artist
Represents an artist.

```go
type Artist struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}
```

### Index
Represents an artist index grouping.

```go
type Index struct {
    Name    string   `json:"name"`
    Artists []Artist `json:"artist"`
}
```

### Hook
Function type for request interception.

```go
type Hook func(w http.ResponseWriter, r *http.Request, endpoint string) bool
```

## Usage

```go
import "github.com/syeo66/subsoxy/models"

// Create a new song
song := models.Song{
    ID:       "123",
    Title:    "Example Song",
    Artist:   "Example Artist",
    Album:    "Example Album",
    Duration: 180,
}

// Create a play event
event := models.PlayEvent{
    SongID:    song.ID,
    EventType: "play",
    Timestamp: time.Now(),
}
```

## Design Notes

- All structures include JSON tags for API serialization
- Time fields use Go's `time.Time` type for proper handling
- Pointer fields (`*string`) are used for optional values
- The `Hook` type provides a clean interface for request interception