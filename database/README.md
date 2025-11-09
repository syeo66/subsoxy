# Database Module

The database module handles all SQLite3 database operations for song tracking and transition analysis with **complete multi-tenancy support**, comprehensive error handling, validation, and advanced connection pooling.

## Overview

This module provides:
- **Multi-tenant database initialization** and schema creation with automatic migration
- Advanced connection pooling with health monitoring and statistics
- **User-isolated song storage** and retrieval with comprehensive input validation
- **Per-user play event recording** with structured error handling
- **User-specific transition probability tracking** with graceful degradation
- Thread-safe database operations with transaction management and user context
- **User ID validation** and sanitization for security
- Comprehensive error context for debugging with user information
- Real-time connection pool performance monitoring

## Multi-Tenant Database Schema ✅ **UPDATED**

### songs (Multi-Tenant)
```sql
CREATE TABLE songs (
    id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    title TEXT NOT NULL,
    artist TEXT NOT NULL,
    album TEXT NOT NULL,
    duration INTEGER NOT NULL,
    last_played DATETIME,
    last_skipped DATETIME,    -- ✅ FIXED: Tracks when songs were skipped
    play_count INTEGER DEFAULT 0,
    skip_count INTEGER DEFAULT 0,
    cover_art TEXT,           -- ✅ NEW: Cover art support
    PRIMARY KEY (id, user_id)
);
```

### play_events (Multi-Tenant)
```sql
CREATE TABLE play_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    song_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    previous_song TEXT,
    FOREIGN KEY (song_id, user_id) REFERENCES songs(id, user_id),
    FOREIGN KEY (previous_song, user_id) REFERENCES songs(id, user_id)
);
```

### song_transitions (Multi-Tenant)
```sql
CREATE TABLE song_transitions (
    user_id TEXT NOT NULL,
    from_song_id TEXT NOT NULL,
    to_song_id TEXT NOT NULL,
    play_count INTEGER DEFAULT 0,
    skip_count INTEGER DEFAULT 0,
    probability REAL DEFAULT 0.0,
    PRIMARY KEY (user_id, from_song_id, to_song_id),
    FOREIGN KEY (from_song_id, user_id) REFERENCES songs(id, user_id),
    FOREIGN KEY (to_song_id, user_id) REFERENCES songs(id, user_id)
);
```

### artist_stats (Multi-Tenant) ✅ **NEW**
```sql
CREATE TABLE artist_stats (
    user_id TEXT NOT NULL,
    artist TEXT NOT NULL,
    play_count INTEGER DEFAULT 0,
    skip_count INTEGER DEFAULT 0,
    ratio REAL DEFAULT 0.5,
    PRIMARY KEY (user_id, artist)
);
```

### Performance Indexes
```sql
CREATE INDEX idx_songs_user_id ON songs(user_id);
CREATE INDEX idx_play_events_user_id ON play_events(user_id);
CREATE INDEX idx_song_transitions_user_id ON song_transitions(user_id);
CREATE INDEX idx_artist_stats_user_id ON artist_stats(user_id);
CREATE INDEX idx_artist_stats_artist ON artist_stats(artist);
```

## API

### Initialization

#### Basic Initialization
```go
import "github.com/syeo66/subsoxy/database"

// Using default connection pool settings
db, err := database.New("/path/to/database.db", logger)
if err != nil {
    // handle error
}
defer db.Close()
```

#### Advanced Initialization with Custom Pool Configuration
```go
// Create custom connection pool configuration
poolConfig := &database.ConnectionPool{
    MaxOpenConns:    50,                // Maximum concurrent connections
    MaxIdleConns:    10,                // Maximum idle connections
    ConnMaxLifetime: 30 * time.Minute,  // Connection lifetime
    ConnMaxIdleTime: 5 * time.Minute,   // Idle timeout
    HealthCheck:     true,              // Enable health checks
}

// Initialize with custom pool settings
db, err := database.NewWithPool("/path/to/database.db", logger, poolConfig)
if err != nil {
    // handle error
}
defer db.Close()  // Properly shuts down health check goroutine
```

