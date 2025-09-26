#!/bin/bash

# Build all Docker images for the project
echo "Building all Docker images..."

# Get script directory to ensure correct paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

cd "$PROJECT_ROOT"

echo "🔨 Building attestor..."
docker build -f docker/dockerfiles/attestor.Dockerfile -t spectra-attestor services/attestor

echo "🔨 Building bridge..."
docker build -f docker/dockerfiles/bridge.Dockerfile -t spectra-bridge services/bridge

echo "🔨 Building hyperlane-monitor..."
docker build -f docker/dockerfiles/hyperlane-monitor.Dockerfile -t spectra-hyperlane-monitor services/hyperlane-monitor

echo "🔨 Building oracle-bridge..."
docker build -f docker/dockerfiles/oracle-bridge.Dockerfile -t spectra-oracle-bridge services/oracle-bridge

echo "✅ All images built successfully!"
echo ""
echo "Available images:"
docker images | grep spectra-