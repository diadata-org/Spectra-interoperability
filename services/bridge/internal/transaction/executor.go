package transaction

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	ethRpc "github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/contracts"
)

type Executor struct {
	receiverClient *contracts.ReceiverClient
	ethClient      ethRpc.EthClient
	chainID        int64
}

func NewExecutor(receiverClient *contracts.ReceiverClient, ethClient ethRpc.EthClient, chainID int64) *Executor {
	return &Executor{
		receiverClient: receiverClient,
		ethClient:      ethClient,
		chainID:        chainID,
	}
}

func (e *Executor) Execute(ctx context.Context, req *Request) (*types.Transaction, error) {
	parsedABI, err := abi.JSON(strings.NewReader(fmt.Sprintf(`[%s]`, req.ABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse method ABI: %w", err)
	}

	if _, exists := parsedABI.Methods[req.MethodName]; !exists {
		return nil, fmt.Errorf("method %s not found in ABI", req.MethodName)
	}

	// Get auth for simulation only (don't allocate nonce yet)
	auth := e.receiverClient.GetAuth()
	contractAddress := common.HexToAddress(req.ContractAddr)

	logger.Infof("[PARAM-DEBUG] Method: %s, Contract: %s, ParamCount: %d", req.MethodName, req.ContractAddr, len(req.Params))

	for i, param := range req.Params {
		if param == nil {
			logger.Infof("[PARAM-DEBUG] params[%d]: NIL", i)
			continue
		}

		switch v := param.(type) {
		case []*big.Int:
			logger.Infof("[PARAM-DEBUG] params[%d]: Type=[]*big.Int, Len=%d", i, len(v))
			if len(v) > 0 && len(v) <= 5 {
				for j, val := range v {
					logger.Infof("[PARAM-DEBUG]   [%d]=%s", j, val.String())
				}
			}
		case []interface{}:
			logger.Infof("[PARAM-DEBUG] params[%d]: Type=[]interface{}, Len=%d - NOT CONVERTED!", i, len(v))
			if len(v) > 0 && len(v) <= 5 {
				for j, val := range v {
					logger.Infof("[PARAM-DEBUG]   [%d]=%T: %v", j, val, val)
				}
			}
		case *big.Int:
			logger.Infof("[PARAM-DEBUG] params[%d]: Type=*big.Int, Value=%s", i, v.String())
		default:
			logger.Infof("[PARAM-DEBUG] params[%d]: Type=%T, Value=%v", i, param, param)
		}
	}

	callData, err := parsedABI.Pack(req.MethodName, req.Params...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack method call: %w", err)
	}

	fromAddress := auth.From

	logger.Infof("Simulating transaction for method %s on contract %s", req.MethodName, req.ContractAddr)
	callMsg := ethereum.CallMsg{
		From:     fromAddress,
		To:       &contractAddress,
		Gas:      req.GasLimit,
		GasPrice: req.GasPrice,
		Value:    big.NewInt(0),
		Data:     callData,
	}

	if _, err := e.ethClient.CallContract(ctx, callMsg, nil); err != nil {
		// Log raw error for debugging - MUST be Error level to see in production
		logger.Errorf("Raw simulation error for chain %d: %v (Type: %T)", e.chainID, err, err)

		revertReason := extractRevertReason(err)

		if revertReason != "" {
			// We have the exact revert reason
			logger.Errorf("Transaction simulation reverted for method %s on contract %s: %s",
				req.MethodName, req.ContractAddr, revertReason)
			return nil, fmt.Errorf("transaction simulation reverted: %s", revertReason)
		} else {
			// Couldn't decode exact error, provide diagnostics
			possibleCauses := identifyPossibleCauses(err)
			logger.Errorf("Transaction simulation failed for method %s on contract %s: %v (Diagnostics: %s)",
				req.MethodName, req.ContractAddr, err, possibleCauses)
			return nil, fmt.Errorf("transaction simulation failed: %w", err)
		}
	}

	logger.Infof("Transaction simulation successful, proceeding to send transaction")

	// CRITICAL: Allocate nonce immediately before sending to minimize staleness window
	// This happens AFTER simulation to reduce the time between allocation and sending
	if err := e.receiverClient.UpdateAuth(ctx, req.GasPrice); err != nil {
		return nil, fmt.Errorf("failed to update auth: %w", err)
	}

	// Refresh auth to get the newly allocated nonce
	auth = e.receiverClient.GetAuth()
	auth.GasLimit = req.GasLimit

	auth.Context = ctx

	// Get the nonce that was allocated
	usedNonce := auth.Nonce.Uint64()

	// Send transaction immediately (within ~1ms of nonce allocation)
	tx, err := bind.NewBoundContract(contractAddress, parsedABI, e.ethClient, e.ethClient, e.ethClient).Transact(auth, req.MethodName, req.Params...)
	if err != nil {
		logger.Errorf("Transaction failed: %v", err)
		// CRITICAL: Notify NonceManager about the error so it can resync if needed
		e.receiverClient.HandleTransactionError(ctx, err, usedNonce)
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	// Mark nonce as successfully sent to mempool
	e.receiverClient.MarkNonceSent(usedNonce, tx.Hash().Hex())

	symbol := "unknown"
	if req.UpdateRequest.Intent != nil {
		symbol = req.UpdateRequest.Intent.Symbol
	}

	chainID := int64(0)
	if req.UpdateRequest.DestinationChain != nil {
		chainID = req.UpdateRequest.DestinationChain.ChainID
	}

	logger.Infof("Transaction sent successfully: %s, router=%s, chain=%d, contract=%s, symbol=%s",
		tx.Hash().Hex(), req.UpdateRequest.RouterID, chainID, req.ContractAddr, symbol)
	return tx, nil
}

func extractRevertReason(err error) string {
	if err == nil {
		return ""
	}

	// Try to extract error data from rpc.DataError
	var dataErr rpc.DataError
	if ok := errors.As(err, &dataErr); ok {
		errorData := dataErr.ErrorData()
		logger.Errorf("RPC DataError detected - ErrorData type: %T, value: %v", errorData, errorData)
		if errorData != nil {
			// Try to decode the error data
			if dataStr, ok := errorData.(string); ok {
				logger.Errorf("Error data as string: %s", dataStr)
				decoded := decodeErrorData(dataStr)
				if decoded != "" {
					return decoded
				}
			}
		}
	}

	msg := err.Error()
	logger.Errorf("Error message for decoding: %s", msg)

	// Try to extract hex error data from error message
	// Look for patterns like "0x82b42900" in the error string
	customError := decodeCustomError(msg)
	if customError != "" {
		logger.Errorf("Decoded custom error: %s", customError)
		return customError
	}

	// Fallback to standard revert reason extraction
	const revertedPrefix = "execution reverted:"
	if strings.Contains(msg, revertedPrefix) {
		parts := strings.SplitN(msg, revertedPrefix, 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}

	logger.Errorf("Failed to extract any error reason from: %v", err)
	return ""
}

// decodeErrorData decodes hex error data and returns the error name
func decodeErrorData(hexData string) string {
	// Remove "0x" prefix if present
	hexData = strings.TrimPrefix(hexData, "0x")

	// Need at least 8 characters for a 4-byte selector
	if len(hexData) < 8 {
		return ""
	}

	// Extract the first 4 bytes (8 hex characters) - this is the error selector
	selector := "0x" + hexData[:8]

	// Look up the error in our known selectors
	errorSelectors := map[string]string{
		"0xe6c4247b": "InvalidAddress() - zero address validation failed",
		"0x4b9257bc": "UnauthorizedMailbox() - sender is not the trusted mailbox",
		"0xca31867a": "UnauthorizedSigner() - signer not in authorizedSigners mapping",
		"0x408b2234": "IntentExpired() - intent timestamp has expired",
		"0x97d96e67": "IntentAlreadyProcessed() - this intent hash was already processed",
		"0x8baa579f": "InvalidSignature() - EIP-712 signature verification failed",
		"0xbbd81708": "NoBalanceToWithdraw() - contract has zero balance",
		"0xf4d678b8": "InsufficientBalance() - withdrawal amount exceeds balance",
		"0x3d56f707": "AmountTransferFailed() - ETH transfer to payment hook failed",
		"0xe76bf378": "InsufficientGasForPayment() - contract balance too low for protocol fees",
		"0x0b7d62e2": "BatchTooLarge() - batch size exceeds MAX_BATCH_SIZE (100)",
		"0x3f71cb25": "InvalidDomainName() - empty domain name provided",
		"0x1703e094": "InvalidDomainVersion() - empty domain version provided",
		"0x7a47c9a2": "InvalidChainId() - zero chain ID provided",
		"0x79548cce": "DomainSeparatorZero() - domain separator is zero",
	}

	if errorName, found := errorSelectors[selector]; found {
		return errorName
	}

	// Unknown selector, return it for debugging
	return fmt.Sprintf("Unknown error selector: %s", selector)
}

// decodeCustomError decodes Solidity custom errors from the error message
func decodeCustomError(errMsg string) string {
	// Map of known error selectors from PushOracleReceiverV2.sol
	errorSelectors := map[string]string{
		"0xe6c4247b": "InvalidAddress() - zero address validation failed",
		"0x4b9257bc": "UnauthorizedMailbox() - sender is not the trusted mailbox",
		"0xca31867a": "UnauthorizedSigner() - signer not in authorizedSigners mapping",
		"0x408b2234": "IntentExpired() - intent timestamp has expired",
		"0x97d96e67": "IntentAlreadyProcessed() - this intent hash was already processed",
		"0x8baa579f": "InvalidSignature() - EIP-712 signature verification failed",
		"0xbbd81708": "NoBalanceToWithdraw() - contract has zero balance",
		"0xf4d678b8": "InsufficientBalance() - withdrawal amount exceeds balance",
		"0x3d56f707": "AmountTransferFailed() - ETH transfer to payment hook failed",
		"0xe76bf378": "InsufficientGasForPayment() - contract balance too low for protocol fees",
		"0x0b7d62e2": "BatchTooLarge() - batch size exceeds MAX_BATCH_SIZE (100)",
		"0x3f71cb25": "InvalidDomainName() - empty domain name provided",
		"0x1703e094": "InvalidDomainVersion() - empty domain version provided",
		"0x7a47c9a2": "InvalidChainId() - zero chain ID provided",
		"0x79548cce": "DomainSeparatorZero() - domain separator is zero",
	}

	// Look for hex error data in the message
	// Common patterns: "0x82b42900", "data: 0x82b42900", etc.
	for selector, errorName := range errorSelectors {
		if strings.Contains(errMsg, selector) {
			return errorName
		}
	}

	// Check for text-based error messages
	errMsgLower := strings.ToLower(errMsg)
	if strings.Contains(errMsgLower, "unauthorized signer") {
		return "UnauthorizedSigner() - signer not in authorizedSigners mapping"
	}
	if strings.Contains(errMsgLower, "already processed") {
		return "IntentAlreadyProcessed() - this intent hash was already processed"
	}
	if strings.Contains(errMsgLower, "invalid signature") {
		return "InvalidSignature() - EIP-712 signature verification failed"
	}
	if strings.Contains(errMsgLower, "insufficient") && strings.Contains(errMsgLower, "gas") {
		return "InsufficientGasForPayment() - contract balance too low for protocol fees"
	}

	return ""
}

// identifyPossibleCauses provides diagnostic suggestions when exact error is unknown
func identifyPossibleCauses(err error) string {
	if err == nil {
		return "unknown"
	}

	// If we have an exact error, don't need possible causes
	errMsg := err.Error()
	if decodeCustomError(errMsg) != "" {
		return "see error details above"
	}

	// Only provide possible causes if we couldn't determine the exact error
	return "Unable to decode exact error. Common issues: 1) signer authorization, 2) duplicate intent, 3) signature validity, 4) contract gas balance, 5) contract not deployed or wrong address"
}