### Multi-Tenant Song Operations ✅ **UPDATED**
```go
// Store multiple songs for a specific user (bulk insert with transaction)
userID := "alice"
songs := []models.Song{...}
err := db.StoreSongs(userID, songs)

// Get all songs for a specific user
userID := "alice"
songs, err := db.GetAllSongs(userID)

// Get existing song IDs for differential sync (NEW)
existingIDs, err := db.GetExistingSongIDs(userID)

// Get existing songs by IDs for change detection (NEW)
songIDs := []string{"song1", "song2", "song3"}
existingSongs, err := db.GetSongsByIDs(userID, songIDs)

// Delete songs that no longer exist upstream (NEW)
songsToDelete := []string{"song1", "song2"}
err := db.DeleteSongs(userID, songsToDelete)

// Each user gets their own isolated song library
bobSongs, err := db.GetAllSongs("bob")  // Completely separate from alice's songs
```

### Multi-Tenant Event Recording ✅ **UPDATED**
```go
// Record a play event for a specific user
userID := "alice"
err := db.RecordPlayEvent(userID, "song123", "play", nil)

// Record a transition for a specific user
err := db.RecordTransition(userID, "song1", "song2", "play")

// Get transition probability for a specific user
prob, err := db.GetTransitionProbability(userID, "song1", "song2")

// Each user's events and transitions are completely isolated
bobProb, err := db.GetTransitionProbability("bob", "song1", "song2")  // Independent from alice's data
```

### Multi-Tenant Filtered Song Retrieval ✅ **FIXED**
```go
// Get count of songs for a user excluding recently played/skipped songs
cutoffTime := time.Now().AddDate(0, 0, -14)  // 2 weeks ago
eligibleCount, err := db.GetSongCountFiltered(userID, cutoffTime)

// Get batch of songs excluding recently played/skipped songs (for 2-week replay prevention)
songs, err := db.GetSongsBatchFiltered(userID, limit, offset, cutoffTime)

// Improved filtering logic with consistent NULL handling:
// WHERE (COALESCE(last_played, '1970-01-01') < cutoff) AND 
//       (COALESCE(last_skipped, '1970-01-01') < cutoff)

// Each user gets filtered results based only on their own play/skip history
bobSongs, err := db.GetSongsBatchFiltered("bob", 50, 0, cutoffTime)  // Independent filtering
```

### Artist Statistics ✅ **NEW**
```go
// Get artist statistics for a specific user and artist
userID := "alice"
stats, err := db.GetArtistStats(userID, "The Beatles")
if err != nil {
    // handle error
}
fmt.Printf("Play count: %d, Skip count: %d, Ratio: %.2f\n",
    stats.PlayCount, stats.SkipCount, stats.Ratio)

// Artist stats are automatically updated when play/skip events are recorded
// The ratio is calculated as: play_count / (play_count + skip_count)

// Manually trigger artist stats migration for a user (usually automatic)
err = db.CalculateInitialArtistStats(userID)

// Migrate artist stats for all users (called automatically on database initialization)
err = db.MigrateArtistStats()
```

### Connection Pool Management
```go
// Get current connection pool statistics
stats := db.GetConnectionStats()
fmt.Printf("Open connections: %d\n", stats.OpenConnections)
fmt.Printf("Idle connections: %d\n", stats.IdleConnections)
fmt.Printf("Health checks: %d\n", stats.HealthChecks)

// Update pool configuration at runtime
newConfig := &database.ConnectionPool{
    MaxOpenConns:    100,
    MaxIdleConns:    20,
    ConnMaxLifetime: 1 * time.Hour,
    ConnMaxIdleTime: 10 * time.Minute,
    HealthCheck:     true,
}
err := db.UpdatePoolConfig(newConfig)
if err != nil {
    // handle configuration error
}
```

## Features

### Database Connection Pooling ✅

**Performance Optimization**:
- **Connection Reuse**: Maintains a pool of database connections to avoid expensive connection creation
- **Configurable Pool Size**: Adjustable maximum open and idle connection limits (default: 25 max open, 5 max idle)
- **Connection Lifecycle Management**: Automatic rotation and cleanup of aged connections (default: 30m lifetime, 5m idle timeout)
- **Health Monitoring**: Periodic health checks every 30 seconds to ensure connection validity

**Thread-Safe Operations**:
- All connection pool operations are protected with mutex locks
- Safe concurrent access from multiple request handlers
- Atomic statistics tracking and updates

**Dynamic Configuration**:
- Runtime pool configuration updates via `UpdatePoolConfig()`
- Configuration validation with detailed error messages
- Live monitoring of connection pool performance via `GetConnectionStats()`

