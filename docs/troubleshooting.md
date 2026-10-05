# Troubleshooting

Start with `-log-level debug`. Errors look like `[category:CODE] message`, and the category tells you which section below applies.

## Startup / configuration

| Error | Fix |
|-------|-----|
| `[config:INVALID_PORT]` | Use a number from 1 to 65535. Check for a stale `PORT` env var |
| `[config:INVALID_UPSTREAM_URL]` | Include the scheme and host: `http://host:4533`, not `host:4533` |
| `[config:INVALID_LOG_LEVEL]` | One of `debug`, `info`, `warn`, `error` |
| `[config:INVALID_DATABASE_PATH]` | The parent directory couldn't be created. Check permissions and free disk space |
| `[config:INVALID_RATE_LIMIT_RPS]` / burst | RPS ≥ 1 and burst ≥ RPS |
| `[config:INVALID_DB_MAX_IDLE_CONNS]` | Idle connections must be ≤ max open connections |
| `[config:INVALID_CREDENTIAL_WORKERS]` | Must be ≥ 1 |

## No songs / shuffle returns nothing

1. **No credentials yet.** Sync only starts after a client sends a valid `/rest/*` request. The log shows `Skipping song sync - no valid credentials available yet`. Send any authenticated request:
   ```bash
   curl "http://localhost:8080/rest/ping?u=USER&p=PASS&f=json"
   ```
   New credentials start a sync right away. Watch for `Successfully completed differential sync for user`.
2. **Credentials rejected.** Test them directly against the upstream server:
   ```bash
   curl "http://upstream:4533/rest/ping?u=USER&p=PASS&v=1.15.0&c=test&f=json"
   ```
   Special characters in the password need to be URL-encoded.
3. **Everything was played recently.** Songs played or skipped in the last 14 days are excluded, and there's no fallback. A small library that you play heavily can run out of eligible songs.
4. **Missing `u` parameter.** That gives HTTP 400 `Missing user parameter`.

## Upstream connectivity

`[network:...]` errors, or sync failures:

```bash
curl -v http://upstream:4533/rest/ping          # reachable from the proxy host?
curl -w "%{time_total}\n" -o /dev/null -s http://upstream:4533/rest/ping   # slow?
```

Credential validation times out after 10s. A failed sync for one user is logged and retried on the next hourly cycle.

If similarity never seems to have any effect, the upstream server probably doesn't provide `/rest/getSonicSimilarTracks`. That's fine: the shuffle carries on without that factor.

## HTTP 429

You've hit the rate limit. Raise it (`-rate-limit-rps 200 -rate-limit-burst 400`) or turn it off for local testing (`-rate-limit-enabled=false`). Violations are logged as `Rate limit exceeded`.

## Database

| Symptom | Check |
|---------|-------|
| `[database:CONNECTION_FAILED]` | File permissions (`ls -la subsoxy.db`), another process holding a lock (`lsof subsoxy.db`), free disk space |
| `[database:QUERY_FAILED]` | `sqlite3 subsoxy.db "PRAGMA integrity_check;"` and `.schema` |
| Corrupt database | Move it aside and restart. A fresh schema is created, but play history is lost |
| DB file keeps growing | Optionally prune old events: `DELETE FROM play_events WHERE timestamp < datetime('now','-1 year'); VACUUM;` |
| Pool errors under load | Try conservative settings: `-db-max-open-conns 10 -db-max-idle-conns 2` |

The first start after upgrading from a version that still had `song_transitions` runs a `VACUUM`. That can take a while on a large database.

## Performance

- Libraries over 5,000 songs switch automatically to the sampled algorithm. Debug logs show the eligible and total song counts for each request.
- Time a request: `time curl -s "http://localhost:8080/rest/getRandomSongs?u=USER&p=PASS&size=500&f=json" > /dev/null`
- If the first request after a play is slow, it's usually the similarity lookup to the upstream server. Results are cached for 5 minutes.

## Shutdown hangs

Shutdown waits for in-flight credential validations, and each of those can take up to 10s against a slow upstream server. If the process still doesn't exit, `pkill subsoxy` and report it with debug logs.

## Reporting a bug

Include the full error line(s), debug-level logs around the problem, your configuration (with secrets removed), `go version`, your OS, and whether the upstream server is reachable (`curl -v .../rest/ping`).
