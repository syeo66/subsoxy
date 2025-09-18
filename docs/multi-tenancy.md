# Multi-Tenancy

The proxy implements **complete multi-tenancy** with full user data isolation at the database level. Each user has their own isolated music library, play history, and statistics.

## Key Features

### Complete User Isolation
- **User-Specific Libraries**: Isolated song collections per user
- **Isolated Play History**: Play/skip events tracked per user with no data bleeding
- **Per-User Statistics**: Play counts, skip counts, last played timestamps, last skipped timestamps
- **Isolated Transition Data**: Song transition probabilities calculated independently
- **Per-User Shuffle**: Weighted recommendations based on individual preferences

### Database Schema

#### Multi-Tenant Tables

**songs**: Primary key `(id, user_id)` - user-isolated song data
- `id` (TEXT): Unique song identifier within user context
- `user_id` (TEXT): User identifier for data isolation
- `title` (TEXT): Song title
- `artist` (TEXT): Artist name
- `album` (TEXT): Album name
- `duration` (INTEGER): Song duration in seconds
- `last_played` (DATETIME): Last time the song was played by this user
- `last_skipped` (DATETIME): Last time the song was skipped by this user ✅ **NEW**
- `play_count` (INTEGER): Number of times the song was played by this user
- `skip_count` (INTEGER): Number of times the song was skipped by this user

**play_events**: Includes `user_id` column - all events tracked per user
- `id` (INTEGER PRIMARY KEY): Auto-incrementing event ID
- `user_id` (TEXT): User identifier for data isolation
- `song_id` (TEXT): Reference to the song within user context
- `event_type` (TEXT): Type of event (start, play, skip)
- `timestamp` (DATETIME): When the event occurred
- `previous_song` (TEXT): ID of the previously played song by this user

**song_transitions**: Primary key `(user_id, from_song_id, to_song_id)` - isolated transitions
- `user_id` (TEXT): User identifier for data isolation
- `from_song_id` (TEXT): ID of the song that was playing before
- `to_song_id` (TEXT): ID of the song that started playing
- `play_count` (INTEGER): Number of times this transition resulted in a play
- `skip_count` (INTEGER): Number of times this transition resulted in a skip
- `probability` (REAL): Calculated probability of playing vs skipping

#### Performance Indexes
Optimized `user_id` indexes on all tables:
- `idx_songs_user_id` on songs(user_id)
- `idx_play_events_user_id` on play_events(user_id)
- `idx_song_transitions_user_id` on song_transitions(user_id)

## Features

### Immediate Sync on First Credentials ✅ **NEW**
- **Instant User Experience**: When new credentials are captured from client requests, the server immediately triggers a full music library sync instead of waiting for the hourly cycle
- **Smart Credential Detection**: Automatically detects first-time credential capture and distinguishes from existing stored credentials
- **Background Processing**: Immediate sync runs in background without blocking client requests
- **Zero Configuration**: Works automatically with no additional setup required

### Differential Sync with Accurate Change Detection ✅ **ENHANCED**
- **Intelligent Library Management**: Automatically removes songs from local database that no longer exist on upstream Subsonic server
- **Precise Change Detection**: Only counts songs as "updated" when metadata actually changes (title, artist, album, duration, cover art)
- **Accurate Sync Reporting**: Distinguishes between new, updated, unchanged, and deleted songs with precise counts
- **Data Preservation**: Preserves user listening history (play counts, skip counts, last played timestamps, last skipped timestamps) for existing songs during sync
- **Historical Data Integrity**: Maintains play events and transition data as historical records even when songs are removed
- **Efficient Algorithm**: Uses map-based comparison combined with metadata comparison to identify actual changes
- **Performance Optimization**: Fetches existing songs in batches for efficient metadata comparison
- **Prevents Database Bloat**: Eliminates "zombie songs" that persist locally after removal from upstream library
- **Enhanced Logging**: Detailed sync statistics showing added, updated, unchanged, and deleted song counts per user

## Security & Validation

- **Required User Parameter**: All endpoints require `u=username` parameter
- **Multi-Auth Support**: Password-based (`u` + `p`) and token-based (`u` + `t` + `s`) authentication
- **Input Validation**: User ID validation with sanitization
- **Per-User Credentials**: Secure credential storage and validation with AES-256-GCM encryption
- **User Context Enforcement**: All database operations filtered by user ID

## API Endpoints

All Subsonic endpoints require user context via `u` parameter with either password or token authentication:

**Password-based authentication:**
```bash
GET /rest/getRandomSongs?u=username&p=password&size=50
GET /rest/stream?u=username&p=password&id=songID
GET /rest/scrobble?u=username&p=password&id=songID&submission=true
```

**Token-based authentication (recommended for modern clients):**
```bash
GET /rest/getRandomSongs?u=username&t=token&s=salt&size=50
GET /rest/stream?u=username&t=token&s=salt&id=songID
GET /rest/scrobble?u=username&t=token&s=salt&id=songID&submission=true
```

**Error Handling**: Missing `u` parameter returns HTTP 400: "Missing user parameter"

## Migration & Compatibility

- **Automatic Migration**: Seamless upgrade from single-tenant to multi-tenant schema
- **Zero Downtime**: Migration runs automatically on server startup
- **Data Backup**: Existing data is backed up before migration
- **Backward Compatibility**: Handles existing installations gracefully

## Benefits

- **Complete Data Separation**: Each user's data is completely isolated from other users
- **Personalized Analytics**: Statistics and probabilities calculated independently per user
- **Individual Learning**: Each user's preferences learned and applied separately
- **Privacy Compliance**: No data bleeding between users ensures privacy requirements are met
- **Scalable Architecture**: Supports unlimited users with optimal performance through user-specific database indexes
- **Security Compliance**: Full data isolation meets privacy requirements with no data bleeding between users