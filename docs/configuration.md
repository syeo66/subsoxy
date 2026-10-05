# Configuration

Every option can be set with a command-line flag or an environment variable. Flags take precedence. All values are validated at startup, and an invalid value stops the server with a structured error such as `[config:INVALID_PORT] port must be a number`.

## Reference

### Server

| Flag | Env | Default | Notes |
|------|-----|---------|-------|
| `-port` | `PORT` | `8080` | 1–65535 |
| `-upstream` | `UPSTREAM_URL` | `http://localhost:4533` | Must be an `http`/`https` URL with a host |
| `-log-level` | `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` (case-insensitive) |
| `-debug-mode` | `DEBUG` | `false` | Enables the `/debug` weight inspector (see [Development](development.md#debug-ui)) |
| `-credential-workers` | `CREDENTIAL_WORKERS` | `100` | ≥1. Max concurrent upstream credential validations |

### Database

| Flag | Env | Default | Notes |
|------|-----|---------|-------|
| `-db-path` | `DB_PATH` | `subsoxy.db` | Parent directories are created if missing |
| `-db-max-open-conns` | `DB_MAX_OPEN_CONNS` | `25` | ≥1 |
| `-db-max-idle-conns` | `DB_MAX_IDLE_CONNS` | `5` | ≥0 and ≤ max open |
| `-db-conn-max-lifetime` | `DB_CONN_MAX_LIFETIME` | `30m` | ≥0 |
| `-db-conn-max-idle-time` | `DB_CONN_MAX_IDLE_TIME` | `5m` | ≥0 |
| `-db-health-check` | `DB_HEALTH_CHECK` | `true` | Pings the pool every 30s |

### Rate limiting

| Flag | Env | Default | Notes |
|------|-----|---------|-------|
| `-rate-limit-enabled` | `RATE_LIMIT_ENABLED` | `true` | |
| `-rate-limit-rps` | `RATE_LIMIT_RPS` | `100` | ≥1 |
| `-rate-limit-burst` | `RATE_LIMIT_BURST` | `200` | ≥ RPS |

### CORS

| Flag | Env | Default | Notes |
|------|-----|---------|-------|
| `-cors-enabled` | `CORS_ENABLED` | `true` | |
| `-cors-allow-origins` | `CORS_ALLOW_ORIGINS` | `*` | Comma-separated. Must not be empty when CORS is enabled |
| `-cors-allow-methods` | `CORS_ALLOW_METHODS` | `GET,POST,PUT,DELETE,OPTIONS` | Valid HTTP methods only |
| `-cors-allow-headers` | `CORS_ALLOW_HEADERS` | `Content-Type,Authorization,X-Requested-With` | May be empty |
| `-cors-allow-credentials` | `CORS_ALLOW_CREDENTIALS` | `false` | |

### Security headers

| Flag | Env | Default |
|------|-----|---------|
| `-security-headers-enabled` | `SECURITY_HEADERS_ENABLED` | `true` |
| `-security-dev-mode` | `SECURITY_DEV_MODE` | `false` (localhost is detected automatically, see [Security](security.md#security-headers)) |
| `-x-content-type-options` | `X_CONTENT_TYPE_OPTIONS` | `nosniff` |
| `-x-frame-options` | `X_FRAME_OPTIONS` | `DENY` |
| `-x-xss-protection` | `X_XSS_PROTECTION` | `1; mode=block` |
| `-strict-transport-security` | `STRICT_TRANSPORT_SECURITY` | `max-age=31536000; includeSubDomains` |
| `-content-security-policy` | `CONTENT_SECURITY_POLICY` | `default-src 'self'; script-src 'self'; object-src 'none';` |
| `-referrer-policy` | `REFERRER_POLICY` | `strict-origin-when-cross-origin` |

## Examples

```bash
# Custom port, upstream and database location
./subsoxy -port 9090 -upstream http://music.example.com:4533 -db-path /var/lib/subsoxy/subsoxy.db

# Same thing with environment variables
PORT=9090 UPSTREAM_URL=http://music.example.com:4533 DB_PATH=/var/lib/subsoxy/subsoxy.db ./subsoxy

# Flags win over env: this listens on 9090
PORT=8080 ./subsoxy -port 9090

# Verbose logging plus the debug UI
./subsoxy -log-level debug -debug-mode

# Restrict CORS to a known web client
./subsoxy -cors-allow-origins "https://myapp.com" -cors-allow-credentials=true

# Tighter rate limit for a public instance
./subsoxy -rate-limit-rps 10 -rate-limit-burst 20
```

## Tuning guidance

| Setting | Low-resource | Default | High-traffic |
|---------|--------------|---------|--------------|
| `-credential-workers` | 25–50 | 100 | 200–500 |
| `-db-max-open-conns` / `-db-max-idle-conns` | 10 / 2 | 25 / 5 | 50–100 / 10–20 |
| `-rate-limit-rps` / `-rate-limit-burst` | 5–20 / 10–40 (public) | 100 / 200 | 200 / 400 |

- Turn off rate limiting during local development (`-rate-limit-enabled=false`).
- In production, list exact CORS origins and only enable credentials when you need them.
- Serve over HTTPS so the HSTS header takes effect.
