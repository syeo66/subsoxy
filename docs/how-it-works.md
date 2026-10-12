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
| `submission=true` (or no `submission` parameter, which the Subsonic API treats as `true`) | Recorded as a **play**, at the time given by the `time` parameter if the client sent one. The song becomes the user's "last played" track |
| `submission=true` again for the same song | Ignored, so client retries don't count twice |
| Previous scrobble was `submission=false`, the new one is a different song, and less than 2× the previous song's duration has passed | Previous song recorded as a **skip** |
| Same as above, but more than 2× the duration has passed | No skip. The user probably paused or stopped |
| Previous song's duration is unknown | The cut-off is 1 hour (`MaxSkipTimeoutHours`) instead of 2× duration |
| Same song scrobbled again | Status updated, never counted as a skip |

**Client requirements:** this works because the client sends a "now playing" scrobble (`submission=false`) when a track starts, and a `submission=true` scrobble once its play threshold is reached. [Voidweaver](https://github.com/syeo66/voidweaver) does both for every track, and queues and retries scrobbles while offline. With a client that only sends `submission=true`, plays are still counted but skips are never detected. When a skip is detected also depends on the client's scrobble threshold. In Voidweaver that's configurable: a minimum play time or percentage, whichever comes first.

A scrobble can name several songs (repeated `id`, each with an optional matching `time`), for example plays a client queued while offline. Each one is handled in turn. A `time` in the future is treated as now.

Recording an event updates the song's `play_count`/`skip_count`, its `last_played`/`last_skipped` timestamp (which only moves forward, even for an event delivered late), and its decayed counters.

### Decayed counters

Raw counts treat a play from three years ago the same as one from yesterday. So each song also stores `adjusted_plays` and `adjusted_skips`, which lose half their value every 180 days (`AdjustedHalfLife`), plus `adjusted_at`, the time they were last brought up to date:

```
value now = stored value × 0.5^((now − adjusted_at) / 180 days)
```

On each event the stored counters are decayed to the event's time and the event adds 1. An event delivered late (an offline play) is decayed by its own age instead, so it counts for what it would have been worth had it arrived on time. Decay depends only on time, so a song skipped years ago and not heard since recovers on its own.

| Age of an event | Counts as |
|-----------------|-----------|
| today | 1.0 |
| 6 months | 0.5 |
| 1 year | 0.25 |
| 2 years | 0.06 |
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

The priors α and β are **empirical**, computed from the songs (or artists, for the artist factor) the user has played or skipped. Songs never heard are left out, since in a large library they would dilute the averages until every user got the same flat prior:

- **Mean** α / (α + β): the user's overall play ratio, (plays + 2) / (plays + skips + 4). The +2/+4 nudges it toward 0.5, so a user with no skips yet still leaves room for skips.
- **Strength** α + β: the average number of adjusted events per heard song (or artist), kept between 2 and 10 (`MinPriorStrength`, `MaxPriorStrength`). The prior then carries about as much evidence as a typical song, and a song's own history soon outweighs it.

A user who skips 80% of what they hear therefore starts every song at a ratio near 0.2. With no history yet the priors default to α = β = 2.0. Priors are cached per user.

### Selection and acoustic chaining

Songs are drawn one at a time by weighted random selection, without replacement. The similarity factor is relative to a moving reference track:

1. The first reference is the user's last played track.
2. After every 5 picks (`SimilarityChainStep`), the most recent pick becomes the new reference.

The queue drifts gradually instead of jumping between moods. Similarity scores come from the upstream `/rest/getSonicSimilarTracks` endpoint, which is provided by the AudioMuse-AI-NV plugin (Navidrome v0.62+, OpenSubsonic `sonicSimilarity` extension). Results are cached for 5 minutes, and concurrent lookups for the same track are deduplicated. If the endpoint is missing or fails, the similarity factor is 1.0 and nothing else changes.

### Streaming the library

Selection runs as an exponential race. Each eligible song gets a random clock that runs at the speed of its weight, and songs are picked in the order their clocks finish. That gives exactly the same odds as drawing one song at a time in proportion to weight, and it lets the library be read in 1,000-row batches while only the requested number of songs is kept in memory. All other songs finish later at their base weight, so they can only overtake when the similarity factor speeds them up. Those songs are loaded from the database when they become similar. Every song in the library is weighted, whatever its size.

Rough timings: about 2 ms for 1k songs, about 40 ms for 10k, about 0.9 s for 50k.

### Request and response

```bash
curl "http://localhost:8080/rest/getRandomSongs?u=alice&p=secret&size=50&f=json"
```

- `size`: defaults to 50, and zero or negative values also get 50. Larger values than 10,000 are served as 10,000. A value that isn't an integer returns a Subsonic error (code 0).
- `f`: `json` (default) or `xml`.
- Each song includes `id`, `title`, `artist`, `album`, `duration` and, when available, `coverArt`. You can pass `coverArt` straight to the upstream `/rest/getCoverArt`.

## Library sync

Subsoxy doesn't need any upstream credentials of its own. It learns them from client requests:

1. A `/rest/*` request with `u` + `p` or `u` + `t` + `s` is validated against upstream `/rest/ping`. For forwarded requests this runs in the background, limited by `-credential-workers`. It's skipped when upstream already confirmed that user's credentials within the last hour, or when every worker is busy, so forwarding never waits on it. A secret upstream rejected is refused for 5 minutes without asking again. For `getRandomSongs` and `scrobble` it runs before Subsoxy handles the request (see [Security](security.md)).
2. If the user is **new**, a sync of that user's library starts right away.
3. After that, every known user is re-synced **hourly**, one after another with a 2s pause between users so the upstream server isn't overloaded. A user whose sync is still running is skipped.

A sync pages through all songs with `search3` and an empty query, 500 songs per request. For a library of about 4,000 songs on Navidrome that's 9 requests instead of about 600. If the server returns no songs that way, or ignores `songOffset`, the sync falls back to walking the library: `getMusicFolders` → `getIndexes` → `getMusicDirectory` (artist) → `getMusicDirectory` (album). An empty search result is never taken to mean an empty library. The result is then diffed against the local copy:

- New songs are inserted.
- Existing songs are updated only when the title, artist, album, duration or cover art changed. Play and skip history is kept.
- Songs no longer on the upstream server are deleted from `songs`. Their `play_events` are kept as history. If any folder, artist or album couldn't be fetched, nothing is deleted in that sync, so an upstream hiccup can't wipe play counts. Each sync request gives up after 30s, so a stalled upstream server can't block later syncs.

Each sync logs added, updated, unchanged and deleted counts per user.

## Multi-user isolation

- `/rest/getRandomSongs` and `/debug` need the `u` parameter. If it's missing, `getRandomSongs` returns a Subsonic error (code 10, `Required parameter is missing: u`) and `/debug` returns HTTP 400 `Missing user parameter`.
- The `songs` table is keyed on `(id, user_id)`, and every query filters by `user_id`.
- Shuffle state (last played track, last scrobble, cached priors, similarity cache) is keyed by user and protected by a mutex.
- One user's plays and skips never affect another user's shuffle.
