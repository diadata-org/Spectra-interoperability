#!/usr/bin/env bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Global variables
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTRACTS_DIR="${ROOT_DIR}/contracts"
ATTESTOR_DIR="${ROOT_DIR}/services/attestor"
BRIDGE_DIR="${ROOT_DIR}/services/bridge"
COMPOSE_FILE="${ROOT_DIR}/docker-compose.local.yml"

ANVIL_RPC="http://localhost:8545"
DEFAULT_KEY="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

CACHE_DIR="${ROOT_DIR}/.cache"
WALLETS_DIR="${ROOT_DIR}/.wallets"
REGISTRY_ADDR_FILE="${CACHE_DIR}/oracle_intent_registry.addr"
RECEIVER_ADDR_FILE="${CACHE_DIR}/push_oracle_receiver_v2.addr"
PROTOCOL_FEE_HOOK_FILE="${CACHE_DIR}/protocol_fee_hook.addr"
DIA_ORACLE_ADDR_FILE="${CACHE_DIR}/dia_oracle_v2.addr"

# Logging functions
log_info() {
    echo -e "${BLUE}ℹ️ $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️ $1${NC}"
}

log_error() {
    echo -e "${RED}❌ $1${NC}" >&2
}

# Cleanup function
cleanup() {
    local exit_code=$?
    if [ $exit_code -ne 0 ]; then
        log_error "Script failed. Cleaning up..."
        # Stop Docker services
        docker compose -f "${COMPOSE_FILE}" down --remove-orphans 2>/dev/null || true
        # Stop Anvil
        if [ -n "${ANVIL_PID:-}" ]; then
            kill $ANVIL_PID 2>/dev/null || true
        fi
        # Stop price updater
        if [ -n "${PRICE_UPDATER_PID:-}" ]; then
            kill $PRICE_UPDATER_PID 2>/dev/null || true
        fi
        # Also try to stop from PID file
        if [ -f "${ROOT_DIR}/.temp/price-updater.pid" ]; then
            kill $(cat "${ROOT_DIR}/.temp/price-updater.pid") 2>/dev/null || true
            rm -f "${ROOT_DIR}/.temp/price-updater.pid"
        fi
    fi
    exit $exit_code
}

trap cleanup EXIT INT TERM

# Step 1: Start Anvil
start_anvil() {
    log_info "Step 1: Starting Anvil blockchain..."

    # Kill any existing anvil processes
    pkill -f "anvil.*8545" || true
    sleep 2

    # Start anvil in background
    anvil --host 0.0.0.0 --port 8545 --chain-id 31337 --balance 10000 &
    ANVIL_PID=$!

    # Wait for anvil to be ready
    log_info "Waiting for Anvil to be ready..."
    for i in {1..30}; do
        if curl -s -X POST -H "Content-Type: application/json" \
           --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
           "$ANVIL_RPC" >/dev/null 2>&1; then
            log_success "Anvil is ready at $ANVIL_RPC"
            return 0
        fi
        sleep 1
    done

    log_error "Anvil failed to start"
    return 1
}

# Step 2: Deploy all contracts
deploy_all_contracts() {
    log_info "Step 2: Deploying smart contracts..."

    # Create cache and wallets directories
    mkdir -p "${CACHE_DIR}" "${WALLETS_DIR}"

    # Deploy contracts in correct order
    deploy_dia_oracle
    deploy_registry
    deploy_protocol_fee_hook
    deploy_receiver
    configure_contracts
    fund_receiver
    initialize_oracle_prices
}

# Deploy DIAOracleV2
deploy_dia_oracle() {
    log_info "🚀 Deploying DIAOracleV2..."
    local output
    cd "${CONTRACTS_DIR}"
    if ! output=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 forge create \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        --broadcast \
        "contracts/DIAOracleV2.sol:DIAOracleV2" 2>&1); then
        log_error "Failed to deploy DIAOracleV2"
        echo "$output" >&2
        return 1
    fi

    local address
    address=$(echo "$output" | awk '/Deployed to:/ {print $3}')
    if [ -z "$address" ]; then
        log_error "Failed to capture DIAOracleV2 address"
        echo "$output" >&2
        return 1
    fi

    echo "$address" > "${DIA_ORACLE_ADDR_FILE}"
    log_success "DIAOracleV2 deployed at $address"
}

