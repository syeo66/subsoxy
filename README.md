# Subsoxy

A Go proxy that sits between your music client and a Subsonic-compatible server (Subsonic, Navidrome, …) and adds a personalized shuffle, play/skip tracking, and per-user listening history. Every other request is forwarded unchanged, so existing clients keep working.

## Features

- **Smart shuffle**: `/rest/getRandomSongs` returns weighted picks instead of uniformly random ones. The weights favor songs you play over songs you skip, give unheard songs a boost, and learn which artists you like.
- **Two-week replay prevention**: Songs you played or skipped in the last 14 days are left out.
- **Acoustic flow**: If the upstream server provides AudioMuse-AI similarity (`getSonicSimilarTracks`), songs that sound like what you just played get a boost.
- **Play/skip tracking**: Plays and skips are worked out from scrobbles, with checks for duplicate submissions and long pauses.
- **Multi-user**: Every user has their own library copy, history and preferences.
- **Library sync**: The library syncs as soon as a new user's credentials are first seen, and again every hour after that.
- **Hardening**: Credentials are encrypted in memory, and the proxy has rate limiting, input sanitization, security headers and CORS.

## Quick start

```bash
go build -o subsoxy
./subsoxy -upstream http://my-subsonic-server:4533 -port 8080
```

Point your client at `http://localhost:8080` instead of the Subsonic server. On your first request Subsoxy checks your credentials against the upstream server, syncs your library in the background, and starts learning from your scrobbles.

For a local run that loads `.env` through [dotenvx](https://dotenvx.com), use `./start_server.sh`.

## Enhanced endpoints

| Endpoint | Behavior |
|----------|----------|
| `/rest/getRandomSongs` | Answered by Subsoxy with a weighted shuffle (JSON or XML, includes `coverArt`) |
| `/rest/scrobble` | Records plays and skips, then forwards to the upstream server |
| `/rest/stream`, `/rest/ping`, `/rest/getLicense` | Logged, then forwarded |
| `/debug` | HTML view of per-song weights (only with `-debug-mode`) |
| everything else | Forwarded unchanged |

## Documentation

| Guide | Contents |
|-------|----------|
| [Configuration](docs/configuration.md) | All flags and environment variables, validation rules, tuning |
| [How it works](docs/how-it-works.md) | Shuffle algorithm, skip detection, library sync, multi-user isolation |
| [Architecture](docs/architecture.md) | Packages, request flow, database schema, error handling |
| [Security](docs/security.md) | Credential handling, input validation, rate limiting, headers |
| [Development](docs/development.md) | Building, testing, debug UI, adding hooks |
| [Troubleshooting](docs/troubleshooting.md) | Common errors and how to fix them |

## License

MIT, see [LICENSE](LICENSE).
