package contracts

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// OracleIntentRegistryABI is the ABI for the OracleIntentRegistry contract
const OracleIntentRegistryABI = `[
	{
		"anonymous": false,
		"inputs": [
			{
				"indexed": true,
				"internalType": "bytes32",
				"name": "intentHash",
				"type": "bytes32"
			},
			{
				"indexed": true,
				"internalType": "string",
				"name": "symbol",
				"type": "string"
			},
			{
				"indexed": false,
				"internalType": "uint256",
				"name": "price",
				"type": "uint256"
			},
			{
				"indexed": false,
				"internalType": "uint256",
				"name": "timestamp",
				"type": "uint256"
			},
			{
				"indexed": false,
				"internalType": "address",
				"name": "signer",
				"type": "address"
			}
		],
		"name": "IntentRegistered",
		"type": "event"
	},
	{
		"inputs": [
			{
				"internalType": "bytes32",
				"name": "intentHash",
				"type": "bytes32"
			}
		],
		"name": "getIntent",
		"outputs": [
			{
				"components": [
					{
						"internalType": "string",
						"name": "intentType",
						"type": "string"
					},
					{
						"internalType": "string",
						"name": "version",
						"type": "string"
					},
					{
						"internalType": "uint256",
						"name": "chainId",
						"type": "uint256"
					},
					{
						"internalType": "uint256",
						"name": "nonce",
						"type": "uint256"
					},
					{
						"internalType": "uint256",
						"name": "expiry",
						"type": "uint256"
					},
					{
						"internalType": "string",
						"name": "symbol",
						"type": "string"
					},
					{
						"internalType": "uint256",
						"name": "price",
						"type": "uint256"
					},
					{
						"internalType": "uint256",
						"name": "timestamp",
						"type": "uint256"
					},
					{
						"internalType": "string",
						"name": "source",
						"type": "string"
					},
					{
						"internalType": "bytes",
						"name": "signature",
						"type": "bytes"
					},
					{
						"internalType": "address",
						"name": "signer",
						"type": "address"
					}
				],
				"internalType": "struct OracleIntentRegistry.OracleIntent",
				"name": "",
				"type": "tuple"
			}
		],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [
			{
				"internalType": "string",
				"name": "symbol",
				"type": "string"
			}
		],
		"name": "getLatestPrice",
		"outputs": [
			{
				"internalType": "uint256",
				"name": "price",
				"type": "uint256"
			},
			{
				"internalType": "uint256",
				"name": "timestamp",
				"type": "uint256"
			},
			{
				"internalType": "string",
				"name": "source",
				"type": "string"
			}
		],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [
			{
				"internalType": "string",
				"name": "",
				"type": "string"
			}
		],
		"name": "latestIntentBySymbol",
		"outputs": [
			{
				"internalType": "bytes32",
				"name": "",
				"type": "bytes32"
			}
		],
		"stateMutability": "view",
		"type": "function"
	}
]`

// RegistryClient wraps the OracleIntentRegistry contract
type RegistryClient struct {
	client   *ethclient.Client
	address  common.Address
	abi      abi.ABI
	contract *BoundContract
}

// BoundContract represents a bound contract instance
type BoundContract struct {
	address common.Address
	abi     abi.ABI
	client  *ethclient.Client
}

// NewRegistryClient creates a new registry client
func NewRegistryClient(client *ethclient.Client, address common.Address) (*RegistryClient, error) {
	parsedABI, err := abi.JSON(strings.NewReader(OracleIntentRegistryABI))
	if err != nil {
		return nil, err
	}

	return &RegistryClient{
		client:  client,
		address: address,
		abi:     parsedABI,
		contract: &BoundContract{
			address: address,
			abi:     parsedABI,
			client:  client,
		},
	}, nil
}

// GetIntent retrieves an intent by its hash
func (r *RegistryClient) GetIntent(ctx context.Context, intentHash common.Hash) (*bridgeTypes.OracleIntent, error) {
	result, err := r.contract.Call(ctx, "getIntent", intentHash)
	if err != nil {
		return nil, err
	}

	// Parse the result
	intentData := result[0].(struct {
		IntentType string         `json:"intentType"`
		Version    string         `json:"version"`
		ChainId    *big.Int       `json:"chainId"`
		Nonce      *big.Int       `json:"nonce"`
		Expiry     *big.Int       `json:"expiry"`
		Symbol     string         `json:"symbol"`
		Price      *big.Int       `json:"price"`
		Timestamp  *big.Int       `json:"timestamp"`
		Source     string         `json:"source"`
		Signature  []byte         `json:"signature"`
		Signer     common.Address `json:"signer"`
	})

	return &bridgeTypes.OracleIntent{
		IntentType: intentData.IntentType,
		Version:    intentData.Version,
		ChainID:    intentData.ChainId,
		Nonce:      intentData.Nonce,
		Expiry:     intentData.Expiry,
		Symbol:     intentData.Symbol,
		Price:      intentData.Price,
		Timestamp:  intentData.Timestamp,
		Source:     intentData.Source,
		Signature:  intentData.Signature,
		Signer:     intentData.Signer,
	}, nil
}

