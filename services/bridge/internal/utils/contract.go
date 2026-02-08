package utils

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
)

// GetValueFromOracleContract calls the getValue method on an oracle contract
func GetValueFromOracleContract(ctx context.Context, client rpc.EthClient, contractAddress common.Address, symbol string) (*big.Int, uint64, error) {
	const getValueABI = `[{
        "inputs": [{"internalType": "string", "name": "key", "type": "string"}],
        "name": "getValue",
        "outputs": [
            {"internalType": "uint128", "name": "value", "type": "uint128"},
            {"internalType": "uint128", "name": "timestamp", "type": "uint128"}
        ],
        "stateMutability": "view",
        "type": "function"
    }]`

	parsedABI, err := abi.JSON(strings.NewReader(getValueABI))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse ABI: %w", err)
	}

	data, err := parsedABI.Pack("getValue", symbol)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to pack input data: %w", err)
	}

	callMsg := ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}

	resultBytes, err := client.CallContract(ctx, callMsg, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("contract call failed: %w", err)
	}

	outputs, err := parsedABI.Unpack("getValue", resultBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to unpack result: %w", err)
	}

	if len(outputs) != 2 {
		return nil, 0, fmt.Errorf("unexpected number of outputs: got %d, want 2", len(outputs))
	}

	value, ok := outputs[0].(*big.Int)
	if !ok {
		return nil, 0, fmt.Errorf("failed to convert value to big.Int, got type %T: %v", outputs[0], outputs[0])
	}

	timestamp, ok := outputs[1].(*big.Int)
	if !ok {
		return nil, 0, fmt.Errorf("failed to convert timestamp to big.Int, got type %T: %v", outputs[1], outputs[1])
	}

	return value, timestamp.Uint64(), nil
}