# Deploy OracleIntentRegistry
deploy_registry() {
    log_info "🚀 Deploying OracleIntentRegistry..."
    local output
    cd "${CONTRACTS_DIR}"
    if ! output=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 forge create \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        --broadcast \
        "contracts/OracleIntentRegistry.sol:OracleIntentRegistry" \
        --constructor-args "SpectraIntents" "1.0" 2>&1); then
        log_error "Failed to deploy OracleIntentRegistry"
        echo "$output" >&2
        return 1
    fi

    local address
    address=$(echo "$output" | awk '/Deployed to:/ {print $3}')
    if [ -z "$address" ]; then
        log_error "Failed to capture OracleIntentRegistry address"
        echo "$output" >&2
        return 1
    fi

    echo "$address" > "${REGISTRY_ADDR_FILE}"
    log_success "OracleIntentRegistry deployed at $address"
}

# Deploy ProtocolFeeHook
deploy_protocol_fee_hook() {
    log_info "🚀 Deploying ProtocolFeeHook..."
    local output
    cd "${CONTRACTS_DIR}"
    if ! output=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 forge create \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        --broadcast \
        "contracts/ProtocolFeeHook.sol:ProtocolFeeHook" 2>&1); then
        log_error "Failed to deploy ProtocolFeeHook"
        echo "$output" >&2
        return 1
    fi

    local address
    address=$(echo "$output" | awk '/Deployed to:/ {print $3}')
    if [ -z "$address" ]; then
        log_error "Failed to capture ProtocolFeeHook address"
        echo "$output" >&2
        return 1
    fi

    echo "$address" > "${PROTOCOL_FEE_HOOK_FILE}"
    log_success "ProtocolFeeHook deployed at $address"
}

# Deploy PushOracleReceiverV2
deploy_receiver() {
    local registry_addr
    registry_addr=$(cat "${REGISTRY_ADDR_FILE}")
    log_info "🚀 Deploying PushOracleReceiverV2 with registry $registry_addr..."

    local output
    cd "${CONTRACTS_DIR}"
    if ! output=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 forge create \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        --broadcast \
        "contracts/PushOracleReceiverV2.sol:PushOracleReceiverV2" \
        --constructor-args "SpectraIntents" "1.0" 31337 "$registry_addr" 2>&1); then
        log_error "Failed to deploy PushOracleReceiverV2"
        echo "$output" >&2
        return 1
    fi

    local address
    address=$(echo "$output" | awk '/Deployed to:/ {print $3}')
    if [ -z "$address" ]; then
        log_error "Failed to capture PushOracleReceiverV2 address"
        echo "$output" >&2
        return 1
    fi

    echo "$address" > "${RECEIVER_ADDR_FILE}"
    log_success "PushOracleReceiverV2 deployed at $address"
}

# Configure contracts
configure_contracts() {
    log_info "🔧 Configuring contracts..."

    local receiver_addr fee_hook_addr registry_addr
    receiver_addr=$(cat "${RECEIVER_ADDR_FILE}")
    fee_hook_addr=$(cat "${PROTOCOL_FEE_HOOK_FILE}")
    registry_addr=$(cat "${REGISTRY_ADDR_FILE}")

    # Set payment hook in PushOracleReceiverV2
    log_info "Setting payment hook in PushOracleReceiverV2..."
    if ! FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        "$receiver_addr" \
        "setPaymentHook(address)" \
        "$fee_hook_addr"; then
        log_warning "Failed to set payment hook (method might not exist)"
    else
        log_success "Payment hook configured"
    fi

    # Authorize signer in registry
    log_info "Authorizing signer in registry..."
    if ! FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        "$registry_addr" \
        "setSignerAuthorization(address,bool)" \
        "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" \
        true; then
        log_warning "Failed to authorize signer (method might not exist)"
    else
        log_success "Signer authorized"
    fi
}

# Fund PushOracleReceiverV2
fund_receiver() {
    log_info "💰 Funding PushOracleReceiverV2 contract..."

    local receiver_addr
    receiver_addr=$(cat "${RECEIVER_ADDR_FILE}")

    # Fund the receiver with 1 ETH
    if ! FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        --value "1ether" \
        "$receiver_addr"; then
        log_error "Failed to fund PushOracleReceiverV2"
        return 1
    fi

    log_success "PushOracleReceiverV2 funded with 1 ETH"
}

