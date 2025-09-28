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

# Generate realistic but mock prices
generate_eth_price() {
    # ETH around $2000-2500 with some volatility
    python3 -c "
import random
import math
base_price = 2250
volatility = 0.05
change = random.uniform(-volatility, volatility)
price = base_price * (1 + change)
print(f'{price:.2f}')
"
}

generate_btc_price() {
    # BTC around $40000-50000 with some volatility
    python3 -c "
import random
import math
base_price = 45000
volatility = 0.03
change = random.uniform(-volatility, volatility)
price = base_price * (1 + change)
print(f'{price:.2f}')
"
}

# Main loop
echo "Starting continuous price updates (Ctrl+C to stop)..."
while true; do
    # Generate new prices
    eth_price=$(generate_eth_price)
    btc_price=$(generate_btc_price)

    # Update prices
    update_price "ETH/USD" "$eth_price"
    update_price "BTC/USD" "$btc_price"

    echo "Updated: ETH/USD=$eth_price, BTC/USD=$btc_price"

    # Wait 10 seconds before next update
    sleep 10
done