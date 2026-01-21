#!/usr/bin/env bash
set -euo pipefail

# Configuration
ORACLE_ADDRESS=${1:-""}
PRIVATE_KEY=${2:-"0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"}
RPC_URL=${3:-"http://localhost:8545"}

if [ -z "$ORACLE_ADDRESS" ]; then
    echo "Usage: $0 <oracle_address> [private_key] [rpc_url]"
    exit 1
fi

echo "Starting price updater for Oracle: $ORACLE_ADDRESS"
echo "RPC: $RPC_URL"

# Price update function
update_price() {
    local symbol=$1
    local price=$2

    echo "Updating $symbol price to $price"

    # Convert price to wei (multiply by 1e18 for 18 decimals)
    local price_wei=$(python3 -c "print(int($price * 1e18))")

    # Get current timestamp
    local timestamp=$(date +%s)

    # Call setValue on the oracle
    cast send \
        --rpc-url "$RPC_URL" \
        --private-key "$PRIVATE_KEY" \
        "$ORACLE_ADDRESS" \
        "setValue(string,uint128,uint128)" \
        "$symbol" \
        "$price_wei" \
        "$timestamp" \
        2>/dev/null || echo "Failed to update $symbol"
}

# Generate realistic but mock prices with configurable deviation
generate_eth_price() {
    local force_deviation=${1:-"false"}
    python3 -c "
import random
base_price = 2250
force_deviation = '$force_deviation' == 'true'

if force_deviation:
    # Force a deviation > 0.5% (between 0.6% and 2%)
    deviation = random.uniform(0.006, 0.02)
    if random.random() > 0.5:
        deviation = -deviation
    change = deviation
else:
    # Normal volatility (sometimes < 0.5%, sometimes > 0.5%)
    volatility = 0.05
    change = random.uniform(-volatility, volatility)

price = base_price * (1 + change)
print(f'{price:.2f}')
"
}

generate_btc_price() {
    local force_deviation=${1:-"false"}
    python3 -c "
import random
base_price = 45000
force_deviation = '$force_deviation' == 'true'

if force_deviation:
    # Force a deviation > 0.5% (between 0.6% and 2%)
    deviation = random.uniform(0.006, 0.02)
    if random.random() > 0.5:
        deviation = -deviation
    change = deviation
else:
    # Normal volatility (sometimes < 0.5%, sometimes > 0.5%)
    volatility = 0.03
    change = random.uniform(-volatility, volatility)

price = base_price * (1 + change)
print(f'{price:.2f}')
"
}

# Main loop
echo "Starting continuous price updates (Ctrl+C to stop)..."
echo "Price deviation threshold: 0.5%"
echo "Time threshold: 2 minutes"
echo ""

counter=0
while true; do
    counter=$((counter + 1))

    # Every 4th update (every 40 seconds), force a large deviation to test deviation-based routing
    # Other updates use normal volatility which may or may not exceed 0.5%
    if [ $((counter % 4)) -eq 0 ]; then
        echo "[DEVIATION TEST] Forcing >0.5% price deviation..."
        eth_price=$(generate_eth_price "true")
        btc_price=$(generate_btc_price "true")
    else
        echo "[NORMAL UPDATE] Regular price update..."
        eth_price=$(generate_eth_price "false")
        btc_price=$(generate_btc_price "false")
    fi

    # Update prices
    update_price "ETH/USD" "$eth_price"
    update_price "BTC/USD" "$btc_price"

    echo "Updated: ETH/USD=$eth_price, BTC/USD=$btc_price"
    echo ""

    # Wait 10 seconds before next update
    sleep 10
done