package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestSubsoxyError(t *testing.T) {
	tests := []struct {
		name     string
		err      *SubsoxyError
		expected string
	}{
		{
			name:     "Error without cause",
			err:      New(CategoryConfig, "TEST_CODE", "test message"),
			expected: "[config:TEST_CODE] test message",
		},
		{
			name:     "Error with cause",
			err:      Wrap(fmt.Errorf("original error"), CategoryDatabase, "TEST_CODE", "test message"),
			expected: "[database:TEST_CODE] test message: original error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("SubsoxyError.Error() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSubsoxyErrorWithContext(t *testing.T) {
	err := New(CategoryConfig, "TEST_CODE", "test message")
	err.WithContext("key1", "value1")
	err.WithContext("key2", 42)

	if len(err.Context) != 2 {
		t.Errorf("Expected 2 context items, got %d", len(err.Context))
	}

	if err.Context["key1"] != "value1" {
		t.Errorf("Expected context key1 to be 'value1', got %v", err.Context["key1"])
	}

	if err.Context["key2"] != 42 {
		t.Errorf("Expected context key2 to be 42, got %v", err.Context["key2"])
	}
}

func TestSubsoxyErrorUnwrap(t *testing.T) {
	originalErr := errors.New("original error")
	wrappedErr := Wrap(originalErr, CategoryDatabase, "TEST_CODE", "test message")

	if unwrapped := wrappedErr.Unwrap(); unwrapped != originalErr {
		t.Errorf("Expected unwrapped error to be original error, got %v", unwrapped)
	}
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      *SubsoxyError
		category string
		code     string
	}{
		{
			name:     "ErrInvalidPort",
			err:      ErrInvalidPort,
			category: CategoryConfig,
			code:     "INVALID_PORT",
		},
		{
			name:     "ErrDatabaseConnection",
			err:      ErrDatabaseConnection,
			category: CategoryDatabase,
			code:     "CONNECTION_FAILED",
		},
		{
			name:     "ErrInvalidCredentials",
			err:      ErrInvalidCredentials,
			category: CategoryCredentials,
			code:     "INVALID_CREDENTIALS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Category != tt.category {
				t.Errorf("Expected category %s, got %s", tt.category, tt.err.Category)
			}
			if tt.err.Code != tt.code {
				t.Errorf("Expected code %s, got %s", tt.code, tt.err.Code)
			}
		})
	}
}

func TestNewAndWrap(t *testing.T) {
	// Test New
	newErr := New(CategoryConfig, "TEST_CODE", "test message")
	if newErr.Category != CategoryConfig {
		t.Errorf("Expected category %s, got %s", CategoryConfig, newErr.Category)
	}
	if newErr.Code != "TEST_CODE" {
		t.Errorf("Expected code TEST_CODE, got %s", newErr.Code)
	}
	if newErr.Message != "test message" {
		t.Errorf("Expected message 'test message', got %s", newErr.Message)
	}
	if newErr.Cause != nil {
		t.Errorf("Expected nil cause, got %v", newErr.Cause)
	}

	// Test Wrap
	originalErr := errors.New("original")
	wrappedErr := Wrap(originalErr, CategoryDatabase, "WRAP_CODE", "wrapped message")
	if wrappedErr.Category != CategoryDatabase {
		t.Errorf("Expected category %s, got %s", CategoryDatabase, wrappedErr.Category)
	}
	if wrappedErr.Code != "WRAP_CODE" {
		t.Errorf("Expected code WRAP_CODE, got %s", wrappedErr.Code)
	}
	if wrappedErr.Message != "wrapped message" {
		t.Errorf("Expected message 'wrapped message', got %s", wrappedErr.Message)
	}
	if wrappedErr.Cause != originalErr {
		t.Errorf("Expected cause to be original error, got %v", wrappedErr.Cause)
	}
}

// Test Go 1.13+ error handling features
func TestErrorIs(t *testing.T) {
	baseErr := New(CategoryConfig, "TEST_CODE", "test message")
	sameErr := New(CategoryConfig, "TEST_CODE", "different message")
	differentErr := New(CategoryConfig, "DIFFERENT_CODE", "test message")

	// Test Is() method
	if !Is(baseErr, sameErr) {
		t.Error("Expected Is() to return true for same category and code")
	}

	if Is(baseErr, differentErr) {
		t.Error("Expected Is() to return false for different code")
	}

	// Test with wrapped errors
	originalErr := errors.New("original")
	wrappedErr := Wrap(originalErr, CategoryDatabase, "WRAP_CODE", "wrapped")

	if !Is(wrappedErr, originalErr) {
		t.Error("Expected Is() to find original error in wrapped error")
	}
}