# Initialize oracle with initial prices
initialize_oracle_prices() {
    log_info "🔮 Initializing DIA Oracle with initial prices..."

    local oracle_addr
    oracle_addr=$(cat "${DIA_ORACLE_ADDR_FILE}")

    # Set initial ETH/USD price (around $2250)
    local eth_price_wei=$(python3 -c "print(int(2250 * 1e18))")
    local timestamp=$(date +%s)

    log_info "Setting initial ETH/USD price..."
    if ! FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        "$oracle_addr" \
        "setValue(string,uint128,uint128)" \
        "ETH/USD" \
        "$eth_price_wei" \
        "$timestamp"; then
        log_warning "Failed to set initial ETH/USD price"
    else
        log_success "ETH/USD price initialized to \$2250"
    fi

    # Set initial BTC/USD price (around $45000)
    local btc_price_wei=$(python3 -c "print(int(45000 * 1e18))")
    local btc_timestamp=$(date +%s)

    log_info "Setting initial BTC/USD price..."
    if ! FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        "$oracle_addr" \
        "setValue(string,uint128,uint128)" \
        "BTC/USD" \
        "$btc_price_wei" \
        "$btc_timestamp"; then
        log_warning "Failed to set initial BTC/USD price"
    else
        log_success "BTC/USD price initialized to \$45000"
    fi
}

# Step 3: Create wallets and configurations
create_wallets_and_configs() {
    log_info "Step 3: Creating wallets and configurations..."

    # Create wallets
    create_wallets

    # Create service configurations
    create_attestor_env
    create_bridge_config
}

# Create wallets
create_wallets() {
    log_info "Creating secure wallets..."

    # Create attestor wallet
    if [ ! -f "${WALLETS_DIR}/attestor.key" ]; then
        echo "${DEFAULT_KEY}" > "${WALLETS_DIR}/attestor.key"
        chmod 600 "${WALLETS_DIR}/attestor.key"
        log_success "Attestor wallet created"
    else
        log_info "Attestor wallet already exists"
    fi

    # Create bridge wallet (same key for local dev)
    if [ ! -f "${WALLETS_DIR}/bridge.key" ]; then
        echo "${DEFAULT_KEY}" > "${WALLETS_DIR}/bridge.key"
        chmod 600 "${WALLETS_DIR}/bridge.key"
        log_success "Bridge wallet created"
    else
        log_info "Bridge wallet already exists"
    fi
}

# Create attestor environment
create_attestor_env() {
    log_info "Creating attestor environment configuration..."
    cat <<ENV > "${ATTESTOR_DIR}/.env"
RPC_URLS=http://host.docker.internal:8545
PRIVATE_KEY=${DEFAULT_KEY}
INTENT_REGISTRY_ADDRESS=$(cat "${REGISTRY_ADDR_FILE}")
SYMBOLS=BTC/USD,ETH/USD
POLLING_TIME=5s
BATCH_MODE=false
INTENT_TYPE=OracleUpdate
INTENT_VERSION=1.0
METRICS_PORT=8080
API_PORT=8081
ENV

    # Create config.yaml for local development
    cat <<YAML > "${ATTESTOR_DIR}/config-local.yaml"
# Attestor Service Configuration for Local Development

# RPC Configuration
rpc:
  url: http://host.docker.internal:8545
  urls:
    - http://host.docker.internal:8545
  registry_url: http://host.docker.internal:8545

# Oracle Configuration
oracle:
  address: "$(cat "${DIA_ORACLE_ADDR_FILE}")"

# Registry Configuration
registry:
  address: "$(cat "${REGISTRY_ADDR_FILE}")"

# Attestor Configuration
attestor:
  symbols:
    - BTC/USD
    - ETH/USD
  polling_time: 5s
  batch_mode: false
  intent_type: OracleUpdate
  intent_version: "1.0"

# Logging Configuration
logging:
  level: info

# Metrics Configuration
metrics:
  port: 8080

# API Server Configuration
api:
  port: 8081
YAML

    log_success "Attestor environment and config created"
}

# Create bridge config
create_bridge_config() {
    log_info "Creating bridge configuration..."
    cat <<JSON > "${BRIDGE_DIR}/config.json"
{
  "chains": [
    {
      "name": "anvil",
      "chain_id": 31337,
      "rpc": {
        "urls": ["http://host.docker.internal:8545"]
      }
    }
  ],
  "receiver_address": "$(cat "${RECEIVER_ADDR_FILE}")",
  "signer_private_key": "${DEFAULT_KEY}",
  "protocol_fee_hook": "$(cat "${PROTOCOL_FEE_HOOK_FILE}")"
}
JSON
    log_success "Bridge configuration created"
}

