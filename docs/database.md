# Database Features

The server automatically creates and manages a SQLite3 database with advanced connection pooling to track song play statistics and build transition probability analysis for song sequences.

## Database Connection Pooling ✅

The application implements advanced database connection pooling for optimal performance under high load.

### Goroutine Leak Prevention ✅ **FIXED**

The database connection pool includes proper goroutine lifecycle management:

**Health Check Goroutine Management**:
- **Shutdown Channel**: Added `shutdownChan chan struct{}` to `DB` struct for clean shutdown signaling
- **Graceful Termination**: `healthCheckLoop()` listens for shutdown signal via `select` statement
- **Proper Cleanup**: `Close()` method signals shutdown by closing the channel
- **Thread Safety**: Uses channel-based signaling for clean shutdown without race conditions
- **Idempotent Close**: Multiple `Close()` calls don't panic or error
- **Resource Leak Prevention**: Guarantees health check goroutine terminates on database close

### Connection Pool Features

**Performance Optimization**:
- **Connection Reuse**: Maintains a pool of database connections to avoid expensive connection creation
- **Configurable Pool Size**: Adjustable maximum open and idle connection limits
- **Connection Lifecycle Management**: Automatic rotation and cleanup of aged connections
- **Health Monitoring**: Periodic health checks to ensure connection validity

**Configuration Options**:
- **Max Open Connections**: Maximum number of concurrent database connections (default: 25)
- **Max Idle Connections**: Maximum number of idle connections to keep open (default: 5)
- **Connection Lifetime**: Maximum time a connection can be reused (default: 30 minutes)
- **Idle Timeout**: Maximum time a connection can stay idle (default: 5 minutes)
- **Health Checks**: Automatic connection health monitoring (default: enabled)

### Connection Pool Architecture

**Thread-Safe Operations**:
- All connection pool operations are protected with mutex locks
- Safe concurrent access from multiple request handlers
- Atomic statistics tracking and updates

**Health Check System**:
- Background health checks every 30 seconds
- Connection statistics monitoring (open, idle, in-use connections)
- Failed connection tracking and logging
- Automatic connection pool statistics via `GetConnectionStats()`

**Dynamic Configuration**:
- Runtime pool configuration updates via `UpdatePoolConfig()`
- Configuration validation with detailed error messages
- Live monitoring of connection pool performance

### Performance Benefits

- ✅ **Reduced Connection Overhead**: Reuse existing connections instead of creating new ones
- ✅ **Better Resource Management**: Automatic cleanup of idle and expired connections
- ✅ **Improved Concurrency**: Handle multiple simultaneous requests efficiently
- ✅ **Health Monitoring**: Early detection of database connection issues
- ✅ **Configurable Scaling**: Adjust pool size based on application load
- ✅ **Thread Safety**: Safe concurrent access from multiple goroutines

## Multi-Tenant Database Schema ✅ **UPDATED**

### songs (Multi-Tenant)
- `id` (TEXT): Unique song identifier within user context
- `user_id` (TEXT): User identifier for data isolation
- `title` (TEXT): Song title
- `artist` (TEXT): Artist name
- `album` (TEXT): Album name
- `duration` (INTEGER): Song duration in seconds
- `last_played` (DATETIME): Last time the song was played by this user
- `play_count` (INTEGER): Number of times the song was played by this user
- `skip_count` (INTEGER): Number of times the song was skipped by this user
- **PRIMARY KEY**: `(id, user_id)` for per-user song isolation

### play_events (Multi-Tenant)
- `id` (INTEGER PRIMARY KEY): Auto-incrementing event ID
- `user_id` (TEXT): User identifier for data isolation
- `song_id` (TEXT): Reference to the song within user context
- `event_type` (TEXT): Type of event (start, play, skip)
- `timestamp` (DATETIME): When the event occurred
- `previous_song` (TEXT): ID of the previously played song by this user (for transition tracking)

### song_transitions (Multi-Tenant)
- `user_id` (TEXT): User identifier for data isolation
- `from_song_id` (TEXT): ID of the song that was playing before (within user context)
- `to_song_id` (TEXT): ID of the song that started playing (within user context)
- `play_count` (INTEGER): Number of times this transition resulted in a play for this user
- `skip_count` (INTEGER): Number of times this transition resulted in a skip for this user
- `probability` (REAL): Calculated probability of playing (vs skipping) this transition for this user
- **PRIMARY KEY**: `(user_id, from_song_id, to_song_id)` for per-user transition isolation

### Multi-Tenancy Database Indexes
- **Performance Optimized**: User-specific indexes on all tables
  - `idx_songs_user_id` on songs(user_id)
  - `idx_play_events_user_id` on play_events(user_id)
  - `idx_song_transitions_user_id` on song_transitions(user_id)
- **Query Optimization**: All database operations filter by user_id for optimal performance

### Migration & Compatibility
- **Automatic Migration**: Seamless upgrade from single-tenant to multi-tenant schema
- **Data Backup**: Existing data is backed up before migration
- **Zero Downtime**: Migration runs automatically on server startup
- **Backward Compatibility**: Handles existing installations gracefully

## Multi-Tenant Features ✅ **UPDATED**

- **Per-User Credential Management**: Automatically captures and validates user credentials from client requests with user isolation
- **User-Isolated Automatic Song Sync**: Fetches all songs from the Subsonic API every hour using validated credentials, with smart startup timing that waits for client requests before syncing
- **Immediate Sync on New Credentials ✅ NEW**: Automatically triggers full library sync when new credentials are first captured, providing instant user experience instead of waiting for hourly cycle
- **Directory Traversal Sync ✅ NEW**: Uses proper Subsonic API methodology (`getMusicFolders` → `getIndexes` → `getMusicDirectory`) for reliable and complete library discovery
- **Per-User Play Tracking**: Records when songs are started, played completely, or skipped with complete user isolation
- **User-Specific Transition Probability Analysis**: Builds transition probabilities between songs for each user independently
- **Isolated Historical Data**: Maintains complete event history for analysis per user

## Multi-Tenant Data Collection

The system automatically tracks per user:
- User credentials from client requests and validates them against the upstream server with user context
- When a song starts playing (`/rest/stream` endpoint) - recorded with user ID
- When a song is marked as played or skipped (`/rest/scrobble` endpoint) - tracked per user
- Transitions between songs for building personalized recommendation data per user

## User Isolation Benefits

- **Complete Data Separation**: Each user's data is completely isolated from other users
- **Personalized Analytics**: Statistics and probabilities calculated independently per user
- **Individual Learning**: Each user's preferences learned and applied separately
- **Privacy Compliance**: No data bleeding between users ensures privacy requirements are met