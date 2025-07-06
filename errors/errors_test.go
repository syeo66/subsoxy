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
			name: "Error without cause",
			err:  New(CategoryConfig, "TEST_CODE", "test message"),
			expected: "[config:TEST_CODE] test message",
		},
		{
			name: "Error with cause",
			err:  Wrap(fmt.Errorf("original error"), CategoryDatabase, "TEST_CODE", "test message"),
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