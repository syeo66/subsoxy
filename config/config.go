package config

import (
	"flag"
	"os"
)

type Config struct {
	ProxyPort    string
	UpstreamURL  string
	LogLevel     string
	DatabasePath string
}

func New() *Config {
	var (
		port     = flag.String("port", getEnvOrDefault("PORT", "8080"), "Proxy server port")
		upstream = flag.String("upstream", getEnvOrDefault("UPSTREAM_URL", "http://localhost:4533"), "Upstream Subsonic server URL")
		logLevel = flag.String("log-level", getEnvOrDefault("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
		dbPath   = flag.String("db-path", getEnvOrDefault("DB_PATH", "subsoxy.db"), "Database file path")
	)
	flag.Parse()

	return &Config{
		ProxyPort:    *port,
		UpstreamURL:  *upstream,
		LogLevel:     *logLevel,
		DatabasePath: *dbPath,
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}