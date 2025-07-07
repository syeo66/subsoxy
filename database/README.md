# Database Module

The database module handles all SQLite3 database operations for song tracking and transition analysis with comprehensive error handling, validation, and advanced connection pooling.

## Overview

This module provides:
- Database initialization and schema creation with error recovery
- Advanced connection pooling with health monitoring and statistics
- Song storage and retrieval with input validation
- Play event recording with structured error handling
- Transition probability tracking with graceful degradation
- Thread-safe database operations with transaction management
- Comprehensive error context for debugging
- Real-time connection pool performance monitoring

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

### Event Recording
- Automatically updates song statistics (play_count, skip_count, last_played)
- Records transition data for recommendation engine
- Maintains complete event history

### Transition Probabilities
- Automatically calculated as `play_count / (play_count + skip_count)`
- Updated whenever transition events are recorded
- Used by the shuffle algorithm for intelligent recommendations