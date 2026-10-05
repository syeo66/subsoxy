# Security

## Credentials

Subsoxy has no credentials of its own. It picks them up from client requests and uses them for background sync and similarity lookups.

- **Supported auth modes**: password (`u` + `p`) and token (`u` + `t` + `s`). Credentials are read from the query string, POST form data, or an Authorization header. Internally a token is stored as `TOKEN:<token>:<salt>` and sent upstream as `t`/`s` again.
- **Validation**: before storing anything, Subsoxy calls upstream `/rest/ping` with the same auth mode (10s timeout). Invalid credentials are never stored.
- **Encryption in memory**: stored credentials are sealed with AES-256-GCM, using a random 32-byte key per process and a fresh nonce for every value. Nothing is written to disk, and a restart throws away both the credentials and the key.
- **Concurrency**: the store is protected by an `RWMutex`. Validation runs in a bounded worker pool (`-credential-workers`) so a flood of requests can't create unlimited goroutines.
- **No credential logging**: upstream URLs are built with `url.Values{}`, and usernames are sanitized before they're logged. Passwords and tokens are never logged.

## Input validation

| Input | Rule |
|-------|------|
| Song ID | Required where used, max 255 chars |
| Username | Max 100 chars (truncated with a warning) |
| `size` on `getRandomSongs` | Positive integer ≤ 10,000, otherwise HTTP 400 |
| Anything logged | Control characters (ASCII 0–31, 127) removed, cut to 1,000 chars |

All SQL uses prepared statements.

## Rate limiting

A global token bucket (`-rate-limit-rps`, `-rate-limit-burst`) runs before hooks and proxying. Requests over the limit get HTTP 429, and the client address and endpoint are logged.

## Security headers

Set by `middleware/security.go` on every response when `-security-headers-enabled` is on. All values can be overridden (see [Configuration](configuration.md#security-headers)).

| | Production | Dev mode |
|-|------------|----------|
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; object-src 'none';` | `default-src 'self' 'unsafe-inline' 'unsafe-eval'; connect-src 'self' ws: wss:; img-src 'self' data: blob:;` |
| `X-Frame-Options` | `DENY` | `SAMEORIGIN` |
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains`, only over HTTPS | not sent |
| `X-Content-Type-Options`, `X-XSS-Protection`, `Referrer-Policy` | sent | sent |

Dev mode is used when `-security-dev-mode` is set **or** the request comes from localhost (`localhost`, `127.0.0.1`, `::1`, including IPv6 forms with brackets and ports).

## Debug endpoint

`/debug` (enabled with `-debug-mode`) doesn't check the caller against the upstream server. Anyone who can reach it can see any user's library and weights by passing `u=<user>`. The optional `p` is only used for similarity lookups. Leave debug mode off on any instance that's reachable from outside.

## Recommendations

- Run behind HTTPS so HSTS applies and credentials aren't sent in clear text.
- Set exact CORS origins in production, and only enable `-cors-allow-credentials` together with them.
- Lower the rate limits for public instances (see [Configuration](configuration.md#tuning-guidance)).
