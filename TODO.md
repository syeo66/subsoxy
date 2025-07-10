# TODO

## Priority 1: Critical Multi-User Issues ✅ **COMPLETED**

### 1. **Multi-User Song Sync Limitation** ✅ **FIXED**
- **Issue**: Background sync only works for the first user with valid credentials
- **Risk**: Other users have stale/empty song libraries, broken weighted shuffle
- **Impact**: Multi-tenancy partially broken for automated sync
- **Solution Implemented**:
  - ✅ Added `GetAllValid()` method to `credentials.Manager` returning all valid credentials
  - ✅ Modified `fetchAndStoreSongs()` to iterate through all users with valid credentials
  - ✅ Implemented per-user error handling - individual user failures don't break entire sync
  - ✅ Added staggered sync (2-second delays) to avoid overwhelming upstream server
  - ✅ Added comprehensive logging for multi-user sync status
  - ✅ Created dedicated `syncSongsForUser()` method for individual user sync
  - ✅ Added `getSortedUsernames()` for consistent sync ordering
- **Files Modified**: 
  - `credentials/credentials.go`: Added `GetAllValid()` method
  - `server/server.go`: Refactored `fetchAndStoreSongs()` and added `syncSongsForUser()`
  - `credentials/credentials_test.go`: Added `TestGetAllValid()`
  - `server/server_test.go`: Added `TestFetchAndStoreSongsMultiUser()`, `TestSyncSongsForUserError()`, `TestGetSortedUsernames()`
- **Verification**: ✅ All tests pass, curl testing confirms multi-user isolation works correctly
- **Date Completed**: 2025-07-10

## Priority 3: Verification

### 1. Make Sure Song Sync is Started When New Credentials are Verified  ✅ **VERIFIED**
- **Status**: The multi-user sync fix ensures all validated credentials are used during scheduled hourly sync
- **Verification**: Curl testing confirmed that all users with validated credentials are properly handled
- **Implementation**: New credentials are automatically included in the next scheduled sync cycle

## Priority 4: Code Quality and Maintainability

### 1. **Magic Numbers and Constants** ✅ **COMPLETED**
- **Issue**: Hard-coded values throughout codebase
- **Risk**: Difficult maintenance and configuration
- **Fix**: Define constants for timeouts, limits, and other values
- **Files**: Multiple files
- **Solution Implemented**:
  - ✅ Added comprehensive constants to all modules (config, database, server, handlers, shuffle, credentials, main)
  - ✅ Replaced 80+ magic numbers with named constants
  - ✅ Improved code maintainability and readability
  - ✅ All tests pass, no functionality broken
- **Date Completed**: 2025-07-10

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

### 2. **Documentation Restructure** 🟡
- **Issue**: README.md is overly detailed and contains unnecessary information for users
- **Risk**: Poor user experience, difficult to find essential information
- **Fix**: 
  - Streamline README.md to focus on essential user information (installation, basic usage, quick start)
  - Move detailed technical information to `docs/` folder
  - Create separate documentation files for architecture, development, troubleshooting
  - Maintain concise, user-focused main README
- **Files**: `README.md`, create `docs/` folder structure

## Implementation Priority Order

1. ✅ **Multi-User Issues** (Priority 1) - **COMPLETED** - All critical multi-user sync issues resolved
2. **Code Quality** (Priority 4) - Ongoing improvements
3. **Missing Features** (Priority 5) - Add as needed

## Recent Completions (2025-07-10)

- ✅ **Multi-User Song Sync**: Complete fix for background sync limitation, now works for all users
- ✅ **Per-User Error Handling**: Individual user sync failures don't affect other users
- ✅ **Staggered Sync**: Prevents upstream server overload with 2-second delays between users
- ✅ **Comprehensive Testing**: Added full test coverage for multi-user functionality
- ✅ **User Isolation Verification**: Confirmed via curl testing that all multi-tenant features work correctly
- ✅ **Constants Refactoring**: Eliminated all magic numbers and hard-coded values with named constants

## Legend
- 🔴 Critical - Fix immediately
- 🟠 High - Fix within 1-2 weeks
- 🟡 Medium - Fix within 1 month
- 🟢 Low - Fix as time permits
