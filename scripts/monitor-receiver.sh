#!/usr/bin/env bash
set -euo pipefail

#######################################
# PushOracleReceiverV2 Monitor
# Monitors oracle updates and logs:
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

RPC_URL="${RPC_URL:-http://localhost:8545}"
RECEIVER_ADDR_FILE="${ROOT_DIR}/.local-stack/contracts/push_oracle_receiver_v2.addr"
DIA_ORACLE_ADDR_FILE="${ROOT_DIR}/.local-stack/contracts/dia_oracle_v2.addr"

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

#######################################
# Initialization
#######################################

init() {
    mkdir -p "$LOG_DIR"
    
    # Check if receiver address exists
    if [ ! -f "$RECEIVER_ADDR_FILE" ]; then
        echo "Error: Receiver address file not found: $RECEIVER_ADDR_FILE"
        echo "Run ./scripts/start-local.sh first"
        exit 1
    fi
    
    RECEIVER_ADDR=$(cat "$RECEIVER_ADDR_FILE")
    
    # Check if DIA oracle address exists
    if [ -f "$DIA_ORACLE_ADDR_FILE" ]; then
        DIA_ORACLE_ADDR=$(cat "$DIA_ORACLE_ADDR_FILE")
    else
        DIA_ORACLE_ADDR=""
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
    "symbols": {}
}
EOF
    
    log_info "Monitor initialized"
    log_info "Receiver: $RECEIVER_ADDR"
    log_info "DIA Oracle: ${DIA_ORACLE_ADDR:-N/A}"
    log_info "Deviation threshold: ${DEVIATION_THRESHOLD}%"
    log_info "Time threshold: ${TIME_THRESHOLD}s"
    log_info "Poll interval: ${POLL_INTERVAL}s"
    log_info "Log file: $LOG_FILE"
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
get_receiver_price() {
    local symbol="$1"
    local result
    result=$(cast call --rpc-url "$RPC_URL" "$RECEIVER_ADDR" \
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
get_source_price() {
    local symbol="$1"
    if [ -z "$DIA_ORACLE_ADDR" ]; then
        echo "0 0"
        return
    fi
    local result
    result=$(cast call --rpc-url "$RPC_URL" "$DIA_ORACLE_ADDR" \
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

# Get previous state for a symbol
get_state() {
    local symbol="$1"
    local key=$(echo "$symbol" | tr '/' '_')
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

# Save state for a symbol
save_state() {
    local symbol="$1"
    local value="$2"
    local timestamp="$3"
    local source_value="$4"
    local source_timestamp="$5"
    local last_deviation_time="${6:-0}"
    local key=$(echo "$symbol" | tr '/' '_')
    
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

check_symbol() {
    local symbol="$1"
    local now=$(date +%s)
    
    # Get current receiver price
    local receiver_result=$(get_receiver_price "$symbol")
    local receiver_value=$(echo "$receiver_result" | awk '{print $1}')
    local receiver_timestamp=$(echo "$receiver_result" | awk '{print $2}')
    
    # Get source oracle price
    local source_result=$(get_source_price "$symbol")
    local source_value=$(echo "$source_result" | awk '{print $1}')
    local source_timestamp=$(echo "$source_result" | awk '{print $2}')
    
    # Get previous state
    local prev_state=$(get_state "$symbol")
    local prev_value=$(echo "$prev_state" | awk '{print $1}')
    local prev_timestamp=$(echo "$prev_state" | awk '{print $2}')
    local prev_source_value=$(echo "$prev_state" | awk '{print $3}')
    local prev_deviation_time=$(echo "$prev_state" | awk '{print $5}')
    
    # Convert to human readable
    local receiver_price=$(wei_to_price "$receiver_value")
    local source_price=$(wei_to_price "$source_value")
    local prev_receiver_price=$(wei_to_price "$prev_value")
    
    # Check for new update (use Python for safe comparison)
    local is_new_update=$(python3 -c "print('yes' if '$receiver_timestamp' != '$prev_timestamp' and '$receiver_timestamp' != '0' else 'no')" 2>/dev/null || echo "no")
    if [ "$is_new_update" = "yes" ]; then
        log_update "$symbol: New update received! Price: \$$receiver_price (ts: $receiver_timestamp)"
        update_metrics "update" "$symbol" "$receiver_price"
    fi
    
    # Check deviation between source and receiver
    local has_values=$(python3 -c "print('yes' if '$source_value' != '0' and '$receiver_value' != '0' else 'no')" 2>/dev/null || echo "no")
    if [ "$has_values" = "yes" ]; then
        local deviation=$(calc_deviation "$receiver_value" "$source_value")
        local deviation_exceeded=$(python3 -c "print('yes' if float('$deviation') >= float('$DEVIATION_THRESHOLD') else 'no')" 2>/dev/null || echo "no")
        
        if [ "$deviation_exceeded" = "yes" ]; then
            log_deviation "$symbol: Deviation detected! Source: \$$source_price, Receiver: \$$receiver_price, Deviation: ${deviation}%"
            update_metrics "deviation" "$symbol"
            
            # Check if update is overdue (use Python for arithmetic)
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
                log_missed "$symbol: UPDATE EXPECTED! Deviation: ${deviation}% > ${DEVIATION_THRESHOLD}%, Time since update: ${time_since}s > ${TIME_THRESHOLD}s"
                update_metrics "missed" "$symbol"
            fi
        fi
    fi
    
    # Check for stale data (use Python for arithmetic)
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
        log_warning "$symbol: Data is stale! Last update: ${age}s ago"
    fi
    
    # Save current state
    save_state "$symbol" "$receiver_value" "$receiver_timestamp" "$source_value" "$source_timestamp" "$now"
}

print_status() {
    echo ""
    echo "═══════════════════════════════════════════════════════════════"
    echo "  PushOracleReceiverV2 Monitor Status - $(timestamp)"
    echo "═══════════════════════════════════════════════════════════════"
    
    for symbol in "${SYMBOLS[@]}"; do
        local receiver_result=$(get_receiver_price "$symbol")
        local receiver_value=$(echo "$receiver_result" | awk '{print $1}')
        local receiver_timestamp=$(echo "$receiver_result" | awk '{print $2}')
        local receiver_price=$(wei_to_price "$receiver_value")
        
        local source_result=$(get_source_price "$symbol")
        local source_value=$(echo "$source_result" | awk '{print $1}')
        local source_price=$(wei_to_price "$source_value")
        
        local now=$(date +%s)
        
        # Calculate age using Python
        local age=$(python3 -c "
try:
    ts = int('$receiver_timestamp')
    if ts == 0:
        print('N/A (no data)')
    else:
        now = int('$now')
        age = now - ts
        print(f'{age}s ago')
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
        print(f'{dev:.4f}%')
except:
    print('N/A')
" 2>/dev/null || echo "N/A")
        
        echo ""
        echo "  $symbol:"
        echo "    Receiver Price:  \$$receiver_price"
        echo "    Source Price:    \$$source_price"
        echo "    Deviation:       $deviation"
        echo "    Last Update:     $age"
    done
    
    echo ""
    echo "═══════════════════════════════════════════════════════════════"
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
        for symbol in "${SYMBOLS[@]}"; do
            check_symbol "$symbol"
        done
        sleep "$POLL_INTERVAL"
    done
}

show_help() {
    echo "PushOracleReceiverV2 Monitor"
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
    echo "  RPC_URL              RPC endpoint (default: http://localhost:8545)"
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
