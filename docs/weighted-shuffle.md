# Weighted Shuffle Feature

The `/rest/getRandomSongs` endpoint provides intelligent song shuffling using a **per-user weighted algorithm** with **memory-efficient performance optimizations** that considers multiple factors to provide personalized music recommendations for each user.

## Performance Optimizations ✅ **NEW**

The shuffle system automatically adapts to library size for optimal performance:

### Small Libraries (≤5,000 songs)
- **Algorithm**: Original algorithm with complete song analysis
- **Memory Usage**: O(total_songs) - all songs loaded into memory
- **Performance**: ~5ms for 1,000 songs, ~25ms for 5,000 songs
- **Quality**: 100% of songs considered for maximum recommendation quality

### Large Libraries (>5,000 songs)
- **Algorithm**: Memory-efficient reservoir sampling with batch processing
- **Memory Usage**: O(sample_size) - only representative sample in memory
- **Performance**: ~106ms for 10,000 songs, ~2.4s for 50,000 songs
- **Quality**: 3x oversampling maintains high recommendation quality
- **Batch Processing**: Processes songs in 1,000-song batches to control memory usage

### Performance Benefits
- **Memory Efficiency**: ~90% reduction in memory usage for large libraries
- **Scalability**: Handles libraries with 100,000+ songs without memory exhaustion
- **Batch Database Queries**: Single query for all transition probabilities (eliminates N+1 query problem)
- **Automatic Algorithm Selection**: Seamlessly switches algorithms based on library size
- **Thread Safety**: Maintained with optimized concurrent access patterns

## How Multi-Tenant Shuffling Works

The shuffle algorithm calculates a weight for each song **per user** based on:

1. **User-Specific Time Decay**: Songs played recently by the user (within 30 days) receive lower weights to encourage variety
2. **Per-User Play/Skip Ratio**: Songs with better play-to-skip ratios for this specific user are more likely to be selected
3. **User-Specific Transition Probabilities**: Uses transition data from this user's listening history to prefer songs that historically follow well from their last played song

## Database Performance Optimizations ✅ **NEW**

- **`GetSongCount()`**: Fast song counting for intelligent algorithm selection
- **`GetSongsBatch()`**: Pagination support with LIMIT/OFFSET for memory-efficient processing
- **`GetTransitionProbabilities()`**: Batch probability queries eliminate N+1 query problems
- **Prepared Statements**: Optimized query performance with connection pooling

## Multi-Tenant Usage

### Format Support ✅ **NEW**
The endpoint now supports both JSON and XML output formats via the `f` parameter:

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
- **User-Specific Repetition Reduction**: Recently played songs by each user are less likely to appear in their shuffle
- **Individual Preference Learning**: Songs each user tends to play (vs skip) are weighted higher for that user only
- **Per-User Context Awareness**: Considers what song was played previously by each user for smoother transitions
- **Individual Discovery**: New and unplayed songs get a boost per user to encourage personalized exploration
- **Complete Isolation**: User recommendations don't affect each other's shuffle algorithms

## Error Handling

- **Missing User Parameter**: Returns HTTP 400 with "Missing user parameter" error
- **Invalid Parameters**: Proper validation with descriptive error messages
- **User Context Validation**: All requests validated for user context before processing

## Algorithm Details

### Weight Calculation Factors

1. **Time Decay Weight**: Recent songs (< 30 days) receive lower weights
2. **Play/Skip Ratio Weight**: Based on user's historical play behavior
3. **Transition Probability Weight**: Uses probabilities from user's last played song
4. **Final Weight**: All factors multiplied together per user

### Memory-Efficient Implementation

For large libraries, the system uses reservoir sampling:
- Samples 3x the requested number of songs
- Processes songs in batches to control memory usage
- Maintains high recommendation quality with reduced memory footprint
- Automatically switches to this mode for libraries >5,000 songs

### Thread Safety

- Protected `lastPlayed` map access with `sync.RWMutex`
- Thread-safe operations across multiple concurrent requests
- Race condition-free implementation verified with Go race detector