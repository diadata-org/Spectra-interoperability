package contracts

import (
	"context"
	"math/big"
	"strings"
	"sync"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
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

// ReceiverClient wraps the PushOracleReceiver contract using a failover RPC client
type ReceiverClient struct {
	client       rpc.EthClient
	address      common.Address
	abi          abi.ABI
	auth         *bind.TransactOpts
	nonceManager *NonceManager
	mu           sync.Mutex
}

// NewReceiverClient creates a new receiver client using an EthClient with failover support
func NewReceiverClient(client rpc.EthClient, address common.Address, privateKey string, maxSafeGap uint64) (*ReceiverClient, error) {
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

	logger.Infof("Creating receiver client for chain ID: %s, maxSafeGap: %d", chainID.String(), maxSafeGap)

	auth, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		return nil, err
	}

	// Log the sender address
	logger.Infof("Receiver client sender address: %s", auth.From.Hex())

	return &ReceiverClient{
		client:       client,
		address:      address,
		abi:          parsedABI,
		auth:         auth,
		nonceManager: NewNonceManager(client, auth.From, chainID.Int64(), maxSafeGap),
	}, nil
}

// UpdateAuth updates the transaction auth with new nonce and gas price
func (r *ReceiverClient) UpdateAuth(ctx context.Context, gasPrice *big.Int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	nonce, err := r.nonceManager.GetNextNonce(ctx)
	if err != nil {
		logger.Errorf("Failed to get next nonce for address %s: %v", r.auth.From.Hex(), err)
		return err
	}

	retryCount := r.nonceManager.GetRetryCount(nonce)
	if retryCount > 0 {
		bumpPercent := 150 + (10 * retryCount)
		if bumpPercent > 300 {
			bumpPercent = 300
		}

		originalGasPrice := new(big.Int).Set(gasPrice)
		gasPrice = new(big.Int).Mul(gasPrice, big.NewInt(int64(bumpPercent)))
		gasPrice.Div(gasPrice, big.NewInt(100))

		logger.Warnf("Replacement transaction for nonce %d (retry %d): bumping gas from %s to %s (%d%%)",
			nonce, retryCount, originalGasPrice.String(), gasPrice.String(), bumpPercent)
	}

	logger.Infof("Nonce allocated for address %s on chain %d: nonce=%d, gas_price=%s",
		r.auth.From.Hex(), r.nonceManager.chainID, nonce, gasPrice.String())

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

	// Pattern 3: Direct revert message
	if strings.Contains(errStr, "revert") {
		return errStr
	}

	return ""
}

// HandleTransactionError forwards transaction errors to the NonceManager for handling
func (r *ReceiverClient) HandleTransactionError(ctx context.Context, err error, usedNonce uint64) {
	r.nonceManager.HandleError(ctx, err, usedNonce)
}

// MarkNonceSent marks a nonce as successfully sent to the mempool
func (r *ReceiverClient) MarkNonceSent(nonce uint64, txHash string) {
	r.nonceManager.MarkSent(nonce, txHash)
}
