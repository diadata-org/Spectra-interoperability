package bridge

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/leader"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/pkg/router"
)

// Constants for gas configuration
const (
	DefaultGasLimit = uint64(300000)
)

// TransactionContext encapsulates all data needed for transaction processing
type TransactionContext struct {
	Ctx           context.Context
	UpdateRequest *bridgetypes.UpdateRequest
	DestClient    *WriteClient
	GasPrice      *big.Int
	Identifier    string
	Symbol        string
}

// TransactionHandler handles the complete lifecycle of a transaction
type TransactionHandler struct {
	writeClients   map[int64]*WriteClient
	routerRegistry *router.GenericRegistry
	metricsTracker *MetricsTracker
	onChainMonitor *leader.OnChainMonitor // Optional: for replica monitoring info
}

// NewTransactionHandler creates a new transaction handler
func NewTransactionHandler(writeClients map[int64]*WriteClient, registry *router.GenericRegistry, tracker *MetricsTracker, monitor *leader.OnChainMonitor) *TransactionHandler {
	return &TransactionHandler{
		writeClients:   writeClients,
		routerRegistry: registry,
		metricsTracker: tracker,
		onChainMonitor: monitor,
	}
}

// Process handles the complete transaction lifecycle
func (h *TransactionHandler) Process(ctx context.Context, updateReq *bridgetypes.UpdateRequest) error {
	txCtx, err := h.buildContext(ctx, updateReq)
	if err != nil {
		return err
	}

	logger.Infof("Processing update for %s on chain %d", txCtx.Identifier, txCtx.UpdateRequest.DestinationChain.ChainID)

	if err := h.validate(txCtx); err != nil {
		return err
	}

	tx, err := h.execute(txCtx)
	if err != nil {
		h.recordFailure(txCtx, "submission", "transaction_failed")
		return fmt.Errorf("failed to send transaction: %w", err)
	}

	triggeredByMonitoring := ""
	if txCtx.UpdateRequest.TriggeredByMonitoring && h.onChainMonitor != nil {
		monitoringInfo := h.getMonitoringInfo(txCtx)
		triggeredByMonitoring = monitoringInfo
	}
	logger.Infof("Transaction sent: %s for %s on chain %d, router=%s, symbol=%s%s",
		tx.Hash().Hex(), txCtx.Identifier, txCtx.UpdateRequest.DestinationChain.ChainID,
		txCtx.UpdateRequest.RouterID, txCtx.Symbol, triggeredByMonitoring)

	return h.confirm(txCtx, tx)
}

// buildContext creates the transaction context with all necessary data
func (h *TransactionHandler) buildContext(ctx context.Context, updateReq *bridgetypes.UpdateRequest) (*TransactionContext, error) {
	if updateReq == nil {
		return nil, fmt.Errorf("update request is nil")
	}

	if updateReq.DestinationChain == nil {
		return nil, fmt.Errorf("destination chain is nil")
	}

	destClient := h.writeClients[updateReq.DestinationChain.ChainID]
	if destClient == nil {
		return nil, fmt.Errorf("destination client not found for chain %d", updateReq.DestinationChain.ChainID)
	}

	gasPrice, err := destClient.getGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %w", err)
	}

	return &TransactionContext{
		Ctx:           ctx,
		UpdateRequest: updateReq,
		DestClient:    destClient,
		GasPrice:      gasPrice,
		Identifier:    extractIdentifier(updateReq),
		Symbol:        extractSymbol(updateReq, h.routerRegistry),
	}, nil
}

// validate performs all validation checks
func (h *TransactionHandler) validate(txCtx *TransactionContext) error {
	if txCtx.UpdateRequest.Intent == nil {
		// Event-based requests don't require intent validation
		return nil
	}

	// Check expiry
	if isExpired(txCtx.UpdateRequest.Intent) {
		h.recordFailure(txCtx, "validation", "intent_expired")
		return fmt.Errorf("intent expired")
	}

	return nil
}

