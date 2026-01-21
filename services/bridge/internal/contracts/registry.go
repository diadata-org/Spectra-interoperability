package contracts

import (
	"context"
	"fmt"

	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
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
	client   rpc.EthClient
	address  common.Address
	abi      abi.ABI
	contract *BoundContract
}

// BoundContract represents a bound contract instance
type BoundContract struct {
	address common.Address
	abi     abi.ABI
	client  rpc.EthClient
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
