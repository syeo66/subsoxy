# Docker Deployment Guide

This guide covers containerized deployment of Subsoxy using Docker and Docker Compose.

## Quick Start

### Production Deployment

```bash
# Copy environment template
cp .env.example .env

# Edit .env with your Subsonic server URL
vim .env

# Start with Docker Compose
./scripts/docker-run.sh --prod --detach

# Or manually
docker compose up -d
```

### Development Setup

```bash
# Start development environment with live reload
./scripts/docker-run.sh --dev

# Or manually
docker compose -f docker-compose.dev.yml up
```

## Docker Images

### Production Image Features

- **Base**: Distroless Debian 12 (minimal attack surface)
- **Size**: ~11MB (vs ~24MB with Alpine)
- **Security**: Non-root user, read-only filesystem, no shell
- **Optimization**: Static binary, stripped symbols
- **Multi-arch**: Supports AMD64 architecture

### Development Image Features

- **Base**: Go 1.24.4 Alpine
- **Tools**: Air live reload, Delve debugger
- **Volumes**: Source code mounted for development
- **Debug**: Port 2345 exposed for debugging

## Build Scripts

### Production Build

```bash
# Build production image
./scripts/docker-build.sh --prod

# Build with custom registry
./scripts/docker-build.sh --prod --registry=docker.io/yourname

# Build and push
./scripts/docker-build.sh --prod --push --registry=docker.io/yourname
```

### Development Build

```bash
# Build development image
./scripts/docker-build.sh --dev

# Build without cache
./scripts/docker-build.sh --dev --no-cache
```

### Test Build

```bash
# Build and run tests
./scripts/docker-build.sh --test
```

## Docker Compose Configurations

### Production (docker-compose.yml)

- **Services**: Subsoxy, optional Prometheus/Grafana
- **Volumes**: Persistent database and logs
- **Security**: Security headers, resource limits, read-only filesystem
- **Networking**: Bridge network with port mapping
- **Profiles**: Monitoring stack with `--profile monitoring`

### Development (docker-compose.dev.yml)

- **Services**: Subsoxy development
- **Features**: Live reload, source mounting, debugger access
- **Volumes**: Source code, Go module cache
- **Debug**: Delve debugger on port 2345

## Environment Variables

### Required

```bash
UPSTREAM_URL=http://your-subsonic-server:4533
```

### Optional Configuration

```bash
# Server
PORT=8080
LOG_LEVEL=info

# Database
DB_PATH=/app/data/subsoxy.db
DB_MAX_OPEN_CONNS=25
DB_MAX_IDLE_CONNS=25

# Performance
RATE_LIMIT_RPS=100
RATE_LIMIT_BURST=200

# Security
SECURITY_HEADERS_ENABLED=true
CORS_ALLOW_ORIGINS=*
DEV_MODE=false
```

## Monitoring Stack

### Enable Monitoring

```bash
# Start with Prometheus and Grafana
./scripts/docker-run.sh --prod --monitor --detach

# Or manually
docker compose --profile monitoring up -d
```

### Access Points

- **Subsoxy**: http://localhost:8080
- **Prometheus**: http://localhost:9090
- **Grafana**: http://localhost:3000 (admin/admin123)

## Volume Management

### Persistent Data

```bash
# Database location
/app/data/subsoxy.db

# View volumes
docker volume ls | grep subsoxy

# Backup database
docker run --rm -v subsoxy_data:/data -v $(pwd):/backup alpine \
  cp /data/subsoxy.db /backup/subsoxy-backup.db
```

### Log Access

```bash
# View logs
docker compose logs -f subsoxy

# Development logs
docker compose -f docker-compose.dev.yml logs -f
```

## Security Features

### Production Security

- **Non-root user**: Runs as UID 65532 (nonroot)
- **Read-only filesystem**: Prevents runtime modifications
- **No shell access**: Distroless image has no shell
- **Security headers**: CSP, HSTS, X-Frame-Options, etc.
- **Resource limits**: CPU and memory constraints
- **Network isolation**: Bridge network with minimal exposure

### Development Security

- **Separate configuration**: Less restrictive for development
- **Debug capabilities**: SYS_PTRACE capability for debugging
- **Source mounting**: Read-write access to source code

## Troubleshooting

### Common Issues

1. **Permission denied on volumes**
   ```bash
   # Fix volume permissions
   docker run --rm -v subsoxy_data:/data alpine chown -R 65532:65532 /data
   ```

2. **Database locked**
   ```bash
   # Stop all containers
   docker compose down
   
   # Check for orphaned processes
   docker ps -a | grep subsoxy
   ```

3. **Port conflicts**
   ```bash
   # Use different port
   PORT=8081 docker compose up
   ```

### Health Checks

The production image relies on container orchestration (Kubernetes, Docker Swarm) for health checks since distroless images lack shell utilities.

For manual health checking:
```bash
# Test if service responds
curl -f http://localhost:8080/rest/ping?u=test&p=test&v=1.15.0&c=test&f=json
```

## Multi-Architecture Support

Currently supports AMD64. To add ARM64 support:

```dockerfile
# Add to Dockerfile build stage
RUN CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o subsoxy .
```

Then build with buildx:
```bash
docker buildx build --platform linux/amd64,linux/arm64 -t subsoxy:latest .
```

## Best Practices

1. **Use specific tags**: Avoid `latest` in production
2. **Regular updates**: Keep base images updated
3. **Resource limits**: Set appropriate CPU/memory limits
4. **Monitoring**: Use the monitoring stack for production
5. **Backups**: Regular database backups
6. **Secrets**: Use Docker secrets or external secret management
7. **Networks**: Use custom networks for service isolation