func TestErrorAs(t *testing.T) {
	baseErr := New(CategoryConfig, "TEST_CODE", "test message")
	baseErr.WithContext("key", "value")

	// Test As() method
	var subsoxyErr *SubsoxyError
	if !As(baseErr, &subsoxyErr) {
		t.Error("Expected As() to return true for SubsoxyError")
	}

	if subsoxyErr.Code != "TEST_CODE" {
		t.Errorf("Expected code TEST_CODE, got %s", subsoxyErr.Code)
	}

	if subsoxyErr.Context["key"] != "value" {
		t.Errorf("Expected context value, got %v", subsoxyErr.Context["key"])
	}

	// Test with wrapped errors
	originalErr := fmt.Errorf("original error")
	wrappedErr := Wrap(originalErr, CategoryDatabase, "WRAP_CODE", "wrapped")

	if !As(wrappedErr, &subsoxyErr) {
		t.Error("Expected As() to return true for wrapped SubsoxyError")
	}

	if subsoxyErr.Code != "WRAP_CODE" {
		t.Errorf("Expected code WRAP_CODE, got %s", subsoxyErr.Code)
	}
}

func TestHelperFunctions(t *testing.T) {
	baseErr := New(CategoryConfig, "TEST_CODE", "test message")
	baseErr.WithContext("key", "value")

	// Test IsCategory
	if !IsCategory(baseErr, CategoryConfig) {
		t.Error("Expected IsCategory to return true")
	}

	if IsCategory(baseErr, CategoryDatabase) {
		t.Error("Expected IsCategory to return false")
	}

	// Test GetErrorCode
	if GetErrorCode(baseErr) != "TEST_CODE" {
		t.Errorf("Expected code TEST_CODE, got %s", GetErrorCode(baseErr))
	}

	// Test GetErrorContext
	ctx := GetErrorContext(baseErr)
	if ctx == nil {
		t.Error("Expected context to be non-nil")
	}

	if ctx["key"] != "value" {
		t.Errorf("Expected context value, got %v", ctx["key"])
	}

	// Test IsCode
	if !IsCode(baseErr, "TEST_CODE") {
		t.Error("Expected IsCode to return true")
	}

	if IsCode(baseErr, "DIFFERENT_CODE") {
		t.Error("Expected IsCode to return false")
	}
}

func TestErrorChains(t *testing.T) {
	// Create nested error chain
	rootErr := errors.New("root cause")
	middleErr := Wrap(rootErr, CategoryDatabase, "MIDDLE_CODE", "middle error")
	topErr := Wrap(middleErr, CategoryServer, "TOP_CODE", "top error")

	// Test HasCategory
	if !HasCategory(topErr, CategoryDatabase) {
		t.Error("Expected HasCategory to find database category in chain")
	}

	if !HasCategory(topErr, CategoryServer) {
		t.Error("Expected HasCategory to find server category in chain")
	}

	if HasCategory(topErr, CategoryConfig) {
		t.Error("Expected HasCategory to not find config category in chain")
	}

	// Test GetRootCause
	if GetRootCause(topErr) != rootErr {
		t.Error("Expected GetRootCause to return root error")
	}

	// Test with non-wrapped error
	singleErr := New(CategoryConfig, "SINGLE_CODE", "single error")
	if GetRootCause(singleErr) != singleErr {
		t.Error("Expected GetRootCause to return same error for non-wrapped error")
	}
}

func TestErrorChainsWithStandardErrors(t *testing.T) {
	// Test with standard Go errors mixed in
	stdErr := fmt.Errorf("standard error")
	wrappedStdErr := fmt.Errorf("wrapped: %w", stdErr)
	subsoxyErr := Wrap(wrappedStdErr, CategoryNetwork, "NETWORK_CODE", "network error")

	// Test that we can still find the original standard error
	if !Is(subsoxyErr, stdErr) {
		t.Error("Expected Is() to find standard error in chain")
	}

	// Test As() with different SubsoxyError
	var targetErr *SubsoxyError
	if !As(subsoxyErr, &targetErr) {
		t.Error("Expected As() to return true for SubsoxyError")
	}

	if targetErr.Code != "NETWORK_CODE" {
		t.Errorf("Expected code NETWORK_CODE, got %s", targetErr.Code)
	}
}
