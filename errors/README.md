# Errors Package

The `errors` package provides structured error handling for the Subsonic Proxy application with categorization, context, and consistent error formatting.

## Overview

This package implements a comprehensive error handling system that categorizes errors, provides contextual information, and maintains consistent error formatting throughout the application.

## Core Types

### SubsoxyError

The main error type that wraps all application errors:

```go
type SubsoxyError struct {
    Category string                 // Error category (config, database, etc.)
    Code     string                 // Specific error code
    Message  string                 // Human-readable error message
    Cause    error                  // Underlying error (if any)
    Context  map[string]interface{} // Additional context information
}
```

### Error Categories

Errors are organized into the following categories:

- **`CategoryConfig`** (`"config"`): Configuration validation errors
- **`CategoryDatabase`** (`"database"`): Database connection, query, and transaction errors
- **`CategoryCredentials`** (`"credentials"`): Authentication and credential validation errors
- **`CategoryServer`** (`"server"`): Server startup, shutdown, and proxy errors
- **`CategoryNetwork`** (`"network"`): Upstream server connectivity and timeout errors
- **`CategoryValidation`** (`"validation"`): Input validation and parameter errors
- **`CategoryAuth`** (`"auth"`): Authentication and authorization errors

## Creating Errors

### Using Predefined Errors

The package provides predefined error constants for common scenarios:

```go
// Configuration errors
if port < 1 || port > 65535 {
    return errors.ErrInvalidPort.WithContext("port", port).WithContext("range", "1-65535")
}

// Validation errors
if songID == "" {
    return errors.ErrMissingParameter.WithContext("parameter", "songID")
}

// Database errors
if err := db.Ping(); err != nil {
    return errors.ErrDatabaseConnection.WithContext("path", dbPath)
}
```

### Creating New Errors

For specific error cases not covered by predefined errors:

```go
// Create a new error with category, code, and message
err := errors.New(errors.CategoryNetwork, "TIMEOUT", "request timeout").
    WithContext("timeout", "10s").
    WithContext("url", requestURL)

// Wrap an existing error with additional context
if err := client.Get(url); err != nil {
    return errors.Wrap(err, errors.CategoryNetwork, "REQUEST_FAILED", "failed to make HTTP request").
        WithContext("url", url).
        WithContext("method", "GET")
}
```

## Predefined Errors

### Configuration Errors
- `ErrInvalidPort`: Invalid port number
- `ErrInvalidUpstreamURL`: Invalid upstream URL
- `ErrInvalidLogLevel`: Invalid log level
- `ErrInvalidDatabasePath`: Invalid database path

### Database Errors
- `ErrDatabaseConnection`: Database connection failed
- `ErrDatabaseQuery`: Database query failed
- `ErrDatabaseMigration`: Database migration failed
- `ErrSongNotFound`: Song not found
- `ErrTransactionFailed`: Database transaction failed

### Credentials Errors
- `ErrInvalidCredentials`: Invalid credentials
- `ErrCredentialsValidation`: Credential validation failed
- `ErrNoValidCredentials`: No valid credentials available
- `ErrUpstreamAuth`: Upstream authentication failed

### Server Errors
- `ErrServerStart`: Server failed to start
- `ErrServerShutdown`: Server shutdown failed
- `ErrProxySetup`: Proxy setup failed
- `ErrHookExecution`: Hook execution failed

### Network Errors
- `ErrNetworkTimeout`: Network timeout
- `ErrNetworkUnavailable`: Network unavailable
- `ErrUpstreamError`: Upstream server error

### Validation Errors
- `ErrValidationFailed`: Validation failed
- `ErrInvalidInput`: Invalid input
- `ErrMissingParameter`: Missing required parameter

## Adding Context

Context provides additional information about the error:

```go
err := errors.ErrDatabaseQuery.
    WithContext("query", "SELECT * FROM songs").
    WithContext("table", "songs").
    WithContext("operation", "select")
```

Common context keys:
- **Field names**: `"field"`, `"parameter"`
- **Values**: `"value"`, `"port"`, `"url"`
- **Operations**: `"operation"`, `"query"`, `"method"`
- **Limits**: `"max_allowed"`, `"range"`
- **Identifiers**: `"song_id"`, `"username"`

## Error Introspection

### Checking Error Categories

```go
if errors.IsCategory(err, errors.CategoryDatabase) {
    // Handle database errors specifically
    log.Error("Database operation failed")
}
```

### Getting Error Information

```go
code := errors.GetErrorCode(err)        // Returns error code
context := errors.GetErrorContext(err)  // Returns context map

if code == "CONNECTION_FAILED" {
    // Handle connection failures
}

if dbPath, ok := context["path"].(string); ok {
    log.Printf("Database path: %s", dbPath)
}
```

## Error Formatting

Errors implement the standard `error` interface and format consistently:

```go
// Without underlying cause
// Output: [config:INVALID_PORT] port must be a number

// With underlying cause  
// Output: [database:CONNECTION_FAILED] failed to open database: unable to open database file
```

## Best Practices

### 1. Use Appropriate Categories
Always categorize errors correctly to enable proper error handling and monitoring.

### 2. Provide Meaningful Context
Include relevant information that helps with debugging:

```go
// Good - provides context
return errors.ErrValidationFailed.
    WithContext("field", "size").
    WithContext("value", size).
    WithContext("max_allowed", 10000)

// Bad - no context
return errors.ErrValidationFailed
```

### 3. Wrap External Errors
When wrapping errors from external libraries, preserve the original error:

```go
if err := http.Get(url); err != nil {
    return errors.Wrap(err, errors.CategoryNetwork, "REQUEST_FAILED", "HTTP request failed").
        WithContext("url", url)
}
```

### 4. Use Consistent Error Codes
Error codes should be:
- **UPPERCASE_WITH_UNDERSCORES**
- **Descriptive** of the specific error condition
- **Consistent** across similar error types

### 5. Log with Context
When logging errors, include the structured context:

```go
logger.WithError(err).
    WithField("song_id", songID).
    Error("Failed to record play event")
```

## Testing

The package includes comprehensive tests that cover:
- Error creation and formatting
- Context addition and retrieval
- Error wrapping and unwrapping
- Helper function behavior
- Predefined error constants

Run tests with:
```bash
go test ./errors/...
```

## Integration

This package is used throughout the application:

- **`config/`**: Configuration validation errors
- **`database/`**: Database operation errors
- **`server/`**: Server lifecycle and proxy errors
- **`credentials/`**: Authentication errors
- **`handlers/`**: HTTP request validation errors

All modules follow the same error handling patterns for consistency and maintainability.