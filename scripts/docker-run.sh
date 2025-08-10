#!/bin/bash
set -euo pipefail

# Docker run script for Subsoxy
# Usage: ./scripts/docker-run.sh [--dev|--prod] [--detach] [--monitor]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_ROOT"

# Default values
MODE="prod"
DETACH=false
MONITOR=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --dev|--development)
      MODE="dev"
      shift
      ;;
    --prod|--production)
      MODE="prod"
      shift
      ;;
    --detach|-d)
      DETACH=true
      shift
      ;;
    --monitor|-m)
      MONITOR=true
      shift
      ;;
    --help)
      echo "Usage: $0 [--dev|--prod] [--detach] [--monitor]"
      echo ""
      echo "Options:"
      echo "  --dev          Run development setup with live reload"
      echo "  --prod         Run production setup (default)"
      echo "  --detach, -d   Run in detached mode"
      echo "  --monitor, -m  Include monitoring stack (Prometheus/Grafana)"
      echo "  --help         Show this help message"
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

# Check if .env file exists
if [[ ! -f .env ]]; then
  if [[ -f .env.example ]]; then
    echo "⚠️  No .env file found. Please copy .env.example to .env and configure it."
    echo "   cp .env.example .env"
    exit 1
  else
    echo "⚠️  No .env or .env.example file found. Please create .env with your configuration."
    exit 1
  fi
fi

# Set compose file and options
COMPOSE_CMD="docker compose"
COMPOSE_OPTIONS=""

if [[ "$MODE" == "dev" ]]; then
  COMPOSE_CMD+=" -f docker-compose.dev.yml"
  echo "🚀 Starting Subsoxy in development mode..."
else
  COMPOSE_CMD+=" -f docker-compose.yml"
  echo "🚀 Starting Subsoxy in production mode..."
fi

if [[ "$MONITOR" == "true" ]]; then
  COMPOSE_CMD+=" --profile monitoring"
  echo "📊 Including monitoring stack (Prometheus + Grafana)..."
fi

if [[ "$DETACH" == "true" ]]; then
  COMPOSE_OPTIONS+=" -d"
fi

# Run the compose command
echo "Running: $COMPOSE_CMD up $COMPOSE_OPTIONS"
$COMPOSE_CMD up $COMPOSE_OPTIONS

# Show useful information after startup
if [[ "$DETACH" == "true" ]]; then
  echo ""
  echo "✅ Subsoxy is running!"
  echo ""
  echo "🔗 Services:"
  echo "   Subsoxy:    http://localhost:${PORT:-8080}"
  
  if [[ "$MONITOR" == "true" ]]; then
    echo "   Prometheus: http://localhost:9090"
    echo "   Grafana:    http://localhost:3000 (admin/admin123)"
  fi
  
  echo ""
  echo "📋 Useful commands:"
  echo "   View logs:    docker compose logs -f"
  echo "   Stop services: docker compose down"
  echo "   View status:  docker compose ps"
fi