#!/usr/bin/env bash
set -euo pipefail

#######################################
# Multi-Chain Local Development Setup
# Starts 10 chains with 12 PushOracleReceiverV2 contracts
#######################################

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Global variables
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTRACTS_DIR="${ROOT_DIR}/contracts"
COMPOSE_FILE="${ROOT_DIR}/docker-compose.local.yml"

# Multi-chain configuration
# Format: CHAIN_ID:PORT:NAME:NUM_RECEIVERS
# 10 chains, 12 receivers total (some chains have 2 receivers)
CHAINS=(
    "31337:8545:anvil-main:2"      # Main chain with 2 receivers
    "31338:8546:anvil-eth:1"       # ETH L2
    "31339:8547:anvil-arb:2"       # Arbitrum-like with 2 receivers
    "31340:8548:anvil-opt:1"       # Optimism-like
    "31341:8549:anvil-base:1"      # Base-like
    "31342:8550:anvil-poly:1"      # Polygon-like
    "31343:8551:anvil-avax:1"      # Avalanche-like
    "31344:8552:anvil-bsc:1"       # BSC-like
    "31345:8553:anvil-ftm:1"       # Fantom-like
    "31346:8554:anvil-zksync:1"    # zkSync-like
)

DEFAULT_KEY="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

# Postgres config
POSTGRES_HOST="localhost"
POSTGRES_PORT="5432"
POSTGRES_USER="bridge"
POSTGRES_PASSWORD="password"
POSTGRES_DB="oracle_bridge"

# Local stack directories
LOCAL_STACK_DIR="${ROOT_DIR}/.local-stack"
CONTRACTS_ADDR_DIR="${LOCAL_STACK_DIR}/contracts"
MULTICHAIN_DIR="${LOCAL_STACK_DIR}/multichain"
CONFIG_DIR="${LOCAL_STACK_DIR}/config"
WALLETS_DIR="${LOCAL_STACK_DIR}/wallets"
PIDS_DIR="${LOCAL_STACK_DIR}/pids"

# Track started processes
declare -a ANVIL_PIDS=()
SERVICES_STARTED=false

#######################################
# Logging Functions
#######################################

timestamp() {
    date -u +"%Y-%m-%dT%H:%M:%SZ"
}

log_info() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${CYAN}[INFO]${NC} $1"
}

log_success() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${GREEN}[OK]${NC} $1"
}

log_warning() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${RED}[ERROR]${NC} $1" >&2
}

log_chain() {
    local chain_id="$1"
    local message="$2"
    echo -e "${BLUE}[$(timestamp)]${NC} ${CYAN}[Chain $chain_id]${NC} $message"
}

#######################################
# Cleanup
#######################################

cleanup() {
    local exit_code=$?
    
    if [ "$SERVICES_STARTED" = "true" ]; then
        log_info "Cleaning up..."
        
        # Stop all Anvil processes
        for pid in "${ANVIL_PIDS[@]}"; do
            kill "$pid" 2>/dev/null || true
        done
        
        # Kill any remaining anvil processes
        pkill -f "anvil.*854" 2>/dev/null || true
        
        # Stop Docker services
        docker compose -f "${COMPOSE_FILE}" down --remove-orphans 2>/dev/null || true
        
        # Stop price updater
        if [ -f "${PIDS_DIR}/price-updater.pid" ]; then
            kill $(cat "${PIDS_DIR}/price-updater.pid") 2>/dev/null || true
        fi
        
        log_success "Cleanup complete"
    fi
    exit $exit_code
}

trap cleanup EXIT INT TERM

#######################################
# Dependency Checks
#######################################

