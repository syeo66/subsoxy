# Architecture

This document provides detailed information about the internal architecture and module design of the Subsonic proxy server.

## Module Architecture

The application uses a modular architecture with the following components:

### Core Modules

- **`config/`**: Configuration management with comprehensive validation and environment variable support
- **`models/`**: Data structures and type definitions  
- **`database/`**: SQLite3 database operations with structured error handling and schema management
- **`handlers/`**: HTTP request handlers for different Subsonic API endpoints with input validation
- **`middleware/`**: HTTP middleware components including security headers with intelligent development mode detection
- **`server/`**: Main proxy server logic, lifecycle management with error recovery, and bounded worker pools for resource safety
- **`credentials/`**: Secure authentication and credential validation with AES-256-GCM encryption and timeout protection
- **`shuffle/`**: Weighted song shuffling algorithm with intelligent preference learning and thread safety
- **`errors/`**: Structured error handling with categorization and context
- **`main.go`**: Entry point that wires all modules together

### Module Dependencies

Each module has clearly defined dependencies:

- `errors/` → No internal dependencies (foundational error handling)
- `config/` → `errors/` (for configuration validation errors)
- `models/` → No internal dependencies (pure data structures)
- `database/` → `errors/`, `models/` (database operations with structured errors)
- `credentials/` → `errors/` (credential validation with structured errors)
- `shuffle/` → `models/`, `database/` (song shuffling algorithms)
- `handlers/` → `errors/`, `shuffle/` (HTTP handlers with validation)
- `server/` → All modules (main orchestration layer)
- `main.go` → `config/`, `server/` (application entry point)

The `errors/` package provides the foundation for structured error handling throughout the application, while `models/` defines core data structures used across modules.

## External Dependencies

This application uses the following external libraries:

- **`github.com/gorilla/mux`**: HTTP router for request handling and middleware
- **`github.com/sirupsen/logrus`**: Structured logging with configurable levels and formatting
- **`github.com/mattn/go-sqlite3`**: SQLite3 database driver for song tracking and analytics
- **`golang.org/x/crypto`**: Cryptographic functions for AES-256-GCM credential encryption
- **`golang.org/x/time/rate`**: Rate limiting implementation using token bucket algorithm
- **Standard Library**: `net/http/httputil`, `crypto/aes`, `crypto/cipher`, `database/sql`, and other Go standard packages

## Performance Features

The application includes several performance optimizations:

- **Database Connection Pooling**: Advanced connection pool management with configurable limits and health monitoring
- **Bounded Worker Pools**: Semaphore-based concurrency control for credential validation (default: 100 workers) prevents goroutine exhaustion under high load
- **Memory-Efficient Shuffle Algorithms**: Automatic algorithm selection based on library size with reservoir sampling for large datasets
- **Batch Database Queries**: Optimized query patterns eliminate N+1 query problems
- **Concurrent Request Handling**: Thread-safe operations with proper synchronization
- **Rate Limiting**: Token bucket algorithm for efficient request throttling
- **Resource Management**: Automatic cleanup of connections and memory with graceful shutdown tracking
- **Health Monitoring**: Background health checks and performance metrics