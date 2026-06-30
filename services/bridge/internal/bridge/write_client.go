package bridge

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/contracts"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/transaction"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// WriteClient represents a client for write operations to a destination chain
type WriteClient struct {
	chainConfig     *config.ChainConfig
	contracts       []*config.ContractConfig
	client          rpc.EthClient
	txClient        *transaction.Client
	lastUpdate      map[string]time.Time
	mu              sync.RWMutex
	receiverAddress string // hex address of the receiver contract, stored at construction
}

// NewWriteClient creates a new write client for destination operations
func NewWriteClient(chainConfig *config.ChainConfig, contractConfigs []*config.ContractConfig, privateKey string, queueManager *transaction.QueueManager, maxSafeGap uint64) (*WriteClient, error) {
	if chainConfig == nil {
		return nil, fmt.Errorf("chain config cannot be nil")
	}
	if contractConfigs == nil {
		return nil, fmt.Errorf("contract configs cannot be nil")
	}
	if privateKey == "" {
		return nil, fmt.Errorf("private key cannot be empty")
	}

	client, err := rpc.NewMultiClient(chainConfig.RPCURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to destination chain: %w", err)
	}
	logger.Infof("Connected to destination chain %s via %s", chainConfig.Name, client.GetCurrentRPCURL())

	var receiverAddress string
	for _, contract := range contractConfigs {
		if (contract.Type == "receiver" || contract.Type == "pushoracle") && contract.Enabled {
			receiverAddress = contract.Address
			break
		}
	}
	if receiverAddress == "" {
		return nil, fmt.Errorf("no enabled receiver contract found")
	}

	// Use the multi-client for on-chain calls (failover + retries)
	receiverClient, err := contracts.NewReceiverClient(
		client,
		common.HexToAddress(receiverAddress),
		privateKey,
		maxSafeGap,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create receiver client: %w", err)
	}

	txClient := transaction.NewClient(receiverClient, client, queueManager, chainConfig.ChainID)

	return &WriteClient{
		chainConfig:     chainConfig,
		contracts:       contractConfigs,
		client:          client,
		txClient:        txClient,
		lastUpdate:      make(map[string]time.Time),
		receiverAddress: receiverAddress,
	}, nil
}

// updateLastUpdate updates the last update time for a specific symbol and contract
func (wc *WriteClient) updateLastUpdate(symbol, contract string) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	// Key format: "chainID-symbol-contract" to track per-oracle updates
	key := fmt.Sprintf("%d-%s-%s", wc.chainConfig.ChainID, symbol, contract)
	wc.lastUpdate[key] = time.Now()
	logger.Debugf("Updated lastUpdate for %s on chain %d", key, wc.chainConfig.ChainID)
}

// getLastUpdate returns the last update time for a specific symbol and contract, or zero time if not found
func (wc *WriteClient) getLastUpdate(symbol, contract string) time.Time {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	// Key format: "chainID-symbol-contract" to track per-oracle updates
	key := fmt.Sprintf("%d-%s-%s", wc.chainConfig.ChainID, symbol, contract)
	return wc.lastUpdate[key]
}

// getGasPrice gets the current gas price for a destination chain
func (wc *WriteClient) getGasPrice(ctx context.Context) (*big.Int, error) {
	gasPrice, err := wc.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, err
	}

	multiplier := wc.chainConfig.GasMultiplier
	if multiplier == 0 {
		multiplier = 1.2
	}

	multiplierInt := int64(multiplier * 100)
	gasPrice.Mul(gasPrice, big.NewInt(multiplierInt))
	gasPrice.Div(gasPrice, big.NewInt(100))

	for _, contract := range wc.contracts {
		if contract.MaxGasPrice != "" {
			maxGasPrice := new(big.Int)
			maxGasPrice, ok := maxGasPrice.SetString(contract.MaxGasPrice, 10)
			if ok && gasPrice.Cmp(maxGasPrice) > 0 {
				logger.Warnf("Gas price %s exceeds max %s, using max", gasPrice.String(), maxGasPrice.String())
				gasPrice = maxGasPrice
			}
			break
		}
	}

	logger.Infof("Using gas price: %s wei (%s gwei)", gasPrice.String(),
		new(big.Int).Div(gasPrice, big.NewInt(1e9)).String())

	return gasPrice, nil
}

func (wc *WriteClient) callRouterMethod(ctx context.Context, updateReq *bridgetypes.UpdateRequest, gasPrice *big.Int, gasLimit uint64) (*types.Transaction, error) {
	methodConfig := updateReq.DestinationMethodConfig

	params, err := wc.txClient.BuildParams(methodConfig, updateReq)
	if err != nil {
		return nil, fmt.Errorf("failed to build method params: %w", err)
	}

	return wc.txClient.CallMethod(ctx, updateReq.Contract.Address, methodConfig.Name, methodConfig.ABI, params, gasPrice, gasLimit, updateReq)
}

func (wc *WriteClient) GetEthClient() rpc.EthClient {
	return wc.client
}

// ---- Destination interface implementation ----
//
// These four methods make *WriteClient satisfy the Destination interface so
// dispatch sites can hold a Destination instead of a concrete *WriteClient.
// The methods delegate to the existing fields without changing the EVM path.

// Kind returns the chain backend kind.
func (wc *WriteClient) Kind() string { return "evm" }

// ReceiverAddress returns the destination contract address (hex with 0x prefix).
func (wc *WriteClient) ReceiverAddress() string {
	return wc.receiverAddress
}

// ChainID returns the EVM chain ID.
func (wc *WriteClient) ChainID() int64 {
	if wc.chainConfig == nil {
		return 0
	}
	return wc.chainConfig.ChainID
}

// Send wraps callRouterMethod and adapts its return shape to TxResult.
// It fetches the gas price internally and uses DefaultGasLimit when the
// UpdateRequest carries no DestinationMethodConfig gas limit.
func (wc *WriteClient) Send(ctx context.Context, req *bridgetypes.UpdateRequest) (TxResult, error) {
	gasPrice, err := wc.getGasPrice(ctx)
	if err != nil {
		return TxResult{}, err
	}

	gasLimit := DefaultGasLimit
	if req.DestinationMethodConfig != nil && req.DestinationMethodConfig.GasLimit > 0 {
		gasLimit = req.DestinationMethodConfig.GasLimit
	}

	tx, err := wc.callRouterMethod(ctx, req, gasPrice, gasLimit)
	if err != nil {
		return TxResult{}, err
	}
	return TxResult{TxID: tx.Hash().Hex(), Status: "Processed"}, nil
}
