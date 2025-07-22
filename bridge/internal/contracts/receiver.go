package contracts

import (
	"context"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// PushOracleReceiverABI is the ABI for the PushOracleReceiver contract
const PushOracleReceiverABI = `[
	{
		"inputs": [
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
				"internalType": "struct IPushOracleReceiver.OracleIntent",
				"name": "intent",
				"type": "tuple"
			}
		],
		"name": "handleIntentUpdate",
		"outputs": [],
		"stateMutability": "payable",
		"type": "function"
	},
	{
		"inputs": [
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
				"internalType": "struct IPushOracleReceiver.OracleIntent[]",
				"name": "intents",
				"type": "tuple[]"
			}
		],
		"name": "handleBatchIntentUpdates",
		"outputs": [],
		"stateMutability": "payable",
		"type": "function"
	},
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
		"name": "IntentBasedUpdateReceived",
		"type": "event"
	},
	{
		"inputs": [
			{
				"internalType": "address",
				"name": "_signer",
				"type": "address"
			}
		],
		"name": "isAuthorizedSigner",
		"outputs": [
			{
				"internalType": "bool",
				"name": "",
				"type": "bool"
			}
		],
		"stateMutability": "view",
		"type": "function"
	}
]`

// ReceiverClient wraps the PushOracleReceiver contract
type ReceiverClient struct {
	client   *ethclient.Client
	address  common.Address
	abi      abi.ABI
	contract *BoundContract
	auth     *bind.TransactOpts
}

// NewReceiverClient creates a new receiver client
func NewReceiverClient(client *ethclient.Client, address common.Address, privateKey string) (*ReceiverClient, error) {
	parsedABI, err := abi.JSON(strings.NewReader(PushOracleReceiverABI))
	if err != nil {
		return nil, err
	}

	// Create auth from private key
	key, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return nil, err
	}

	chainID, err := client.ChainID(context.Background())
	if err != nil {
		return nil, err
	}

	auth, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		return nil, err
	}

	return &ReceiverClient{
		client:  client,
		address: address,
		abi:     parsedABI,
		contract: &BoundContract{
			address: address,
			abi:     parsedABI,
			client:  client,
		},
		auth: auth,
	}, nil
}

// HandleIntentUpdate sends a single intent update to the receiver contract
func (r *ReceiverClient) HandleIntentUpdate(ctx context.Context, intent *bridgeTypes.OracleIntent, gasLimit uint64, gasPrice *big.Int) (*types.Transaction, error) {
	// Set gas parameters
	r.auth.GasLimit = gasLimit
	r.auth.GasPrice = gasPrice
	r.auth.Context = ctx

	// Pack the intent data
	input, err := r.abi.Pack("handleIntentUpdate", r.intentToContractStruct(intent))
	if err != nil {
		return nil, err
	}

	// Create transaction
	tx := types.NewTransaction(
		r.auth.Nonce.Uint64(),
		r.address,
		r.auth.Value,
		gasLimit,
		gasPrice,
		input,
	)

	// Sign transaction
	signedTx, err := r.auth.Signer(r.auth.From, tx)
	if err != nil {
		return nil, err
	}

	// Send transaction
	err = r.client.SendTransaction(ctx, signedTx)
	if err != nil {
		return nil, err
	}

	return signedTx, nil
}

// HandleBatchIntentUpdates sends multiple intent updates in a single transaction
func (r *ReceiverClient) HandleBatchIntentUpdates(ctx context.Context, intents []*bridgeTypes.OracleIntent, gasLimit uint64, gasPrice *big.Int) (*types.Transaction, error) {
	// Set gas parameters
	r.auth.GasLimit = gasLimit
	r.auth.GasPrice = gasPrice
	r.auth.Context = ctx

	// Convert intents to contract structs
	contractIntents := make([]interface{}, len(intents))
	for i, intent := range intents {
		contractIntents[i] = r.intentToContractStruct(intent)
	}

	// Pack the intent data
	input, err := r.abi.Pack("handleBatchIntentUpdates", contractIntents)
	if err != nil {
		return nil, err
	}

	// Create transaction
	tx := types.NewTransaction(
		r.auth.Nonce.Uint64(),
		r.address,
		r.auth.Value,
		gasLimit,
		gasPrice,
		input,
	)

	// Sign transaction
	signedTx, err := r.auth.Signer(r.auth.From, tx)
	if err != nil {
		return nil, err
	}

	// Send transaction
	err = r.client.SendTransaction(ctx, signedTx)
	if err != nil {
		return nil, err
	}

	return signedTx, nil
}

// IsAuthorizedSigner checks if an address is an authorized signer
func (r *ReceiverClient) IsAuthorizedSigner(ctx context.Context, signer common.Address) (bool, error) {
	result, err := r.contract.Call(ctx, "isAuthorizedSigner", signer)
	if err != nil {
		return false, err
	}

	return result[0].(bool), nil
}

// UpdateAuth updates the transaction auth with new nonce and gas price
func (r *ReceiverClient) UpdateAuth(ctx context.Context, gasPrice *big.Int) error {
	// Get pending nonce
	nonce, err := r.client.PendingNonceAt(ctx, r.auth.From)
	if err != nil {
		return err
	}

	r.auth.Nonce = big.NewInt(int64(nonce))
	r.auth.GasPrice = gasPrice

	return nil
}

// GetAddress returns the contract address
func (r *ReceiverClient) GetAddress() common.Address {
	return r.address
}

// GetFromAddress returns the transaction sender address
func (r *ReceiverClient) GetFromAddress() common.Address {
	return r.auth.From
}

// intentToContractStruct converts a bridgeTypes.OracleIntent to the contract struct format
func (r *ReceiverClient) intentToContractStruct(intent *bridgeTypes.OracleIntent) interface{} {
	return struct {
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
	}{
		IntentType: intent.IntentType,
		Version:    intent.Version,
		ChainId:    intent.ChainID,
		Nonce:      intent.Nonce,
		Expiry:     intent.Expiry,
		Symbol:     intent.Symbol,
		Price:      intent.Price,
		Timestamp:  intent.Timestamp,
		Source:     intent.Source,
		Signature:  intent.Signature,
		Signer:     intent.Signer,
	}
}