check_dependencies() {
    log_info "Checking dependencies..."
    local missing=()
    
    command -v docker &>/dev/null || missing+=("docker")
    command -v anvil &>/dev/null || missing+=("anvil")
    command -v cast &>/dev/null || missing+=("cast")
    command -v forge &>/dev/null || missing+=("forge")
    command -v python3 &>/dev/null || missing+=("python3")
    
    if [ ${#missing[@]} -gt 0 ]; then
        log_error "Missing dependencies: ${missing[*]}"
        exit 1
    fi
    
    if ! docker info &>/dev/null; then
        log_error "Docker is not running"
        exit 1
    fi
    
    log_success "All dependencies satisfied"
}

#######################################
# Initialize Directories
#######################################

init_directories() {
    log_info "Initializing directories..."
    
    mkdir -p "$LOCAL_STACK_DIR"
    mkdir -p "$CONTRACTS_ADDR_DIR"
    mkdir -p "$MULTICHAIN_DIR"
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$WALLETS_DIR"
    mkdir -p "$PIDS_DIR"
    mkdir -p "${LOCAL_STACK_DIR}/logs"
    
    # Create chain-specific directories
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        mkdir -p "${MULTICHAIN_DIR}/${chain_id}"
    done
    
    log_success "Directories initialized"
}

#######################################
# Start Anvil Chains
#######################################

start_anvil_chain() {
    local chain_id="$1"
    local port="$2"
    local name="$3"
    
    log_chain "$chain_id" "Starting $name on port $port..."
    
    # Kill any existing process on this port
    pkill -f "anvil.*--port $port" 2>/dev/null || true
    sleep 1
    
    # Start anvil with low base fee for local testing
    anvil --host 0.0.0.0 --port "$port" --chain-id "$chain_id" --balance 10000 \
        --base-fee 1000000000 --silent &
    local pid=$!
    
    # Verify anvil started
    sleep 0.5
    if ! kill -0 "$pid" 2>/dev/null; then
        log_error "Anvil process $pid died immediately"
        return 1
    fi
    
    ANVIL_PIDS+=("$pid")
    echo "$pid" > "${PIDS_DIR}/anvil-${chain_id}.pid"
    
    # Wait for chain to be ready with proper RPC check
    local rpc="http://localhost:$port"
    for i in {1..30}; do
        local response
        response=$(curl -s -X POST -H "Content-Type: application/json" \
           --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
           "$rpc" 2>/dev/null)
        if echo "$response" | grep -q "result"; then
            log_chain "$chain_id" "Ready at $rpc (PID: $pid)"
            return 0
        fi
        sleep 0.5
    done
    
    log_error "Chain $chain_id failed to start"
    return 1
}

start_all_chains() {
    log_info "Starting ${#CHAINS[@]} Anvil chains..."
    
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        start_anvil_chain "$chain_id" "$port" "$name"
    done
    
    # Extra delay to ensure all chains are fully ready
    sleep 2
    
    log_success "All ${#CHAINS[@]} chains started"
}

#######################################
# Deploy Contracts
#######################################

deploy_contract() {
    local rpc="$1"
    local contract_path="$2"
    shift 2
    local constructor_args=("$@")
    
    local output
    local exit_code=0
    
    if [ ${#constructor_args[@]} -gt 0 ]; then
        output=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 forge create \
            --rpc-url "$rpc" \
            --private-key "$DEFAULT_KEY" \
            --broadcast \
            "$contract_path" \
            --constructor-args "${constructor_args[@]}" 2>&1) || exit_code=$?
    else
        output=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 forge create \
            --rpc-url "$rpc" \
            --private-key "$DEFAULT_KEY" \
            --broadcast \
            "$contract_path" 2>&1) || exit_code=$?
    fi
    
    if [ $exit_code -ne 0 ]; then
        log_error "Failed to deploy $contract_path"
        log_error "$output"
        echo ""
        return 1
    fi
    
    local address=$(echo "$output" | grep -i "Deployed to:" | awk '{print $3}')
    
    if [ -z "$address" ]; then
        log_error "Could not parse address from: $output"
        echo ""
        return 1
    fi
    
    echo "$address"
}

# Verify chain is ready before deploying
verify_chain_ready() {
    local rpc="$1"
    local chain_id="$2"
    local max_attempts=10
    
    for i in $(seq 1 $max_attempts); do
        if cast chain-id --rpc-url "$rpc" &>/dev/null; then
            return 0
        fi
        sleep 1
    done
    
    log_error "Chain $chain_id at $rpc is not responding"
    return 1
}

# Deploy source chain contracts (main chain only - has Registry)
deploy_source_chain() {
    local chain_id="31337"
    local port="8545"
    local rpc="http://localhost:$port"
    local chain_dir="${MULTICHAIN_DIR}/${chain_id}"
    
    # Verify chain is ready
    verify_chain_ready "$rpc" "$chain_id" || return 1
    
    log_chain "$chain_id" "Deploying SOURCE chain contracts..."
    
    cd "$CONTRACTS_DIR"
    
    # Deploy DIAOracleV2 (source of price data)
    local oracle_addr
    oracle_addr=$(deploy_contract "$rpc" "contracts/DIAOracleV2.sol:DIAOracleV2")
    if [ -z "$oracle_addr" ]; then
        log_error "Failed to deploy DIAOracleV2"
        return 1
    fi
    echo "$oracle_addr" > "${chain_dir}/dia_oracle_v2.addr"
    echo "$oracle_addr" > "${CONTRACTS_ADDR_DIR}/dia_oracle_v2.addr"
    log_chain "$chain_id" "DIAOracleV2 (Source): $oracle_addr"
    
    # Deploy OracleIntentRegistry (ONLY on source chain)
    # Constructor args: name, version
    local registry_addr
    registry_addr=$(deploy_contract "$rpc" \
        "contracts/OracleIntentRegistry.sol:OracleIntentRegistry" \
        "DIA Oracle" "1.0")
    if [ -z "$registry_addr" ]; then
        log_error "Failed to deploy OracleIntentRegistry"
        return 1
    fi
    echo "$registry_addr" > "${chain_dir}/oracle_intent_registry.addr"
    echo "$registry_addr" > "${CONTRACTS_ADDR_DIR}/oracle_intent_registry.addr"
    log_chain "$chain_id" "OracleIntentRegistry: $registry_addr"
    
    # Authorize signer in registry
    local signer_addr
    signer_addr=$(FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast wallet address --private-key "$DEFAULT_KEY")
    FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send --rpc-url "$rpc" --private-key "$DEFAULT_KEY" \
        "$registry_addr" "setSignerAuthorization(address,bool)" \
        "$signer_addr" true &>/dev/null || log_warning "Failed to authorize signer in registry"
    
    # Initialize oracle prices
    local eth_price btc_price ts
    eth_price=$(python3 -c "print(int(2250 * 1e18))")
    btc_price=$(python3 -c "print(int(45000 * 1e18))")
    ts=$(date +%s)
    
    FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send --rpc-url "$rpc" --private-key "$DEFAULT_KEY" \
        "$oracle_addr" "setValue(string,uint128,uint128)" "ETH/USD" "$eth_price" "$ts" &>/dev/null || true
    FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send --rpc-url "$rpc" --private-key "$DEFAULT_KEY" \
        "$oracle_addr" "setValue(string,uint128,uint128)" "BTC/USD" "$btc_price" "$ts" &>/dev/null || true
    
    log_chain "$chain_id" "Source chain configured"
}

# Deploy destination chain contracts (ProtocolFeeHook + Receivers)
deploy_chain_contracts() {
    local chain_id="$1"
    local port="$2"
    local num_receivers="$3"
    local rpc="http://localhost:$port"
    local chain_dir="${MULTICHAIN_DIR}/${chain_id}"
    
    # Verify chain is ready
    verify_chain_ready "$rpc" "$chain_id" || return 1
    
    # Get the source chain registry address (for EIP-712 domain)
    local source_registry=$(cat "${CONTRACTS_ADDR_DIR}/oracle_intent_registry.addr" 2>/dev/null)
    if [ -z "$source_registry" ]; then
        log_error "Source registry not found. Deploy source chain first."
        return 1
    fi
    
    log_chain "$chain_id" "Deploying destination contracts (${num_receivers} receiver(s))..."
    
    cd "$CONTRACTS_DIR"
    
    # Deploy ProtocolFeeHook (required on EVERY chain for receivers)
    local fee_hook_addr=$(deploy_contract "$rpc" "contracts/ProtocolFeeHook.sol:ProtocolFeeHook")
    echo "$fee_hook_addr" > "${chain_dir}/protocol_fee_hook.addr"
    log_chain "$chain_id" "ProtocolFeeHook: $fee_hook_addr"
    
    # Deploy PushOracleReceiverV2 (multiple if needed)
    # Constructor args: domainName, domainVersion, sourceChainId, verifyingContract (registry on source)
    # Source chain ID is always 31337 (main chain)
    for i in $(seq 1 "$num_receivers"); do
        local receiver_addr
        receiver_addr=$(deploy_contract "$rpc" \
            "contracts/PushOracleReceiverV2.sol:PushOracleReceiverV2" \
            "DIA Oracle" "1.0" "31337" "$source_registry")
        echo "$receiver_addr" > "${chain_dir}/push_oracle_receiver_v2_${i}.addr"
        log_chain "$chain_id" "Receiver #$i: $receiver_addr"
        
        # Configure receiver - set payment hook
        FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send --rpc-url "$rpc" --private-key "$DEFAULT_KEY" \
            "$receiver_addr" "setPaymentHook(address)" "$fee_hook_addr" &>/dev/null || true
        
        # Authorize signer in receiver
        FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send --rpc-url "$rpc" --private-key "$DEFAULT_KEY" \
            "$receiver_addr" "setSignerAuthorization(address,bool)" \
            "$(cast wallet address --private-key $DEFAULT_KEY)" true &>/dev/null || true
        
        # Fund receiver with ETH for gas
        FOUNDRY_DISABLE_NIGHTLY_WARNING=1 cast send --rpc-url "$rpc" --private-key "$DEFAULT_KEY" \
            --value "100ether" "$receiver_addr" &>/dev/null || true
    done
    
    log_chain "$chain_id" "Destination chain configured"
}

deploy_all_contracts() {
    log_info "Deploying contracts to all chains..."
    
    # Step 1: Deploy source chain first (has OracleIntentRegistry)
    deploy_source_chain
    
    # Step 2: Deploy destination contracts on ALL chains (including main chain receivers)
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        deploy_chain_contracts "$chain_id" "$port" "$num_receivers"
    done
    
    log_success "All contracts deployed: 1 Registry (source), ${#CHAINS[@]} ProtocolFeeHooks, 12 Receivers"
}

#######################################
# Generate Bridge Configuration
#######################################

generate_bridge_config() {
    log_info "Generating bridge configuration..."
    
    local bridge_config_dir="${CONFIG_DIR}/bridge-modular"
    local routers_dir="${bridge_config_dir}/routers"
    
    # Clean up old config files to avoid stale router references
    log_info "Cleaning old bridge config..."
    rm -rf "${bridge_config_dir}"
    mkdir -p "$routers_dir"
    
    # Generate infrastructure.yaml
    cat > "${bridge_config_dir}/infrastructure.yaml" << 'EOF'
database:
    driver: postgres
    dsn_env: DATABASE_DSN
source:
    chain_id: 31337
    name: Anvil Main
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
    maxworkers: 10
    taskqueuesize: 200
    tasktimeout: 2m
    retrydelay: 10s
    maxretries: 3
health_check:
    enabled: true
    checkinterval: 30s
    timeout: 10s
    maxprocessinglag: 2m
    maxqueuesize: 50
api:
    enabled: true
    listenaddr: :8080
    enablecors: true
metrics:
    enabled: true
    namespace: oracle_bridge
dry_run: false
EOF

    # Generate chains.yaml with all chains
    cat > "${bridge_config_dir}/chains.yaml" << EOF
chains:
EOF
    
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        cat >> "${bridge_config_dir}/chains.yaml" << EOF
    "${chain_id}":
        chain_id: ${chain_id}
        name: ${name}
        rpc_urls:
            - http://host.docker.internal:${port}
        enabled: true
        default_gas_limit: 300000
        gas_multiplier: 1.2
        max_gas_price: "100000000000"
EOF
    done
    
    # Generate contracts.yaml with all receivers
    cat > "${bridge_config_dir}/contracts.yaml" << EOF
contracts:
EOF
    
    local contract_idx=1
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        local chain_dir="${MULTICHAIN_DIR}/${chain_id}"
        
        for i in $(seq 1 "$num_receivers"); do
            local receiver_addr=$(cat "${chain_dir}/push_oracle_receiver_v2_${i}.addr" 2>/dev/null || echo "0x0")
            cat >> "${bridge_config_dir}/contracts.yaml" << EOF
    push_oracle_receiver_${chain_id}_${i}:
        chain_id: ${chain_id}
        address: ${receiver_addr}
        type: pushoracle
        enabled: true
        abi: '[{"name":"handleIntentUpdate","type":"function","inputs":[{"name":"intent","type":"tuple","components":[{"name":"intentType","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"nonce","type":"uint256"},{"name":"expiry","type":"uint256"},{"name":"symbol","type":"string"},{"name":"price","type":"uint256"},{"name":"timestamp","type":"uint256"},{"name":"source","type":"string"},{"name":"signature","type":"bytes"},{"name":"signer","type":"address"}]}]}]'
        gas_limit: 300000
        methods:
            intent_update:
                methodname: handleIntentUpdate
                fieldsmapping:
                    intent: fullIntent
                gaslimit: 300000
EOF
            contract_idx=$((contract_idx + 1))
        done
    done
    
    # Generate events.yaml (registry is only on source chain 31337)
    local source_registry=$(cat "${CONTRACTS_ADDR_DIR}/oracle_intent_registry.addr" 2>/dev/null || echo "0x0")
    cat > "${bridge_config_dir}/events.yaml" << EOF
event_definitions:
    IntentRegistered:
        contract: ${source_registry}
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
EOF

    # Generate routers for each chain/receiver combination
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        
        for i in $(seq 1 "$num_receivers"); do
            cat > "${routers_dir}/router_${chain_id}_${i}.yaml" << EOF
router:
    id: router_${chain_id}_${i}
    name: router_chain_${chain_id}_receiver_${i}
    type: event
    enabled: true
    private_key_env: PRIVATE_KEY
    triggers:
        events:
            - IntentRegistered
        conditions: []
    processing:
        datasource: enrichment
        transformations: []
        validationenabled: true
    destinations:
        - contract_ref: push_oracle_receiver_${chain_id}_${i}
          time_threshold: 2s
          price_deviation: "0.5%"
          method:
            name: handleIntentUpdate
            abi: '{"name":"handleIntentUpdate","type":"function","inputs":[{"name":"intent","type":"tuple","components":[{"name":"intentType","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"nonce","type":"uint256"},{"name":"expiry","type":"uint256"},{"name":"symbol","type":"string"},{"name":"price","type":"uint256"},{"name":"timestamp","type":"uint256"},{"name":"source","type":"string"},{"name":"signature","type":"bytes"},{"name":"signer","type":"address"}]}]}'
            params:
                intent: \${enrichment.fullIntent}
            value: "0"
            gaslimit: 300000
            gasmultiplier: 1.2
EOF
        done
    done
    
    log_success "Bridge configuration generated with $(ls ${routers_dir}/*.yaml | wc -l | tr -d ' ') routers"
}

#######################################
# Generate Attestor Configuration
#######################################

generate_attestor_config() {
    log_info "Generating attestor configuration..."
    
    local main_oracle=$(cat "${MULTICHAIN_DIR}/31337/dia_oracle_v2.addr" 2>/dev/null || echo "0x0")
    local main_registry=$(cat "${MULTICHAIN_DIR}/31337/oracle_intent_registry.addr" 2>/dev/null || echo "0x0")
    
    cat > "${CONFIG_DIR}/attestor.env" << EOF
ATTESTOR_RPC_URLS=http://host.docker.internal:8545
ATTESTOR_ORACLE_ADDRESS=${main_oracle}
ATTESTOR_ORACLE_CLIENT_TYPE=dia_v2
ATTESTOR_REGISTRY_ADDRESS=${main_registry}
ATTESTOR_ATTESTOR_PRIVATE_KEY=${DEFAULT_KEY}
ATTESTOR_ATTESTOR_SYMBOLS=BTC/USD,ETH/USD
ATTESTOR_ATTESTOR_POLLING_TIME=5s
ATTESTOR_ATTESTOR_BATCH_MODE=false
ATTESTOR_ATTESTOR_MODE=prime
ATTESTOR_METRICS_PORT=8080
ATTESTOR_API_PORT=8081
EOF

    cat > "${CONFIG_DIR}/attestor-local.yaml" << EOF
rpc:
  url: http://host.docker.internal:8545
  urls:
    - http://host.docker.internal:8545
  registry_url: http://host.docker.internal:8545
  registry_urls:
    - http://host.docker.internal:8545

oracle:
  address: "${main_oracle}"
  client_type: "dia_v2"

registry:
  address: "${main_registry}"

attestor:
  private_key: "${DEFAULT_KEY}"
  symbols:
    - BTC/USD
    - ETH/USD
  polling_time: 5s
  batch_mode: false
  mode: prime

logging:
  level: debug

metrics:
  port: 8080

api:
  port: 8081
EOF

    log_success "Attestor configuration generated"
}

#######################################
# Start Docker Services (Bridge, Attestor, Postgres)
#######################################

start_docker_services() {
    log_info "Starting Docker services (Postgres, Attestor, Bridge)..."
    
    # Export environment variables for docker-compose
    export INTENT_REGISTRY_ADDRESS=$(cat "${CONTRACTS_ADDR_DIR}/oracle_intent_registry.addr" 2>/dev/null || echo "")
    export PRIVATE_KEY="${DEFAULT_KEY}"
    export POSTGRES_HOST="${POSTGRES_HOST}"
    export POSTGRES_PORT="${POSTGRES_PORT}"
    export POSTGRES_USER="${POSTGRES_USER}"
    export POSTGRES_PASSWORD="${POSTGRES_PASSWORD}"
    export POSTGRES_DB="${POSTGRES_DB}"
    # Use 'postgres' hostname for Docker container networking
    export POSTGRES_DSN="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"
    
    # Build and start services
    log_info "Building Docker images..."
    if ! docker compose -f "${COMPOSE_FILE}" build attestor bridge 2>&1 | tail -5; then
        log_warning "Docker build had issues, continuing..."
    fi
    
    log_info "Starting Postgres, Attestor, and Bridge..."
    if ! docker compose -f "${COMPOSE_FILE}" up -d postgres attestor bridge; then
        log_error "Failed to start Docker services"
        return 1
    fi
    
    log_success "Docker services started"
}

wait_for_services() {
    log_info "Waiting for services to be healthy..."
    
    # Wait for Postgres
    log_info "Waiting for Postgres..."
    for i in {1..30}; do
        if docker exec spectra-interoperability-postgres-1 pg_isready -U bridge &>/dev/null; then
            log_success "Postgres is ready"
            break
        fi
        sleep 1
    done
    
    # Wait for Attestor
    log_info "Waiting for Attestor..."
    for i in {1..60}; do
        if curl -s http://localhost:8081/health &>/dev/null; then
            log_success "Attestor is ready"
            break
        fi
        if ! docker ps --filter "name=spectra-interoperability-attestor-1" --filter "status=running" | grep -q attestor; then
            log_warning "Attestor container not running"
            docker logs spectra-interoperability-attestor-1 --tail 10 2>/dev/null || true
            break
        fi
        sleep 2
    done
    
    # Wait for Bridge
    log_info "Waiting for Bridge..."
    for i in {1..60}; do
        if curl -s http://localhost:8082/metrics &>/dev/null; then
            log_success "Bridge is ready"
            break
        fi
        if ! docker ps --filter "name=spectra-interoperability-bridge-1" --filter "status=running" | grep -q bridge; then
            log_warning "Bridge container not running"
            docker logs spectra-interoperability-bridge-1 --tail 10 2>/dev/null || true
            break
        fi
        sleep 2
    done
    
    log_success "Services health check complete"
}

#######################################
# Start Price Updater
#######################################

start_price_updater() {
    log_info "Starting price updater for all chains..."
    
    # Create multi-chain price updater
    cat > "${LOCAL_STACK_DIR}/price-updater-multichain.sh" << 'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

MULTICHAIN_DIR="$1"
DEFAULT_KEY="$2"

update_price() {
    local rpc="$1"
    local oracle="$2"
    local symbol="$3"
    local price="$4"
    
    local price_wei=$(python3 -c "print(int($price * 1e18))")
    local ts=$(date +%s)
    
    cast send --rpc-url "$rpc" --private-key "$DEFAULT_KEY" \
        "$oracle" "setValue(string,uint128,uint128)" "$symbol" "$price_wei" "$ts" \
        2>/dev/null || true
}

while true; do
    eth_price=$(python3 -c "import random; print(f'{2250 * (1 + random.uniform(-0.05, 0.05)):.2f}')")
    btc_price=$(python3 -c "import random; print(f'{45000 * (1 + random.uniform(-0.03, 0.03)):.2f}')")
    
    for chain_dir in "$MULTICHAIN_DIR"/*/; do
        chain_id=$(basename "$chain_dir")
        oracle_file="${chain_dir}/dia_oracle_v2.addr"
        
        if [ -f "$oracle_file" ]; then
            oracle=$(cat "$oracle_file")
            # Determine port from chain_id
            port=$((8545 + chain_id - 31337))
            rpc="http://localhost:$port"
            
            update_price "$rpc" "$oracle" "ETH/USD" "$eth_price"
            update_price "$rpc" "$oracle" "BTC/USD" "$btc_price"
        fi
    done
    
    echo "[$(date -u +%H:%M:%S)] Updated prices: ETH/USD=$eth_price, BTC/USD=$btc_price"
    sleep 10
done
SCRIPT
    chmod +x "${LOCAL_STACK_DIR}/price-updater-multichain.sh"
    
    nohup "${LOCAL_STACK_DIR}/price-updater-multichain.sh" "$MULTICHAIN_DIR" "$DEFAULT_KEY" \
        > "${LOCAL_STACK_DIR}/logs/price-updater.log" 2>&1 &
    echo $! > "${PIDS_DIR}/price-updater.pid"
    
    log_success "Price updater started (PID: $(cat ${PIDS_DIR}/price-updater.pid))"
}

#######################################
# Show Summary
#######################################

show_summary() {
    echo ""
    echo "═══════════════════════════════════════════════════════════════════════════"
    echo "  Multi-Chain Local Development Environment"
    echo "═══════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "  Architecture:"
    echo "    Source Chain: 31337 (anvil-main)"
    echo "      - DIAOracleV2 (price source)"
    echo "      - OracleIntentRegistry (intent registration & EIP-712 domain)"
    echo ""
    echo "    Destination Chains: ${#CHAINS[@]} chains"
    echo "      - ProtocolFeeHook (1 per chain)"
    echo "      - PushOracleReceiverV2 (12 total across all chains)"
    echo ""
    echo "  Source Chain (31337):"
    echo "    Oracle: $(cat ${CONTRACTS_ADDR_DIR}/dia_oracle_v2.addr 2>/dev/null || echo 'N/A')"
    echo "    Registry: $(cat ${CONTRACTS_ADDR_DIR}/oracle_intent_registry.addr 2>/dev/null || echo 'N/A')"
    echo ""
    echo "  Destination Chains:"
    
    local total_receivers=0
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        local chain_dir="${MULTICHAIN_DIR}/${chain_id}"
        
        echo ""
        echo "  ┌─ Chain $chain_id ($name) - http://localhost:$port"
        echo "  │  FeeHook: $(cat ${chain_dir}/protocol_fee_hook.addr 2>/dev/null || echo 'N/A')"
        
        for i in $(seq 1 "$num_receivers"); do
            local receiver=$(cat "${chain_dir}/push_oracle_receiver_v2_${i}.addr" 2>/dev/null || echo 'N/A')
            echo "  │  Receiver #$i: $receiver"
            total_receivers=$((total_receivers + 1))
        done
        echo "  └─"
    done
    
    echo ""
    echo "  Total: ${#CHAINS[@]} chains, ${#CHAINS[@]} FeeHooks, $total_receivers Receivers"
    echo ""
    echo "  Docker Services:"
    echo "    Postgres:  localhost:5432"
    echo "    Attestor:  localhost:8080 (metrics), localhost:8081 (API)"
    echo "    Bridge:    localhost:8082 (metrics)"
    echo ""
    echo "  Configuration:"
    echo "    Bridge: ${CONFIG_DIR}/bridge-modular/"
    echo "    Attestor: ${CONFIG_DIR}/attestor-local.yaml"
    echo ""
    echo "  Logs:"
    echo "    Attestor: docker logs -f spectra-interoperability-attestor-1"
    echo "    Bridge:   docker logs -f spectra-interoperability-bridge-1"
    echo "    Prices:   tail -f ${LOCAL_STACK_DIR}/logs/price-updater.log"
    echo ""
    echo "  Commands:"
    echo "    Monitor receivers: ./scripts/monitor-multichain.sh"
    echo "    Service logs:      docker compose -f docker-compose.local.yml logs -f"
    echo "    Stop all:          ./scripts/start-multichain.sh stop"
    echo ""
    echo "═══════════════════════════════════════════════════════════════════════════"
}

#######################################
# Stop All
#######################################

stop_all() {
    log_info "Stopping all services..."
    
    # Stop Anvil processes
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        if [ -f "${PIDS_DIR}/anvil-${chain_id}.pid" ]; then
            kill $(cat "${PIDS_DIR}/anvil-${chain_id}.pid") 2>/dev/null || true
            rm -f "${PIDS_DIR}/anvil-${chain_id}.pid"
        fi
    done
    
    # Kill any remaining anvil
    pkill -f "anvil.*854" 2>/dev/null || true
    
    # Stop price updater
    if [ -f "${PIDS_DIR}/price-updater.pid" ]; then
        kill $(cat "${PIDS_DIR}/price-updater.pid") 2>/dev/null || true
        rm -f "${PIDS_DIR}/price-updater.pid"
    fi
    
    # Stop Docker
    docker compose -f "${COMPOSE_FILE}" down --remove-orphans 2>/dev/null || true
    
    log_success "All services stopped"
}

#######################################
# Show Status
#######################################

show_status() {
    echo ""
    echo "Chain Status:"
    echo ""
    
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        local rpc="http://localhost:$port"
        
        if curl -s -X POST -H "Content-Type: application/json" \
           --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
           "$rpc" &>/dev/null; then
            echo -e "  ${GREEN}●${NC} Chain $chain_id ($name): Running at $rpc"
        else
            echo -e "  ${RED}●${NC} Chain $chain_id ($name): Not running"
        fi
    done
    echo ""
}

#######################################
# Main
#######################################

main() {
    local command="${1:-start}"
    
    case "$command" in
        start)
            log_info "Starting Multi-Chain Local Environment..."
            SERVICES_STARTED=true
            
            check_dependencies
            init_directories
            start_all_chains
            deploy_all_contracts
            generate_bridge_config
            generate_attestor_config
            start_docker_services
            wait_for_services
            start_price_updater
            show_summary
            
            log_info "Press Ctrl+C to stop all chains and services"
            
            # Wait for first anvil to keep script running
            wait "${ANVIL_PIDS[0]}"
            ;;
        stop)
            stop_all
            ;;
        status)
            show_status
            ;;
        *)
            echo "Usage: $0 [start|stop|status]"
            exit 1
            ;;
    esac
}

main "$@"
