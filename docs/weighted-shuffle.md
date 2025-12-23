# Weighted Shuffle Feature

The `/rest/getRandomSongs` endpoint provides intelligent song shuffling using a **per-user weighted algorithm** with **2-week replay prevention** and **memory-efficient performance optimizations** that considers multiple factors to provide personalized music recommendations for each user.

## 2-Week Replay Prevention ✅ **ENHANCED**

The shuffle system strictly prevents songs from being replayed too frequently:

- **Minimum Replay Interval**: Songs are not replayed for at least 14 days after being played OR skipped
- **Skip Tracking**: Skipped songs are now tracked with `last_skipped` timestamps and excluded from replay
- **Comprehensive Prevention**: Both played and skipped songs respect the 2-week prevention period
- **Strict Enforcement**: Songs within the 2-week window are strictly excluded from results
- **Database-Level Filtering**: For large libraries (>5,000 songs), filtering happens at the database level for memory efficiency
- **Configurable**: `TwoWeekReplayThreshold = 14` constant can be adjusted if needed

## Exponential Decay System ✅ **NEW**

The shuffle system now implements **incremental exponential decay** for play and skip counts, making recent listening behavior more influential than older history.

### How Decay Works

Instead of treating all plays and skips equally, the system applies a decay factor of **0.95** to existing values each time a new event occurs:

- **On Play Event**:
  - `adjusted_plays = 1.0 + (old_adjusted_plays × 0.95)`
  - `adjusted_skips = old_adjusted_skips × 0.95`

- **On Skip Event**:
  - `adjusted_skips = 1.0 + (old_adjusted_skips × 0.95)`
  - `adjusted_plays = old_adjusted_plays × 0.95`

### Mathematical Properties

The decay formula creates a **geometric series** that converges to a finite limit:

- **Convergence Limit**: 1/(1-0.95) = **20.0**
- **10 consecutive events**: ~6.513 adjusted weight
- **20 consecutive events**: ~12.84 adjusted weight
- **100+ consecutive events**: ~20.0 (essentially at convergence)

This ensures that:
- Recent events have maximum impact (weight = 1.0)
- Each older event contributes 5% less than the previous one
- Total weight never grows unbounded regardless of play count
- Very old events have minimal influence on current recommendations

### Benefits

1. **Recency Emphasis**: Recent plays/skips matter more than ancient history
2. **Adaptive Preferences**: User taste changes are reflected faster
3. **Bounded Growth**: Prevents songs with thousands of plays from dominating
4. **Smooth Transitions**: Gradual decay prevents sudden weight changes
5. **User-Specific**: Each user's decay is calculated independently

### Example Scenarios

| Event History | Raw Count | Adjusted Count | Impact |
|--------------|-----------|----------------|--------|
| 1 recent play | 1 | 1.0 | Full weight |
| 2 consecutive plays | 2 | 1.95 | Nearly double |
| 5 consecutive plays | 5 | 4.108 | ~82% of raw count |
| 10 consecutive plays | 10 | 6.513 | ~65% of raw count |
| 100 consecutive plays | 100 | ~20.0 | Converged to limit |

### Database Storage

- **Fields**: `adjusted_plays` and `adjusted_skips` stored as REAL (float64)
- **Migration**: Automatically initializes from raw counts for existing data
- **Updates**: Applied incrementally on each play/skip event
- **Performance**: No runtime calculation overhead - values pre-computed

## Performance Optimizations

The shuffle system automatically adapts to library size for optimal performance:

### Small Libraries (≤5,000 songs)
- **Algorithm**: Original algorithm with complete song analysis and 2-week filtering for both played and skipped songs
- **Memory Usage**: O(total_songs) - all songs loaded into memory with date filtering
- **Performance**: ~5ms for 1,000 songs, ~25ms for 5,000 songs
- **Quality**: 100% of songs considered for maximum recommendation quality
- **Replay Prevention**: In-memory filtering by last played and last skipped dates

### Large Libraries (>5,000 songs)
- **Algorithm**: Memory-efficient reservoir sampling with database-level 2-week filtering for both played and skipped songs
- **Memory Usage**: O(sample_size) - only representative sample in memory
- **Performance**: ~106ms for 10,000 songs, ~2.4s for 50,000 songs
- **Quality**: 3x oversampling maintains high recommendation quality
- **Batch Processing**: Processes songs in 1,000-song batches to control memory usage
- **Replay Prevention**: Database queries exclude songs played OR skipped within 14 days

### Performance Benefits
- **Memory Efficiency**: ~90% reduction in memory usage for large libraries
- **Scalability**: Handles libraries with 100,000+ songs without memory exhaustion
- **Batch Database Queries**: Single query for all transition probabilities (eliminates N+1 query problem)
- **Automatic Algorithm Selection**: Seamlessly switches algorithms based on library size
- **Thread Safety**: Maintained with optimized concurrent access patterns

## How Multi-Tenant Shuffling Works

The shuffle algorithm calculates a weight for each song **per user** based on:

1. **Never-Presented Boost**: Songs that have never been played OR skipped receive a 4.0x weight multiplier to encourage discovery
2. **User-Specific Time Decay**: ✅ **ENHANCED** - Uses the most recent timestamp between last_played and last_skipped to accurately track when a song was presented to the listener. Recently presented songs (within 30 days) receive lower weights to encourage variety
3. **Per-User Play/Skip Ratio with Bayesian Categorization**: ✅ **ENHANCED** - Uses Bayesian Beta-Binomial model for robust weight calculation that handles uncertainty in small sample sizes. Songs with better play-to-skip ratios for this specific user are more likely to be selected, with conservative estimates for songs with few plays/skips
4. **User-Specific Transition Probabilities**: Uses transition data from this user's listening history to prefer songs that historically follow well from their last played song
5. **Artist Preference Weighting**: ✅ **NEW** - Artists with better play/skip ratios for this user receive higher weight multipliers (0.5x to 1.5x)

