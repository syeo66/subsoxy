# TODO

## Priority 2: Performance Issues

### 1. **Memory-Intensive Shuffling** 🟠
- **Issue**: Loads all songs into memory for shuffling
- **Risk**: Memory exhaustion with large music libraries
- **Fix**: Implement streaming or pagination-based shuffling
- **Files**: `shuffle/shuffle.go`

### 2. **Inefficient Database Queries** 🟠
- **Issue**: Complex subqueries in transition recording
- **Risk**: Poor performance with large datasets
- **Fix**: Optimize with prepared statements and batch operations
- **Files**: `database/database.go`

## Priority 3: Concurrency and Thread Safety

### 1. **Goroutine Leak Risk** 🟡
- **Issue**: Goroutines started without proper cleanup tracking
- **Risk**: Resource leaks under error conditions
- **Fix**: Use context-based cancellation and proper lifecycle management
- **Files**: `server/server.go`

### 2. **Race Conditions** 🟡
- **Issue**: `lastPlayed` field not protected by mutex
- **Risk**: Race conditions in multi-threaded access
- **Fix**: Add mutex protection for shared fields
- **Files**: `shuffle/shuffle.go`

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

1. **Performance Issues** (Priority 2) - Fix within 1-2 weeks
2. **Concurrency Issues** (Priority 3) - Fix within 1 month
3. **Code Quality** (Priority 4) - Ongoing improvements
4. **Missing Features** (Priority 5) - Add as needed

## Legend
- 🔴 Critical - Fix immediately
- 🟠 High - Fix within 1-2 weeks
- 🟡 Medium - Fix within 1 month
- 🟢 Low - Fix as time permits
