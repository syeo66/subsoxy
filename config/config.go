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

// Default configuration values
const (
	DefaultProxyPort            = "8080"
	DefaultUpstreamURL          = "http://localhost:4533"
	DefaultLogLevel             = "info"
	DefaultDatabasePath         = "subsoxy.db"
	DefaultRateLimitRPS         = 100
	DefaultRateLimitBurst       = 200
	DefaultRateLimitEnabled     = true
	DefaultDBMaxOpenConns       = 25
	DefaultDBMaxIdleConns       = 5
	DefaultDBConnMaxLifetime    = 30 * time.Minute
	DefaultDBConnMaxIdleTime    = 5 * time.Minute
	DefaultDBHealthCheck        = true
	DefaultCORSEnabled          = true
	DefaultCORSAllowOrigins     = "*"
	DefaultCORSAllowMethods     = "GET,POST,PUT,DELETE,OPTIONS"
	DefaultCORSAllowHeaders     = "Content-Type,Authorization,X-Requested-With"
	DefaultCORSAllowCredentials = false
	DefaultDirPermissions       = 0755
	// Security headers
	DefaultSecurityHeadersEnabled  = true
	DefaultSecurityDevMode         = false
	DefaultXContentTypeOptions     = "nosniff"
	DefaultXFrameOptions           = "DENY"
	DefaultXXSSProtection          = "1; mode=block"
	DefaultStrictTransportSecurity = "max-age=31536000; includeSubDomains"
	DefaultContentSecurityPolicy   = "default-src 'self'; script-src 'self'; object-src 'none';"
	DefaultReferrerPolicy          = "strict-origin-when-cross-origin"
	DefaultDebugMode               = false
)

// Validation limits
const (
	MinPortNumber     = 1
	MaxPortNumber     = 65535
	MinRateLimitRPS   = 1
	MinRateLimitBurst = 1
	MinDBMaxOpenConns = 1
	MinDBMaxIdleConns = 0
	MinDBConnLifetime = 0
	MinDBConnIdleTime = 0
)

type Config struct {
	ProxyPort        string
	UpstreamURL      string
	LogLevel         string
	DatabasePath     string
	RateLimitRPS     int
	RateLimitBurst   int
	RateLimitEnabled bool
	// Database connection pool settings
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
	DBConnMaxIdleTime time.Duration
	DBHealthCheck     bool
	// CORS settings
	CORSEnabled          bool
	CORSAllowOrigins     []string
	CORSAllowMethods     []string
	CORSAllowHeaders     []string
	CORSAllowCredentials bool
	// Security headers settings
	SecurityHeadersEnabled  bool
	SecurityDevMode         bool
	XContentTypeOptions     string
	XFrameOptions           string
	XXSSProtection          string
	StrictTransportSecurity string
	ContentSecurityPolicy   string
	ReferrerPolicy          string
	// Debug mode
	DebugMode bool
}

