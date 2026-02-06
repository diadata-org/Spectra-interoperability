#!/usr/bin/env bash
set -euo pipefail

#######################################
# PushOracleReceiverV2 Multi-Chain Monitor
# Monitors ALL chains and ALL receivers:
# - Received updates
# - Expected updates (based on deviation)
# - Missed updates
#######################################

# Configuration
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="${ROOT_DIR}/.local-stack/logs"
LOG_FILE="${LOG_DIR}/receiver-monitor.log"
STATE_FILE="${LOG_DIR}/receiver-state.json"
METRICS_FILE="${LOG_DIR}/receiver-metrics.json"
MULTICHAIN_DIR="${ROOT_DIR}/.local-stack/multichain"
CONTRACTS_DIR="${ROOT_DIR}/.local-stack/contracts"

# Monitoring parameters
POLL_INTERVAL="${POLL_INTERVAL:-5}"           # Seconds between checks
DEVIATION_THRESHOLD="${DEVIATION_THRESHOLD:-0.5}"  # Percentage deviation to trigger update
TIME_THRESHOLD="${TIME_THRESHOLD:-120}"       # Seconds before update is expected
SYMBOLS=("ETH/USD" "BTC/USD")

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Store discovered receivers: "chain_id:receiver_num:address"
RECEIVERS=()
DIA_ORACLE_ADDR=""
SOURCE_CHAIN_ID=""

# Get port for a chain ID
get_chain_port() {
    local chain_id="$1"
    # Ports start at 8545 for chain 31337
    echo $((8545 + chain_id - 31337))
}

#######################################
# Initialization
#######################################

