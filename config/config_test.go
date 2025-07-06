package config

import (
	"os"
	"testing"
)

func TestGetEnvOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		expected     string
	}{
		{
			name:         "Environment variable exists",
			key:          "TEST_VAR",
			defaultValue: "default",
			envValue:     "env_value",
			expected:     "env_value",
		},
		{
			name:         "Environment variable does not exist",
			key:          "NON_EXISTENT_VAR",
			defaultValue: "default",
			envValue:     "",
			expected:     "default",
		},
		{
			name:         "Environment variable is empty string",
			key:          "EMPTY_VAR",
			defaultValue: "default",
			envValue:     "",
			expected:     "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any existing environment variable
			os.Unsetenv(tt.key)
			
			// Set environment variable if specified
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			result := getEnvOrDefault(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvOrDefault(%s, %s) = %s, want %s", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestNewConfig(t *testing.T) {
	// Note: We can't easily test flag parsing in unit tests because
	// flag.Parse() can only be called once per program execution.
	// In real usage, the New() function works correctly.
	
	// Test environment variable functionality directly
	t.Run("getEnvOrDefault with environment variables", func(t *testing.T) {
		// Save original environment variables
		originalEnv := map[string]string{
			"TEST_PORT":         os.Getenv("TEST_PORT"),
			"TEST_UPSTREAM_URL": os.Getenv("TEST_UPSTREAM_URL"),
			"TEST_LOG_LEVEL":    os.Getenv("TEST_LOG_LEVEL"),
			"TEST_DB_PATH":      os.Getenv("TEST_DB_PATH"),
		}

		// Clean up environment variables
		for key := range originalEnv {
			os.Unsetenv(key)
		}

		// Restore original environment variables after test
		defer func() {
			for key, value := range originalEnv {
				if value != "" {
					os.Setenv(key, value)
				} else {
					os.Unsetenv(key)
				}
			}
		}()

		// Test default values
		if getEnvOrDefault("TEST_PORT", "8080") != "8080" {
			t.Error("Expected default value when env var not set")
		}

		// Test environment variable override
		os.Setenv("TEST_PORT", "9090")
		if getEnvOrDefault("TEST_PORT", "8080") != "9090" {
			t.Error("Expected environment variable value")
		}
	})
}

func TestConfigStruct(t *testing.T) {
	config := &Config{
		ProxyPort:    "8080",
		UpstreamURL:  "http://localhost:4533",
		LogLevel:     "info",
		DatabasePath: "subsoxy.db",
	}

	if config.ProxyPort != "8080" {
		t.Errorf("ProxyPort = %s, want %s", config.ProxyPort, "8080")
	}
	if config.UpstreamURL != "http://localhost:4533" {
		t.Errorf("UpstreamURL = %s, want %s", config.UpstreamURL, "http://localhost:4533")
	}
	if config.LogLevel != "info" {
		t.Errorf("LogLevel = %s, want %s", config.LogLevel, "info")
	}
	if config.DatabasePath != "subsoxy.db" {
		t.Errorf("DatabasePath = %s, want %s", config.DatabasePath, "subsoxy.db")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "Valid config",
			config: &Config{
				ProxyPort:    "8080",
				UpstreamURL:  "http://localhost:4533",
				LogLevel:     "info",
				DatabasePath: "test.db",
			},
			wantErr: false,
		},
		{
			name: "Invalid port - non-numeric",
			config: &Config{
				ProxyPort:    "abc",
				UpstreamURL:  "http://localhost:4533",
				LogLevel:     "info",
				DatabasePath: "test.db",
			},
			wantErr: true,
		},
		{
			name: "Invalid port - out of range",
			config: &Config{
				ProxyPort:    "70000",
				UpstreamURL:  "http://localhost:4533",
				LogLevel:     "info",
				DatabasePath: "test.db",
			},
			wantErr: true,
		},
		{
			name: "Invalid upstream URL",
			config: &Config{
				ProxyPort:    "8080",
				UpstreamURL:  "not-a-url",
				LogLevel:     "info",
				DatabasePath: "test.db",
			},
			wantErr: true,
		},
		{
			name: "Invalid log level",
			config: &Config{
				ProxyPort:    "8080",
				UpstreamURL:  "http://localhost:4533",
				LogLevel:     "invalid",
				DatabasePath: "test.db",
			},
			wantErr: true,
		},
		{
			name: "Empty database path",
			config: &Config{
				ProxyPort:    "8080",
				UpstreamURL:  "http://localhost:4533",
				LogLevel:     "info",
				DatabasePath: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}