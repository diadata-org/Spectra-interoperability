# Docker Configuration

This directory contains all Docker-related configurations for the Spectra Interoperability project.

## Structure

```
docker/
├── dockerfiles/           # Service Dockerfiles
│   ├── attestor.Dockerfile
│   ├── bridge.Dockerfile
│   ├── hyperlane-monitor.Dockerfile
│   ├── oracle-bridge.Dockerfile
│   ├── hyperlane-relayer.Dockerfile
│   └── hyperlane-validator.Dockerfile
├── compose/              # Docker Compose configurations
│   ├── development/      # Development environment
│   │   ├── docker-compose.yml          # Main dev setup
│   │   ├── docker-compose.full.yml     # Full stack
│   │   └── docker-compose.sunday.yml   # Sunday deployment
│   ├── production/       # Production environment
│   │   └── docker-compose.prod.yml     # Production ready
│   └── hyperlane/        # Hyperlane specific
│       ├── docker-compose.yml          # Hyperlane config
│       └── docker-compose.aws.yml      # AWS specific
└── scripts/              # Helper scripts
    ├── build-all.sh      # Build all images
    ├── start-dev.sh      # Start development
    └── cleanup.sh        # Cleanup resources
```

## Quick Start

### Build All Images
```bash
./docker/scripts/build-all.sh
```

### Start Development Environment
```bash
./docker/scripts/start-dev.sh
```

### Production Deployment
```bash
cd docker/compose/production
docker-compose -f docker-compose.prod.yml up -d
```

### Cleanup
```bash
./docker/scripts/cleanup.sh
```

## Environment Variables

Create a `.env` file in the project root with required variables:

```bash
# Required
ATTESTOR_PRIVATE_KEY=your_private_key
ATTESTOR_REGISTRY_ADDRESS=your_registry_address
BRIDGE_PRIVATE_KEY=your_bridge_private_key
POSTGRES_PASSWORD=secure_password

# Optional (with defaults)
ATTESTOR_RPC_URL=https://rpc-dia-lasernet-dipfsyyx2w.t.conduit.xyz
ATTESTOR_ORACLE_ADDRESS=0x0087342f5f4c7AB23a37c045c3EF710749527c88
```

## Services

### Attestor
- **Purpose**: Publishes oracle intents to registry
- **Ports**: 8080 (metrics), 8081 (API)
- **Image**: `spectra-attestor`

### Bridge
- **Purpose**: Monitors registry and routes to receivers
- **Ports**: 8080 (API), 8082 (gRPC), 8083 (metrics)
- **Image**: `spectra-bridge`

### Hyperlane Monitor
- **Purpose**: Monitors message delivery and triggers failover
- **Ports**: 9091 (metrics)
- **Image**: `spectra-hyperlane-monitor`

### Oracle Bridge
- **Purpose**: Oracle bridge service
- **Image**: `spectra-oracle-bridge`

## Development vs Production

### Development
- Uses local bind mounts for configuration
- Exposes all ports for debugging
- Includes development tools

### Production
- Uses secrets management
- Minimal exposed ports
- Optimized for security and performance
- Health checks and restart policies
- Proper logging configuration

## Maintenance

### Update Images
```bash
# Build specific service
docker build -f docker/dockerfiles/attestor.Dockerfile -t spectra-attestor services/attestor

# Or build all
./docker/scripts/build-all.sh
```

### View Logs
```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f attestor
```

### Health Checks
All services include health checks accessible at `/health` endpoints.