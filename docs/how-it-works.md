# How It Works

Subsoxy is a reverse proxy. Every request goes to the upstream Subsonic server unless a hook handles it. Three things happen alongside the proxying:

1. **Learning**: Scrobbles become play and skip events that update each song's statistics.
2. **Syncing**: Each user's library is mirrored into a local SQLite database.
3. **Shuffling**: `/rest/getRandomSongs` is answered locally from that data.

All data is kept per user. Each user has their own songs, events and statistics (see [Multi-user isolation](#multi-user-isolation)).

## Play and skip detection

Only `/rest/scrobble` affects tracking. `/rest/stream` is logged but ignored, so a client that preloads tracks doesn't produce fake skips.

For each user, Subsoxy remembers the last scrobble it saw. When a new scrobble arrives:

| Situation | Result |
|-----------|--------|
| `submission=true` | Recorded as a **play**. The song becomes the user's "last played" track |
| `submission=true` again for the same song | Ignored, so client retries don't count twice |
| Previous scrobble was `submission=false`, the new one is a different song, and less than 2× the previous song's duration has passed | Previous song recorded as a **skip** |
| Same as above, but more than 2× the duration has passed | No skip. The user probably paused or stopped |
| Previous song's duration is unknown | The cut-off is 1 hour (`MaxSkipTimeoutHours`) instead of 2× duration |
| Same song scrobbled again | Status updated, never counted as a skip |

**Client requirements:** this works because the client sends a "now playing" scrobble (`submission=false`) when a track starts, and a `submission=true` scrobble once its play threshold is reached. [Voidweaver](https://github.com/syeo66/voidweaver) does both for every track, and queues and retries scrobbles while offline. With a client that only sends `submission=true`, plays are still counted but skips are never detected. When a skip is detected also depends on the client's scrobble threshold. In Voidweaver that's configurable: a minimum play time or percentage, whichever comes first.

Recording an event updates the song's `play_count`/`skip_count`, its `last_played`/`last_skipped` timestamp, and its decayed counters.

### Decayed counters

Raw counts treat a play from three years ago the same as one from yesterday. So each song also stores `adjusted_plays` and `adjusted_skips`, which are updated on every event with a decay factor of 0.95:

```
on play:  adjusted_plays = 1 + 0.95 × adjusted_plays;  adjusted_skips = 0.95 × adjusted_skips
on skip:  adjusted_skips = 1 + 0.95 × adjusted_skips;  adjusted_plays = 0.95 × adjusted_plays
```

The newest event counts fully, and each older event counts 5% less. A counter can never go above 1/(1−0.95) = 20.

| Consecutive plays | Raw | Adjusted |
|-------------------|-----|----------|
| 1 | 1 | 1.0 |
| 5 | 5 | 4.11 |
| 10 | 10 | 6.51 |
| 100 | 100 | ≈20.0 |

The shuffle uses these adjusted values, not the raw counts.

## Weighted shuffle

### Eligibility

A song can only be picked if it was neither played **nor** skipped in the last 14 days (`TwoWeekReplayThreshold`). There's no fallback: if fewer songs are eligible than requested, the response is shorter.

### Weight factors

Each eligible song gets a weight that's the product of four factors:

```
weight = time × play_skip × artist × similarity
```

| Factor | Range | What it does |
|--------|-------|--------------|
| **time** | 0.1–2.0, or 4.0 | Based on whichever is newer, `last_played` or `last_skipped`. Never presented: **4.0**. Under 30 days ago: 0.1 rising to 1.0. Older: `1 + min(days/365, 1)`, up to 2.0 |
| **play_skip** | 0.2–1.8, or 1.5 | Bayesian play ratio from the song's adjusted counts (see below). No history: **1.5** |
| **artist** | 0.5–1.5 | The same Bayesian ratio, using the sum of adjusted counts over all the artist's songs. Unknown artist: 1.0 |
| **similarity** | 1.0–1.5 | `1 + 0.5 × score`, where `score` (0–1) is how acoustically close the song is to the current reference track. 1.0 when similarity is unavailable |

A song that has never been played gets 4.0 × 1.5 × artist × similarity, which puts discovery well ahead of familiar songs.

### Bayesian play ratio

A song with 1 play and 0 skips shouldn't count as a "100% play" song. Subsoxy smooths the ratio using a Beta-Binomial posterior:

```
ratio  = (adjusted_plays + α) / (adjusted_plays + adjusted_skips + α + β)
weight = min + ratio × (max − min)
```

The priors α and β are **empirical**: they're the user's average adjusted plays and skips per song (or per artist, for the artist factor), with a floor of 1.0. If the user has no history yet they default to α = β = 2.0. A user who skips a lot therefore gets a stronger skip prior. Priors are cached per user.

### Selection and acoustic chaining

Songs are drawn one at a time by weighted random selection, without replacement. The similarity factor is relative to a moving reference track:

1. The first reference is the user's last played track.
2. After every 5 picks (`SimilarityChainStep`), the most recent pick becomes the new reference.

The queue drifts gradually instead of jumping between moods. Similarity scores come from the upstream `/rest/getSonicSimilarTracks` endpoint, which is provided by the AudioMuse-AI-NV plugin (Navidrome v0.62+, OpenSubsonic `sonicSimilarity` extension). Results are cached for 5 minutes, and concurrent lookups for the same track are deduplicated. If the endpoint is missing or fails, the similarity factor is 1.0 and nothing else changes.

### Large libraries

| Library size | Strategy |
|--------------|----------|
| ≤ 5,000 songs | Load all songs, filter in memory, weight all of them |
| > 5,000 songs | Filter at the database level, then reservoir-sample 3× the requested count in 1,000-row batches, then weight and select from the sample |

The large-library path keeps memory proportional to the sample size, not the library size. Rough timings: about 5 ms for 1k songs, about 100 ms for 10k, about 2.4 s for 50k.

### Request and response

```bash
curl "http://localhost:8080/rest/getRandomSongs?u=alice&p=secret&size=50&f=json"
```

- `size`: defaults to 50, maximum 10,000. Invalid or oversized values return HTTP 400.
- `f`: `json` (default) or `xml`.
- Each song includes `id`, `title`, `artist`, `album`, `duration` and, when available, `coverArt`. You can pass `coverArt` straight to the upstream `/rest/getCoverArt`.

## Library sync

Subsoxy doesn't need any upstream credentials of its own. It learns them from client requests:

1. A `/rest/*` request with `u` + `p` or `u` + `t` + `s` is validated against upstream `/rest/ping`. This runs in the background, limited by `-credential-workers`.
2. If the credentials are **new**, a full sync starts right away.
3. After that, every known user is re-synced **hourly**, with staggered delays so the upstream server isn't overloaded.

A sync walks the library through the standard Subsonic API: `getMusicFolders` → `getIndexes` → `getMusicDirectory` (artist) → `getMusicDirectory` (album). The result is then diffed against the local copy:

- New songs are inserted.
- Existing songs are updated only when the title, artist, album, duration or cover art changed. Play and skip history is kept.
- Songs no longer on the upstream server are deleted from `songs`. Their `play_events` are kept as history.

Each sync logs added, updated, unchanged and deleted counts per user.

## Multi-user isolation

- `/rest/getRandomSongs` and `/debug` need the `u` parameter. If it's missing, they fail with HTTP 400 `Missing user parameter`.
- The `songs` table is keyed on `(id, user_id)`, and every query filters by `user_id`.
- Shuffle state (last played track, last scrobble, cached priors, similarity cache) is keyed by user and protected by a mutex.
- One user's plays and skips never affect another user's shuffle.