**Health Check System**:
- Background health checks with connection validation
- Connection statistics monitoring (open, idle, in-use connections)
- Failed connection tracking and logging
- Automatic pool performance metrics
- **Goroutine Leak Prevention**: ✅ **FIXED** - Proper health check goroutine shutdown via channel signaling

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
- **Database Connection Pooling**: Advanced connection pool management for high-concurrency scenarios
- **Indexes**: Optimized indexes on frequently queried columns (song_id, timestamp, transitions)
- **Bulk Inserts**: Transaction-based bulk operations for song synchronization
- **Prepared Statements**: Cached prepared statements for repeated operations
- **Connection Lifecycle**: Automatic connection rotation and cleanup for optimal resource usage
- **Health Monitoring**: Background connection health checks to prevent stale connections

### Data Integrity
- Foreign key constraints
- UPSERT operations to handle duplicates
- Automatic probability calculation

## Implementation Details

### Song Storage
- Uses `INSERT OR REPLACE` to handle duplicates
- Preserves existing play/skip counts when updating song metadata
- Batch processing with transactions for performance

### Differential Sync with Change Detection ✅ **ENHANCED**
- **GetExistingSongIDs()**: Efficiently retrieves all existing song IDs for a user as a map for O(1) lookup
- **GetSongsByIDs()**: Fetches existing songs by IDs for metadata comparison and change detection
- **Change Detection**: Only counts songs as "updated" when metadata actually changes (title, artist, album, duration, cover art)
- **Accurate Sync Reporting**: Distinguishes between new, updated, unchanged, and deleted songs
- **DeleteSongs()**: Removes songs by ID while preserving historical play events and transition data
- **Data Preservation**: Maintains user listening history even when songs are removed from the library
- **Historical Integrity**: Intentionally preserves play_events and song_transitions as historical records
- **Transaction Safety**: All operations use transactions to ensure atomicity and consistency
- **Comprehensive Logging**: Detailed logging with accurate change counts (added, updated, unchanged, deleted)

### Event Recording ✅ **ENHANCED**
- Automatically updates song statistics (play_count, skip_count, last_played, last_skipped)
- Records transition data for recommendation engine
- Maintains complete event history
- **Accurate Skip Detection**: Only increments skip_count for actual user skips, not songs that ended without meeting play thresholds
- **Artist Statistics Integration**: ✅ **NEW** - Automatically updates artist-level play/skip stats for weighted shuffle algorithm
- **Robust Error Handling**: ✅ **IMPROVED** - Still records play events even when song doesn't exist in database (gracefully skips stats updates)

### Enhanced Skip Detection Logic ✅ **ENHANCED**
The system now implements robust, preload-resistant skip detection:

- **Pending Song Tracking**: Songs are tracked when streaming starts without immediate skip detection
- **Scrobble-Based Processing**: Skip detection occurs only when scrobble events are received
- **True Skips** (`eventType = "skip"`): Songs that are never scrobbled or when later songs get scrobbled first
- **Play Completions** (`eventType = "play"`): When a song receives `submission=true` from the client
- **Timeout Handling**: Songs pending >5 minutes without scrobble are automatically marked as skipped
- **Preload Support**: Multiple concurrent stream requests don't trigger false skip detection

This ensures accurate skip detection even with aggressive client preloading strategies.

### Transition Probabilities
- Automatically calculated as `play_count / (play_count + skip_count)`
- Updated whenever transition events are recorded
- Now more accurate due to enhanced preload-resistant skip detection
- Used by the shuffle algorithm for intelligent recommendations

### Goroutine Management ✅ **FIXED**

**Health Check Goroutine Lifecycle**:
- **Shutdown Channel**: Database struct includes `shutdownChan chan struct{}` for clean shutdown
- **Graceful Termination**: Health check goroutine listens for shutdown signal via `select` statement
- **Resource Cleanup**: `Close()` method properly signals health check goroutine to stop
- **No Leaks**: Guarantees all background goroutines terminate on database close
- **Thread Safety**: Uses channel-based signaling for race-free shutdown
- **Test Coverage**: Comprehensive tests verify proper goroutine lifecycle management

**Implementation**:
```go
// Health check loop with proper shutdown handling
func (db *DB) healthCheckLoop() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            db.performHealthCheck()
        case <-db.shutdownChan:
            db.logger.Debug("Database health check loop shutting down")
            return  // Goroutine exits cleanly
        }
    }
}
```

**Benefits**:
- **Production Ready**: Eliminates goroutine leak causing resource consumption
- **Clean Shutdown**: Server shutdown doesn't hang waiting for background goroutines
- **Resource Efficient**: Proper goroutine cleanup prevents memory leaks
- **Monitoring Safe**: Health checks stop cleanly without affecting application shutdown