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

#######################################
# DEPENDENCY CHECKS
#######################################

check_dependencies() {
    log_info "Checking required dependencies..."
    local missing_deps=()

    # Check Docker
    if ! command -v docker &> /dev/null; then
        missing_deps+=("docker")
    else
        if ! docker info &> /dev/null; then
            log_error "Docker is installed but not running. Please start Docker Desktop."
            exit 1
        fi
        log_success "Docker: $(docker --version | cut -d' ' -f3 | tr -d ',')"
    fi

    # Check Docker Compose
    if ! docker compose version &> /dev/null; then
        missing_deps+=("docker-compose")
    else
        log_success "Docker Compose: $(docker compose version --short)"
    fi

    # Check Foundry tools (anvil, cast, forge)
    if ! command -v anvil &> /dev/null; then
        missing_deps+=("anvil (Foundry)")
    else
        log_success "Anvil: $(anvil --version 2>/dev/null | head -1 | cut -d' ' -f2)"
    fi

    if ! command -v cast &> /dev/null; then
        missing_deps+=("cast (Foundry)")
    else
        log_success "Cast: $(cast --version 2>/dev/null | head -1 | cut -d' ' -f2)"
    fi

    if ! command -v forge &> /dev/null; then
        missing_deps+=("forge (Foundry)")
    else
        log_success "Forge: $(forge --version 2>/dev/null | head -1 | cut -d' ' -f2)"
    fi

    # Check Python3
    if ! command -v python3 &> /dev/null; then
        missing_deps+=("python3")
    else
        log_success "Python3: $(python3 --version | cut -d' ' -f2)"
    fi

    # Check Go (optional, only needed for local builds)
    if command -v go &> /dev/null; then
        log_success "Go: $(go version | cut -d' ' -f3)"
    else
        log_warning "Go not found (optional, Docker builds will work)"
    fi

    # Report missing dependencies
    if [ ${#missing_deps[@]} -gt 0 ]; then
        log_error "Missing required dependencies:"
        for dep in "${missing_deps[@]}"; do
            echo "  - $dep"
        done
        echo ""
        log_info "Installation instructions:"
        echo "  Docker: https://docs.docker.com/get-docker/"
        echo "  Foundry: curl -L https://foundry.paradigm.xyz | bash && foundryup"
        echo "  Python3: brew install python3 (macOS) or apt install python3 (Linux)"
        exit 1
    fi

    log_success "All dependencies satisfied!"
    echo ""
}
CONTRACTS_DIR="${ROOT_DIR}/contracts"
LOCAL_CONTRACTS_DIR="${CONTRACTS_DIR}"
ATTESTOR_DIR="${ROOT_DIR}/services/attestor"
BRIDGE_DIR="${ROOT_DIR}/services/bridge"
COMPOSE_FILE="${ROOT_DIR}/docker-compose.local.yml"

ANVIL_RPC="http://localhost:8545"
DEFAULT_KEY="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

POSTGRES_HOST="postgres"
POSTGRES_PORT="5432"
POSTGRES_USER="bridge"
POSTGRES_PASSWORD="password"
POSTGRES_DB="oracle_bridge"
POSTGRES_DSN="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"

# Calculate the address from the private key
get_address_from_key() {
    local private_key="$1"
    FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast wallet address --private-key "$private_key"
}

# Get the default signer address
DEFAULT_ADDRESS=$(get_address_from_key "$DEFAULT_KEY")

LOCAL_STACK_DIR="${ROOT_DIR}/.local-stack"
CONTRACTS_ADDR_DIR="${LOCAL_STACK_DIR}/contracts"
WALLETS_DIR="${LOCAL_STACK_DIR}/wallets"
CONFIG_DIR="${LOCAL_STACK_DIR}/config"
REGISTRY_ADDR_FILE="${CONTRACTS_ADDR_DIR}/oracle_intent_registry.addr"
RECEIVER_ADDR_FILE="${CONTRACTS_ADDR_DIR}/push_oracle_receiver_v2.addr"
PROTOCOL_FEE_HOOK_FILE="${CONTRACTS_ADDR_DIR}/protocol_fee_hook.addr"
DIA_ORACLE_ADDR_FILE="${CONTRACTS_ADDR_DIR}/dia_oracle_v2.addr"
DIA_ORACLE_RANDOMNESS_ADDR_FILE="${CONTRACTS_ADDR_DIR}/dia_oracle_randomness.addr"
RANDOM_REQUEST_MANAGER_ADDR_FILE="${CONTRACTS_ADDR_DIR}/random_request_manager.addr"

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

# Flag to indicate if we started services (for cleanup)
SERVICES_STARTED=false

# Cleanup function
cleanup() {
    local exit_code=$?
    
    # Only cleanup if services were started
    if [ "$SERVICES_STARTED" = "true" ]; then
        log_info "Cleaning up..."
        
        # Stop Docker services
        docker compose -f "${COMPOSE_FILE}" down --remove-orphans 2>/dev/null || true
        
        # Stop Anvil
        if [ -n "${ANVIL_PID:-}" ]; then
            kill $ANVIL_PID 2>/dev/null || true
        fi
        # Also kill any running anvil processes on port 8545
        pkill -f "anvil.*8545" 2>/dev/null || true
        
        # Stop price updater
        if [ -n "${PRICE_UPDATER_PID:-}" ]; then
            kill $PRICE_UPDATER_PID 2>/dev/null || true
        fi
        # Also try to stop from PID file
        if [ -f "${ROOT_DIR}/.temp/price-updater.pid" ]; then
            kill $(cat "${ROOT_DIR}/.temp/price-updater.pid") 2>/dev/null || true
            rm -f "${ROOT_DIR}/.temp/price-updater.pid"
        fi
        
        if [ $exit_code -ne 0 ]; then
            log_error "Script exited with error code: $exit_code"
        else
            log_success "Cleanup complete"
        fi
    fi
    exit $exit_code
}

trap cleanup EXIT INT TERM

# Stop command for easy cleanup
stop_all() {
    log_info "Stopping all services..."
    
    # Stop Docker services
    docker compose -f "${COMPOSE_FILE}" down --remove-orphans 2>/dev/null || true
    
    # Stop Anvil
    pkill -f "anvil.*8545" 2>/dev/null || true
    
    # Stop price updater
    if [ -f "${ROOT_DIR}/.temp/price-updater.pid" ]; then
        kill $(cat "${ROOT_DIR}/.temp/price-updater.pid") 2>/dev/null || true
        rm -f "${ROOT_DIR}/.temp/price-updater.pid"
    fi
    
    log_success "All services stopped"
}



# Step 1: Start Anvil
start_anvil() {
    log_info "Step 1: Starting Anvil blockchain..."

    # Kill any existing anvil processes
    pkill -f "anvil.*8545" || true
    sleep 2

    # Start anvil in background with WebSocket support
    # --ipc enables IPC, which is needed for WebSocket subscriptions to work
    anvil --host 0.0.0.0 --port 8545 --chain-id 31337 --balance 10000 --ipc &
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

# Step 2: Clone contracts and deploy
setup_and_deploy_contracts() {
    log_info "Step 2: Setting up contracts repository and deploying..."

    # Create local stack directories
    mkdir -p "${LOCAL_STACK_DIR}" "${CONTRACTS_ADDR_DIR}" "${WALLETS_DIR}" "${CONFIG_DIR}" "${ROOT_DIR}/.temp"

    # Deploy all contracts
    deploy_all_contracts
}


# Deploy all contracts
deploy_all_contracts() {
    log_info "Deploying smart contracts..."

    # Deploy contracts in correct order
    deploy_dia_oracle
    deploy_registry
    deploy_protocol_fee_hook
    deploy_receiver
    deploy_dia_oracle_randomness
    deploy_random_request_manager
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
        --constructor-args "DIA Oracle" "1.0" 2>&1); then
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
    # IMPORTANT: Use same domain parameters as OracleIntentRegistry for EIP-712 signature verification
    # Domain: "DIA Oracle" version "1.0", chainId 31337, verifyingContract = registry_addr
    if ! output=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 forge create \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        --broadcast \
        "contracts/PushOracleReceiverV2.sol:PushOracleReceiverV2" \
        --constructor-args "DIA Oracle" "1.0" 31337 "$registry_addr" 2>&1); then
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

    # Verify domain separator matches by getting it from both contracts
    log_info "Verifying domain separator consistency..."
    local registry_domain receiver_domain

    registry_domain=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast call \
        --rpc-url "${ANVIL_RPC}" \
        "$registry_addr" \
        "domainSeparator()(bytes32)" 2>/dev/null || echo "")

    receiver_domain=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast call \
        --rpc-url "${ANVIL_RPC}" \
        "$address" \
        "domainSeparator()(bytes32)" 2>/dev/null || echo "")

    if [ -n "$registry_domain" ] && [ -n "$receiver_domain" ]; then
        if [ "$registry_domain" = "$receiver_domain" ]; then
            log_success "✅ Domain separators match: $registry_domain"
        else
            log_error "❌ Domain separator mismatch!"
            log_error "   Registry: $registry_domain"
            log_error "   Receiver: $receiver_domain"
            log_error "   Signatures will fail - intents cannot be verified!"
            return 1
        fi
    else
        log_warning "⚠️  Could not verify domain separators (contracts may not expose domainSeparator())"
    fi
}

