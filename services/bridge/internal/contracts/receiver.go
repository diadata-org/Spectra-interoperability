package contracts

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
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
	client       *ethclient.Client
	address      common.Address
	abi          abi.ABI
	contract     *BoundContract
	auth         *bind.TransactOpts
	nonceManager *NonceManager
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

	logger.Infof("Creating receiver client for chain ID: %s", chainID.String())

	auth, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		return nil, err
	}
	
	// Log the sender address
	logger.Infof("Receiver client sender address: %s", auth.From.Hex())

	return &ReceiverClient{
		client:  client,
		address: address,
		abi:     parsedABI,
		contract: &BoundContract{
			address: address,
			abi:     parsedABI,
			client:  client,
		},
		auth:         auth,
		nonceManager: NewNonceManager(client, auth.From),
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
		logger.Errorf("Failed to pack intent data for symbol %s: %v", intent.Symbol, err)
		return nil, err
	}

	// Simulate the transaction first to check for reverts
	callMsg := ethereum.CallMsg{
		From:     r.auth.From,
		To:       &r.address,
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Value:    r.auth.Value,
		Data:     input,
	}
	
	_, err = r.client.CallContract(ctx, callMsg, nil)
	if err != nil {
		logger.Errorf("Transaction simulation failed for symbol %s: %v", intent.Symbol, err)
		// Try to extract revert reason
		if revertReason := r.extractRevertReason(ctx, callMsg); revertReason != "" {
			logger.Errorf("Revert reason for %s: %s", intent.Symbol, revertReason)
		}
		return nil, fmt.Errorf("simulation failed: %w", err)
	}

	// Log transaction details before sending
	logger.Infof("Preparing transaction for symbol %s: nonce=%d, gas_limit=%d, gas_price=%s, from=%s, to=%s",
		intent.Symbol, r.auth.Nonce.Uint64(), gasLimit, gasPrice.String(), 
		r.auth.From.Hex(), r.address.Hex())

	// Get chain ID for transaction
	chainID, err := r.client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	// Create EIP-1559 transaction for better compatibility
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     r.auth.Nonce.Uint64(),
		GasTipCap: gasPrice,     // Use gasPrice as tip
		GasFeeCap: gasPrice,     // Use gasPrice as max fee
		Gas:       gasLimit,
		To:        &r.address,
		Value:     r.auth.Value,
		Data:      input,
	})

	// Sign transaction
	signedTx, err := r.auth.Signer(r.auth.From, tx)
	if err != nil {
		logger.Errorf("Failed to sign transaction for symbol %s, nonce %d: %v", 
			intent.Symbol, r.auth.Nonce.Uint64(), err)
		return nil, err
	}

	// Send transaction
	usedNonce := r.auth.Nonce.Uint64()
	
	// Log transaction details before sending
	logger.Infof("Sending raw transaction: nonce=%d, gas=%d, gasPrice=%s, value=%s, to=%s, dataLen=%d",
		usedNonce, signedTx.Gas(), signedTx.GasPrice().String(), signedTx.Value().String(),
		signedTx.To().Hex(), len(signedTx.Data()))
	
	err = r.client.SendTransaction(ctx, signedTx)
	if err != nil {
		logger.Errorf("Failed to send transaction for symbol %s, nonce %d, tx_hash %s: %v", 
			intent.Symbol, usedNonce, signedTx.Hash().Hex(), err)
		// Handle the error appropriately
		r.nonceManager.HandleError(ctx, err, usedNonce)
		return nil, err
	}

	logger.Infof("Transaction sent successfully for symbol %s: tx_hash=%s, nonce=%d", 
		intent.Symbol, signedTx.Hash().Hex(), usedNonce)
	
	// Immediately check if transaction is in mempool
	go func() {
		time.Sleep(2 * time.Second)
		_, pending, err := r.client.TransactionByHash(context.Background(), signedTx.Hash())
		if err != nil {
			logger.Errorf("Transaction %s not found in mempool after 2s: %v", signedTx.Hash().Hex(), err)
		} else {
			logger.Infof("Transaction %s found in mempool, pending=%v", signedTx.Hash().Hex(), pending)
		}
	}()
	
	// Don't confirm nonce immediately - wait for actual confirmation
	// r.nonceManager.ConfirmNonce(usedNonce)

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
	symbols := make([]string, len(intents))
	for i, intent := range intents {
		contractIntents[i] = r.intentToContractStruct(intent)
		symbols[i] = intent.Symbol
	}

	// Pack the intent data
	input, err := r.abi.Pack("handleBatchIntentUpdates", contractIntents)
	if err != nil {
		logger.Errorf("Failed to pack batch intent data for symbols %v: %v", symbols, err)
		return nil, err
	}

	// Simulate the transaction first to check for reverts
	callMsg := ethereum.CallMsg{
		From:     r.auth.From,
		To:       &r.address,
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Value:    r.auth.Value,
		Data:     input,
	}
	
	_, err = r.client.CallContract(ctx, callMsg, nil)
	if err != nil {
		logger.Errorf("Batch transaction simulation failed for symbols %v: %v", symbols, err)
		// Try to extract revert reason
		if revertReason := r.extractRevertReason(ctx, callMsg); revertReason != "" {
			logger.Errorf("Revert reason for batch %v: %s", symbols, revertReason)
		}
		return nil, fmt.Errorf("simulation failed: %w", err)
	}

	// Log transaction details before sending
	logger.Infof("Preparing batch transaction for %d intents (symbols: %v): nonce=%d, gas_limit=%d, gas_price=%s, from=%s, to=%s",
		len(intents), symbols, r.auth.Nonce.Uint64(), gasLimit, gasPrice.String(), 
		r.auth.From.Hex(), r.address.Hex())

	// Get chain ID for transaction
	chainID, err := r.client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	// Create EIP-1559 transaction for better compatibility
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     r.auth.Nonce.Uint64(),
		GasTipCap: gasPrice,     // Use gasPrice as tip
		GasFeeCap: gasPrice,     // Use gasPrice as max fee
		Gas:       gasLimit,
		To:        &r.address,
		Value:     r.auth.Value,
		Data:      input,
	})

	// Sign transaction
	signedTx, err := r.auth.Signer(r.auth.From, tx)
	if err != nil {
		logger.Errorf("Failed to sign batch transaction for symbols %v, nonce %d: %v", 
			symbols, r.auth.Nonce.Uint64(), err)
		return nil, err
	}

	// Send transaction
	usedNonce := r.auth.Nonce.Uint64()
	err = r.client.SendTransaction(ctx, signedTx)
	if err != nil {
		logger.Errorf("Failed to send batch transaction for symbols %v, nonce %d, tx_hash %s: %v", 
			symbols, usedNonce, signedTx.Hash().Hex(), err)
		// Handle the error appropriately
		r.nonceManager.HandleError(ctx, err, usedNonce)
		return nil, err
	}

	logger.Infof("Batch transaction sent successfully for symbols %v: tx_hash=%s, nonce=%d", 
		symbols, signedTx.Hash().Hex(), usedNonce)
	
	// Confirm the nonce was used successfully
	r.nonceManager.ConfirmNonce(usedNonce)

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
	// Get next nonce from nonce manager
	nonce, err := r.nonceManager.GetNextNonce(ctx)
	if err != nil {
		logger.Errorf("Failed to get next nonce for address %s: %v", r.auth.From.Hex(), err)
		return err
	}

	// Check retry count for this nonce
	retryCount := r.nonceManager.GetRetryCount(nonce)
	if retryCount > 0 {
		// This is a replacement transaction - bump gas price significantly
		// Increase by 50% minimum, plus 10% per retry
		bumpPercent := 150 + (10 * retryCount)
		if bumpPercent > 300 {
			bumpPercent = 300 // Cap at 3x original price
		}
		
		originalGasPrice := new(big.Int).Set(gasPrice)
		gasPrice = new(big.Int).Mul(gasPrice, big.NewInt(int64(bumpPercent)))
		gasPrice.Div(gasPrice, big.NewInt(100))
		
		logger.Warnf("Replacement transaction for nonce %d (retry %d): bumping gas from %s to %s (%d%%)", 
			nonce, retryCount, originalGasPrice.String(), gasPrice.String(), bumpPercent)
	}

	logger.Infof("Nonce allocated for address %s: nonce=%d, gas_price=%s", 
		r.auth.From.Hex(), nonce, gasPrice.String())

	r.auth.Nonce = big.NewInt(int64(nonce))
	r.auth.GasPrice = gasPrice

	return nil
}

