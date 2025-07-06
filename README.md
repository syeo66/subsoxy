# Subsonic API Proxy Server

A Go-based proxy server that relays requests to a Subsonic API server with configurable endpoint hooks for monitoring and interception. Includes SQLite3 database functionality for tracking played songs and building transition probability analysis.

## Features

- **Reverse Proxy**: Forwards all requests to upstream Subsonic server
- **Hook System**: Intercept and process requests at any endpoint
- **Credential Management**: Secure credential handling with dynamic validation
- **Song Tracking**: SQLite3 database tracks played songs with play/skip statistics
- **Transition Probability Analysis**: Builds transition probabilities between songs
- **Weighted Shuffle**: Intelligent song shuffling based on play history and preferences
- **Automatic Sync**: Fetches and updates song library from Subsonic API
- **Logging**: Structured logging with configurable levels
- **Configuration**: Command-line flags and environment variables for easy setup

## Installation

```bash
go build -o subsoxy
```

## Usage

```bash
./subsoxy [options]
```

### Configuration

Configuration can be set via command-line flags or environment variables. Command-line flags take precedence over environment variables.

#### Command-line flags
- `-port string`: Proxy server port (default: 8080)
- `-upstream string`: Upstream Subsonic server URL (default: http://localhost:4533)
- `-log-level string`: Log level - debug, info, warn, error (default: info)
- `-db-path string`: SQLite database file path (default: subsoxy.db)

#### Environment variables
- `PORT`: Proxy server port
- `UPSTREAM_URL`: Upstream Subsonic server URL
- `LOG_LEVEL`: Log level (debug, info, warn, error)
- `DB_PATH`: SQLite database file path

### Examples

```bash
# Basic usage (creates subsoxy.db in current directory)
./subsoxy

# Quick start with environment variables (uses dotenvx)
./start_server.sh

# Custom port and upstream
./subsoxy -port 9090 -upstream http://my-subsonic-server:4533

# Custom database path
./subsoxy -db-path /path/to/music-stats.db

# Debug logging
./subsoxy -log-level debug

# Using environment variables
PORT=9090 UPSTREAM_URL=http://my-subsonic-server:4533 LOG_LEVEL=debug DB_PATH=/path/to/music.db ./subsoxy
```

## Hook System

The proxy includes a hook system that allows you to intercept requests at specific endpoints. Hooks are functions that can:

- Log or monitor specific API calls
- Block requests (return `true` to prevent forwarding)
- Allow requests to continue (return `false` to forward normally)

### Built-in Hooks

The server includes built-in hooks for:
- `/rest/ping` - Logs ping requests
- `/rest/getLicense` - Logs license requests
- `/rest/stream` - Records song start events for play tracking
- `/rest/scrobble` - Records song play/skip events and updates transition data
- `/rest/getRandomSongs` - Returns weighted shuffle of songs based on play history and preferences

### Adding Custom Hooks

```go
server.AddHook("/rest/getArtists", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    // Your custom logic here
    log.Printf("Artist list requested by %s", r.RemoteAddr)
    return false // Continue with normal proxy behavior
})
```

## Credential Management

The proxy implements secure credential handling to ensure authenticated access to the upstream Subsonic server:

### How It Works

1. **Automatic Capture**: The proxy monitors all `/rest/*` requests and extracts username/password parameters
2. **Validation**: Credentials are validated against the upstream server using a ping request
3. **Secure Storage**: Valid credentials are stored in memory with thread-safe access
4. **Background Operations**: Stored credentials are used for automated tasks like song syncing
5. **Error Handling**: Invalid credentials are handled gracefully with proper logging

### Security Features

- **No Hardcoded Credentials**: All credentials come from authenticated client requests
- **Dynamic Validation**: Credentials are validated in real-time against the upstream server
- **Timeout Protection**: Validation requests have a 10-second timeout to prevent hanging
- **Asynchronous Processing**: Credential validation doesn't block client requests
- **Automatic Cleanup**: Invalid credentials are automatically removed from storage

### Client Usage

Clients should provide credentials in their Subsonic API requests as usual:

```bash
# The proxy will automatically capture and validate these credentials
curl "http://localhost:8080/rest/ping?u=myuser&p=mypass&c=myclient&f=json"
```

The proxy transparently forwards all requests to the upstream server while maintaining valid credentials for background operations.

## Database Features

The server automatically creates and manages a SQLite3 database to track song play statistics and build transition probability analysis for song sequences.

### Database Schema

#### songs
- `id` (TEXT PRIMARY KEY): Unique song identifier
- `title` (TEXT): Song title
- `artist` (TEXT): Artist name
- `album` (TEXT): Album name
- `duration` (INTEGER): Song duration in seconds
- `last_played` (DATETIME): Last time the song was played
- `play_count` (INTEGER): Number of times the song was played
- `skip_count` (INTEGER): Number of times the song was skipped

#### play_events
- `id` (INTEGER PRIMARY KEY): Auto-incrementing event ID
- `song_id` (TEXT): Reference to the song
- `event_type` (TEXT): Type of event (start, play, skip)
- `timestamp` (DATETIME): When the event occurred
- `previous_song` (TEXT): ID of the previously played song (for transition tracking)

#### song_transitions
- `from_song_id` (TEXT): ID of the song that was playing before
- `to_song_id` (TEXT): ID of the song that started playing
- `play_count` (INTEGER): Number of times this transition resulted in a play
- `skip_count` (INTEGER): Number of times this transition resulted in a skip
- `probability` (REAL): Calculated probability of playing (vs skipping) this transition

### Features

- **Credential Management**: Automatically captures and validates user credentials from client requests
- **Automatic Song Sync**: Fetches all songs from the Subsonic API every hour using validated credentials
- **Play Tracking**: Records when songs are started, played completely, or skipped
- **Transition Probability Analysis**: Builds transition probabilities between songs
- **Historical Data**: Maintains complete event history for analysis

### Data Collection

The system automatically tracks:
- User credentials from client requests and validates them against the upstream server
- When a song starts playing (`/rest/stream` endpoint)
- When a song is marked as played or skipped (`/rest/scrobble` endpoint)
- Transitions between songs for building recommendation data

## Weighted Shuffle Feature

The `/rest/getRandomSongs` endpoint provides intelligent song shuffling using a weighted algorithm that considers multiple factors to provide better music recommendations.

### How It Works

The shuffle algorithm calculates a weight for each song based on:

1. **Time Decay**: Songs played recently (within 30 days) receive lower weights to encourage variety
2. **Play/Skip Ratio**: Songs with better play-to-skip ratios are more likely to be selected
3. **Transition Probabilities**: Uses transition data to prefer songs that historically follow well from the last played song

### Usage

```bash
# Get 50 weighted-shuffled songs (default)
curl "http://localhost:8080/rest/getRandomSongs?u=admin&p=admin&c=subsoxy&f=json"

# Get 100 weighted-shuffled songs
curl "http://localhost:8080/rest/getRandomSongs?size=100&u=admin&p=admin&c=subsoxy&f=json"
```

### Benefits

- **Reduces repetition**: Recently played songs are less likely to appear
- **Learns preferences**: Songs you tend to play (vs skip) are weighted higher
- **Context-aware**: Considers what song was played previously for smoother transitions
- **Balances discovery**: New and unplayed songs get a boost to encourage exploration

## Development

```bash
# Install dependencies
go mod tidy

# Run tests
go test ./...

# Build
go build -o subsoxy
```

## License

MIT License