# Architecture

## Packages

| Package | Responsibility | Depends on |
|---------|----------------|------------|
| `main.go` | Builds config and server, registers hooks | `config`, `server` |
| `config/` | Flag/env parsing and validation | `errors` |
| `server/` | Reverse proxy, middleware chain, credential capture, library sync, shutdown | all |
| `handlers/` | Hook implementations: shuffle, scrobble, stream, debug UI; input validation | `errors`, `shuffle` |
| `shuffle/` | Weighting, selection, skip detection, similarity cache | `models`, `database` |
| `database/` | SQLite schema, migrations, queries, connection pool | `errors`, `models` |
| `credentials/` | Upstream validation and encrypted in-memory credential store | `errors` |
| `middleware/` | Security headers | `config` |
| `models/` | Shared structs (`Song`, `PlayEvent`, Subsonic response types, `Hook`) | — |
| `errors/` | Structured `SubsoxyError` type | — |

External dependencies: `gorilla/mux` (routing), `sirupsen/logrus` (logging), `mattn/go-sqlite3` (CGO SQLite driver), `golang.org/x/time/rate` (rate limiter), `golang.org/x/sync/singleflight` (deduplicating similarity fetches).

## Request flow

```
client ─▶ security headers middleware ─▶ proxyHandler
                                           1. CORS headers (OPTIONS preflight answered here)
                                           2. per-client rate limiter (HTTP 429 when exceeded)
                                           3. read parameters from the query and a form-encoded POST body (body left intact for forwarding)
                                           4. /rest/*: capture u+p or u+t+s, validate async in bounded worker pool (skipped if recently validated or pool full)
                                           5. run hooks for the path; a hook returning true has sent the response
                                           6. otherwise forward via httputil.ReverseProxy
```

A **hook** is `func(w http.ResponseWriter, r *http.Request, endpoint string) bool`. Returning `true` means the hook wrote the response itself and the request isn't forwarded. `main.go` registers these hooks:

| Path | Handler | Forwards? |
|------|---------|-----------|
| `/rest/ping`, `/rest/getLicense` | log only | yes |
| `/rest/stream` | log only | yes |
| `/rest/scrobble` | `HandleScrobble`: play/skip detection | yes |
| `/rest/getRandomSongs` | `HandleShuffle`: weighted shuffle | no |
| `/debug` (with `-debug-mode`) | `HandleDebug`: HTML weight table | no |

## Background work and shutdown

- **Hourly sync ticker**: re-syncs every user with known credentials (see [How it works](how-it-works.md#library-sync)).
- **Immediate sync**: syncs only the new user, started once their credentials are first validated. At most one sync runs per user at a time.
- **DB health check**: runs every 30s when `-db-health-check` is on.

`Shutdown(ctx)` closes the shutdown channel (which stops the sync loop), shuts down the HTTP server (letting in-flight requests finish), waits for in-flight credential validations, and then closes the database (which stops the health-check goroutine).

## Database

SQLite, one file (`-db-path`), in WAL mode with a 5s busy timeout so a library sync and incoming scrobbles can write without failing on a locked database. WAL keeps `-wal` and `-shm` files next to the database, so put all three on the same volume. Every table that holds user data is keyed or indexed by `user_id`.

```sql
CREATE TABLE songs (
    id             TEXT NOT NULL,
    user_id        TEXT NOT NULL,
    title          TEXT NOT NULL,
    artist         TEXT NOT NULL,
    album          TEXT NOT NULL,
    duration       INTEGER NOT NULL,       -- seconds
    last_played    DATETIME,
    last_skipped   DATETIME,
    play_count     INTEGER DEFAULT 0,      -- raw
    skip_count     INTEGER DEFAULT 0,      -- raw
    adjusted_plays REAL DEFAULT 0.0,       -- decayed, see how-it-works.md
    adjusted_skips REAL DEFAULT 0.0,
    adjusted_at    INTEGER,                -- Unix time the adjusted counters were last updated
    cover_art      TEXT,
    PRIMARY KEY (id, user_id)
);

CREATE TABLE play_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT NOT NULL,
    song_id       TEXT NOT NULL,
    event_type    TEXT NOT NULL,           -- play | skip
    timestamp     DATETIME NOT NULL,
    previous_song TEXT
);

CREATE INDEX idx_songs_user_id         ON songs(user_id);
CREATE INDEX idx_play_events_user_id   ON play_events(user_id);
```

There's no separate artist table. The shuffle's artist factor sums `adjusted_plays`/`adjusted_skips` from `songs` (`GetAllArtistAdjustedStats`, one grouped query per shuffle).

### Migrations

These run automatically at startup and are idempotent:

- Single-tenant to multi-tenant schema upgrade (existing data is backed up first).
- Add missing columns `cover_art`, `last_skipped`, `adjusted_plays`, `adjusted_skips`. The adjusted values start out equal to the raw counts.
- Drop the old `song_transitions` table (and its indexes and `song_transitions_backup`), then `VACUUM`. That table grew quadratically with library size and rarely affected the shuffle.
- Drop the old `artist_stats` table (and its indexes). It held raw per-artist counts that the shuffle no longer read.

### Connection pool

`database.NewWithPool` applies the `-db-*` settings. With `-db-health-check`, a goroutine pings the database every 30s and logs pool statistics at debug level. `Close()` is idempotent and stops the health-check goroutine.

## Error handling

All internal errors are `errors.SubsoxyError`:

```go
type SubsoxyError struct {
    Category string                 // config, database, credentials, server, network, validation, auth
    Code     string                 // e.g. INVALID_PORT, QUERY_FAILED
    Message  string
    Cause    error
    Context  map[string]interface{}
}
```

They format as `[category:CODE] message: cause` and support `Unwrap`, `Is` and `As`. Predefined values (`ErrInvalidPort`, `ErrDatabaseQuery`, `ErrMissingParameter`, …) are extended with `.WithContext(key, value)`. Use `errors.Wrap(err, category, code, msg)` for errors that come from outside the project. `IsCategory`, `GetErrorCode` and `GetErrorContext` let you inspect them.

What happens on failure:

- **Config errors**: fatal at startup.
- **Invalid `getRandomSongs` input or shuffle failures**: a Subsonic error response (`"status": "failed"` with an `error` code and message) sent with HTTP 200, in the requested format, since Subsonic clients read errors from the response body. A missing `u` is code 10, anything else code 0.
- **Invalid `/debug` input**: HTTP 400.
- **Upstream errors during sync**: logged, and that user is skipped until the next cycle.
- **Failed credential validation**: the credentials are not stored.
