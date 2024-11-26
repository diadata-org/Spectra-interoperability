package oracle

import (
	"context"
	"math/big"
	"oracleservice/internal/ethclient"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type OracleMetadata struct {
	client  ethclient.EthereumClientProvider
	abi     abi.ABI
	address common.Address
}

func NewOracleMetadata(client ethclient.EthereumClientProvider, abiJSON, address string) (*OracleMetadata, error) {
	parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, err
	}
	return &OracleMetadata{
		client:  client,
		abi:     parsedABI,
		address: common.HexToAddress(address),
	}, nil
}

func (om *OracleMetadata) GetLatestValue(ctx context.Context, symbol string) (*big.Int, error) {
	input, err := om.abi.Pack("getValue", symbol)
	if err != nil {
		return nil, err
	}

	msg := ethereum.CallMsg{To: &om.address, Data: input}
	result, err := om.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, err
	}

	var value1, value2 *big.Int
	err = om.abi.UnpackIntoInterface(&[]interface{}{&value1, &value2}, "getValue", result)
	if err != nil {
		return nil, err
	}

	return value1, nil
}

// Test
// func (om *OracleMetadata) GetLatestValue(ctx context.Context, symbol string) (*big.Int, error) {
// 	fmt.Println("callCount", om.callCount)
// 	if om.currentValue == nil {
// 		om.currentValue = new(big.Int)
// 		om.currentValue.SetInt64(10000000000) // Set initial value to 100
// 		om.callCount = 1
// 		return om.currentValue, nil
// 	}

// 	om.callCount++

// 	// Calculate new value based on call count
// 	newValue := new(big.Int)
// 	newValue.Set(om.currentValue)

// 	if om.callCount == 2 {
// 		om.currentValue.SetInt64(10600000000) // Set initial value to 100

// 	} else if om.callCount > 2 {
// 		om.currentValue.SetInt64(10500000000) // Set initial value to 100

// 	}

// 	return om.currentValue, nil
// }