// GetAddress returns the contract address
func (r *ReceiverClient) GetAddress() common.Address {
	return r.address
}

// GetAuth returns the transactor auth
func (r *ReceiverClient) GetAuth() *bind.TransactOpts {
	return r.auth
}

// GetFromAddress returns the transaction sender address
func (r *ReceiverClient) GetFromAddress() common.Address {
	return r.auth.From
}

// intentToContractStruct converts a bridgeTypes.OracleIntent to the contract struct format
func (r *ReceiverClient) intentToContractStruct(intent *bridgeTypes.OracleIntent) interface{} {
	return struct {
		IntentType string
		Version    string
		ChainId    *big.Int
		Nonce      *big.Int
		Expiry     *big.Int
		Symbol     string
		Price      *big.Int
		Timestamp  *big.Int
		Source     string
		Signature  []byte
		Signer     common.Address
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

// extractRevertReason attempts to extract the revert reason from a failed call
func (r *ReceiverClient) extractRevertReason(ctx context.Context, callMsg ethereum.CallMsg) string {
	// Try to get more detailed error by calling eth_call
	_, err := r.client.CallContract(ctx, callMsg, nil)
	if err == nil {
		return ""
	}
	
	errStr := err.Error()
	
	// Common patterns for revert reasons
	// Pattern 1: "execution reverted: <reason>"
	if strings.Contains(errStr, "execution reverted: ") {
		parts := strings.Split(errStr, "execution reverted: ")
		if len(parts) > 1 {
			return strings.TrimSpace(parts[1])
		}
	}
	
	// Pattern 2: Error in hex format (0x08c379a0... for Error(string))
	if strings.Contains(errStr, "0x") {
		// Extract hex data
		start := strings.Index(errStr, "0x")
		if start >= 0 {
			hexData := errStr[start:]
			// Find end of hex string
			end := strings.IndexFunc(hexData, func(r rune) bool {
				return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F'))
			})
			if end > 0 {
				hexData = hexData[:end]
			}
			
			// Try to decode as Error(string)
			if reason := r.decodeErrorString(hexData); reason != "" {
				return reason
			}
		}
	}
	
	// Pattern 3: Direct revert message
	if strings.Contains(errStr, "revert") {
		return errStr
	}
	
	return ""
}

// decodeErrorString decodes the standard Error(string) revert reason
func (r *ReceiverClient) decodeErrorString(hexData string) string {
	// Error(string) selector is 0x08c379a0
	errorSelector := "0x08c379a0"
	
	if !strings.HasPrefix(hexData, errorSelector) {
		return ""
	}
	
	// Remove 0x prefix
	data := strings.TrimPrefix(hexData, "0x")
	
	// Remove function selector (4 bytes = 8 hex chars)
	if len(data) < 8 {
		return ""
	}
	data = data[8:]
	
	// Decode the remaining data as string
	bytes, err := hex.DecodeString(data)
	if err != nil {
		return ""
	}
	
	// The string is ABI encoded: offset (32 bytes) + length (32 bytes) + data
	if len(bytes) < 64 {
		return ""
	}
	
	// Get string length (bytes 32-63)
	length := new(big.Int).SetBytes(bytes[32:64]).Uint64()
	if uint64(len(bytes)) < 64+length {
		return ""
	}
	
	// Extract string data
	return string(bytes[64 : 64+length])
}