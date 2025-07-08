# TODO

## Priority 2: Performance Issues ✅ **COMPLETED**

### 1. **Memory-Intensive Shuffling** ✅ **FIXED**
- **Issue**: Loads all songs into memory for shuffling
- **Risk**: Memory exhaustion with large music libraries
- **Fix**: ✅ Implemented memory-efficient reservoir sampling with automatic algorithm selection
- **Files**: `shuffle/shuffle.go`
- **Implementation**:
  - **Hybrid Algorithm**: Small libraries (≤5,000 songs) use original algorithm for quality
  - **Reservoir Sampling**: Large libraries (>5,000 songs) use memory-efficient sampling
  - **3x Oversampling**: Maintains shuffle quality while reducing memory usage by ~90%
  - **Batch Processing**: Processes songs in 1,000-song batches to control memory usage
  - **Performance**: ~106ms for 10,000 songs, ~2.4s for 50,000 songs vs. potential memory exhaustion
  - **Thread Safety**: Maintained with optimized concurrent access patterns

### 2. **Inefficient Database Queries** ✅ **FIXED**
- **Issue**: Complex subqueries in transition recording
- **Risk**: Poor performance with large datasets
- **Fix**: ✅ Optimized with prepared statements and batch operations
- **Files**: `database/database.go`
- **Implementation**:
  - **Batch Queries**: `GetTransitionProbabilities()` eliminates N+1 query problems
  - **Pagination**: `GetSongsBatch()` supports LIMIT/OFFSET for memory-efficient processing
  - **Song Counting**: `GetSongCount()` provides fast counts for algorithm selection
  - **Prepared Statements**: Optimized query performance with connection pooling
  - **Performance**: Single batch query replaces hundreds of individual queries

## Priority 3: Concurrency and Thread Safety

### 1. **Goroutine Leak Risk** ✅ **FIXED**
- **Issue**: Database health check goroutine had no shutdown mechanism
- **Risk**: Resource leaks on server shutdown
- **Fix**: ✅ Added shutdown channel to database struct with proper cleanup
- **Files**: `database/database.go`
- **Implementation**:
  - **Shutdown Channel**: Added `shutdownChan chan struct{}` to `DB` struct
  - **Graceful Shutdown**: `healthCheckLoop()` listens for shutdown signal via `select`
  - **Proper Cleanup**: `Close()` method signals shutdown by closing the channel
  - **Thread Safety**: Uses channel-based signaling for clean shutdown
  - **Idempotent Close**: Multiple `Close()` calls don't panic or error
  - **Test Coverage**: Added `TestHealthCheckShutdown` and `TestHealthCheckDisabled`

### 2. **Race Conditions** ✅ **FIXED**
- **Issue**: `lastPlayed` field not protected by mutex
- **Risk**: Race conditions in multi-threaded access
- **Fix**: ✅ Added `sync.RWMutex` protection for `lastPlayed` map access
- **Files**: `shuffle/shuffle.go`
- **Implementation**: 
  - Added `mu sync.RWMutex` field to Service struct
  - Protected write operations in `SetLastPlayed()` with `Lock()/Unlock()`
  - Protected read operations in `calculateTransitionWeight()` with `RLock()/RUnlock()`
  - Added `TestConcurrentAccess()` test with 100 goroutines × 10 iterations
  - Verified with Go race detector - no race conditions detected
  - Test coverage increased from 94.6% to 95.0%

## Priority 4: Code Quality and Maintainability

### 1. **Magic Numbers and Constants** 🟡
- **Issue**: Hard-coded values throughout codebase
- **Risk**: Difficult maintenance and configuration
- **Fix**: Define constants for timeouts, limits, and other values
- **Files**: Multiple files

### 2. **Error Handling Inconsistency** 🟡
- **Issue**: Custom error implementation incomplete
- **Risk**: Inconsistent error handling patterns
- **Fix**: Implement proper error wrapping compatible with Go 1.13+
- **Files**: `errors/errors.go`

### 3. **Test Coverage Gaps** 🟡
- **Issue**: Several modules under 70% coverage
- **Risk**: Untested edge cases and error conditions
- **Fix**: Add comprehensive tests, especially for error scenarios
- **Files**: All `*_test.go` files

## Priority 5: Missing Features

### 1. **Security Headers** 🟡
- **Issue**: No security headers in HTTP responses
- **Risk**: Vulnerability to XSS and other attacks
- **Fix**: Add security middleware with proper headers
- **Files**: `server/server.go`

## Implementation Priority Order

1. **Performance Issues** (Priority 2) - ✅ **COMPLETED**
2. **Concurrency Issues** (Priority 3) - ✅ **COMPLETED**
3. **Code Quality** (Priority 4) - Ongoing improvements
4. **Missing Features** (Priority 5) - Add as needed

## Legend
- 🔴 Critical - Fix immediately
- 🟠 High - Fix within 1-2 weeks
- 🟡 Medium - Fix within 1 month
- 🟢 Low - Fix as time permits