# Deploy DIAOracleRandomness
deploy_dia_oracle_randomness() {
    log_info "🚀 Deploying DIAOracleRandomness..."
    local output
    cd "${CONTRACTS_DIR}"
    if ! output=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 forge create \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        --broadcast \
        "contracts/VRF/DIAOracleRandomness.sol:DIAOracleRandomness" 2>&1); then
        log_error "Failed to deploy DIAOracleRandomness"
        echo "$output" >&2
        return 1
    fi

    local address
    address=$(echo "$output" | awk '/Deployed to:/ {print $3}')
    if [ -z "$address" ]; then
        log_error "Failed to capture DIAOracleRandomness address"
        echo "$output" >&2
        return 1
    fi

    echo "$address" > "${DIA_ORACLE_RANDOMNESS_ADDR_FILE}"
    log_success "DIAOracleRandomness deployed at $address"
}

# Deploy RandomRequestManager
deploy_random_request_manager() {
    log_info "🚀 Deploying RandomRequestManager..."
    local output
    cd "${CONTRACTS_DIR}"
    if ! output=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 forge create \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        --broadcast \
        "contracts/VRF/RandomRequestManager.sol:RandomRequestManager" 2>&1); then
        log_error "Failed to deploy RandomRequestManager"
        echo "$output" >&2
        return 1
    fi

    local address
    address=$(echo "$output" | awk '/Deployed to:/ {print $3}')
    if [ -z "$address" ]; then
        log_error "Failed to capture RandomRequestManager address"
        echo "$output" >&2
        return 1
    fi

    echo "$address" > "${RANDOM_REQUEST_MANAGER_ADDR_FILE}"
    log_success "RandomRequestManager deployed at $address"
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
    log_info "Authorizing signer in registry ($DEFAULT_ADDRESS)..."
    if ! FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        "$registry_addr" \
        "setSignerAuthorization(address,bool)" \
        "$DEFAULT_ADDRESS" \
        true; then
        log_warning "Failed to authorize signer (method might not exist)"
    else
        log_success "Signer authorized in registry: $DEFAULT_ADDRESS"
    fi

    # Authorize signer in PushOracleReceiverV2
    log_info "Authorizing signer in PushOracleReceiverV2 ($DEFAULT_ADDRESS)..."
    if ! FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        "$receiver_addr" \
        "setSignerAuthorization(address,bool)" \
        "$DEFAULT_ADDRESS" \
        true; then
        log_warning "Failed to authorize signer in receiver (method might not exist)"
    else
        log_success "Signer authorized in PushOracleReceiverV2: $DEFAULT_ADDRESS"
    fi
}

