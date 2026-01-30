#!/usr/bin/env bash
set -euo pipefail

#######################################
# Multi-Chain PushOracleReceiverV2 Monitor
#######################################

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MULTICHAIN_DIR="${ROOT_DIR}/.local-stack/multichain"
LOG_DIR="${ROOT_DIR}/.local-stack/logs"
LOG_FILE="${LOG_DIR}/multichain-monitor.log"
STATE_FILE="${LOG_DIR}/multichain-state.json"

# Chain configuration (must match start-multichain.sh)
CHAINS=(
    "31337:8545:anvil-main:2"
    "31338:8546:anvil-eth:1"
    "31339:8547:anvil-arb:2"
    "31340:8548:anvil-opt:1"
    "31341:8549:anvil-base:1"
    "31342:8550:anvil-poly:1"
    "31343:8551:anvil-avax:1"
    "31344:8552:anvil-bsc:1"
    "31345:8553:anvil-ftm:1"
    "31346:8554:anvil-zksync:1"
)

SYMBOLS=("ETH/USD" "BTC/USD")
POLL_INTERVAL="${POLL_INTERVAL:-10}"
DEVIATION_THRESHOLD="${DEVIATION_THRESHOLD:-0.5}"

#######################################
# Logging
#######################################

timestamp() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }

