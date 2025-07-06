# Database Module

The database module handles all SQLite3 database operations for song tracking and transition analysis with comprehensive error handling and validation.

## Overview

This module provides:
- Database initialization and schema creation with error recovery
- Song storage and retrieval with input validation
- Play event recording with structured error handling
- Transition probability tracking with graceful degradation
- Thread-safe database operations with transaction management
- Comprehensive error context for debugging

## Database Schema

### songs
```sql
CREATE TABLE songs (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    artist TEXT NOT NULL,
    album TEXT NOT NULL,
    duration INTEGER NOT NULL,
    last_played DATETIME,
    play_count INTEGER DEFAULT 0,
    skip_count INTEGER DEFAULT 0
);
```

### play_events
```sql
CREATE TABLE play_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    song_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    previous_song TEXT,
    FOREIGN KEY (song_id) REFERENCES songs(id),
    FOREIGN KEY (previous_song) REFERENCES songs(id)
);
```

### song_transitions
```sql
CREATE TABLE song_transitions (
    from_song_id TEXT NOT NULL,
    to_song_id TEXT NOT NULL,
    play_count INTEGER DEFAULT 0,
    skip_count INTEGER DEFAULT 0,
    probability REAL DEFAULT 0.0,
    PRIMARY KEY (from_song_id, to_song_id),
    FOREIGN KEY (from_song_id) REFERENCES songs(id),
    FOREIGN KEY (to_song_id) REFERENCES songs(id)
);
```

## API

### Initialization
```go
import "github.com/syeo66/subsoxy/database"

db, err := database.New("/path/to/database.db", logger)
if err != nil {
    // handle error
}
defer db.Close()
```

### Song Operations
```go
// Store multiple songs (bulk insert with transaction)
songs := []models.Song{...}
err := db.StoreSongs(songs)

// Get all songs
songs, err := db.GetAllSongs()
```

### Event Recording
```go
// Record a play event
err := db.RecordPlayEvent("song123", "play", nil)

// Record a transition
err := db.RecordTransition("song1", "song2", "play")

// Get transition probability
prob, err := db.GetTransitionProbability("song1", "song2")
```

## Features

### Transaction Management
- Bulk operations use transactions for performance
- Automatic rollback on errors
- Prepared statements for efficiency

### Error Handling
- Structured errors with categorization and context using the `errors` package
- Input validation with descriptive error messages
- Graceful degradation for missing records vs. actual errors
- Detailed logging of failed operations with context
- Transaction rollback and retry logic
- Connection failure handling with helpful diagnostics

#### Error Examples
```go
// Connection error with context
[database:CONNECTION_FAILED] failed to open database
Context: {"path": "/invalid/path/db.sqlite"}

// Query error with query context
[database:QUERY_FAILED] failed to record play event
Context: {"song_id": "123", "event_type": "play"}

// Validation error
[validation:MISSING_PARAMETER] missing required parameter
Context: {"field": "songID"}
```

### Performance Optimization
- Indexes on frequently queried columns
- Bulk inserts for song synchronization
- Prepared statements for repeated operations

### Data Integrity
- Foreign key constraints
- UPSERT operations to handle duplicates
- Automatic probability calculation

## Implementation Details

### Song Storage
- Uses `INSERT OR REPLACE` to handle duplicates
- Preserves existing play/skip counts when updating song metadata
- Batch processing with transactions for performance

### Event Recording
- Automatically updates song statistics (play_count, skip_count, last_played)
- Records transition data for recommendation engine
- Maintains complete event history

### Transition Probabilities
- Automatically calculated as `play_count / (play_count + skip_count)`
- Updated whenever transition events are recorded
- Used by the shuffle algorithm for intelligent recommendations