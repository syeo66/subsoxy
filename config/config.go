package config

import (
	"flag"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/syeo66/subsoxy/errors"
)

type Config struct {
	ProxyPort         string
	UpstreamURL       string
	LogLevel          string
	DatabasePath      string
	RateLimitRPS      int
	RateLimitBurst    int
	RateLimitEnabled  bool
	// Database connection pool settings
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
	DBConnMaxIdleTime time.Duration
	DBHealthCheck     bool
}

func New() (*Config, error) {
	var (
		port             = flag.String("port", getEnvOrDefault("PORT", "8080"), "Proxy server port")
		upstream         = flag.String("upstream", getEnvOrDefault("UPSTREAM_URL", "http://localhost:4533"), "Upstream Subsonic server URL")
		logLevel         = flag.String("log-level", getEnvOrDefault("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
		dbPath           = flag.String("db-path", getEnvOrDefault("DB_PATH", "subsoxy.db"), "Database file path")
		rateLimitRPS     = flag.Int("rate-limit-rps", getEnvIntOrDefault("RATE_LIMIT_RPS", 100), "Rate limit requests per second")
		rateLimitBurst   = flag.Int("rate-limit-burst", getEnvIntOrDefault("RATE_LIMIT_BURST", 200), "Rate limit burst size")
		rateLimitEnabled = flag.Bool("rate-limit-enabled", getEnvBoolOrDefault("RATE_LIMIT_ENABLED", true), "Enable rate limiting")
		// Database connection pool flags
		dbMaxOpenConns    = flag.Int("db-max-open-conns", getEnvIntOrDefault("DB_MAX_OPEN_CONNS", 25), "Maximum number of open database connections")
		dbMaxIdleConns    = flag.Int("db-max-idle-conns", getEnvIntOrDefault("DB_MAX_IDLE_CONNS", 5), "Maximum number of idle database connections")
		dbConnMaxLifetime = flag.Duration("db-conn-max-lifetime", getEnvDurationOrDefault("DB_CONN_MAX_LIFETIME", 30*time.Minute), "Maximum connection lifetime")
		dbConnMaxIdleTime = flag.Duration("db-conn-max-idle-time", getEnvDurationOrDefault("DB_CONN_MAX_IDLE_TIME", 5*time.Minute), "Maximum connection idle time")
		dbHealthCheck     = flag.Bool("db-health-check", getEnvBoolOrDefault("DB_HEALTH_CHECK", true), "Enable database health checks")
	)
	flag.Parse()

	config := &Config{
		ProxyPort:         *port,
		UpstreamURL:       *upstream,
		LogLevel:          *logLevel,
		DatabasePath:      *dbPath,
		RateLimitRPS:      *rateLimitRPS,
		RateLimitBurst:    *rateLimitBurst,
		RateLimitEnabled:  *rateLimitEnabled,
		DBMaxOpenConns:    *dbMaxOpenConns,
		DBMaxIdleConns:    *dbMaxIdleConns,
		DBConnMaxLifetime: *dbConnMaxLifetime,
		DBConnMaxIdleTime: *dbConnMaxIdleTime,
		DBHealthCheck:     *dbHealthCheck,
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
	
	if err := c.validateRateLimit(); err != nil {
		return err
	}
	
	if err := c.validateDatabasePool(); err != nil {
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

func (c *Config) validateRateLimit() error {
	if c.RateLimitRPS < 1 {
		return errors.New(errors.CategoryConfig, "INVALID_RATE_LIMIT_RPS", "rate limit RPS must be at least 1").
			WithContext("rps", c.RateLimitRPS)
	}
	
	if c.RateLimitBurst < 1 {
		return errors.New(errors.CategoryConfig, "INVALID_RATE_LIMIT_BURST", "rate limit burst must be at least 1").
			WithContext("burst", c.RateLimitBurst)
	}
	
	if c.RateLimitBurst < c.RateLimitRPS {
		return errors.New(errors.CategoryConfig, "INVALID_RATE_LIMIT_BURST", "rate limit burst must be at least equal to RPS").
			WithContext("burst", c.RateLimitBurst).
			WithContext("rps", c.RateLimitRPS)
	}
	
	return nil
}

// GetDatabasePoolConfig returns database pool configuration
func (c *Config) GetDatabasePoolConfig() *DatabasePoolConfig {
	return &DatabasePoolConfig{
		MaxOpenConns:    c.DBMaxOpenConns,
		MaxIdleConns:    c.DBMaxIdleConns,
		ConnMaxLifetime: c.DBConnMaxLifetime,
		ConnMaxIdleTime: c.DBConnMaxIdleTime,
		HealthCheck:     c.DBHealthCheck,
	}
}

// DatabasePoolConfig represents database connection pool configuration
type DatabasePoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	HealthCheck     bool
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBoolOrDefault(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func (c *Config) validateDatabasePool() error {
	if c.DBMaxOpenConns < 1 {
		return errors.New(errors.CategoryConfig, "INVALID_DB_MAX_OPEN_CONNS", "database max open connections must be at least 1").
			WithContext("db_max_open_conns", c.DBMaxOpenConns)
	}
	
	if c.DBMaxIdleConns < 0 {
		return errors.New(errors.CategoryConfig, "INVALID_DB_MAX_IDLE_CONNS", "database max idle connections cannot be negative").
			WithContext("db_max_idle_conns", c.DBMaxIdleConns)
	}
	
	if c.DBMaxIdleConns > c.DBMaxOpenConns {
		return errors.New(errors.CategoryConfig, "INVALID_DB_MAX_IDLE_CONNS", "database max idle connections cannot exceed max open connections").
			WithContext("db_max_idle_conns", c.DBMaxIdleConns).
			WithContext("db_max_open_conns", c.DBMaxOpenConns)
	}
	
	if c.DBConnMaxLifetime < 0 {
		return errors.New(errors.CategoryConfig, "INVALID_DB_CONN_MAX_LIFETIME", "database connection max lifetime cannot be negative").
			WithContext("db_conn_max_lifetime", c.DBConnMaxLifetime)
	}
	
	if c.DBConnMaxIdleTime < 0 {
		return errors.New(errors.CategoryConfig, "INVALID_DB_CONN_MAX_IDLE_TIME", "database connection max idle time cannot be negative").
			WithContext("db_conn_max_idle_time", c.DBConnMaxIdleTime)
	}
	
	return nil
}