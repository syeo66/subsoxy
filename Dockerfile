# Enhanced Dockerfile for Subsoxy
# Multi-stage build optimized for security, performance, and size

# Build stage - use specific Go version matching go.mod
FROM golang:1.24.4-alpine AS build-stage

# Install build dependencies for CGO (required for SQLite)
RUN apk add --no-cache gcc musl-dev sqlite-dev ca-certificates git

# Create non-root user for build
RUN adduser -D -s /bin/sh -u 1001 appuser

WORKDIR /app

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build optimized binary with security flags
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o subsoxy .

# Test stage - run tests in isolated stage
FROM build-stage AS test-stage
RUN go test -v -race -coverprofile=coverage.out ./...

# Production stage - use distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot AS production

# Import ca-certificates from build stage
COPY --from=build-stage /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary with correct ownership
COPY --from=build-stage --chown=nonroot:nonroot /app/subsoxy /usr/local/bin/subsoxy

# Create directory for database with proper permissions
USER nonroot
WORKDIR /app

# Health check using a simple TCP connection test
# Note: In distroless, we'll rely on container orchestration for health checks
# This would work with a busybox-based runtime:
# HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
#     CMD ["/bin/sh", "-c", "nc -z localhost 8080 || exit 1"]

# Default port (can be overridden)
EXPOSE 8080

# Use exec form for proper signal handling
ENTRYPOINT ["/usr/local/bin/subsoxy"]