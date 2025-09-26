#!/bin/bash

# Start local development environment with secure configuration
echo "🚀 Starting Spectra local development environment..."

# Get script directory to ensure correct paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

cd "$PROJECT_ROOT"

# Check if local configuration exists
if [ ! -f "config/secrets/.env.local" ]; then
    echo "❌ Local configuration not found!"
    echo "Please run: ./config/setup-local.sh"
    exit 1
fi

if [ ! -f "docker-compose.local.yml" ]; then
    echo "❌ Local docker-compose file not found!"
    echo "Please run: ./config/setup-local.sh"
    exit 1
fi

echo "🔧 Using secure local configuration..."
echo "📁 Config file: config/secrets/.env.local (git-ignored)"
echo "🐳 Compose file: docker-compose.local.yml (git-ignored)"
echo ""

# Start services
echo "🚀 Starting services..."
docker-compose -f docker-compose.local.yml up -d

echo "✅ Local development environment started!"
echo ""
echo "Services:"
echo "  - Attestor: http://localhost:8080 (metrics), http://localhost:8081 (API)"
echo "  - Bridge: http://localhost:8082 (API), :8083 (gRPC), :8084 (metrics)"
echo "  - PostgreSQL: localhost:5432"
echo ""
echo "To view logs: docker-compose -f docker-compose.local.yml logs -f"
echo "To stop: docker-compose -f docker-compose.local.yml down"