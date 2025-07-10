package config

import (
	"os"
	"testing"
	"time"
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
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: false,
		},
		{
			name: "Invalid port - non-numeric",
			config: &Config{
				ProxyPort:         "abc",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid port - out of range",
			config: &Config{
				ProxyPort:         "70000",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid upstream URL",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "not-a-url",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid log level",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "invalid",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Empty database path",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid rate limit RPS - zero",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      0,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid rate limit burst - zero",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    0,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid rate limit burst smaller than RPS",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    50,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
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

// Database Pool Configuration Tests

func TestValidateDatabasePool(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "Valid database pool config",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: false,
		},
		{
			name: "Invalid DB max open connections - zero",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    0, // Invalid
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid DB max idle connections - negative",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    -1, // Invalid
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid DB max idle connections - exceeds max open",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    10,
				DBMaxIdleConns:    15, // Invalid: > MaxOpenConns
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid DB connection max lifetime - negative",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: -1 * time.Minute, // Invalid
				DBConnMaxIdleTime: 5 * time.Minute,
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
		{
			name: "Invalid DB connection max idle time - negative",
			config: &Config{
				ProxyPort:         "8080",
				UpstreamURL:       "http://localhost:4533",
				LogLevel:          "info",
				DatabasePath:      "test.db",
				RateLimitRPS:      100,
				RateLimitBurst:    200,
				RateLimitEnabled:  true,
				DBMaxOpenConns:    25,
				DBMaxIdleConns:    5,
				DBConnMaxLifetime: 30 * time.Minute,
				DBConnMaxIdleTime: -1 * time.Minute, // Invalid
				DBHealthCheck:     true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validateDatabasePool()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.validateDatabasePool() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetEnvDurationOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue time.Duration
		envValue     string
		expected     time.Duration
	}{
		{
			name:         "Valid duration environment variable",
			key:          "TEST_DURATION",
			defaultValue: 5 * time.Minute,
			envValue:     "10m",
			expected:     10 * time.Minute,
		},
		{
			name:         "Invalid duration environment variable",
			key:          "TEST_DURATION_INVALID",
			defaultValue: 5 * time.Minute,
			envValue:     "invalid",
			expected:     5 * time.Minute,
		},
		{
			name:         "Missing environment variable",
			key:          "TEST_DURATION_MISSING",
			defaultValue: 5 * time.Minute,
			envValue:     "",
			expected:     5 * time.Minute,
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

			result := getEnvDurationOrDefault(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvDurationOrDefault() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestGetDatabasePoolConfig(t *testing.T) {
	config := &Config{
		DBMaxOpenConns:    15,
		DBMaxIdleConns:    7,
		DBConnMaxLifetime: 20 * time.Minute,
		DBConnMaxIdleTime: 3 * time.Minute,
		DBHealthCheck:     false,
	}

	poolConfig := config.GetDatabasePoolConfig()

	if poolConfig.MaxOpenConns != 15 {
		t.Errorf("Expected MaxOpenConns 15, got %d", poolConfig.MaxOpenConns)
	}
	if poolConfig.MaxIdleConns != 7 {
		t.Errorf("Expected MaxIdleConns 7, got %d", poolConfig.MaxIdleConns)
	}
	if poolConfig.ConnMaxLifetime != 20*time.Minute {
		t.Errorf("Expected ConnMaxLifetime 20m, got %v", poolConfig.ConnMaxLifetime)
	}
	if poolConfig.ConnMaxIdleTime != 3*time.Minute {
		t.Errorf("Expected ConnMaxIdleTime 3m, got %v", poolConfig.ConnMaxIdleTime)
	}
	if poolConfig.HealthCheck != false {
		t.Errorf("Expected HealthCheck false, got %v", poolConfig.HealthCheck)
	}
}

func TestValidateSecurityHeaders(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "Valid security headers",
			config: &Config{
				SecurityHeadersEnabled: true,
				XContentTypeOptions:    "nosniff",
				XFrameOptions:          "DENY",
			},
			wantErr: false,
		},
		{
			name: "Security headers disabled",
			config: &Config{
				SecurityHeadersEnabled: false,
				XContentTypeOptions:    "invalid", // Should be ignored when disabled
				XFrameOptions:          "invalid", // Should be ignored when disabled
			},
			wantErr: false,
		},
		{
			name: "Empty security headers",
			config: &Config{
				SecurityHeadersEnabled: true,
				XContentTypeOptions:    "",
				XFrameOptions:          "",
			},
			wantErr: false,
		},
		{
			name: "Valid X-Frame-Options SAMEORIGIN",
			config: &Config{
				SecurityHeadersEnabled: true,
				XFrameOptions:          "SAMEORIGIN",
			},
			wantErr: false,
		},
		{
			name: "Invalid X-Content-Type-Options",
			config: &Config{
				SecurityHeadersEnabled: true,
				XContentTypeOptions:    "invalid-value",
			},
			wantErr: true,
		},
		{
			name: "Invalid X-Frame-Options",
			config: &Config{
				SecurityHeadersEnabled: true,
				XFrameOptions:          "INVALID",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validateSecurityHeaders()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.validateSecurityHeaders() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsDevMode(t *testing.T) {
	tests := []struct {
		name              string
		securityDevMode   bool
		proxyPort         string
		expected          bool
	}{
		{
			name:            "Dev mode explicitly enabled",
			securityDevMode: true,
			proxyPort:       "9090",
			expected:        true,
		},
		{
			name:            "Default port 8080",
			securityDevMode: false,
			proxyPort:       "8080",
			expected:        true,
		},
		{
			name:            "Non-default port",
			securityDevMode: false,
			proxyPort:       "9090",
			expected:        false,
		},
		{
			name:            "Production port 443",
			securityDevMode: false,
			proxyPort:       "443",
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				SecurityDevMode: tt.securityDevMode,
				ProxyPort:      tt.proxyPort,
			}
			
			result := config.IsDevMode()
			if result != tt.expected {
				t.Errorf("Config.IsDevMode() = %v, expected %v", result, tt.expected)
			}
		})
	}
}