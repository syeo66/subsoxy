# TODO

## Priority 1: Critical Security Issues (Fix Immediately)

### 1. **Password Logging Vulnerability** ✅ **FIXED**
- **Issue**: Passwords were logged in URLs in `server/server.go:210`
- **Risk**: Passwords exposed in logs, debug output, and error messages
- **Fix**: ✅ **COMPLETED** - Replaced direct URL string formatting with secure `url.Values{}` parameter encoding
- **Files**: `server/server.go` - Updated `fetchAndStoreSongs()` function to use proper URL parameter encoding
- **Security Impact**: Passwords are no longer exposed in server logs or debug output

### 2. **Rate Limiting** ✅ **FIXED**
- **Issue**: No protection against DoS attacks
- **Risk**: Application vulnerable to abuse and resource exhaustion
- **Fix**: ✅ **COMPLETED** - Implemented comprehensive rate limiting using `golang.org/x/time/rate`
- **Files**: `server/server.go`, `config/config.go`, `handlers/handlers.go`
- **Features**: 
  - Token bucket algorithm with configurable RPS and burst
  - Applied before hook processing for maximum security
  - HTTP 429 responses with logging for rate limit violations
  - Can be disabled for development/testing
  - Comprehensive test coverage
- **Configuration**: Default 100 RPS, 200 burst, enabled by default
- **Testing**: ✅ Verified with curl testing - blocks rapid requests correctly

### 3. **Credential Storage Security** ✅ **FIXED**
- **Issue**: Valid credentials stored in memory without encryption
- **Risk**: Memory dumps could expose credentials
- **Fix**: ✅ **COMPLETED** - Implemented AES-256-GCM encryption for all stored credentials
- **Files**: `credentials/credentials.go`, `credentials/credentials_test.go`
- **Features**:
  - AES-256-GCM authenticated encryption for all passwords
  - Unique 32-byte encryption keys per server instance
  - Secure memory management with encrypted data zeroing
  - Thread-safe encryption/decryption operations
  - Comprehensive test coverage including encryption validation
- **Security Impact**: 
  - Passwords no longer visible in memory dumps or process inspection
  - Authenticated encryption prevents tampering
  - Per-instance keys provide isolation between servers
  - Forward security through key regeneration on restart
- **Testing**: ✅ Verified with comprehensive test suite and live server testing

### 4. **Input Validation and Sanitization** ✅ **FIXED**
- **Issue**: Limited input validation and sanitization, potential log injection attacks
- **Risk**: Log injection, DoS attacks via oversized inputs, data corruption
- **Fix**: ✅ **COMPLETED** - Implemented comprehensive input validation and sanitization
- **Files**: `handlers/handlers.go`, `server/server.go`
- **Features**:
  - **Log Injection Protection**: `SanitizeForLogging()` removes control characters (ASCII 0-31, 127)
  - **Input Length Limits**: Song IDs (255 chars), usernames (100 chars), general inputs (1000 chars)
  - **Song ID Validation**: `ValidateSongID()` with format and length validation
  - **Control Character Filtering**: Prevents newline, carriage return, tab, and escape sequence injection
  - **DoS Prevention**: Input truncation with "..." suffix for oversized inputs
  - **Comprehensive Testing**: Security test suite covering malicious inputs
- **Security Impact**:
  - ✅ Log injection attacks completely prevented
  - ✅ DoS attacks via oversized inputs blocked
  - ✅ Control character injection neutralized
  - ✅ Audit trail integrity maintained
  - ✅ Memory exhaustion attacks prevented
- **Testing**: ✅ Verified with malicious input testing, control character injection tests, and live server validation

## Priority 2: Performance Issues

