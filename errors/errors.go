package errors

import (
	"fmt"
)

// Error categories for structured error handling
const (
	CategoryConfig      = "config"
	CategoryDatabase    = "database"
	CategoryCredentials = "credentials"
	CategoryServer      = "server"
	CategoryValidation  = "validation"
	CategoryNetwork     = "network"
	CategoryAuth        = "auth"
)

// SubsoxyError represents a structured error with category and context
type SubsoxyError struct {
	Category string
	Code     string
	Message  string
	Cause    error
	Context  map[string]interface{}
}

func (e *SubsoxyError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Category, e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Category, e.Code, e.Message)
}

func (e *SubsoxyError) Unwrap() error {
	return e.Cause
}

func (e *SubsoxyError) WithContext(key string, value interface{}) *SubsoxyError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// New creates a new SubsoxyError
func New(category, code, message string) *SubsoxyError {
	return &SubsoxyError{
		Category: category,
		Code:     code,
		Message:  message,
		Context:  make(map[string]interface{}),
	}
}

// Wrap wraps an existing error with SubsoxyError
func Wrap(err error, category, code, message string) *SubsoxyError {
	return &SubsoxyError{
		Category: category,
		Code:     code,
		Message:  message,
		Cause:    err,
		Context:  make(map[string]interface{}),
	}
}

// Config errors
var (
	ErrInvalidPort        = New(CategoryConfig, "INVALID_PORT", "invalid port number")
	ErrInvalidUpstreamURL = New(CategoryConfig, "INVALID_UPSTREAM_URL", "invalid upstream URL")
	ErrInvalidLogLevel    = New(CategoryConfig, "INVALID_LOG_LEVEL", "invalid log level")
	ErrInvalidDatabasePath = New(CategoryConfig, "INVALID_DATABASE_PATH", "invalid database path")
)

// Database errors
var (
	ErrDatabaseConnection = New(CategoryDatabase, "CONNECTION_FAILED", "database connection failed")
	ErrDatabaseQuery      = New(CategoryDatabase, "QUERY_FAILED", "database query failed")
	ErrDatabaseMigration  = New(CategoryDatabase, "MIGRATION_FAILED", "database migration failed")
	ErrSongNotFound       = New(CategoryDatabase, "SONG_NOT_FOUND", "song not found")
	ErrTransactionFailed  = New(CategoryDatabase, "TRANSACTION_FAILED", "database transaction failed")
)

// Credentials errors
var (
	ErrInvalidCredentials = New(CategoryCredentials, "INVALID_CREDENTIALS", "invalid credentials")
	ErrCredentialsValidation = New(CategoryCredentials, "VALIDATION_FAILED", "credential validation failed")
	ErrNoValidCredentials = New(CategoryCredentials, "NO_VALID_CREDENTIALS", "no valid credentials available")
	ErrUpstreamAuth       = New(CategoryCredentials, "UPSTREAM_AUTH_FAILED", "upstream authentication failed")
)

// Server errors
var (
	ErrServerStart    = New(CategoryServer, "START_FAILED", "server failed to start")
	ErrServerShutdown = New(CategoryServer, "SHUTDOWN_FAILED", "server shutdown failed")
	ErrProxySetup     = New(CategoryServer, "PROXY_SETUP_FAILED", "proxy setup failed")
	ErrHookExecution  = New(CategoryServer, "HOOK_EXECUTION_FAILED", "hook execution failed")
)

// Network errors
var (
	ErrNetworkTimeout     = New(CategoryNetwork, "TIMEOUT", "network timeout")
	ErrNetworkUnavailable = New(CategoryNetwork, "UNAVAILABLE", "network unavailable")
	ErrUpstreamError      = New(CategoryNetwork, "UPSTREAM_ERROR", "upstream server error")
)

// Validation errors
var (
	ErrValidationFailed = New(CategoryValidation, "VALIDATION_FAILED", "validation failed")
	ErrInvalidInput     = New(CategoryValidation, "INVALID_INPUT", "invalid input")
	ErrMissingParameter = New(CategoryValidation, "MISSING_PARAMETER", "missing required parameter")
)

// Helper functions for common error patterns
func IsCategory(err error, category string) bool {
	var subsoxyErr *SubsoxyError
	if !As(err, &subsoxyErr) {
		return false
	}
	return subsoxyErr.Category == category
}

func GetErrorCode(err error) string {
	var subsoxyErr *SubsoxyError
	if !As(err, &subsoxyErr) {
		return ""
	}
	return subsoxyErr.Code
}

func GetErrorContext(err error) map[string]interface{} {
	var subsoxyErr *SubsoxyError
	if !As(err, &subsoxyErr) {
		return nil
	}
	return subsoxyErr.Context
}

// As is a wrapper around errors.As
func As(err error, target interface{}) bool {
	if err == nil {
		return false
	}
	// Simple type assertion for our use case
	if subsoxyErr, ok := err.(*SubsoxyError); ok {
		if targetPtr, ok := target.(**SubsoxyError); ok {
			*targetPtr = subsoxyErr
			return true
		}
	}
	return false
}