func New() (*Config, error) {
	var (
		port             = flag.String("port", getEnvOrDefault("PORT", DefaultProxyPort), "Proxy server port")
		upstream         = flag.String("upstream", getEnvOrDefault("UPSTREAM_URL", DefaultUpstreamURL), "Upstream Subsonic server URL")
		logLevel         = flag.String("log-level", getEnvOrDefault("LOG_LEVEL", DefaultLogLevel), "Log level (debug, info, warn, error)")
		dbPath           = flag.String("db-path", getEnvOrDefault("DB_PATH", DefaultDatabasePath), "Database file path")
		rateLimitRPS     = flag.Int("rate-limit-rps", getEnvIntOrDefault("RATE_LIMIT_RPS", DefaultRateLimitRPS), "Rate limit requests per second")
		rateLimitBurst   = flag.Int("rate-limit-burst", getEnvIntOrDefault("RATE_LIMIT_BURST", DefaultRateLimitBurst), "Rate limit burst size")
		rateLimitEnabled = flag.Bool("rate-limit-enabled", getEnvBoolOrDefault("RATE_LIMIT_ENABLED", DefaultRateLimitEnabled), "Enable rate limiting")
		// Database connection pool flags
		dbMaxOpenConns    = flag.Int("db-max-open-conns", getEnvIntOrDefault("DB_MAX_OPEN_CONNS", DefaultDBMaxOpenConns), "Maximum number of open database connections")
		dbMaxIdleConns    = flag.Int("db-max-idle-conns", getEnvIntOrDefault("DB_MAX_IDLE_CONNS", DefaultDBMaxIdleConns), "Maximum number of idle database connections")
		dbConnMaxLifetime = flag.Duration("db-conn-max-lifetime", getEnvDurationOrDefault("DB_CONN_MAX_LIFETIME", DefaultDBConnMaxLifetime), "Maximum connection lifetime")
		dbConnMaxIdleTime = flag.Duration("db-conn-max-idle-time", getEnvDurationOrDefault("DB_CONN_MAX_IDLE_TIME", DefaultDBConnMaxIdleTime), "Maximum connection idle time")
		dbHealthCheck     = flag.Bool("db-health-check", getEnvBoolOrDefault("DB_HEALTH_CHECK", DefaultDBHealthCheck), "Enable database health checks")
		// CORS flags
		corsEnabled          = flag.Bool("cors-enabled", getEnvBoolOrDefault("CORS_ENABLED", DefaultCORSEnabled), "Enable CORS headers")
		corsAllowOrigins     = flag.String("cors-allow-origins", getEnvOrDefault("CORS_ALLOW_ORIGINS", DefaultCORSAllowOrigins), "CORS allowed origins (comma-separated)")
		corsAllowMethods     = flag.String("cors-allow-methods", getEnvOrDefault("CORS_ALLOW_METHODS", DefaultCORSAllowMethods), "CORS allowed methods (comma-separated)")
		corsAllowHeaders     = flag.String("cors-allow-headers", getEnvOrDefault("CORS_ALLOW_HEADERS", DefaultCORSAllowHeaders), "CORS allowed headers (comma-separated)")
		corsAllowCredentials = flag.Bool("cors-allow-credentials", getEnvBoolOrDefault("CORS_ALLOW_CREDENTIALS", DefaultCORSAllowCredentials), "CORS allow credentials")
		// Security headers flags
		securityHeadersEnabled  = flag.Bool("security-headers-enabled", getEnvBoolOrDefault("SECURITY_HEADERS_ENABLED", DefaultSecurityHeadersEnabled), "Enable security headers")
		securityDevMode         = flag.Bool("security-dev-mode", getEnvBoolOrDefault("SECURITY_DEV_MODE", DefaultSecurityDevMode), "Enable development mode (relaxed security headers for localhost)")
		xContentTypeOptions     = flag.String("x-content-type-options", getEnvOrDefault("X_CONTENT_TYPE_OPTIONS", DefaultXContentTypeOptions), "X-Content-Type-Options header value")
		xFrameOptions           = flag.String("x-frame-options", getEnvOrDefault("X_FRAME_OPTIONS", DefaultXFrameOptions), "X-Frame-Options header value")
		xxxxProtection          = flag.String("x-xss-protection", getEnvOrDefault("X_XSS_PROTECTION", DefaultXXSSProtection), "X-XSS-Protection header value")
		strictTransportSecurity = flag.String("strict-transport-security", getEnvOrDefault("STRICT_TRANSPORT_SECURITY", DefaultStrictTransportSecurity), "Strict-Transport-Security header value")
		contentSecurityPolicy   = flag.String("content-security-policy", getEnvOrDefault("CONTENT_SECURITY_POLICY", DefaultContentSecurityPolicy), "Content-Security-Policy header value")
		referrerPolicy          = flag.String("referrer-policy", getEnvOrDefault("REFERRER_POLICY", DefaultReferrerPolicy), "Referrer-Policy header value")
		debugMode               = flag.Bool("debug-mode", getEnvBoolOrDefault("DEBUG", DefaultDebugMode), "Enable debug endpoint")
	)
	flag.Parse()

	config := &Config{
		ProxyPort:               *port,
		UpstreamURL:             *upstream,
		LogLevel:                *logLevel,
		DatabasePath:            *dbPath,
		RateLimitRPS:            *rateLimitRPS,
		RateLimitBurst:          *rateLimitBurst,
		RateLimitEnabled:        *rateLimitEnabled,
		DBMaxOpenConns:          *dbMaxOpenConns,
		DBMaxIdleConns:          *dbMaxIdleConns,
		DBConnMaxLifetime:       *dbConnMaxLifetime,
		DBConnMaxIdleTime:       *dbConnMaxIdleTime,
		DBHealthCheck:           *dbHealthCheck,
		CORSEnabled:             *corsEnabled,
		CORSAllowOrigins:        parseCommaSeparatedString(*corsAllowOrigins),
		CORSAllowMethods:        parseCommaSeparatedString(*corsAllowMethods),
		CORSAllowHeaders:        parseCommaSeparatedString(*corsAllowHeaders),
		CORSAllowCredentials:    *corsAllowCredentials,
		SecurityHeadersEnabled:  *securityHeadersEnabled,
		SecurityDevMode:         *securityDevMode,
		XContentTypeOptions:     *xContentTypeOptions,
		XFrameOptions:           *xFrameOptions,
		XXSSProtection:          *xxxxProtection,
		StrictTransportSecurity: *strictTransportSecurity,
		ContentSecurityPolicy:   *contentSecurityPolicy,
		ReferrerPolicy:          *referrerPolicy,
		DebugMode:               *debugMode,
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

	if err := c.validateCORS(); err != nil {
		return err
	}

	if err := c.validateSecurityHeaders(); err != nil {
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

	if port < MinPortNumber || port > MaxPortNumber {
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
				if err := os.MkdirAll(dir, DefaultDirPermissions); err != nil {
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
	if c.RateLimitRPS < MinRateLimitRPS {
		return errors.New(errors.CategoryConfig, "INVALID_RATE_LIMIT_RPS", "rate limit RPS must be at least 1").
			WithContext("rps", c.RateLimitRPS)
	}

	if c.RateLimitBurst < MinRateLimitBurst {
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

func parseCommaSeparatedString(input string) []string {
	if input == "" {
		return []string{}
	}
	result := strings.Split(input, ",")
	// Trim whitespace from each element
	for i := range result {
		result[i] = strings.TrimSpace(result[i])
	}
	return result
}

func (c *Config) validateDatabasePool() error {
	if c.DBMaxOpenConns < MinDBMaxOpenConns {
		return errors.New(errors.CategoryConfig, "INVALID_DB_MAX_OPEN_CONNS", "database max open connections must be at least 1").
			WithContext("db_max_open_conns", c.DBMaxOpenConns)
	}

	if c.DBMaxIdleConns < MinDBMaxIdleConns {
		return errors.New(errors.CategoryConfig, "INVALID_DB_MAX_IDLE_CONNS", "database max idle connections cannot be negative").
			WithContext("db_max_idle_conns", c.DBMaxIdleConns)
	}

	if c.DBMaxIdleConns > c.DBMaxOpenConns {
		return errors.New(errors.CategoryConfig, "INVALID_DB_MAX_IDLE_CONNS", "database max idle connections cannot exceed max open connections").
			WithContext("db_max_idle_conns", c.DBMaxIdleConns).
			WithContext("db_max_open_conns", c.DBMaxOpenConns)
	}

	if c.DBConnMaxLifetime < MinDBConnLifetime {
		return errors.New(errors.CategoryConfig, "INVALID_DB_CONN_MAX_LIFETIME", "database connection max lifetime cannot be negative").
			WithContext("db_conn_max_lifetime", c.DBConnMaxLifetime)
	}

	if c.DBConnMaxIdleTime < MinDBConnIdleTime {
		return errors.New(errors.CategoryConfig, "INVALID_DB_CONN_MAX_IDLE_TIME", "database connection max idle time cannot be negative").
			WithContext("db_conn_max_idle_time", c.DBConnMaxIdleTime)
	}

	return nil
}

func (c *Config) validateCORS() error {
	// If CORS is disabled, skip validation
	if !c.CORSEnabled {
		return nil
	}

	// Validate origins - empty list is invalid if CORS is enabled
	if len(c.CORSAllowOrigins) == 0 {
		return errors.New(errors.CategoryConfig, "INVALID_CORS_ORIGINS", "CORS origins cannot be empty when CORS is enabled").
			WithContext("cors_enabled", c.CORSEnabled)
	}

	// Validate methods - must have at least one method
	if len(c.CORSAllowMethods) == 0 {
		return errors.New(errors.CategoryConfig, "INVALID_CORS_METHODS", "CORS methods cannot be empty when CORS is enabled").
			WithContext("cors_enabled", c.CORSEnabled)
	}

	// Validate that methods are reasonable HTTP methods
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true,
		"OPTIONS": true, "HEAD": true, "PATCH": true,
	}

	for _, method := range c.CORSAllowMethods {
		upperMethod := strings.ToUpper(method)
		if !validMethods[upperMethod] {
			return errors.New(errors.CategoryConfig, "INVALID_CORS_METHOD", "invalid HTTP method in CORS configuration").
				WithContext("method", method).
				WithContext("valid_methods", []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD", "PATCH"})
		}
	}

	// Validate headers - empty list is allowed
	if len(c.CORSAllowHeaders) == 0 {
		// Headers can be empty, that's fine
	}

	return nil
}

func (c *Config) validateSecurityHeaders() error {
	// If security headers are disabled, skip validation
	if !c.SecurityHeadersEnabled {
		return nil
	}

	// Validate X-Content-Type-Options
	if c.XContentTypeOptions != "" && c.XContentTypeOptions != "nosniff" {
		return errors.New(errors.CategoryConfig, "INVALID_X_CONTENT_TYPE_OPTIONS", "X-Content-Type-Options must be 'nosniff' or empty").
			WithContext("x_content_type_options", c.XContentTypeOptions)
	}

	// Validate X-Frame-Options
	validFrameOptions := []string{"DENY", "SAMEORIGIN"}
	if c.XFrameOptions != "" {
		valid := false
		for _, option := range validFrameOptions {
			if c.XFrameOptions == option {
				valid = true
				break
			}
		}
		if !valid {
			return errors.New(errors.CategoryConfig, "INVALID_X_FRAME_OPTIONS", "X-Frame-Options must be 'DENY', 'SAMEORIGIN', or empty").
				WithContext("x_frame_options", c.XFrameOptions).
				WithContext("valid_options", validFrameOptions)
		}
	}

	return nil
}

// IsDevMode checks if the server is running in development mode
// Development mode is enabled when:
// 1. SecurityDevMode is explicitly set to true, OR
// 2. The proxy is running on default port (8080) suggesting local development
func (c *Config) IsDevMode() bool {
	if c.SecurityDevMode {
		return true
	}

	// Check if running on default development port
	if c.ProxyPort == DefaultProxyPort {
		return true
	}

	return false
}
