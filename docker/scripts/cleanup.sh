#!/bin/bash

# Cleanup Docker resources for the project
echo "Cleaning up Spectra Docker resources..."

# Stop and remove containers
echo "🛑 Stopping containers..."
docker-compose -f docker/compose/development/docker-compose.yml down
docker-compose -f docker/compose/development/docker-compose.full.yml down
docker-compose -f docker/compose/development/docker-compose.sunday.yml down

# Remove images
echo "🗑️  Removing Spectra images..."
docker rmi -f $(docker images | grep spectra- | awk '{print $3}') 2>/dev/null || echo "No Spectra images to remove"

# Remove unused volumes and networks
echo "🧹 Cleaning up unused volumes and networks..."
docker volume prune -f
docker network prune -f

echo "✅ Cleanup complete!"