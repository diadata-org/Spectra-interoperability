package bridge

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
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
	bridge *Bridge
}

// NewTransactionHandler creates a new transaction handler
func NewTransactionHandler(b *Bridge) *TransactionHandler {
	return &TransactionHandler{bridge: b}
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

	logger.Infof("Transaction sent: %s for %s on chain %d, router=%s, symbol=%s",
		tx.Hash().Hex(), txCtx.Identifier, txCtx.UpdateRequest.DestinationChain.ChainID,
		txCtx.UpdateRequest.RouterID, txCtx.Symbol)

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

	destClient := h.bridge.writeClients[updateReq.DestinationChain.ChainID]
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
		Symbol:        extractSymbol(updateReq, h.bridge.routerRegistry),
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

	// Check and update authorization for intents with signers
	// if txCtx.UpdateRequest.Intent.Signer != (common.Address{}) {
	// 	return h.authorizeIntent(txCtx)
	// }

	return nil
}

// func (h *TransactionHandler) authorizeIntent(txCtx *TransactionContext) error {
// 	signer := txCtx.UpdateRequest.Intent.Signer
// 	contract := txCtx.DestClient.receiverClient.GetAddress()

// 	logger.Infof("Checking signer authorization for %s on chain %d, contract %s",
// 		signer.Hex(), txCtx.UpdateRequest.DestinationChain.ChainID, contract.Hex())

// 	isAuthorized, err := txCtx.DestClient.receiverClient.IsAuthorizedSigner(txCtx.Ctx, signer)
// 	if err != nil {
// 		return fmt.Errorf("failed to check signer authorization: %w", err)
// 	}

// 	if !isAuthorized {
// 		return fmt.Errorf("signer %s is not authorized on contract %s", signer.Hex(), contract.Hex())
// 	}

// 	// UpdateAuth sets up the transaction auth with correct nonce and gas price
// 	// This must be called before sending the transaction to ensure sequential nonce
// 	if err := txCtx.DestClient.receiverClient.UpdateAuth(txCtx.Ctx, txCtx.GasPrice); err != nil {
// 		return fmt.Errorf("failed to update auth: %w", err)
// 	}

// 	return nil
// }

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

	tx, err := txCtx.DestClient.callRouterMethod(txCtx.Ctx, txCtx.DestClient, txCtx.UpdateRequest, txCtx.GasPrice, gasLimit)
	if err != nil {
		logTransactionError(err, txCtx.UpdateRequest.Intent)
		return nil, err
	}

	return tx, nil
}

// executeLegacy executes using legacy HandleIntentUpdate method
// func (h *TransactionHandler) executeLegacy(txCtx *TransactionContext) (*types.Transaction, error) {
// 	logger.Infof("Sending transaction for %s on chain %d with gas limit %d (legacy), router=%s, symbol=%s",
// 		txCtx.Identifier, txCtx.UpdateRequest.DestinationChain.ChainID, DefaultGasLimit,
// 		txCtx.UpdateRequest.RouterID, txCtx.Symbol)

// 	tx, err := txCtx.DestClient.receiverClient.HandleIntentUpdate(
// 		txCtx.Ctx, txCtx.UpdateRequest.Intent, DefaultGasLimit, txCtx.GasPrice)
// 	if err != nil {
// 		logTransactionError(err, txCtx.UpdateRequest.Intent)
// 		return nil, err
// 	}

// 	return tx, nil
// }

// confirm waits for transaction confirmation and updates state
func (h *TransactionHandler) confirm(txCtx *TransactionContext, tx *types.Transaction) error {
	h.recordSubmission(txCtx, tx.Hash().Hex())

	receipt, err := h.bridge.waitForReceipt(txCtx.Ctx, txCtx.DestClient.client, tx.Hash())
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
	if h.bridge.metricsTracker != nil && txCtx.UpdateRequest.Intent != nil {
		h.bridge.metricsTracker.RecordIntentSubmitted(
			txCtx.UpdateRequest.Intent,
			fmt.Sprintf("%d", txCtx.UpdateRequest.DestinationChain.ChainID),
			txHash,
			txCtx.GasPrice,
		)
	}
}

// recordFailure records transaction failure metrics
func (h *TransactionHandler) recordFailure(txCtx *TransactionContext, stage, reason string) {
	if h.bridge.metricsTracker != nil && txCtx.UpdateRequest.Intent != nil {
		h.bridge.metricsTracker.RecordIntentFailed(txCtx.UpdateRequest.Intent, stage, reason)
	}
}

// recordConfirmation records transaction confirmation metrics
func (h *TransactionHandler) recordConfirmation(txCtx *TransactionContext, txHash string, gasUsed uint64) {
	if h.bridge.metricsTracker != nil && txCtx.UpdateRequest.Intent != nil {
		h.bridge.metricsTracker.RecordIntentConfirmed(txCtx.UpdateRequest.Intent, txHash, gasUsed)
	}
}

// updateState updates bridge state after successful transaction
func (h *TransactionHandler) updateState(txCtx *TransactionContext) {
	if txCtx.UpdateRequest.Intent != nil {
		txCtx.DestClient.updateLastUpdate(txCtx.UpdateRequest.Intent.Symbol)
	}

	if txCtx.UpdateRequest.RouterID != "" && h.bridge.routerRegistry != nil {
		router := h.bridge.routerRegistry.GetRouterByID(txCtx.UpdateRequest.RouterID)
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