// execute builds and sends the transaction
func (h *TransactionHandler) execute(txCtx *TransactionContext) (*types.Transaction, error) {
	if txCtx.UpdateRequest.DestinationMethodConfig != nil {
		return h.executeWithMethodConfig(txCtx)
	}
	return nil, fmt.Errorf("no destination method configuration provided")
}

// executeWithMethodConfig executes using router-specified method configuration
func (h *TransactionHandler) executeWithMethodConfig(txCtx *TransactionContext) (*types.Transaction, error) {
	methodConfig := txCtx.UpdateRequest.DestinationMethodConfig
	gasLimit := getGasLimit(methodConfig)

	logger.Infof("Sending transaction for %s on chain %d using method %s with gas limit %d, router=%s, symbol=%s",
		txCtx.Identifier, txCtx.UpdateRequest.DestinationChain.ChainID, methodConfig.Name,
		gasLimit, txCtx.UpdateRequest.RouterID, txCtx.Symbol)

	tx, err := txCtx.DestClient.callRouterMethod(txCtx.Ctx, txCtx.UpdateRequest, txCtx.GasPrice, gasLimit)
	if err != nil {
		logTransactionError(err, txCtx.UpdateRequest.Intent)
		return nil, err
	}

	return tx, nil
}

// confirm waits for transaction confirmation and updates state
func (h *TransactionHandler) confirm(txCtx *TransactionContext, tx *types.Transaction) error {
	h.recordSubmission(txCtx, tx.Hash().Hex())

	receipt, err := h.waitForReceipt(txCtx.Ctx, txCtx.DestClient.client, tx.Hash())
	if err != nil {
		h.recordFailure(txCtx, "confirmation", "receipt_timeout")
		return fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	if receipt.Status == 0 {
		h.recordFailure(txCtx, "confirmation", "transaction_reverted")
		logRevertedTransaction(tx, receipt, txCtx)
		return fmt.Errorf("transaction reverted (status: 0): hash=%s, symbol=%s, gas=%d",
			tx.Hash().Hex(), txCtx.Symbol, receipt.GasUsed)
	}

	h.recordConfirmation(txCtx, tx.Hash().Hex(), receipt.GasUsed)
	h.updateState(txCtx)

	logger.Infof("Transaction confirmed: %s, status: %d, gas used: %d, router=%s, symbol=%s",
		tx.Hash().Hex(), receipt.Status, receipt.GasUsed, txCtx.UpdateRequest.RouterID, txCtx.Symbol)

	return nil
}

// recordSubmission records transaction submission metrics
func (h *TransactionHandler) recordSubmission(txCtx *TransactionContext, txHash string) {
	if h.metricsTracker != nil && txCtx.UpdateRequest.Intent != nil {
		h.metricsTracker.RecordIntentSubmitted(
			txCtx.UpdateRequest.Intent,
			fmt.Sprintf("%d", txCtx.UpdateRequest.DestinationChain.ChainID),
			txHash,
			txCtx.GasPrice,
		)
	}
}

// recordFailure records transaction failure metrics
func (h *TransactionHandler) recordFailure(txCtx *TransactionContext, stage, reason string) {
	if h.metricsTracker != nil && txCtx.UpdateRequest.Intent != nil {
		h.metricsTracker.RecordIntentFailed(txCtx.UpdateRequest.Intent, stage, reason)
	}
}

// recordConfirmation records transaction confirmation metrics
func (h *TransactionHandler) recordConfirmation(txCtx *TransactionContext, txHash string, gasUsed uint64) {
	if h.metricsTracker != nil && txCtx.UpdateRequest.Intent != nil {
		h.metricsTracker.RecordIntentConfirmed(txCtx.UpdateRequest.Intent, txHash, gasUsed)
	}
}

// updateState updates bridge state after successful transaction
func (h *TransactionHandler) updateState(txCtx *TransactionContext) {
	if txCtx.UpdateRequest.Intent != nil && txCtx.UpdateRequest.Contract != nil {
		txCtx.DestClient.updateLastUpdate(txCtx.UpdateRequest.Intent.Symbol, txCtx.UpdateRequest.Contract.Address)
	}

	if txCtx.UpdateRequest.RouterID != "" && h.routerRegistry != nil {
		router := h.routerRegistry.GetRouterByID(txCtx.UpdateRequest.RouterID)
		if router != nil {
			eventName := ""
			if txCtx.UpdateRequest.Event != nil {
				eventName = txCtx.UpdateRequest.Event.EventName
			}
			router.OnRouted(eventName, txCtx.UpdateRequest.ExtractedData)
		}
	}
}

// Helper functions

func extractIdentifier(updateReq *bridgetypes.UpdateRequest) string {
	if updateReq.Intent != nil {
		return updateReq.Intent.Symbol
	}
	if updateReq.Event != nil {
		return fmt.Sprintf("%s(requestId:%s)", updateReq.Event.EventName, updateReq.Event.RequestId.String())
	}
	return "unknown"
}

// extractSymbol extracts the symbol from the request
func extractSymbol(updateReq *bridgetypes.UpdateRequest, registry *router.GenericRegistry) string {
	if updateReq.Intent != nil && updateReq.Intent.Symbol != "" {
		return updateReq.Intent.Symbol
	}

	// Try to extract from router if available
	if updateReq.ExtractedData != nil && updateReq.RouterID != "" && registry != nil {
		if routerInstance := registry.GetRouterByID(updateReq.RouterID); routerInstance != nil {
			if symbol := routerInstance.GetSymbolFromData(updateReq.ExtractedData); symbol != "" && symbol != "unknown" {
				return symbol
			}
		}
	}

	return "unknown"
}

// isExpired checks if an intent has expired
func isExpired(intent *bridgetypes.OracleIntent) bool {
	currentTime := time.Now().Unix()
	if intent.Expiry.Int64() < currentTime {
		expiryTime := time.Unix(intent.Expiry.Int64(), 0)
		logger.Warnf("Intent expired for %s: expired at %s (current: %s)",
			intent.Symbol,
			expiryTime.Format(time.RFC3339),
			time.Unix(currentTime, 0).Format(time.RFC3339))
		return true
	}
	return false
}

// getGasLimit returns the gas limit from method config or default
func getGasLimit(methodConfig *config.DestinationMethodConfig) uint64 {
	if methodConfig.GasLimit > 0 {
		return methodConfig.GasLimit
	}
	return DefaultGasLimit
}

// logTransactionError logs detailed information about transaction errors
func logTransactionError(err error, intent *bridgetypes.OracleIntent) {
	if intent == nil {
		return
	}

	logger.Errorf("Transaction error: %v", err)

	// Log additional detail for simulation failures
	if contains(err.Error(), "simulation failed") {
		logger.Errorf("Intent details: symbol=%s, price=%s, timestamp=%s, nonce=%s, expiry=%s, signer=%s",
			intent.Symbol,
			intent.Price.String(),
			intent.Timestamp.String(),
			intent.Nonce.String(),
			intent.Expiry.String(),
			intent.Signer.Hex())
	}
}

// logRevertedTransaction logs detailed information about reverted transactions
func logRevertedTransaction(tx *types.Transaction, receipt *types.Receipt, txCtx *TransactionContext) {
	logger.Errorf("Transaction REVERTED: hash=%s, symbol=%s, gas_used=%d, chain=%d",
		tx.Hash().Hex(), txCtx.Symbol, receipt.GasUsed, txCtx.UpdateRequest.DestinationChain.ChainID)
	logger.Debugf("Revert details: router=%s, contract=%s",
		txCtx.UpdateRequest.RouterID, txCtx.UpdateRequest.Contract.Address)
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// getMonitoringInfo returns detailed monitoring information when transaction is triggered by replica monitoring
func (h *TransactionHandler) getMonitoringInfo(txCtx *TransactionContext) string {
	if h.onChainMonitor == nil {
		return " (triggered by replica monitoring/failover)"
	}

	chainID := txCtx.UpdateRequest.DestinationChain.ChainID
	contractAddress := common.HexToAddress(txCtx.UpdateRequest.Contract.Address)
	symbol := txCtx.Symbol

	// Get configured threshold (base + 10% of base)
	threshold := h.onChainMonitor.GetPriceDeviationThresholdWithOffset(chainID, contractAddress, symbol)
	if threshold == nil {
		return " (triggered by replica monitoring/failover)"
	}
	thresholdPercent := new(big.Float).Mul(threshold, big.NewFloat(100))

	// Get on-chain value and calculate deviation
	var deviationPercent *big.Float
	var triggerReason string

	// Get monitoring info from monitor
	onChainValue, lastTimestamp, timeThreshold := h.onChainMonitor.GetMonitoringInfo(chainID, contractAddress, symbol)

	// Calculate deviation from incoming price
	if txCtx.UpdateRequest.Intent != nil && txCtx.UpdateRequest.Intent.Price != nil && onChainValue != nil && onChainValue.Sign() != 0 {
		incomingPrice := txCtx.UpdateRequest.Intent.Price
		diff := new(big.Int).Sub(incomingPrice, onChainValue)
		oldFloat := new(big.Float).SetInt(onChainValue)
		diffFloat := new(big.Float).SetInt(diff)
		deviationPercent = new(big.Float).Quo(diffFloat, oldFloat)
		deviationPercent.Mul(deviationPercent, big.NewFloat(100))
	}

	// Determine trigger reason
	if lastTimestamp > 0 {
		timeSinceUpdate := time.Since(time.Unix(int64(lastTimestamp), 0))
		totalThreshold := timeThreshold + h.onChainMonitor.GetTimeThresholdOffset()
		if timeSinceUpdate > totalThreshold {
			triggerReason = fmt.Sprintf("time threshold exceeded (%v > %v)", timeSinceUpdate, totalThreshold)
		} else if deviationPercent != nil {
			absDeviation := new(big.Float).Abs(deviationPercent)
			if absDeviation.Cmp(thresholdPercent) > 0 {
				triggerReason = fmt.Sprintf("price deviation threshold exceeded (%.2f%% > %.2f%%)", absDeviation, thresholdPercent)
			} else {
				// This shouldn't happen if we're processing, but log it for debugging
				triggerReason = fmt.Sprintf("price deviation + 10%% of base not reached (%.2f%% <= %.2f%%)", absDeviation, thresholdPercent)
			}
		}
	}

	// Build info string
	info := " (triggered by replica monitoring/failover"
	if thresholdPercent != nil {
		info += fmt.Sprintf(", configured_threshold=%.2f%%", thresholdPercent)
	}
	if deviationPercent != nil {
		absDeviation := new(big.Float).Abs(deviationPercent)
		info += fmt.Sprintf(", deviation_from_onchain=%.2f%%", absDeviation)
	} else {
		info += ", deviation_from_onchain=N/A"
	}
	if triggerReason != "" {
		info += fmt.Sprintf(", trigger=%s", triggerReason)
	} else {
		info += ", trigger=unknown"
	}
	info += ")"

	return info
}

// waitForReceipt waits for a transaction receipt
func (h *TransactionHandler) waitForReceipt(ctx context.Context, client rpc.EthClient, txHash common.Hash) (*types.Receipt, error) {
	logger.Infof("Waiting for transaction receipt: %s", txHash.Hex())

	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	attempts := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for transaction receipt after 5 minutes")
		case <-ticker.C:
			attempts++
			receipt, err := client.TransactionReceipt(ctx, txHash)
			if err != nil {
				if attempts%12 == 0 { // Log every minute
					logger.Debugf("Still waiting for receipt %s (attempt %d): %v", txHash.Hex(), attempts, err)
				}
				continue
			}
			logger.Infof("Transaction receipt received: %s, status: %d, gas used: %d",
				txHash.Hex(), receipt.Status, receipt.GasUsed)
			return receipt, nil
		}
	}
}
