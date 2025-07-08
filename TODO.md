# TODO

## Priority 3: Verification

### 1. Verify User Isolation on Song Sync 🟠

### 2. Make Sure Song Sync is Started When New Credentials are Verified  🟠

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

1. **Code Quality** (Priority 4) - Ongoing improvements
2. **Missing Features** (Priority 5) - Add as needed

## Legend
- 🔴 Critical - Fix immediately
- 🟠 High - Fix within 1-2 weeks
- 🟡 Medium - Fix within 1 month
- 🟢 Low - Fix as time permits