# Step 4: Start Docker services
start_docker_services() {
    log_info "Step 4: Starting Docker services..."

    # Export environment variables for docker-compose
    export INTENT_REGISTRY_ADDRESS=$(cat "${REGISTRY_ADDR_FILE}")
    export RECEIVER_ADDRESS=$(cat "${RECEIVER_ADDR_FILE}")
    export PROTOCOL_FEE_HOOK_ADDRESS=$(cat "${PROTOCOL_FEE_HOOK_FILE}")
    export PRIVATE_KEY="${DEFAULT_KEY}"

    # Start only the services (not anvil since we have it running on host)
    if ! docker compose -f "${COMPOSE_FILE}" up -d attestor bridge; then
        log_error "Failed to start Docker services"
        return 1
    fi

    log_success "Docker services started successfully"
}

# Wait for services to be healthy
wait_for_services() {
    log_info "Waiting for services to start..."
    sleep 5

    # Check if services are running
    if docker ps --filter "name=spectra-interoperability-attestor-1" --filter "status=running" | grep -q attestor; then
        log_success "Attestor service is running"
    else
        log_warning "Attestor service may still be starting"
    fi

    if docker ps --filter "name=spectra-interoperability-bridge-1" --filter "status=running" | grep -q bridge; then
        log_success "Bridge service is running"
    else
        log_warning "Bridge service may still be starting"
    fi
}

# Start price updater in background
start_price_updater() {
    log_info "🔄 Starting price updater for DIA Oracle..."

    local oracle_addr
    oracle_addr=$(cat "${DIA_ORACLE_ADDR_FILE}")

    # Start price updater in background
    nohup "${ROOT_DIR}/.temp/price-updater.sh" "$oracle_addr" "$DEFAULT_KEY" "$ANVIL_RPC" > "${ROOT_DIR}/.temp/price-updater.log" 2>&1 &
    PRICE_UPDATER_PID=$!

    # Store PID for cleanup
    echo "$PRICE_UPDATER_PID" > "${ROOT_DIR}/.temp/price-updater.pid"

    log_success "Price updater started with PID: $PRICE_UPDATER_PID"
    log_info "Price updates every 10 seconds for ETH/USD and BTC/USD"
}

# Display summary
show_summary() {
    echo ""
    log_success "🎉 Local development environment is ready!"
    echo ""
    echo "📋 Deployment Summary:"
    echo "  🔮 DIAOracleV2: $(cat "${DIA_ORACLE_ADDR_FILE}")"
    echo "  🏭 OracleIntentRegistry: $(cat "${REGISTRY_ADDR_FILE}")"
    echo "  💰 ProtocolFeeHook: $(cat "${PROTOCOL_FEE_HOOK_FILE}")"
    echo "  📡 PushOracleReceiverV2: $(cat "${RECEIVER_ADDR_FILE}")"
    echo "  💰 Receiver Balance: 1 ETH"
    echo ""
    echo "🔧 Configuration Files:"
    echo "  ⚖️  Attestor env: ${ATTESTOR_DIR}/.env"
    echo "  📋 Attestor config: ${ATTESTOR_DIR}/config-local.yaml"
    echo "  🌉 Bridge config: ${BRIDGE_DIR}/config.json"
    echo "  🔑 Wallets: ${WALLETS_DIR}/"
    echo ""
    echo "🐳 Docker Services:"
    echo "  📜 View logs: docker compose -f ${COMPOSE_FILE} logs -f"
    echo "  🛑 Stop services: docker compose -f ${COMPOSE_FILE} down"
    echo ""
    echo "🔗 Endpoints:"
    echo "  ⛏️  Anvil RPC: ${ANVIL_RPC}"
    echo "  📊 Attestor metrics: http://localhost:8080/metrics"
    echo "  🔍 Attestor API: http://localhost:8081/health"
    echo ""
    echo "📈 Oracle Information:"
    echo "  🔄 Price updates every 10 seconds (ETH/USD & BTC/USD)"
    echo "  📊 Price updater log: ${ROOT_DIR}/.temp/price-updater.log"
    echo ""
    log_info "Anvil is running with PID: $ANVIL_PID"
    if [ -n "${PRICE_UPDATER_PID:-}" ]; then
        log_info "Price updater is running with PID: $PRICE_UPDATER_PID"
    fi
    log_info "Press Ctrl+C to stop everything and exit"
}

# Main execution
main() {
    log_info "🚀 Starting Spectra Local Development Environment"
    echo ""

    # Step 1: Start Anvil blockchain
    start_anvil

    # Step 2: Deploy all smart contracts
    deploy_all_contracts

    # Step 3: Create wallets and configurations
    create_wallets_and_configs

    # Step 4: Start Docker services
    start_docker_services

    # Wait for services to be healthy
    wait_for_services

    # Start price updater
    start_price_updater

    # Show summary
    show_summary

    # Keep script running (Anvil in foreground)
    wait $ANVIL_PID
}

main "$@"