// GetLatestPrice retrieves the latest price for a symbol
func (r *RegistryClient) GetLatestPrice(ctx context.Context, symbol string) (*big.Int, *big.Int, string, error) {
	result, err := r.contract.Call(ctx, "getLatestPrice", symbol)
	if err != nil {
		return nil, nil, "", err
	}

	price := result[0].(*big.Int)
	timestamp := result[1].(*big.Int)
	source := result[2].(string)

	return price, timestamp, source, nil
}

// GetLatestIntentBySymbol retrieves the latest intent hash for a symbol
func (r *RegistryClient) GetLatestIntentBySymbol(ctx context.Context, symbol string) (common.Hash, error) {
	result, err := r.contract.Call(ctx, "latestIntentBySymbol", symbol)
	if err != nil {
		return common.Hash{}, err
	}

	return result[0].(common.Hash), nil
}

// WatchIntentRegistered watches for IntentRegistered events
func (r *RegistryClient) WatchIntentRegistered(ctx context.Context, fromBlock uint64, callback func(*bridgeTypes.IntentRegisteredEvent)) error {
	// Create filter query
	query := ethereum.FilterQuery{
		Addresses: []common.Address{r.address},
		FromBlock: new(big.Int).SetUint64(fromBlock),
		Topics: [][]common.Hash{
			{r.abi.Events["IntentRegistered"].ID},
		},
	}

	// Subscribe to logs
	logs := make(chan types.Log)
	sub, err := r.client.SubscribeFilterLogs(ctx, query, logs)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-sub.Err():
			return err
		case vLog := <-logs:
			event, err := r.parseIntentRegisteredEvent(vLog)
			if err != nil {
				continue // Skip invalid events
			}
			callback(event)
		}
	}
}

// GetIntentRegisteredEvents retrieves historical IntentRegistered events
func (r *RegistryClient) GetIntentRegisteredEvents(ctx context.Context, fromBlock, toBlock uint64) ([]*bridgeTypes.IntentRegisteredEvent, error) {
	// Create filter query
	query := ethereum.FilterQuery{
		Addresses: []common.Address{r.address},
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
		Topics: [][]common.Hash{
			{r.abi.Events["IntentRegistered"].ID},
		},
	}

	// Get logs
	logs, err := r.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, err
	}

	// Parse events
	var events []*bridgeTypes.IntentRegisteredEvent
	for _, vLog := range logs {
		event, err := r.parseIntentRegisteredEvent(vLog)
		if err != nil {
			continue // Skip invalid events
		}
		events = append(events, event)
	}

	return events, nil
}

// parseIntentRegisteredEvent parses an IntentRegistered event from a log
func (r *RegistryClient) parseIntentRegisteredEvent(vLog types.Log) (*bridgeTypes.IntentRegisteredEvent, error) {
	event := &bridgeTypes.IntentRegisteredEvent{
		BlockNumber: vLog.BlockNumber,
		TxHash:      vLog.TxHash,
	}

	// Parse event data
	err := r.abi.UnpackIntoInterface(event, "IntentRegistered", vLog.Data)
	if err != nil {
		return nil, err
	}

	// Parse indexed topics
	if len(vLog.Topics) >= 2 {
		event.IntentHash = vLog.Topics[1]
	}
	if len(vLog.Topics) >= 3 {
		// Symbol is indexed but it's a string, so we need to handle it differently
		// For now, we'll extract it from the event data
	}

	return event, nil
}

// Call makes a contract call
func (bc *BoundContract) Call(ctx context.Context, method string, params ...interface{}) ([]interface{}, error) {
	input, err := bc.abi.Pack(method, params...)
	if err != nil {
		return nil, err
	}

	msg := ethereum.CallMsg{
		To:   &bc.address,
		Data: input,
	}

	result, err := bc.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, err
	}

	// Check if result is empty
	if len(result) == 0 {
		return nil, fmt.Errorf("empty result from contract call to %s", method)
	}

	return bc.abi.Unpack(method, result)
}