discover_receivers() {
    RECEIVERS=()
    
    # Check for multichain setup first
    if [ -d "$MULTICHAIN_DIR" ]; then
        for chain_dir in "$MULTICHAIN_DIR"/*/; do
            if [ -d "$chain_dir" ]; then
                local chain_id=$(basename "$chain_dir")
                
                # Find DIA Oracle on source chain (31337)
                if [ "$chain_id" = "31337" ] && [ -f "${chain_dir}/dia_oracle_v2.addr" ]; then
                    DIA_ORACLE_ADDR=$(cat "${chain_dir}/dia_oracle_v2.addr")
                    SOURCE_CHAIN_ID="31337"
                fi
                
                # Find all receivers on this chain
                shopt -s nullglob
                for receiver_file in "${chain_dir}"/push_oracle_receiver_v2_*.addr; do
                    if [ -f "$receiver_file" ]; then
                        local receiver_num=$(echo "$receiver_file" | sed 's/.*push_oracle_receiver_v2_\([0-9]*\)\.addr/\1/')
                        local receiver_addr=$(cat "$receiver_file")
                        RECEIVERS+=("${chain_id}:${receiver_num}:${receiver_addr}")
                    fi
                done
                shopt -u nullglob
            fi
        done
    fi
    
    # Fallback to single-chain setup
    if [ ${#RECEIVERS[@]} -eq 0 ]; then
        if [ -f "${CONTRACTS_DIR}/push_oracle_receiver_v2.addr" ]; then
            local receiver_addr=$(cat "${CONTRACTS_DIR}/push_oracle_receiver_v2.addr")
            RECEIVERS+=("31337:1:${receiver_addr}")
            SOURCE_CHAIN_ID="31337"
        fi
        if [ -f "${CONTRACTS_DIR}/dia_oracle_v2.addr" ]; then
            DIA_ORACLE_ADDR=$(cat "${CONTRACTS_DIR}/dia_oracle_v2.addr")
        fi
    fi
}

init() {
    mkdir -p "$LOG_DIR"
    
    discover_receivers
    
    if [ ${#RECEIVERS[@]} -eq 0 ]; then
        echo "Error: No receivers found"
        echo "Run ./scripts/start-local.sh or ./scripts/start-multichain.sh first"
        exit 1
    fi
    
    # Initialize state file if not exists
    if [ ! -f "$STATE_FILE" ]; then
        echo '{}' > "$STATE_FILE"
    fi
    
    # Initialize metrics file
    cat > "$METRICS_FILE" << EOF
{
    "started_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "total_updates": 0,
    "total_deviations_detected": 0,
    "total_missed_updates": 0,
    "receivers": ${#RECEIVERS[@]},
    "symbols": {}
}
EOF
    
    log_info "Monitor initialized"
    log_info "Source Oracle: ${DIA_ORACLE_ADDR:-N/A} (Chain: ${SOURCE_CHAIN_ID:-N/A})"
    log_info "Receivers discovered: ${#RECEIVERS[@]}"
    for receiver in "${RECEIVERS[@]}"; do
        IFS=':' read -r chain_id num addr <<< "$receiver"
        log_info "  Chain $chain_id Receiver $num: $addr"
    done
    log_info "Deviation threshold: ${DEVIATION_THRESHOLD}%"
    log_info "Time threshold: ${TIME_THRESHOLD}s"
    log_info "Poll interval: ${POLL_INTERVAL}s"
    log_info "Log file: $LOG_FILE"
}

get_rpc_url() {
    local chain_id="$1"
    local port=$(get_chain_port "$chain_id")
    echo "http://localhost:$port"
}

#######################################
# Logging Functions
#######################################

timestamp() {
    date -u +"%Y-%m-%dT%H:%M:%SZ"
}

log_to_file() {
    local level="$1"
    local message="$2"
    echo "[$(timestamp)] [$level] $message" >> "$LOG_FILE"
}

log_info() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${CYAN}[INFO]${NC} $1"
    log_to_file "INFO" "$1"
}

log_update() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${GREEN}[UPDATE]${NC} $1"
    log_to_file "UPDATE" "$1"
}

log_deviation() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${YELLOW}[DEVIATION]${NC} $1"
    log_to_file "DEVIATION" "$1"
}

log_missed() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${RED}[MISSED]${NC} $1"
    log_to_file "MISSED" "$1"
}

log_warning() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${YELLOW}[WARNING]${NC} $1"
    log_to_file "WARNING" "$1"
}

log_error() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${RED}[ERROR]${NC} $1"
    log_to_file "ERROR" "$1"
}

#######################################
# Contract Query Functions
#######################################

# Get price from PushOracleReceiverV2 (returns "value timestamp" as integers)
# Args: symbol rpc_url receiver_addr
get_receiver_price() {
    local symbol="$1"
    local rpc_url="$2"
    local receiver_addr="$3"
    local result
    result=$(cast call --rpc-url "$rpc_url" "$receiver_addr" \
        "getValue(string)(uint128,uint128)" "$symbol" 2>/dev/null || echo "0\n0")
    # Parse and normalize the output
    # Format is: "value [scientific]\ntimestamp [scientific]"
    python3 -c "
import re
raw = '''$result'''
lines = raw.strip().split('\n')
val = 0
ts = 0
try:
    if len(lines) >= 2:
        # First line is value, second line is timestamp
        # Each line format: 'number [scientific]' - we want the first number
        val_match = re.match(r'^(\d+)', lines[0].strip())
        ts_match = re.match(r'^(\d+)', lines[1].strip())
        if val_match:
            val = int(val_match.group(1))
        if ts_match:
            ts = int(ts_match.group(1))
    elif len(lines) == 1:
        # Single line with space-separated values
        parts = lines[0].split()
        if len(parts) >= 2:
            val = int(re.match(r'^(\d+)', parts[0]).group(1))
            ts = int(re.match(r'^(\d+)', parts[1]).group(1))
except Exception as e:
    pass
print(f'{val} {ts}')
" 2>/dev/null || echo "0 0"
}

# Get price from DIAOracleV2 (source oracle)
# Uses source chain RPC
get_source_price() {
    local symbol="$1"
    if [ -z "$DIA_ORACLE_ADDR" ] || [ -z "$SOURCE_CHAIN_ID" ]; then
        echo "0 0"
        return
    fi
    local source_rpc=$(get_rpc_url "$SOURCE_CHAIN_ID")
    local result
    result=$(cast call --rpc-url "$source_rpc" "$DIA_ORACLE_ADDR" \
        "getValue(string)(uint128,uint128)" "$symbol" 2>/dev/null || echo "0\n0")
    # Parse and normalize the output
    python3 -c "
import re
raw = '''$result'''
lines = raw.strip().split('\n')
val = 0
ts = 0
try:
    if len(lines) >= 2:
        val_match = re.match(r'^(\d+)', lines[0].strip())
        ts_match = re.match(r'^(\d+)', lines[1].strip())
        if val_match:
            val = int(val_match.group(1))
        if ts_match:
            ts = int(ts_match.group(1))
    elif len(lines) == 1:
        parts = lines[0].split()
        if len(parts) >= 2:
            val = int(re.match(r'^(\d+)', parts[0]).group(1))
            ts = int(re.match(r'^(\d+)', parts[1]).group(1))
except Exception as e:
    pass
print(f'{val} {ts}')
" 2>/dev/null || echo "0 0"
}

# Convert wei to human readable price
wei_to_price() {
    local wei="$1"
    if [ "$wei" = "0" ] || [ -z "$wei" ]; then
        echo "0.00"
        return
    fi
    python3 -c "
try:
    val = int('$wei')
    print(f'{val / 1e18:.2f}')
except:
    print('0.00')
" 2>/dev/null || echo "0.00"
}

# Calculate percentage deviation
calc_deviation() {
    local old_price="$1"
    local new_price="$2"
    if [ "$old_price" = "0" ] || [ -z "$old_price" ]; then
        echo "100.00"
        return
    fi
    python3 -c "
try:
    old = int('$old_price')
    new = int('$new_price')
    if old == 0:
        print('100.00')
    else:
        deviation = abs(new - old) / old * 100
        print(f'{deviation:.4f}')
except:
    print('0.00')
" 2>/dev/null || echo "0.00"
}

#######################################
# State Management
#######################################

# Get previous state for a receiver+symbol
# Args: chain_id receiver_num symbol
get_state() {
    local chain_id="$1"
    local receiver_num="$2"
    local symbol="$3"
    local key="${chain_id}_${receiver_num}_$(echo "$symbol" | tr '/' '_')"
    python3 -c "
import json
try:
    with open('$STATE_FILE', 'r') as f:
        state = json.load(f)
    data = state.get('$key', {})
    print(f\"{data.get('value', '0')} {data.get('timestamp', '0')} {data.get('source_value', '0')} {data.get('source_timestamp', '0')} {data.get('last_deviation_time', '0')}\")
except:
    print('0 0 0 0 0')
" 2>/dev/null || echo "0 0 0 0 0"
}

# Save state for a receiver+symbol
# Args: chain_id receiver_num symbol value timestamp source_value source_timestamp [last_deviation_time]
save_state() {
    local chain_id="$1"
    local receiver_num="$2"
    local symbol="$3"
    local value="$4"
    local timestamp="$5"
    local source_value="$6"
    local source_timestamp="$7"
    local last_deviation_time="${8:-0}"
    local key="${chain_id}_${receiver_num}_$(echo "$symbol" | tr '/' '_')"
    
    python3 -c "
import json
try:
    with open('$STATE_FILE', 'r') as f:
        state = json.load(f)
except:
    state = {}

state['$key'] = {
    'value': '$value',
    'timestamp': '$timestamp',
    'source_value': '$source_value',
    'source_timestamp': '$source_timestamp',
    'last_deviation_time': '$last_deviation_time',
    'last_check': '$(timestamp)'
}

with open('$STATE_FILE', 'w') as f:
    json.dump(state, f, indent=2)
" 2>/dev/null
}

# Update metrics
update_metrics() {
    local event_type="$1"
    local symbol="$2"
    local details="${3:-}"
    
    python3 -c "
import json
from datetime import datetime

try:
    with open('$METRICS_FILE', 'r') as f:
        metrics = json.load(f)
except:
    metrics = {'symbols': {}}

# Update counters
if '$event_type' == 'update':
    metrics['total_updates'] = metrics.get('total_updates', 0) + 1
elif '$event_type' == 'deviation':
    metrics['total_deviations_detected'] = metrics.get('total_deviations_detected', 0) + 1
elif '$event_type' == 'missed':
    metrics['total_missed_updates'] = metrics.get('total_missed_updates', 0) + 1

# Update per-symbol metrics
symbol_key = '$symbol'.replace('/', '_')
if symbol_key not in metrics['symbols']:
    metrics['symbols'][symbol_key] = {
        'updates': 0,
        'deviations': 0,
        'missed': 0,
        'last_update': None,
        'last_price': None
    }

sym = metrics['symbols'][symbol_key]
if '$event_type' == 'update':
    sym['updates'] += 1
    sym['last_update'] = '$(timestamp)'
    if '$details':
        sym['last_price'] = '$details'
elif '$event_type' == 'deviation':
    sym['deviations'] += 1
elif '$event_type' == 'missed':
    sym['missed'] += 1

metrics['last_check'] = '$(timestamp)'

with open('$METRICS_FILE', 'w') as f:
    json.dump(metrics, f, indent=2)
" 2>/dev/null
}

#######################################
# Monitoring Logic
#######################################

# Check a single receiver for a symbol
# Args: chain_id receiver_num receiver_addr symbol
check_receiver_symbol() {
    local chain_id="$1"
    local receiver_num="$2"
    local receiver_addr="$3"
    local symbol="$4"
    local now=$(date +%s)
    
    local rpc_url=$(get_rpc_url "$chain_id")
    local short_addr="${receiver_addr:0:6}...${receiver_addr: -4}"
    
    # Get current receiver price
    local receiver_result=$(get_receiver_price "$symbol" "$rpc_url" "$receiver_addr")
    local receiver_value=$(echo "$receiver_result" | awk '{print $1}')
    local receiver_timestamp=$(echo "$receiver_result" | awk '{print $2}')
    
    # Get source oracle price
    local source_result=$(get_source_price "$symbol")
    local source_value=$(echo "$source_result" | awk '{print $1}')
    local source_timestamp=$(echo "$source_result" | awk '{print $2}')
    
    # Get previous state
    local prev_state=$(get_state "$chain_id" "$receiver_num" "$symbol")
    local prev_value=$(echo "$prev_state" | awk '{print $1}')
    local prev_timestamp=$(echo "$prev_state" | awk '{print $2}')
    local prev_source_value=$(echo "$prev_state" | awk '{print $3}')
    local prev_deviation_time=$(echo "$prev_state" | awk '{print $5}')
    
    # Convert to human readable
    local receiver_price=$(wei_to_price "$receiver_value")
    local source_price=$(wei_to_price "$source_value")
    
    # Check for new update
    local is_new_update=$(python3 -c "print('yes' if '$receiver_timestamp' != '$prev_timestamp' and '$receiver_timestamp' != '0' else 'no')" 2>/dev/null || echo "no")
    if [ "$is_new_update" = "yes" ]; then
        log_update "[Chain:$chain_id] [${short_addr}] $symbol: New update received! Price: \$$receiver_price (ts: $receiver_timestamp)"
        update_metrics "update" "${chain_id}_${receiver_num}_${symbol}" "$receiver_price"
    fi
    
    # Check deviation between source and receiver
    local has_values=$(python3 -c "print('yes' if '$source_value' != '0' and '$receiver_value' != '0' else 'no')" 2>/dev/null || echo "no")
    if [ "$has_values" = "yes" ]; then
        local deviation=$(calc_deviation "$receiver_value" "$source_value")
        local deviation_exceeded=$(python3 -c "print('yes' if float('$deviation') >= float('$DEVIATION_THRESHOLD') else 'no')" 2>/dev/null || echo "no")
        
        if [ "$deviation_exceeded" = "yes" ]; then
            log_deviation "[Chain:$chain_id] [${short_addr}] $symbol: Deviation detected! Source: \$$source_price, Receiver: \$$receiver_price, Deviation: ${deviation}%"
            update_metrics "deviation" "${chain_id}_${receiver_num}_${symbol}"
            
            # Check if update is overdue
            local time_check=$(python3 -c "
try:
    now = int('$now')
    ts = int('$receiver_timestamp')
    threshold = int('$TIME_THRESHOLD')
    time_since = now - ts
    if time_since > threshold:
        print(f'overdue {time_since}')
    else:
        print('ok')
except:
    print('ok')
" 2>/dev/null || echo "ok")
            
            if [[ "$time_check" == overdue* ]]; then
                local time_since=$(echo "$time_check" | awk '{print $2}')
                log_missed "[Chain:$chain_id] [${short_addr}] $symbol: UPDATE EXPECTED! Deviation: ${deviation}% > ${DEVIATION_THRESHOLD}%, Time since update: ${time_since}s > ${TIME_THRESHOLD}s"
                update_metrics "missed" "${chain_id}_${receiver_num}_${symbol}"
            fi
        fi
    fi
    
    # Check for stale data
    local stale_check=$(python3 -c "
try:
    ts = int('$receiver_timestamp')
    if ts == 0:
        print('no_data')
    else:
        now = int('$now')
        threshold = int('$TIME_THRESHOLD') * 2
        age = now - ts
        if age > threshold:
            print(f'stale {age}')
        else:
            print('ok')
except:
    print('ok')
" 2>/dev/null || echo "ok")
    
    if [[ "$stale_check" == stale* ]]; then
        local age=$(echo "$stale_check" | awk '{print $2}')
        log_warning "[Chain:$chain_id] [${short_addr}] $symbol: Data is stale! Last update: ${age}s ago"
    fi
    
    # Save current state
    save_state "$chain_id" "$receiver_num" "$symbol" "$receiver_value" "$receiver_timestamp" "$source_value" "$source_timestamp" "$now"
}

# Check all receivers for all symbols
check_all_receivers() {
    for receiver in "${RECEIVERS[@]}"; do
        IFS=':' read -r chain_id receiver_num receiver_addr <<< "$receiver"
        for symbol in "${SYMBOLS[@]}"; do
            check_receiver_symbol "$chain_id" "$receiver_num" "$receiver_addr" "$symbol"
        done
    done
}

print_status() {
    echo ""
    echo "══════════════════════════════════════════════════════════════════════════════════════"
    echo "  Multi-Chain PushOracleReceiverV2 Monitor - $(timestamp)"
    echo "  Source Oracle: ${DIA_ORACLE_ADDR:-N/A} (Chain: ${SOURCE_CHAIN_ID:-N/A})"
    echo "  Receivers: ${#RECEIVERS[@]} | Symbols: ${SYMBOLS[*]}"
    echo "══════════════════════════════════════════════════════════════════════════════════════"
    
    local now=$(date +%s)
    
    # Get and display source prices
    echo ""
    echo "  Source Prices:"
    local eth_source_result=$(get_source_price "ETH/USD")
    local eth_source_value=$(echo "$eth_source_result" | awk '{print $1}')
    local eth_source_price=$(wei_to_price "$eth_source_value")
    echo "    ETH/USD: \$$eth_source_price"
    
    local btc_source_result=$(get_source_price "BTC/USD")
    local btc_source_value=$(echo "$btc_source_result" | awk '{print $1}')
    local btc_source_price=$(wei_to_price "$btc_source_value")
    echo "    BTC/USD: \$$btc_source_price"
    
    echo ""
    printf "  %-8s | %-10s | %-42s | %-10s | %-12s | %-10s | %-8s\n" \
        "Chain" "Receiver" "Contract" "Symbol" "Price" "Deviation" "Age"
    echo "  ---------|------------|--------------------------------------------|-----------.|--------------|------------|--------"
    
    for receiver in "${RECEIVERS[@]}"; do
        IFS=':' read -r chain_id receiver_num receiver_addr <<< "$receiver"
        local rpc_url=$(get_rpc_url "$chain_id")
        
        for symbol in "${SYMBOLS[@]}"; do
            local receiver_result=$(get_receiver_price "$symbol" "$rpc_url" "$receiver_addr")
            local receiver_value=$(echo "$receiver_result" | awk '{print $1}')
            local receiver_timestamp=$(echo "$receiver_result" | awk '{print $2}')
            local receiver_price=$(wei_to_price "$receiver_value")
            
            # Get source value for this symbol
            local source_value
            if [ "$symbol" = "ETH/USD" ]; then
                source_value="$eth_source_value"
            else
                source_value="$btc_source_value"
            fi
            
            # Calculate age
            local age=$(python3 -c "
try:
    ts = int('$receiver_timestamp')
    if ts == 0:
        print('N/A')
    else:
        now = int('$now')
        age = now - ts
        print(f'{age}s')
except:
    print('N/A')
" 2>/dev/null || echo "N/A")
            
            # Calculate deviation
            local deviation=$(python3 -c "
try:
    src = int('$source_value')
    rcv = int('$receiver_value')
    if src == 0 or rcv == 0:
        print('N/A')
    else:
        dev = abs(src - rcv) / rcv * 100
        print(f'{dev:.2f}%')
except:
    print('N/A')
" 2>/dev/null || echo "N/A")
            
            # Color code based on status
            local status_color="$NC"
            if [[ "$deviation" != "N/A" ]]; then
                local dev_num=$(echo "$deviation" | tr -d '%')
                local is_high=$(python3 -c "print('yes' if float('$dev_num') >= float('$DEVIATION_THRESHOLD') else 'no')" 2>/dev/null || echo "no")
                if [ "$is_high" = "yes" ]; then
                    status_color="$YELLOW"
                fi
            fi
            if [[ "$age" == "N/A" ]] || [[ "$receiver_price" == "0.00" ]]; then
                status_color="$RED"
            fi
            
            printf "  ${status_color}%-8s${NC} | ${status_color}%-10s${NC} | ${status_color}%-42s${NC} | ${status_color}%-10s${NC} | ${status_color}\$%-11s${NC} | ${status_color}%-10s${NC} | ${status_color}%-8s${NC}\n" \
                "$chain_id" "#$receiver_num" "$receiver_addr" "$symbol" "$receiver_price" "$deviation" "$age"
        done
    done
    
    echo ""
    echo "══════════════════════════════════════════════════════════════════════════════════════"
    echo ""
}

#######################################
# Main Loop
#######################################

monitor_loop() {
    log_info "Starting monitoring loop (Ctrl+C to stop)..."
    
    # Print initial status
    print_status
    
    while true; do
        check_all_receivers
        sleep "$POLL_INTERVAL"
    done
}

show_help() {
    echo "Multi-Chain PushOracleReceiverV2 Monitor"
    echo ""
    echo "Monitors ALL chains and ALL receivers from the multichain deployment."
    echo ""
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  start     Start continuous monitoring (default)"
    echo "  status    Show current status once"
    echo "  logs      Show recent log entries"
    echo "  metrics   Show metrics summary"
    echo "  help      Show this help"
    echo ""
    echo "Environment Variables:"
    echo "  POLL_INTERVAL        Seconds between checks (default: 5)"
    echo "  DEVIATION_THRESHOLD  Deviation % to trigger alert (default: 0.5)"
    echo "  TIME_THRESHOLD       Seconds before update expected (default: 120)"
}

show_metrics() {
    if [ -f "$METRICS_FILE" ]; then
        echo "=== Receiver Metrics ==="
        cat "$METRICS_FILE" | python3 -m json.tool 2>/dev/null || cat "$METRICS_FILE"
    else
        echo "No metrics file found. Run monitor first."
    fi
}

show_logs() {
    if [ -f "$LOG_FILE" ]; then
        echo "=== Recent Logs (last 50 lines) ==="
        tail -50 "$LOG_FILE"
    else
        echo "No log file found. Run monitor first."
    fi
}

#######################################
# Entry Point
#######################################

main() {
    local command="${1:-start}"
    
    case "$command" in
        start)
            init
            monitor_loop
            ;;
        status)
            init
            print_status
            ;;
        logs)
            show_logs
            ;;
        metrics)
            show_metrics
            ;;
        help|--help|-h)
            show_help
            ;;
        *)
            echo "Unknown command: $command"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
