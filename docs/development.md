# Development

## Build and run

Requires Go 1.25+ and a C toolchain (`mattn/go-sqlite3` uses CGO).

```bash
make build          # go build -o subsoxy
make test           # go test ./...
make clean          # rm subsoxy
./start_server.sh   # build to a temp file and run with dotenvx (.env)
```

`.air.toml` is included for live reload with [air](https://github.com/air-verse/air). The `Dockerfile` builds a static binary, runs the tests in a separate stage, and produces an Alpine image. `make deploy` merges `main` into `stage` and pushes it.

CI (`.github/workflows/go.yml`) builds and tests on every push and pull request to `main`.

## Testing

```bash
go test ./...                       # everything
go test ./... -race                 # with the race detector (recommended before committing)
go test ./... -coverprofile=c.out && go tool cover -html=c.out

# Focused runs
go test ./shuffle -v -run TestCalculateSongWeight
go test ./shuffle -v -run TestProcessScrobbleTimeBasedSkipDetection
go test ./database -run ErrorHandling
go test ./handlers -run BoundaryConditions
go test ./credentials -run Network

# Performance
go test ./shuffle -bench BenchmarkShuffle -benchtime 3s
go test ./shuffle -run TestMemoryUsage -v
```

Tests use temporary SQLite files and `httptest` servers in place of a real upstream. If a run is interrupted, delete any leftover `test*.db` files.

### Against a real server

```bash
rm -f subsoxy.db
./subsoxy -upstream https://your-server -port 8081 -log-level debug &

# First request captures credentials and starts a sync right away
curl -s "http://localhost:8081/rest/ping?u=me&p=secret&v=1.15.0&c=dev&f=json"
sleep 10 && sqlite3 subsoxy.db "SELECT COUNT(*) FROM songs WHERE user_id='me';"

curl -s "http://localhost:8081/rest/getRandomSongs?u=me&p=secret&v=1.15.0&c=dev&size=5&f=json" | jq .
curl -s "http://localhost:8081/rest/scrobble?u=me&p=secret&v=1.15.0&c=dev&id=SONG_ID&submission=true"

# CORS
curl -i -X OPTIONS -H "Origin: http://localhost:3000" -H "Access-Control-Request-Method: GET" http://localhost:8081/rest/ping
```

## Debug UI

Start with `-debug-mode` (or `DEBUG=1`) and open:

```
http://localhost:8080/debug?u=USER&p=PASSWORD[&id=SONG_ID]
```

The page lists every song with its final weight and each factor (time, play/skip, artist, similarity), color-coded high/medium/low. It also shows raw and adjusted play/skip counts and the last played/skipped times. Similarity is measured against `id`, or against the last played track if `id` is missing. Click a song ID to make it the reference, and the password is carried over in the link. See [Security](security.md#debug-endpoint) before turning this on anywhere public.

## Adding a hook

```go
proxyServer.AddHook("/rest/getArtists", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
    logger.Info("artists requested")
    return false // false = keep proxying, true = this hook wrote the response
})
```

Put the logic in a `handlers` method, register it in `main.go`, validate inputs with `handlers.ValidateSongID` and similar helpers, and pass anything user-supplied through `SanitizeForLogging` before logging it.

## Conventions

- `go fmt`. Use structured logging with `logrus.Fields`.
- Return `errors.SubsoxyError` values (predefined or `errors.New`/`errors.Wrap`) with useful `WithContext` fields (see [Architecture](architecture.md#error-handling)).
- Every DB query filters by `user_id`.
- Never log passwords or tokens. Build upstream URLs with `url.Values{}`.
- Add tests for new behavior, including error paths and boundary values. Run `go test ./... -race` before you push.
- Update the relevant guide in `docs/` when behavior or configuration changes.
