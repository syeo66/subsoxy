#!/bin/bash
set -euo pipefail

# Docker build script for Subsoxy
# Usage: ./scripts/docker-build.sh [--dev|--prod|--test] [--push] [--no-cache]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_ROOT"

# Default values
BUILD_TARGET="production"
PUSH_IMAGE=false
NO_CACHE=false
IMAGE_NAME="subsoxy"
REGISTRY=""

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --dev)
      BUILD_TARGET="dev"
      shift
      ;;
    --prod|--production)
      BUILD_TARGET="production"
      shift
      ;;
    --test)
      BUILD_TARGET="test-stage"
      shift
      ;;
    --push)
      PUSH_IMAGE=true
      shift
      ;;
    --no-cache)
      NO_CACHE=true
      shift
      ;;
    --registry=*)
      REGISTRY="${1#*=}"
      shift
      ;;
    --help)
      echo "Usage: $0 [--dev|--prod|--test] [--push] [--no-cache] [--registry=REGISTRY]"
      echo ""
      echo "Options:"
      echo "  --dev          Build development image"
      echo "  --prod         Build production image (default)"
      echo "  --test         Build and run tests only"
      echo "  --push         Push image to registry after build"
      echo "  --no-cache     Build without using cache"
      echo "  --registry=X   Use custom registry (e.g., --registry=docker.io/user)"
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

# Set image tag based on target
case $BUILD_TARGET in
  "dev")
    IMAGE_TAG="dev"
    DOCKERFILE="Dockerfile.dev"
    ;;
  "production")
    IMAGE_TAG="latest"
    DOCKERFILE="Dockerfile"
    ;;
  "test-stage")
    IMAGE_TAG="test"
    DOCKERFILE="Dockerfile"
    ;;
esac

# Construct full image name
FULL_IMAGE_NAME="${REGISTRY:+$REGISTRY/}${IMAGE_NAME}:${IMAGE_TAG}"

echo "Building Docker image..."
echo "  Target: $BUILD_TARGET"
echo "  Image: $FULL_IMAGE_NAME"
echo "  Dockerfile: $DOCKERFILE"

# Build command
BUILD_CMD="docker build"

if [[ "$NO_CACHE" == "true" ]]; then
  BUILD_CMD+=" --no-cache"
fi

if [[ "$BUILD_TARGET" != "dev" ]]; then
  BUILD_CMD+=" --target $BUILD_TARGET"
fi

BUILD_CMD+=" -f $DOCKERFILE"
BUILD_CMD+=" -t $FULL_IMAGE_NAME"
BUILD_CMD+=" ."

echo "Running: $BUILD_CMD"
eval $BUILD_CMD

echo "✅ Build completed successfully!"

# Run tests if building test stage
if [[ "$BUILD_TARGET" == "test-stage" ]]; then
  echo "Running tests in container..."
  docker run --rm "$FULL_IMAGE_NAME"
  echo "✅ Tests passed!"
fi

# Push if requested
if [[ "$PUSH_IMAGE" == "true" ]]; then
  echo "Pushing image to registry..."
  docker push "$FULL_IMAGE_NAME"
  echo "✅ Image pushed successfully!"
fi

echo "🎉 All operations completed!"
echo "Image: $FULL_IMAGE_NAME"