# TODO

## Current Priorities

### 1. **Song Removal During Sync** 🟠 High Priority
- **Issue**: Removed tracks from upstream server are not deleted from local database during sync
- **Risk**: Database grows indefinitely with stale entries, "zombie songs" persist locally
- **Current Behavior**: `INSERT OR REPLACE` only adds/updates existing upstream songs
- **Fix**: Implement **Option B: Differential sync** strategy
- **Implementation Plan**:
  1. Fetch current upstream song list during sync (`syncSongsForUser()`)
  2. Query existing local songs for the user from database
  3. Compare lists to identify songs to add, update, and delete
  4. Execute changes in transaction: `INSERT` new, `UPDATE` existing, `DELETE` removed
  5. Preserve user data (play counts, skip counts, listening history) for existing songs
- **Benefits**: Efficient, preserves user data, network friendly, handles partial failures
- **Files**: `server/server.go:544`, `database/database.go:271`
- **Impact**: Prevents database bloat and ensures accurate music library representation

### 2. **Test Coverage Gaps** 🟡 Medium Priority
- **Issue**: Several modules could benefit from additional test coverage
- **Risk**: Untested edge cases and error conditions  
- **Fix**: Add comprehensive tests, especially for error scenarios and edge cases
- **Files**: All `*_test.go` files
- **Areas to focus on**:
  - Error handling edge cases
  - Concurrent access scenarios
  - Input validation boundary conditions
  - Network timeout and failure scenarios

### 3. **Documentation Restructure** 🟡 Medium Priority
- **Issue**: README.md is comprehensive but could be more user-focused
- **Risk**: Poor user experience, difficult to find essential information quickly
- **Fix**: 
  - Consider moving some technical details to dedicated documentation files
  - Create separate documentation files for architecture, development, troubleshooting
  - Maintain focus on user needs in main README
  - Create `docs/` folder structure for advanced topics
- **Files**: `README.md`, create `docs/` folder structure

## Future Enhancements (Low Priority)

### Performance Optimizations 🟢
- **Database Query Optimization**: Review and optimize complex queries
- **Memory Usage Profiling**: Profile memory usage under high load
- **Connection Pool Tuning**: Fine-tune database connection pool parameters
- **Caching Layer**: Consider adding caching for frequently accessed data

## Implementation Notes

- **Current State**: All critical issues have been resolved
- **Code Quality**: High - comprehensive error handling, testing, and documentation
- **Security**: Enterprise-grade with security headers middleware
- **Multi-Tenancy**: Fully implemented with complete user isolation
- **Performance**: Optimized with connection pooling and efficient algorithms

## Completed Major Features ✅

The following major features have been successfully implemented:

- ✅ **Test Suite Fixes**: Fixed compilation errors and mock server issues for comprehensive test coverage
- ✅ **Background Sync Authentication Fix**: Fixed token authentication in song sync for modern Subsonic clients
- ✅ **Multi-User Song Sync**: Complete background sync for all users
- ✅ **Security Headers Middleware**: Production-grade security with dev mode
- ✅ **Error Handling**: Go 1.13+ compatible structured error handling
- ✅ **Database Connection Pooling**: Advanced pool management with health monitoring
- ✅ **Rate Limiting**: DoS protection with token bucket algorithm
- ✅ **CORS Support**: Comprehensive cross-origin resource sharing
- ✅ **Input Validation**: Complete sanitization and validation
- ✅ **Credential Security**: AES-256-GCM encrypted credential storage
- ✅ **Weighted Shuffle**: Intelligent personalized song recommendations
- ✅ **Multi-Tenancy**: Complete user data isolation

## Legend
- 🔴 Critical - Fix immediately
- 🟠 High - Fix within 1-2 weeks  
- 🟡 Medium - Fix within 1 month
- 🟢 Low - Fix as time permits
