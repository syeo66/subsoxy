package config

import (
	"flag"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/syeo66/subsoxy/errors"
)

type Config struct {
	ProxyPort    string
	UpstreamURL  string
	LogLevel     string
	DatabasePath string
}

func New() (*Config, error) {
	var (
		port     = flag.String("port", getEnvOrDefault("PORT", "8080"), "Proxy server port")
		upstream = flag.String("upstream", getEnvOrDefault("UPSTREAM_URL", "http://localhost:4533"), "Upstream Subsonic server URL")
		logLevel = flag.String("log-level", getEnvOrDefault("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
		dbPath   = flag.String("db-path", getEnvOrDefault("DB_PATH", "subsoxy.db"), "Database file path")
	)
	flag.Parse()

	config := &Config{
		ProxyPort:    *port,
		UpstreamURL:  *upstream,
		LogLevel:     *logLevel,
		DatabasePath: *dbPath,
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// Validate validates the configuration and returns structured errors
func (c *Config) Validate() error {
	if err := c.validatePort(); err != nil {
		return err
	}
	
	if err := c.validateUpstreamURL(); err != nil {
		return err
	}
	
	if err := c.validateLogLevel(); err != nil {
		return err
	}
	
	if err := c.validateDatabasePath(); err != nil {
		return err
	}
	
	return nil
}

func (c *Config) validatePort() error {
	if c.ProxyPort == "" {
		return errors.ErrInvalidPort.WithContext("port", c.ProxyPort)
	}
	
	port, err := strconv.Atoi(c.ProxyPort)
	if err != nil {
		return errors.Wrap(err, errors.CategoryConfig, "INVALID_PORT", "port must be a number").
			WithContext("port", c.ProxyPort)
	}
	
	if port < 1 || port > 65535 {
		return errors.ErrInvalidPort.WithContext("port", c.ProxyPort).
			WithContext("range", "1-65535")
	}
	
	return nil
}

func (c *Config) validateUpstreamURL() error {
	if c.UpstreamURL == "" {
		return errors.ErrInvalidUpstreamURL.WithContext("url", c.UpstreamURL)
	}
	
	parsedURL, err := url.Parse(c.UpstreamURL)
	if err != nil {
		return errors.Wrap(err, errors.CategoryConfig, "INVALID_UPSTREAM_URL", "invalid upstream URL format").
			WithContext("url", c.UpstreamURL)
	}
	
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return errors.ErrInvalidUpstreamURL.WithContext("url", c.UpstreamURL).
			WithContext("scheme", parsedURL.Scheme).
			WithContext("allowed_schemes", []string{"http", "https"})
	}
	
	if parsedURL.Host == "" {
		return errors.ErrInvalidUpstreamURL.WithContext("url", c.UpstreamURL).
			WithContext("reason", "missing host")
	}
	
	return nil
}

func (c *Config) validateLogLevel() error {
	if c.LogLevel == "" {
		return errors.ErrInvalidLogLevel.WithContext("level", c.LogLevel)
	}
	
	validLevels := []string{"debug", "info", "warn", "error"}
	level := strings.ToLower(c.LogLevel)
	
	for _, validLevel := range validLevels {
		if level == validLevel {
			return nil
		}
	}
	
	return errors.ErrInvalidLogLevel.WithContext("level", c.LogLevel).
		WithContext("valid_levels", validLevels)
}

func (c *Config) validateDatabasePath() error {
	if c.DatabasePath == "" {
		return errors.ErrInvalidDatabasePath.WithContext("path", c.DatabasePath)
	}
	
	// Check if the directory exists (create if it doesn't exist)
	if strings.Contains(c.DatabasePath, "/") {
		dir := c.DatabasePath[:strings.LastIndex(c.DatabasePath, "/")]
		if dir != "" {
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return errors.Wrap(err, errors.CategoryConfig, "INVALID_DATABASE_PATH", "cannot create database directory").
						WithContext("path", c.DatabasePath).
						WithContext("directory", dir)
				}
			}
		}
	}
	
	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}