# Fund PushOracleReceiverV2
fund_receiver() {
    log_info "💰 Funding PushOracleReceiverV2 contract..."

    local receiver_addr
    receiver_addr=$(cat "${RECEIVER_ADDR_FILE}")

    # Fund the receiver with 100 ETH for extensive testing
    if ! FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send \
        --rpc-url "${ANVIL_RPC}" \
        --private-key "${DEFAULT_KEY}" \
        --value "100ether" \
        "$receiver_addr"; then
        log_error "Failed to fund PushOracleReceiverV2"
        return 1
    fi

    log_success "PushOracleReceiverV2 funded with 100 ETH"
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
    cat <<ENV > "${CONFIG_DIR}/attestor.env"
ATTESTOR_RPC_URLS=http://host.docker.internal:8545
ATTESTOR_ORACLE_ADDRESS=$(cat "${DIA_ORACLE_ADDR_FILE}")
ATTESTOR_ORACLE_CLIENT_TYPE=dia_v2
ATTESTOR_REGISTRY_ADDRESS=$(cat "${REGISTRY_ADDR_FILE}")
ATTESTOR_ATTESTOR_PRIVATE_KEY=${DEFAULT_KEY}
ATTESTOR_ATTESTOR_SYMBOLS=BTC/USD,ETH/USD
ATTESTOR_ATTESTOR_POLLING_TIME=5s
ATTESTOR_ATTESTOR_BATCH_MODE=false
ATTESTOR_ATTESTOR_MODE=prime
ATTESTOR_ATTESTOR_INTENT_TYPE=OracleUpdate
ATTESTOR_ATTESTOR_INTENT_VERSION=1.0
ATTESTOR_METRICS_PORT=8080
ATTESTOR_API_PORT=8081
ENV

    # Create config.yaml for local development
    cat <<YAML > "${CONFIG_DIR}/attestor-local.yaml"
# Attestor Service Configuration for Local Development

# RPC Configuration
rpc:
  url: http://host.docker.internal:8545
  urls:
    - http://host.docker.internal:8545
  registry_url: http://host.docker.internal:8545
  registry_urls:
    - http://host.docker.internal:8545

# Oracle Configuration
oracle:
  address: "$(cat "${DIA_ORACLE_ADDR_FILE}")"
  client_type: "dia_v2"

# Registry Configuration
registry:
  address: "$(cat "${REGISTRY_ADDR_FILE}")"

# Attestor Configuration
attestor:
  private_key: "${DEFAULT_KEY}"
  symbols:
    - BTC/USD
    - ETH/USD
  polling_time: 5s
  batch_mode: false
  mode: prime

# Logging Configuration
logging:
  level: debug

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
    log_info "Creating bridge modular YAML configuration..."

    # Create modular config directory structure
    local BRIDGE_CONFIG_DIR="${CONFIG_DIR}/bridge-modular"
    local ROUTERS_DIR="${BRIDGE_CONFIG_DIR}/routers"
    mkdir -p "${ROUTERS_DIR}"

    # 1. Create infrastructure.yaml
    cat <<YAML > "${BRIDGE_CONFIG_DIR}/infrastructure.yaml"
database:
    driver: postgres
    dsn_env: DATABASE_DSN
source:
    chain_id: 31337
    name: Anvil Local
    rpc_urls:
        - env:SOURCE_RPC_URL
    ws_url: ws://host.docker.internal:8545
    start_block: 0
private_key_env: PRIVATE_KEY
event_monitor:
    enabled: true
    reconnectinterval: 5s
    maxreconnectattempts: 10
block_scanner:
    enabled: true
    scaninterval: 10s
    blockrange: 100
    maxblockgap: 1000
    backwardsync: true
event_processor:
    batchsize: 10
    validationtimeout: 30s
    dedupcachesize: 1000
    dedupcachettl: 1h
    enableparallelmode: false
worker_pool:
    maxworkers: 5
    taskqueuesize: 100
    tasktimeout: 2m
    retrydelay: 10s
    maxretries: 3
health_check:
    enabled: true
    checkinterval: 30s
    timeout: 10s
    maxprocessinglag: 2m
    maxqueuesize: 50
recovery:
    enabled: true
    minfailures: 3
    maxattempts: 5
    retryinterval: 30s
    recoverytimeout: 5m
api:
    enabled: true
    listenaddr: :8080
    enablecors: true
metrics:
    enabled: true
    namespace: oracle_bridge
dry_run: false
YAML

    # 2. Create chains.yaml
    cat <<YAML > "${BRIDGE_CONFIG_DIR}/chains.yaml"
chains:
    "31337":
        chain_id: 31337
        name: Anvil Local
        rpc_urls:
            - http://host.docker.internal:8545
        enabled: true
        default_gas_limit: 300000
        gas_multiplier: 1.2
        max_gas_price: "100000000000"
YAML

    # 3. Create contracts.yaml
    cat <<YAML > "${BRIDGE_CONFIG_DIR}/contracts.yaml"
contracts:
    push_oracle_receiver:
        chain_id: 31337
        address: $(cat "${RECEIVER_ADDR_FILE}")
        type: pushoracle
        enabled: true
        abi: '[{"name":"handleIntentUpdate","type":"function","inputs":[{"name":"intent","type":"tuple","components":[{"name":"intentType","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"nonce","type":"uint256"},{"name":"expiry","type":"uint256"},{"name":"symbol","type":"string"},{"name":"price","type":"uint256"},{"name":"timestamp","type":"uint256"},{"name":"source","type":"string"},{"name":"signature","type":"bytes"},{"name":"signer","type":"address"}]}]}]'
        gas_limit: 300000
        gas_multiplier: 1.2
        max_gas_price: "100000000000"
        methods:
            intent_update:
                methodname: handleIntentUpdate
                fieldsmapping:
                    intent: fullIntent
                gaslimit: 300000
    random_request_manager:
        chain_id: 31337
        address: $(cat "${RANDOM_REQUEST_MANAGER_ADDR_FILE}")
        type: randomness
        enabled: true
        abi: '[{"name":"fulfillRandomInt","type":"function","inputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"}]}]'
        gas_limit: 500000
        gas_multiplier: 1.2
        max_gas_price: "100000000000"
        methods:
            fulfill_random:
                methodname: fulfillRandomInt
                fieldsmapping:
                    requestId: requestId
                    randomInts: randomInts
                gaslimit: 500000
YAML

    # 4. Create events.yaml
    cat <<YAML > "${BRIDGE_CONFIG_DIR}/events.yaml"
event_definitions:
    IntentRegistered:
        contract: $(cat "${REGISTRY_ADDR_FILE}")
        abi: '{"name":"IntentRegistered","type":"event","inputs":[{"name":"intentHash","type":"bytes32","indexed":true},{"name":"symbol","type":"string","indexed":true},{"name":"price","type":"uint256","indexed":true},{"name":"timestamp","type":"uint256","indexed":false},{"name":"signer","type":"address","indexed":false}]}'
        dataextraction:
            intentHash: topics[1]
            symbol: topics[2]
            price: topics[3]
            timestamp: timestamp
            signer: signer
        enrichment:
            contract: ""
            method: getIntent
            abi: '{"name":"getIntent","type":"function","inputs":[{"name":"intentHash","type":"bytes32"}],"outputs":[{"name":"intent","type":"tuple","components":[{"name":"intentType","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"nonce","type":"uint256"},{"name":"expiry","type":"uint256"},{"name":"symbol","type":"string"},{"name":"price","type":"uint256"},{"name":"timestamp","type":"uint256"},{"name":"source","type":"string"},{"name":"signature","type":"bytes"},{"name":"signer","type":"address"}]}]}'
            params:
                - \${event.intentHash}
            returns:
                fullIntent: "0"
    IntArraySet:
        contract: $(cat "${DIA_ORACLE_RANDOMNESS_ADDR_FILE}")
        abi: '{"name":"IntArraySet","type":"event","inputs":[{"name":"requestId","type":"uint256","indexed":false},{"name":"round","type":"int256","indexed":true},{"name":"seed","type":"string","indexed":false},{"name":"signature","type":"string","indexed":false}]}'
        dataextraction:
            requestId: requestId
            round: topics[1]
            seed: seed
            signature: signature
        enrichment:
            contract: ""
            method: getIntArray
            abi: '{"name":"getIntArray","type":"function","inputs":[{"name":"requestId_","type":"uint256"}],"outputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"},{"name":"round","type":"int64"},{"name":"seed","type":"string"},{"name":"signature","type":"string"}]}'
            params:
                - \${event.requestId}
            returns:
                randomInts: "1"
                fullRound: "2"
                fullSeed: "3"
                fullSignature: "4"
YAML

    # 5. Create router configs
    local router_files=(
        "oracle_intent_router_btc.yaml"
        "oracle_intent_router_eth.yaml"
        "oracle_intent_router_sol.yaml"
        "oracle_intent_router.yaml"
        "randomness_router.yaml"
    )
    local router_names=(
        "oracle_intent_router_btc"
        "oracle_intent_router_eth"
        "oracle_intent_router_sol"
        "oracle_intent_router"
        "randomness_router_local"
    )
    local router_ids=(
        "oracle_intent_router_btc_001"
        "oracle_intent_router_eth_001"
        "oracle_intent_router_sol_001"
        "oracle_intent_router_001"
        "randomness_router_001"
    )
    local router_thresholds=("1s" "1s" "1s" "2s" "1s")
    local router_conditions=(
        $'        conditions:\n            - field: ${enrichment.fullIntent.Symbol}\n              operator: eq\n              value: BTC/USD\n'
        $'        conditions:\n            - field: ${enrichment.fullIntent.Symbol}\n              operator: eq\n              value: ETH/USD\n'
        $'        conditions:\n            - field: ${enrichment.fullIntent.Symbol}\n              operator: eq\n              value: SOL/USD\n'
        $'        conditions:\n            - field: ${enrichment.fullIntent.Symbol}\n              operator: ne\n              value: BTC/USD\n            - field: ${enrichment.fullIntent.Symbol}\n              operator: ne\n              value: ETH/USD\n            - field: ${enrichment.fullIntent.Symbol}\n              operator: ne\n              value: SOL/USD\n'
        $'        conditions: []\n'
    )

    for ((i = 0; i < ${#router_files[@]}; i++)); do
        local file_path="${ROUTERS_DIR}/${router_files[$i]}"
        local router_name="${router_names[$i]}"
        local router_id="${router_ids[$i]}"
        local time_threshold="${router_thresholds[$i]}"
        local conditions_block="${router_conditions[$i]}"

        # Special handling for randomness router
        if [ "$router_name" = "randomness_router_local" ]; then
            cat <<YAML > "${file_path}"
router:
    id: ${router_id}
    name: ${router_name}
    type: event
    enabled: true
    private_key_env: PRIVATE_KEY
    triggers:
        events:
            - IntArraySet
${conditions_block}
    processing:
        datasource: enrichment
        transformations: []
        validationenabled: true
    destinations:
        - contract_ref: random_request_manager
          time_threshold: ${time_threshold}
          method:
            name: fulfillRandomInt
            abi: '{"name":"fulfillRandomInt","type":"function","inputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"}]}'
            params:
                requestId: \${event.requestId}
                randomInts: \${enrichment.randomInts}
            value: "0"
            gaslimit: 500000
            gasmultiplier: 1.2
YAML
        else
            cat <<YAML > "${file_path}"
router:
    id: ${router_id}
    name: ${router_name}
    type: event
    enabled: true
    private_key_env: PRIVATE_KEY
    triggers:
        events:
            - IntentRegistered
${conditions_block}
    processing:
        datasource: enrichment
        transformations: []
        validationenabled: true
    destinations:
        - contract_ref: push_oracle_receiver
          time_threshold: ${time_threshold}
          price_deviation: "0.5%"
          method:
            name: handleIntentUpdate
            abi: '{"name":"handleIntentUpdate","type":"function","inputs":[{"name":"intent","type":"tuple","components":[{"name":"intentType","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"nonce","type":"uint256"},{"name":"expiry","type":"uint256"},{"name":"symbol","type":"string"},{"name":"price","type":"uint256"},{"name":"timestamp","type":"uint256"},{"name":"source","type":"string"},{"name":"signature","type":"bytes"},{"name":"signer","type":"address"}]}]}'
            params:
                intent: \${enrichment.fullIntent}
            value: "0"
            gaslimit: 300000
            gasmultiplier: 1.2
YAML
        fi
    done

    log_success "Bridge modular YAML configuration created at ${BRIDGE_CONFIG_DIR} with ${#router_files[@]} routers"
}

# Step 4: Start Docker services
start_docker_services() {
    log_info "Step 4: Starting Docker services..."

    # Export environment variables for docker-compose
    export INTENT_REGISTRY_ADDRESS=$(cat "${REGISTRY_ADDR_FILE}")
    export RECEIVER_ADDRESS=$(cat "${RECEIVER_ADDR_FILE}")
    export PROTOCOL_FEE_HOOK_ADDRESS=$(cat "${PROTOCOL_FEE_HOOK_FILE}")
    export PRIVATE_KEY="${DEFAULT_KEY}"
    export POSTGRES_HOST="${POSTGRES_HOST}"
    export POSTGRES_PORT="${POSTGRES_PORT}"
    export POSTGRES_USER="${POSTGRES_USER}"
    export POSTGRES_PASSWORD="${POSTGRES_PASSWORD}"
    export POSTGRES_DB="${POSTGRES_DB}"
    export POSTGRES_DSN="${POSTGRES_DSN}"

    # Build images first
    log_info "Building Docker images..."
    if ! docker compose -f "${COMPOSE_FILE}" build --no-cache attestor bridge; then
        log_error "Failed to build Docker images"
        return 1
    fi
    log_success "Docker images built successfully"

    # Start all services (not anvil since we have it running on host)
    if ! docker compose -f "${COMPOSE_FILE}" up -d postgres attestor bridge; then
        log_error "Failed to start Docker services"
        return 1
    fi

    log_success "Docker services started successfully"
}

# Wait for services to be healthy with proper health checks
wait_for_services() {
    log_info "Waiting for services to be healthy..."

    # Wait for Postgres
    log_info "Waiting for Postgres..."
    local postgres_ready=false
    for i in {1..30}; do
        if docker exec spectra-interoperability-postgres-1 pg_isready -U bridge &>/dev/null; then
            postgres_ready=true
            break
        fi
        sleep 1
    done
    if $postgres_ready; then
        log_success "Postgres is healthy"
    else
        log_error "Postgres failed to become healthy"
        docker logs spectra-interoperability-postgres-1 --tail 20 2>/dev/null || true
        return 1
    fi

    # Wait for Attestor
    log_info "Waiting for Attestor..."
    local attestor_ready=false
    for i in {1..60}; do
        if curl -s http://localhost:8081/health &>/dev/null; then
            attestor_ready=true
            break
        fi
        # Check if container is still running
        if ! docker ps --filter "name=spectra-interoperability-attestor-1" --filter "status=running" | grep -q attestor; then
            log_error "Attestor container exited unexpectedly"
            docker logs spectra-interoperability-attestor-1 --tail 30 2>/dev/null || true
            return 1
        fi
        sleep 2
    done
    if $attestor_ready; then
        log_success "Attestor is healthy"
    else
        log_warning "Attestor health endpoint not responding (may still be starting)"
        docker logs spectra-interoperability-attestor-1 --tail 10 2>/dev/null || true
    fi

    # Wait for Bridge
    log_info "Waiting for Bridge..."
    local bridge_ready=false
    for i in {1..60}; do
        if curl -s http://localhost:8082/metrics &>/dev/null; then
            bridge_ready=true
            break
        fi
        # Check if container is still running
        if ! docker ps --filter "name=spectra-interoperability-bridge-1" --filter "status=running" | grep -q bridge; then
            log_error "Bridge container exited unexpectedly"
            docker logs spectra-interoperability-bridge-1 --tail 30 2>/dev/null || true
            return 1
        fi
        sleep 2
    done
    if $bridge_ready; then
        log_success "Bridge is healthy"
    else
        log_warning "Bridge metrics endpoint not responding (may still be starting)"
        docker logs spectra-interoperability-bridge-1 --tail 10 2>/dev/null || true
    fi

    echo ""
    log_success "All core services are running!"
}

# Health check function to verify system is operational
run_health_checks() {
    log_info "Running system health checks..."
    local all_healthy=true

    # Check Anvil RPC
    if curl -s -X POST -H "Content-Type: application/json" \
       --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
       "$ANVIL_RPC" &>/dev/null; then
        log_success "Anvil RPC: OK"
    else
        log_error "Anvil RPC: FAILED"
        all_healthy=false
    fi

    # Check Postgres
    if docker exec spectra-interoperability-postgres-1 pg_isready -U bridge &>/dev/null; then
        log_success "Postgres: OK"
    else
        log_error "Postgres: FAILED"
        all_healthy=false
    fi

    # Check contract deployments
    local registry_addr=$(cat "${REGISTRY_ADDR_FILE}" 2>/dev/null || echo "")
    if [ -n "$registry_addr" ]; then
        local code=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast code --rpc-url "$ANVIL_RPC" "$registry_addr" 2>/dev/null || echo "0x")
        if [ "$code" != "0x" ] && [ -n "$code" ]; then
            log_success "Contracts deployed: OK"
        else
            log_error "Contracts: No code at registry address"
            all_healthy=false
        fi
    else
        log_error "Contracts: Registry address file not found"
        all_healthy=false
    fi

    # Check Docker services
    local services=("postgres" "attestor" "bridge")
    for svc in "${services[@]}"; do
        if docker ps --filter "name=spectra-interoperability-${svc}-1" --filter "status=running" | grep -q "$svc"; then
            log_success "Docker $svc: Running"
        else
            log_warning "Docker $svc: Not running"
        fi
    done

    if $all_healthy; then
        log_success "All health checks passed!"
    else
        log_warning "Some health checks failed"
    fi
}

# Start price updater in background
start_price_updater() {
    log_info "🔄 Starting price updater for DIA Oracle..."

    local oracle_addr
    oracle_addr=$(cat "${DIA_ORACLE_ADDR_FILE}")

    # Start price updater in background
    nohup "${ROOT_DIR}/scripts/price-updater.sh" "$oracle_addr" "$DEFAULT_KEY" "$ANVIL_RPC" > "${ROOT_DIR}/.temp/price-updater.log" 2>&1 &
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
    echo "  🎲 DIAOracleRandomness: $(cat "${DIA_ORACLE_RANDOMNESS_ADDR_FILE}")"
    echo "  🎯 RandomRequestManager: $(cat "${RANDOM_REQUEST_MANAGER_ADDR_FILE}")"
    echo "  💰 Receiver Balance: 100 ETH"
    echo "  🔑 Authorized Signer: $DEFAULT_ADDRESS"
    echo "  🗄️  Postgres DSN: ${POSTGRES_DSN}"
    echo ""
    echo "🔐 EIP-712 Domain Configuration:"
    local registry_addr receiver_addr registry_domain
    registry_addr=$(cat "${REGISTRY_ADDR_FILE}")
    receiver_addr=$(cat "${RECEIVER_ADDR_FILE}")
    registry_domain=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast call \
        --rpc-url "${ANVIL_RPC}" \
        "$registry_addr" \
        "domainSeparator()(bytes32)" 2>/dev/null || echo "N/A")
    echo "  📝 Domain Name: DIA Oracle"
    echo "  🔢 Domain Version: 1.0"
    echo "  🔗 Chain ID: 31337"
    echo "  ✍️  Verifying Contract: $registry_addr (OracleIntentRegistry)"
    echo "  🔑 Domain Separator: $registry_domain"
    echo ""
    echo "🔧 Configuration Files:"
    echo "  ⚖️  Attestor env: ${CONFIG_DIR}/attestor.env"
    echo "  📋 Attestor config: ${CONFIG_DIR}/attestor-local.yaml"
    echo "  🌉 Bridge config: ${CONFIG_DIR}/bridge-modular/"
    echo "    ├── infrastructure.yaml"
    echo "    ├── chains.yaml"
    echo "    ├── contracts.yaml"
    echo "    ├── events.yaml"
    echo "    └── routers/"
    echo "        ├── oracle_intent_router_btc.yaml"
    echo "        ├── oracle_intent_router_eth.yaml"
    echo "        ├── oracle_intent_router_sol.yaml"
    echo "        ├── oracle_intent_router.yaml"
    echo "        └── randomness_router.yaml"
    echo "  📄 Contract addresses: ${CONTRACTS_ADDR_DIR}/"
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
    echo "  🔍 Bridge metrics: http://localhost:8082/metrics"
    echo ""
    echo "📈 Oracle Information:"
    echo "  🔄 Price updates every 10 seconds (ETH/USD & BTC/USD)"
    echo "  📊 Price updater log: ${ROOT_DIR}/.temp/price-updater.log"
    echo ""
    echo "🌉 Bridge Router Configuration:"
    echo "  📝 Events:"
    echo "    - IntentRegistered from OracleIntentRegistry"
    echo "    - IntArraySet from DIAOracleRandomness"
    echo "  🎯 Destinations:"
    echo "    - PushOracleReceiverV2.handleIntentUpdate()"
    echo "    - RandomRequestManager.fulfillRandomInt()"
    echo "  🧭 Routers: BTC, ETH, SOL dedicated routers, fallback router, and randomness router"
    echo "  🔐 Router Signer: $DEFAULT_ADDRESS (authorized in registry)"
    echo ""
    log_info "Anvil is running with PID: $ANVIL_PID"
    if [ -n "${PRICE_UPDATER_PID:-}" ]; then
        log_info "Price updater is running with PID: $PRICE_UPDATER_PID"
    fi
    log_info "Press Ctrl+C to stop everything and exit"
}

# Show usage
show_usage() {
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  start     Start the local development environment (default)"
    echo "  stop      Stop all running services"
    echo "  status    Show status of all services"
    echo "  logs      Show logs from Docker services"
    echo "  help      Show this help message"
}

# Show status of services
show_status() {
    log_info "Service Status:"
    echo ""
    
    # Check Anvil
    if curl -s -X POST -H "Content-Type: application/json" \
       --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
       "$ANVIL_RPC" &>/dev/null; then
        log_success "Anvil: Running at $ANVIL_RPC"
    else
        log_error "Anvil: Not running"
    fi
    
    # Check Docker services
    echo ""
    docker compose -f "${COMPOSE_FILE}" ps 2>/dev/null || log_warning "Docker compose not running"
}

# Show logs
show_logs() {
    docker compose -f "${COMPOSE_FILE}" logs -f --tail 100
}

# Main execution
main() {
    local command="${1:-start}"
    
    case "$command" in
        start)
            log_info "🚀 Starting Spectra Local Development Environment"
            echo ""

            # Mark that we're starting services (for cleanup)
            SERVICES_STARTED=true

            # Step 0: Check dependencies
            check_dependencies

            # Step 1: Start Anvil blockchain
            start_anvil

            # Step 2: Set up contracts and deploy
            setup_and_deploy_contracts

            # Step 3: Create wallets and configurations
            create_wallets_and_configs

            # Step 4: Start Docker services
            start_docker_services

            # Wait for services to be healthy
            wait_for_services

            # Run health checks
            run_health_checks

            # Start price updater
            start_price_updater

            # Show summary
            show_summary

            # Keep script running (Anvil in foreground)
            wait $ANVIL_PID
            ;;
        stop)
            stop_all
            ;;
        status)
            show_status
            ;;
        logs)
            show_logs
            ;;
        help|--help|-h)
            show_usage
            ;;
        *)
            log_error "Unknown command: $command"
            show_usage
            exit 1
            ;;
    esac
}

main "$@"