### 1. **Database Connection Management** ✅ **FIXED**
- **Issue**: Single SQLite connection without pooling
- **Risk**: Performance bottlenecks under high load
- **Fix**: ✅ **COMPLETED** - Implemented comprehensive connection pooling with health checks
- **Files**: `database/database.go`, `config/config.go`, `server/server.go`
- **Features**:
  - Connection pool with configurable max open/idle connections
  - Connection lifecycle management with configurable timeouts
  - Background health checks with statistics monitoring
  - Thread-safe operations with mutex protection
  - Runtime configuration updates and validation
  - Comprehensive test coverage including concurrent access tests
- **Configuration**: Default 25 max open, 5 max idle, 30m lifetime, 5m idle timeout
- **Testing**: ✅ Verified with concurrent database access tests and health check validation

### 2. **Memory-Intensive Shuffling** 🟠
- **Issue**: Loads all songs into memory for shuffling
- **Risk**: Memory exhaustion with large music libraries
- **Fix**: Implement streaming or pagination-based shuffling
- **Files**: `shuffle/shuffle.go`

### 3. **Inefficient Database Queries** 🟠
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

### 2. **Monitoring and Observability** 🟡
- **Issue**: No metrics or health endpoints
- **Risk**: Difficult to monitor and troubleshoot in production
- **Fix**: Add Prometheus metrics and health endpoints
- **Files**: `server/server.go`

### 3. **Configuration Management** 🟡
- **Issue**: No hot-reload capability
- **Risk**: Requires restart for configuration changes
- **Fix**: Implement configuration watching and hot-reload
- **Files**: `config/config.go`

## Priority 6: Database Multi-Tenancy Implementation

The application currently stores all data globally (shared across all users). This needs to be changed to store data per user (multi-tenant).

### Required Changes

1. **Database Schema Updates**
   - Add `user_id` field to all tables:
     - `songs` table: Add `user_id TEXT NOT NULL`
     - `play_events` table: Add `user_id TEXT NOT NULL`
     - `song_transitions` table: Add `user_id TEXT NOT NULL`
   - Update primary keys and indexes to include `user_id`
   - Create migration scripts for existing data

2. **Database Operations**
   - Update all queries to filter by `user_id`
   - Modify insert operations to include `user_id`
   - Update transaction handling for user-specific operations

3. **User Context Management**
   - Extract user identification from Subsonic API requests
   - Pass user context through request handlers
   - Maintain user context in database operations

4. **API Endpoint Updates**
   - Update all handlers to extract and validate user identity
   - Ensure song synchronization is per-user
   - Modify weighted shuffle to work with user-specific data

5. **Credential Management**
   - Associate credentials with specific users
   - Implement per-user credential validation
   - Update background operations to handle multiple users

6. **Data Isolation**
   - Ensure complete data isolation between users
   - Implement user-specific recommendations
   - Maintain separate play statistics per user

### Impact Areas

- `database/database.go`: All database operations
- `models/models.go`: Data structure definitions
- `handlers/handlers.go`: Request handling and user context
- `credentials/credentials.go`: User credential management
- `shuffle/shuffle.go`: User-specific weighted shuffling
- `server/server.go`: Background operations and user context

### Migration Strategy

Since the project is not in productive use, no data migration is needed:

1. Add new schema with `user_id` fields
2. ~~Migrate existing data~~ (Not needed - no production data)
3. Update application code to use user-specific operations
4. Test thoroughly with multiple users
5. ~~Remove old global data structures~~ (Not needed - direct implementation of new schema)

## Implementation Priority Order

1. **Critical Security Issues** (Priority 1) - Fix immediately
2. **Performance Issues** (Priority 2) - Fix within 1-2 weeks
3. **Concurrency Issues** (Priority 3) - Fix within 1 month
4. **Code Quality** (Priority 4) - Ongoing improvements
5. **Missing Features** (Priority 5) - Add as needed
6. **Multi-Tenancy** (Priority 6) - Major architectural change

## Legend
- 🔴 Critical - Fix immediately
- 🟠 High - Fix within 1-2 weeks
- 🟡 Medium - Fix within 1 month
- 🟢 Low - Fix as time permits