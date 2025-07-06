# Config Module

The config module provides centralized configuration management for the Subsonic proxy server.

## Overview

This module handles:
- Command-line flag parsing
- Environment variable support
- Configuration validation
- Default value management

## Usage

```go
import "github.com/syeo66/subsoxy/config"

cfg := config.New()
// Configuration is now ready to use
```

## Configuration Options

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `-port` | `PORT` | `8080` | Proxy server port |
| `-upstream` | `UPSTREAM_URL` | `http://localhost:4533` | Upstream Subsonic server URL |
| `-log-level` | `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `-db-path` | `DB_PATH` | `subsoxy.db` | SQLite database file path |

## Examples

```bash
# Using command-line flags
./subsoxy -port 9090 -upstream http://my-server:4533 -log-level debug

# Using environment variables
PORT=9090 UPSTREAM_URL=http://my-server:4533 LOG_LEVEL=debug ./subsoxy

# Mixed usage (flags override environment variables)
PORT=8080 ./subsoxy -port 9090  # Will use port 9090
```

## Implementation Details

- Command-line flags take precedence over environment variables
- The `New()` function calls `flag.Parse()` automatically
- Invalid log levels default to "info"
- All configuration is validated at startup