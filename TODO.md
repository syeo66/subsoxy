# TODO

## Priority 1: Critical Multi-User Issues

### 1. **Multi-User Song Sync Limitation** 🔴
- **Issue**: Background sync only works for the first user with valid credentials
- **Risk**: Other users have stale/empty song libraries, broken weighted shuffle
- **Impact**: Multi-tenancy partially broken for automated sync
- **Fix**: 
  - Add `GetAllValid()` method to `credentials.Manager`
  - Modify `fetchAndStoreSongs()` to iterate through all users
  - Implement per-user error handling in sync process
  - Add staggered sync to avoid overwhelming upstream server
- **Files**: `server/server.go:383-387`, `credentials/credentials.go:139-153`
- **Details**: `credentials.GetValid()` only returns first valid credential, causing background sync to sync only one user's songs

## Priority 3: Verification

### 1. Make Sure Song Sync is Started When New Credentials are Verified  🟠

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

1. **Multi-User Issues** (Priority 1) - Fix immediately - CRITICAL
2. **Code Quality** (Priority 4) - Ongoing improvements
3. **Missing Features** (Priority 5) - Add as needed

## Legend
- 🔴 Critical - Fix immediately
- 🟠 High - Fix within 1-2 weeks
- 🟡 Medium - Fix within 1 month
- 🟢 Low - Fix as time permits