## Database Performance Optimizations ✅ **UPDATED**

- **`GetSongCount()`**: Fast song counting for intelligent algorithm selection
- **`GetSongsBatch()`**: Pagination support with LIMIT/OFFSET for memory-efficient processing
- **`GetSongsBatchFiltered()`**: ✅ **NEW** - Time-based filtering at database level for 2-week replay prevention
- **`GetSongCountFiltered()`**: ✅ **NEW** - Efficient counting of songs outside replay window
- **`GetTransitionProbabilities()`**: Batch probability queries eliminate N+1 query problems
- **Prepared Statements**: Optimized query performance with connection pooling

## Multi-Tenant Usage

### Format Support ✅ **NEW**
The endpoint now supports both JSON and XML output formats via the `f` parameter with **cover art information** included:

```bash
# JSON format (default)
curl "http://localhost:8080/rest/getRandomSongs?u=alice&p=password&c=subsoxy"

# JSON format (explicit)
curl "http://localhost:8080/rest/getRandomSongs?u=alice&p=password&c=subsoxy&f=json"

# XML format ✅ **NEW**
curl "http://localhost:8080/rest/getRandomSongs?u=alice&p=password&c=subsoxy&f=xml"
```

### Usage Examples

```bash
# Get 50 user-specific weighted-shuffled songs (REQUIRED user parameter)
curl "http://localhost:8080/rest/getRandomSongs?u=alice&p=password&c=subsoxy&f=json"

# Different user gets different personalized recommendations
curl "http://localhost:8080/rest/getRandomSongs?u=bob&p=password&c=subsoxy&f=json"

# Get 100 user-specific weighted-shuffled songs in XML format
curl "http://localhost:8080/rest/getRandomSongs?size=100&u=alice&p=password&c=subsoxy&f=xml"

# Token-based authentication with XML output
curl "http://localhost:8080/rest/getRandomSongs?u=alice&t=token&s=salt&c=subsoxy&f=xml"
```

## Multi-Tenancy Benefits

- **Personalized Recommendations**: Each user gets recommendations based on their individual listening history
- **2-Week Replay Prevention**: ✅ **ENHANCED** - Each user's songs are strictly prevented from replaying for 14 days after being played OR skipped individually
- **User-Specific Repetition Reduction**: Recently played songs by each user are excluded from their shuffle for 14 days
- **Individual Preference Learning**: Songs each user tends to play (vs skip) are weighted higher for that user only
- **Per-User Context Awareness**: Considers what song was played previously by each user for smoother transitions
- **Individual Discovery**: New and unplayed songs get a boost per user to encourage personalized exploration
- **Artist-Level Learning**: ✅ **NEW** - Learns each user's artist preferences and boosts/reduces songs accordingly
- **Complete Isolation**: User recommendations don't affect each other's shuffle algorithms

## Error Handling

- **Missing User Parameter**: Returns HTTP 400 with "Missing user parameter" error
- **Invalid Parameters**: Proper validation with descriptive error messages
- **User Context Validation**: All requests validated for user context before processing

## Algorithm Details

### Weight Calculation Factors

1. **2-Week Replay Filter**: ✅ **ENHANCED** - Songs played OR skipped within 14 days are excluded first
2. **Never-Presented Bonus**: ✅ **ENHANCED** - Songs that have never been played OR skipped receive 4.0x weight (increased from 2.0x to prioritize discovery)
3. **Time Decay Weight**: ✅ **ENHANCED** - Uses the most recent timestamp between last_played and last_skipped. Recently presented songs (< 30 days) receive lower weights (0.1x-0.9x), while songs presented long ago receive higher weights (up to 2.0x)
4. **Play/Skip Ratio Weight with Empirical Bayesian Categorization and Exponential Decay**: ✅ **ENHANCED** - Uses Beta-Binomial model with time-decayed play/skip counts for robust, recency-aware weight calculation:
   - **Exponential Decay**: Recent plays/skips have more influence than older ones using incremental decay (factor: 0.95)
   - **Adaptive Priors**: Priors (α, β) dynamically calculated from each user's overall listening patterns
   - **Formula**: `bayesianPlayRatio = (adjustedPlays + α) / (adjustedPlays + adjustedSkips + α + β)`
   - **Range**: 0.2x to 1.8x based on Bayesian-smoothed play ratio with decayed counts
   - **Benefits**: Conservative estimates for songs with few observations, converges to true ratio with more data, emphasizes recent behavior
   - **Example**: Song with 10 recent plays gets ~6.513 adjusted weight (geometric series convergence), older plays contribute progressively less
5. **Transition Probability Weight**: Uses probabilities from user's last played song
6. **Artist Preference Weight with Exponential Decay**: ✅ **NEW** - Multiplies by 0.5x to 1.5x based on user's artist play/skip ratio using time-decayed adjusted values aggregated from all artist's songs
7. **Final Weight**: All factors multiplied together per user

### Memory-Efficient Implementation

For large libraries, the system uses reservoir sampling with strict replay prevention:
- **Pre-filtering**: Database-level filtering excludes songs played OR skipped within 14 days
- **Sampling**: Samples 3x the requested number of songs from eligible candidates
- **Strict Filtering**: Only returns songs outside the 2-week replay window
- **Batch Processing**: Processes songs in batches to control memory usage
- **High Quality**: Maintains recommendation quality with reduced memory footprint
- **Automatic Switching**: Switches to this mode for libraries >5,000 songs

### Thread Safety

- Protected `lastPlayed` map access with `sync.RWMutex`
- Thread-safe operations across multiple concurrent requests
- Race condition-free implementation verified with Go race detector