log_to_file() {
    echo "[$(timestamp)] [$1] $2" >> "$LOG_FILE"
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

log_error() {
    echo -e "${BLUE}[$(timestamp)]${NC} ${RED}[ERROR]${NC} $1"
    log_to_file "ERROR" "$1"
}

#######################################
# Query Functions
#######################################

get_price() {
    local rpc="$1"
    local contract="$2"
    local symbol="$3"
    
    local result=$(cast call --rpc-url "$rpc" "$contract" \
        "getValue(string)(uint128,uint128)" "$symbol" 2>/dev/null || echo "0\n0")
    
    python3 -c "
import re
raw = '''$result'''
lines = raw.strip().split('\n')
val, ts = 0, 0
try:
    if len(lines) >= 2:
        val_match = re.match(r'^(\d+)', lines[0].strip())
        ts_match = re.match(r'^(\d+)', lines[1].strip())
        if val_match: val = int(val_match.group(1))
        if ts_match: ts = int(ts_match.group(1))
except: pass
print(f'{val} {ts}')
" 2>/dev/null || echo "0 0"
}

wei_to_price() {
    python3 -c "print(f'{int(\"${1:-0}\") / 1e18:.2f}')" 2>/dev/null || echo "0.00"
}

#######################################
# Status Display
#######################################

print_status() {
    clear
    echo ""
    echo "═══════════════════════════════════════════════════════════════════════════════════"
    echo "  Multi-Chain PushOracleReceiverV2 Monitor - $(timestamp)"
    echo "═══════════════════════════════════════════════════════════════════════════════════"
    echo ""
    
    local now=$(date +%s)
    local total_receivers=0
    local active_receivers=0
    
    for chain_config in "${CHAINS[@]}"; do
        IFS=':' read -r chain_id port name num_receivers <<< "$chain_config"
        local rpc="http://localhost:$port"
        local chain_dir="${MULTICHAIN_DIR}/${chain_id}"
        
        # Check if chain is running
        local chain_status="${RED}OFFLINE${NC}"
        if curl -s -X POST -H "Content-Type: application/json" \
           --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
           "$rpc" &>/dev/null; then
            chain_status="${GREEN}ONLINE${NC}"
        fi
        
        echo -e "  ┌─ ${CYAN}Chain $chain_id${NC} ($name) - $chain_status"
        echo -e "  │  RPC: $rpc"
        
        # Get source oracle price
        local oracle_addr=$(cat "${chain_dir}/dia_oracle_v2.addr" 2>/dev/null || echo "")
        local source_eth="" source_btc=""
        
        if [ -n "$oracle_addr" ]; then
            local src_result=$(get_price "$rpc" "$oracle_addr" "ETH/USD")
            source_eth=$(echo "$src_result" | awk '{print $1}')
            src_result=$(get_price "$rpc" "$oracle_addr" "BTC/USD")
            source_btc=$(echo "$src_result" | awk '{print $1}')
        fi
        
        # Check each receiver
        for i in $(seq 1 "$num_receivers"); do
            local receiver_file="${chain_dir}/push_oracle_receiver_v2_${i}.addr"
            total_receivers=$((total_receivers + 1))
            
            if [ -f "$receiver_file" ]; then
                local receiver=$(cat "$receiver_file")
                local short_addr="${receiver:0:10}...${receiver: -6}"
                
                echo "  │"
                echo -e "  │  ${YELLOW}Receiver #$i${NC}: $short_addr"
                
                for symbol in "${SYMBOLS[@]}"; do
                    local result=$(get_price "$rpc" "$receiver" "$symbol")
                    local value=$(echo "$result" | awk '{print $1}')
                    local ts=$(echo "$result" | awk '{print $2}')
                    local price=$(wei_to_price "$value")
                    
                    # Calculate age
                    local age="N/A"
                    if [ "$ts" != "0" ] && [ -n "$ts" ]; then
                        local age_sec=$((now - ts))
                        if [ "$age_sec" -lt 60 ]; then
                            age="${age_sec}s"
                        elif [ "$age_sec" -lt 3600 ]; then
                            age="$((age_sec / 60))m"
                        else
                            age="$((age_sec / 3600))h"
                        fi
                        active_receivers=$((active_receivers + 1))
                    fi
                    
                    # Calculate deviation from source
                    local deviation="N/A"
                    local source_val=""
                    if [ "$symbol" = "ETH/USD" ]; then
                        source_val="$source_eth"
                    else
                        source_val="$source_btc"
                    fi
                    
                    if [ -n "$source_val" ] && [ "$source_val" != "0" ] && [ "$value" != "0" ]; then
                        deviation=$(python3 -c "
src = int('$source_val')
rcv = int('$value')
if rcv > 0:
    dev = abs(src - rcv) / rcv * 100
    print(f'{dev:.2f}%')
else:
    print('N/A')
" 2>/dev/null || echo "N/A")
                    fi
                    
                    # Color code based on status
                    local status_color="$GREEN"
                    if [ "$age" = "N/A" ]; then
                        status_color="$RED"
                    elif [ "${deviation%\%}" != "N/A" ]; then
                        local dev_num=$(echo "$deviation" | tr -d '%')
                        if (( $(echo "$dev_num > $DEVIATION_THRESHOLD" | bc -l 2>/dev/null || echo 0) )); then
                            status_color="$YELLOW"
                        fi
                    fi
                    
                    printf "  │    %-8s \$%-10s  Age: %-6s  Dev: %s\n" \
                        "$symbol" "$price" "$age" "$deviation"
                done
            fi
        done
        echo "  └─"
        echo ""
    done
    
    echo "═══════════════════════════════════════════════════════════════════════════════════"
    echo "  Summary: $active_receivers/$((total_receivers * 2)) price feeds active across $total_receivers receivers"
    echo "  Log: $LOG_FILE"
    echo "  Press Ctrl+C to exit"
    echo "═══════════════════════════════════════════════════════════════════════════════════"
}

#######################################
# Continuous Monitoring
#######################################

monitor_loop() {
    mkdir -p "$LOG_DIR"
    log_info "Starting multi-chain monitor..."
    log_info "Watching ${#CHAINS[@]} chains, $POLL_INTERVAL second interval"
    
    while true; do
        print_status
        sleep "$POLL_INTERVAL"
    done
}

#######################################
# JSON Status Export
#######################################

export_status() {
    mkdir -p "$LOG_DIR"
    
    python3 -c "
import json
from datetime import datetime
import subprocess
import re

chains = '''${CHAINS[*]}'''.split()
multichain_dir = '$MULTICHAIN_DIR'
now = int(datetime.now().timestamp())

status = {
    'timestamp': datetime.utcnow().isoformat() + 'Z',
    'chains': {}
}

for chain_config in chains:
    parts = chain_config.split(':')
    chain_id, port, name, num_receivers = parts[0], parts[1], parts[2], int(parts[3])
    
    chain_status = {
        'name': name,
        'port': int(port),
        'rpc': f'http://localhost:{port}',
        'receivers': []
    }
    
    chain_dir = f'{multichain_dir}/{chain_id}'
    
    for i in range(1, num_receivers + 1):
        receiver_file = f'{chain_dir}/push_oracle_receiver_v2_{i}.addr'
        try:
            with open(receiver_file) as f:
                addr = f.read().strip()
            chain_status['receivers'].append({
                'id': i,
                'address': addr
            })
        except:
            pass
    
    status['chains'][chain_id] = chain_status

print(json.dumps(status, indent=2))
" > "$STATE_FILE"
    
    echo "Status exported to: $STATE_FILE"
    cat "$STATE_FILE"
}

#######################################
# Main
#######################################

show_help() {
    echo "Multi-Chain PushOracleReceiverV2 Monitor"
    echo ""
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  start     Start continuous monitoring (default)"
    echo "  status    Show current status once"
    echo "  export    Export status to JSON"
    echo "  logs      Show recent log entries"
    echo "  help      Show this help"
    echo ""
    echo "Environment Variables:"
    echo "  POLL_INTERVAL        Seconds between updates (default: 10)"
    echo "  DEVIATION_THRESHOLD  Alert threshold % (default: 0.5)"
}

main() {
    local command="${1:-start}"
    
    case "$command" in
        start)
            monitor_loop
            ;;
        status)
            print_status
            ;;
        export)
            export_status
            ;;
        logs)
            if [ -f "$LOG_FILE" ]; then
                tail -50 "$LOG_FILE"
            else
                echo "No log file found"
            fi
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
