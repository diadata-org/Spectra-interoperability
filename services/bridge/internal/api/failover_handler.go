package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/contracts"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// FailoverHandler handles failover requests from the Hyperlane monitor
type FailoverHandler struct {
	db            *database.DB
	privateKey    *ecdsa.PrivateKey
	destinations  map[int64]*DestinationConfig
	clients       map[int64]*ethclient.Client
	requestStatus map[string]*FailoverStatus
	mu            sync.RWMutex
	metrics       *metrics.Metrics
	intentMetrics *metrics.IntentMetrics
}

// DestinationConfig holds configuration for a destination chain
type DestinationConfig struct {
	ChainID         int64
	ReceiverAddress common.Address
	GasLimit        uint64
}

// FailoverStatus tracks the status of a failover request
type FailoverStatus struct {
	RequestID       string
	Status          string
	TransactionHash string
	Error           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewFailoverHandler creates a new failover handler
func NewFailoverHandler(cfgService *config.ConfigService, db *database.DB, serviceMetrics *metrics.Metrics, intentMetrics *metrics.IntentMetrics) (*FailoverHandler, error) {
	// Parse private key
	privateKeyHex := cfgService.GetInfrastructure().PrivateKey
	if len(privateKeyHex) == 0 {
		return nil, fmt.Errorf("private key is required")
	}
	// Remove 0x prefix if present
	if strings.HasPrefix(privateKeyHex, "0x") {
		privateKeyHex = privateKeyHex[2:]
	}
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	handler := &FailoverHandler{
		db:            db,
		privateKey:    privateKey,
		destinations:  make(map[int64]*DestinationConfig),
		clients:       make(map[int64]*ethclient.Client),
		requestStatus: make(map[string]*FailoverStatus),
		metrics:       serviceMetrics,
		intentMetrics: intentMetrics,
	}

	// Configure destinations from enabled chains
	for _, chain := range cfgService.GetEnabledChains() {
		// Find receiver contract for this chain
		var receiverAddr string
		contracts := cfgService.GetContractsForChain(chain.ChainID)
		for _, contract := range contracts {
			if contract.Type == "pushoracle" && contract.Enabled {
				receiverAddr = contract.Address
				break
			}
		}

		if receiverAddr == "" {
			logger.Warnf("No receiver contract found for chain %d", chain.ChainID)
			continue
		}

		handler.destinations[chain.ChainID] = &DestinationConfig{
			ChainID:         chain.ChainID,
			ReceiverAddress: common.HexToAddress(receiverAddr),
			GasLimit:        500000, // Default gas limit
		}

		// Create client
		if len(chain.RPCURLs) > 0 {
			client, err := ethclient.Dial(chain.RPCURLs[0])
			if err != nil {
				logger.Errorf("Failed to connect to chain %d: %v", chain.ChainID, err)
				continue
			}
			handler.clients[chain.ChainID] = client
		}
	}

	return handler, nil
}

// FailoverRequest represents a request to trigger failover delivery
type FailoverRequest struct {
	MessageID          string                    `json:"message_id"`
	IntentHash         string                    `json:"intent_hash"`
	PairID             string                    `json:"pair_id"`
	SourceChainID      int                       `json:"source_chain_id"`
	DestinationChainID int                       `json:"destination_chain_id"`
	ReceiverAddress    string                    `json:"receiver_address"`
	IntentData         *bridgetypes.OracleIntent `json:"intent_data"`
	Reason             string                    `json:"reason"`

	// Phase tracking timestamps
	DetectionTimestamp       int64  `json:"detection_timestamp"`
	MonitoringStartTimestamp int64  `json:"monitoring_start_timestamp"`
	FailoverTimestamp        int64  `json:"failover_timestamp"`
	ReceiverKey              string `json:"receiver_key"`
}

// FailoverResponse represents the response to a failover request
type FailoverResponse struct {
	RequestID       string    `json:"request_id"`
	Status          string    `json:"status"`
	TransactionHash string    `json:"transaction_hash,omitempty"`
	Error           string    `json:"error,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

// RegisterRoutes registers the failover API routes
func (h *FailoverHandler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/failover/trigger", h.TriggerFailover).Methods("POST")
	router.HandleFunc("/failover/status/{requestId}", h.GetFailoverStatus).Methods("GET")
	router.HandleFunc("/failover/stats", h.GetFailoverStats).Methods("GET")
}

// TriggerFailover handles POST /api/v1/failover/trigger
func (h *FailoverHandler) TriggerFailover(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	defer func() {
		// Record HTTP request metrics
		if h.metrics != nil {
			h.metrics.RecordHTTPRequest("POST", "/api/v1/failover/trigger", "202", time.Since(startTime).Seconds(), 0)
		}
	}()

	// Read the body first to debug it
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, "Failed to read request body: "+err.Error())
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Debug log the raw request
	logger.WithFields(logger.Fields{
		"body_size": len(bodyBytes),
		"body":      string(bodyBytes),
	}).Info("Raw failover request body")

	var req FailoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Validate request
	if req.IntentData == nil {
		h.sendError(w, http.StatusBadRequest, "Intent data is required")
		return
	}

	if req.ReceiverAddress == "" {
		h.sendError(w, http.StatusBadRequest, "Receiver address is required")
		return
	}

	// Generate request ID
	requestID := uuid.New().String()

	// Record failover request metric
	if h.metrics != nil {
		h.metrics.RecordFailoverRequest(
			fmt.Sprintf("%d", req.SourceChainID),
			fmt.Sprintf("%d", req.DestinationChainID),
		)
	}

	logger.WithFields(logger.Fields{
		"request_id":  requestID,
		"message_id":  req.MessageID,
		"intent_hash": req.IntentHash,
		"source":      req.SourceChainID,
		"destination": req.DestinationChainID,
		"receiver":    req.ReceiverAddress,
		"reason":      req.Reason,
	}).Info("Received failover request")

	// Check if destination is configured
	destConfig, exists := h.destinations[int64(req.DestinationChainID)]
	if !exists {
		h.sendError(w, http.StatusBadRequest, fmt.Sprintf("Destination chain %d not configured", req.DestinationChainID))
		return
	}

	// Check if client exists
	client, exists := h.clients[int64(req.DestinationChainID)]
	if !exists {
		h.sendError(w, http.StatusServiceUnavailable, fmt.Sprintf("No client available for chain %d", req.DestinationChainID))
		return
	}

	// Store initial status
	h.mu.Lock()
	h.requestStatus[requestID] = &FailoverStatus{
		RequestID: requestID,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	h.mu.Unlock()

	// Debug log the intent data
	if req.IntentData != nil {
		sigHex := ""
		if req.IntentData.Signature != nil {
			sigHex = fmt.Sprintf("0x%x", req.IntentData.Signature)
		}
		logger.WithFields(logger.Fields{
			"intent_type":   req.IntentData.IntentType,
			"symbol":        req.IntentData.Symbol,
			"signature_len": len(req.IntentData.Signature),
			"signature_nil": req.IntentData.Signature == nil,
			"signature":     sigHex,
			"signer":        req.IntentData.Signer.Hex(),
		}).Info("Received intent data in failover request")
	}

	// Process asynchronously
	go h.processFailover(requestID, req, destConfig, client)

	// Send response
	response := FailoverResponse{
		RequestID: requestID,
		Status:    "accepted",
		Timestamp: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

// intentToContractStruct converts a bridgeTypes.OracleIntent to the contract struct format
func (h *FailoverHandler) intentToContractStruct(intent *bridgetypes.OracleIntent) interface{} {
	// Debug log all fields
	logger.WithFields(logger.Fields{
		"intentType":    intent.IntentType,
		"version":       intent.Version,
		"chainId":       intent.ChainID,
		"nonce":         intent.Nonce,
		"expiry":        intent.Expiry,
		"symbol":        intent.Symbol,
		"price":         intent.Price,
		"timestamp":     intent.Timestamp,
		"source":        intent.Source,
		"signature_len": len(intent.Signature),
		"signature_nil": intent.Signature == nil,
		"signer":        intent.Signer.Hex(),
	}).Info("Converting intent to contract struct")

	// Ensure all big.Int fields are not nil
	chainId := intent.ChainID
	if chainId == nil {
		chainId = big.NewInt(0)
	}
	nonce := intent.Nonce
	if nonce == nil {
		nonce = big.NewInt(0)
	}
	expiry := intent.Expiry
	if expiry == nil {
		expiry = big.NewInt(0)
	}
	price := intent.Price
	if price == nil {
		price = big.NewInt(0)
	}
	timestamp := intent.Timestamp
	if timestamp == nil {
		timestamp = big.NewInt(0)
	}
	signature := []byte(intent.Signature)
	if signature == nil {
		signature = []byte{}
	}

	// Include ABI tags so fields align with the contract tuple
	return struct {
		IntentType string         `abi:"intentType"`
		Version    string         `abi:"version"`
		ChainId    *big.Int       `abi:"chainId"`
		Nonce      *big.Int       `abi:"nonce"`
		Expiry     *big.Int       `abi:"expiry"`
		Symbol     string         `abi:"symbol"`
		Price      *big.Int       `abi:"price"`
		Timestamp  *big.Int       `abi:"timestamp"`
		Source     string         `abi:"source"`
		Signature  []byte         `abi:"signature"`
		Signer     common.Address `abi:"signer"`
	}{
		IntentType: intent.IntentType,
		Version:    intent.Version,
		ChainId:    chainId,
		Nonce:      nonce,
		Expiry:     expiry,
		Symbol:     intent.Symbol,
		Price:      price,
		Timestamp:  timestamp,
		Source:     intent.Source,
		Signature:  signature,
		Signer:     intent.Signer,
	}
}

// processFailover handles the actual failover transaction
func (h *FailoverHandler) processFailover(requestID string, req FailoverRequest, destConfig *DestinationConfig, client *ethclient.Client) {
	startTime := time.Now()

	// Update status
	h.updateStatus(requestID, "processing", "", "")

	// Prepare transaction data
	intentData := req.IntentData

	// Validate intent data
	if intentData == nil {
		h.updateStatus(requestID, "failed", "", "Intent data is nil")
		if h.metrics != nil {
			h.metrics.RecordFailoverProcessing(
				fmt.Sprintf("%d", req.SourceChainID),
				fmt.Sprintf("%d", req.DestinationChainID),
				time.Since(startTime).Seconds(),
				false,
				"intent_data_nil",
			)
		}
		return
	}

	// Log intent data for debugging
	logger.WithFields(logger.Fields{
		"intent_type":   intentData.IntentType,
		"symbol":        intentData.Symbol,
		"chainId":       intentData.ChainID,
		"price":         intentData.Price,
		"nonce":         intentData.Nonce,
		"signature_len": len(intentData.Signature),
		"signer":        intentData.Signer.Hex(),
	}).Info("Processing failover with intent data")

	// Track intent lifecycle using the intent's timestamp
	if h.intentMetrics != nil && intentData.Timestamp != nil {
		intentTime := time.Unix(intentData.Timestamp.Int64(), 0)
		intentHash := req.IntentHash

		// Record intent as created (using the original timestamp)
		h.intentMetrics.RecordIntentCreated(
			intentHash,
			intentData.Symbol,
			"hyperlane",
			intentTime,
		)

		// Record the age of the intent when received
		intentAge := time.Since(intentTime).Seconds()
		receiverKey := req.ReceiverKey
		if receiverKey == "" {
			receiverKey = "unknown"
		}
		h.intentMetrics.RecordIntentAge(
			intentData.Symbol,
			"hyperlane",
			receiverKey,
			intentAge,
		)

		// Record phase tracking metrics if timestamps are provided
		if req.DetectionTimestamp > 0 && req.MonitoringStartTimestamp > 0 && req.FailoverTimestamp > 0 {
			detectionTime := time.Unix(req.DetectionTimestamp, 0)
			monitoringStartTime := time.Unix(req.MonitoringStartTimestamp, 0)
			failoverTime := time.Unix(req.FailoverTimestamp, 0)

			// Calculate phase durations
			intentToEventDuration := detectionTime.Sub(intentTime).Seconds()
			eventDetectionDuration := monitoringStartTime.Sub(detectionTime).Seconds()
			hyperlaneWaitDuration := failoverTime.Sub(monitoringStartTime).Seconds()

			// Record phase durations using the unified metric
			if h.metrics != nil {
				// Intent to Event phase
				h.metrics.RecordTimelinePhaseDuration("intent_to_event", intentToEventDuration, receiverKey)

				// Event Detection phase
				h.metrics.RecordTimelinePhaseDuration("event_detection", eventDetectionDuration, receiverKey)

				// Hyperlane Wait phase
				h.metrics.RecordTimelinePhaseDuration("wait", hyperlaneWaitDuration, receiverKey)
			}

			// Log phase durations
			logger.WithFields(logger.Fields{
				"intent_hash":             intentHash,
				"receiver_key":            receiverKey,
				"intent_to_event":         intentToEventDuration,
				"event_detection":         eventDetectionDuration,
				"hyperlane_wait":          hyperlaneWaitDuration,
				"detection_to_monitoring": monitoringStartTime.Sub(detectionTime).Seconds(),
				"monitoring_to_failover":  failoverTime.Sub(monitoringStartTime).Seconds(),
				"total_time":              failoverTime.Sub(intentTime).Seconds(),
			}).Info("Phase tracking metrics")
		}

		logger.WithFields(logger.Fields{
			"intent_hash":        intentHash,
			"intent_age_seconds": intentAge,
			"intent_timestamp":   intentTime.Format(time.RFC3339),
		}).Info("Processing intent with age tracking")
	}

	// Use the same ABI as receiver.go
	contractABI, err := abi.JSON(strings.NewReader(contracts.PushOracleReceiverABI))
	if err != nil {
		h.updateStatus(requestID, "failed", "", fmt.Sprintf("Failed to parse ABI: %v", err))
		return
	}

	// Log the signature before packing
	logger.WithFields(logger.Fields{
		"signature_hex": fmt.Sprintf("0x%x", intentData.Signature),
		"signature_len": len(intentData.Signature),
		"signer":        intentData.Signer.Hex(),
	}).Info("About to pack intent for contract call")

	// Pack the data (using helper function like receiver.go)
	callData, err := contractABI.Pack("handleIntentUpdate", h.intentToContractStruct(intentData))
	if err != nil {
		h.updateStatus(requestID, "failed", "", fmt.Sprintf("Failed to pack data: %v", err))
		return
	}

	// Get nonce
	ctx := context.Background()
	fromAddress := crypto.PubkeyToAddress(h.privateKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		h.updateStatus(requestID, "failed", "", fmt.Sprintf("Failed to get nonce: %v", err))
		return
	}

	// Get gas price
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		h.updateStatus(requestID, "failed", "", fmt.Sprintf("Failed to get gas price: %v", err))
		return
	}

	// Create transaction - use receiver address from request
	receiverAddr := common.HexToAddress(req.ReceiverAddress)
	tx := types.NewTransaction(
		nonce,
		receiverAddr,
		big.NewInt(0),
		destConfig.GasLimit,
		gasPrice,
		callData,
	)

	// Get chain ID
	chainID := big.NewInt(destConfig.ChainID)

	// Sign transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), h.privateKey)
	if err != nil {
		h.updateStatus(requestID, "failed", "", fmt.Sprintf("Failed to sign transaction: %v", err))
		return
	}

	// Send transaction
	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		h.updateStatus(requestID, "failed", "", fmt.Sprintf("Failed to send transaction: %v", err))
		if h.metrics != nil {
			h.metrics.RecordFailoverProcessing(
				fmt.Sprintf("%d", req.SourceChainID),
				fmt.Sprintf("%d", req.DestinationChainID),
				time.Since(startTime).Seconds(),
				false,
				"send_transaction_failed",
			)
		}
		return
	}

	txHash := signedTx.Hash().Hex()
	h.updateStatus(requestID, "sent", txHash, "")

	// Record transaction submitted metric
	if h.metrics != nil {
		h.metrics.RecordTransaction(
			fmt.Sprintf("%d", destConfig.ChainID),
			"PushOracleReceiver",
			destConfig.GasLimit,
			gasPrice.Uint64()*destConfig.GasLimit,
		)
	}

	// Record intent submission in lifecycle
	if h.intentMetrics != nil && intentData.Timestamp != nil {
		intentTime := time.Unix(intentData.Timestamp.Int64(), 0)
		lifecycle := &metrics.IntentLifecycle{
			IntentHash:       req.IntentHash,
			Symbol:           intentData.Symbol,
			SourceChain:      fmt.Sprintf("%d", req.SourceChainID),
			DestinationChain: fmt.Sprintf("%d", destConfig.ChainID),
			IntentTime:       intentTime, // Set the original intent time
			SubmissionTime:   time.Now(),
			TxHash:           txHash,
			GasPrice:         float64(gasPrice.Uint64()) / 1e9, // Convert to Gwei
		}
		h.intentMetrics.RecordIntentSubmitted(lifecycle)
	}

	logger.WithFields(logger.Fields{
		"request_id": requestID,
		"tx_hash":    txHash,
		"chain_id":   destConfig.ChainID,
		"symbol":     intentData.Symbol,
		"price":      intentData.Price,
	}).Info("Failover transaction sent")

	// Wait for confirmation
	confirmStartTime := time.Now()
	receipt, err := h.waitForReceipt(ctx, client, signedTx.Hash())
	if err != nil {
		h.updateStatus(requestID, "failed", txHash, fmt.Sprintf("Failed to get receipt: %v", err))
		if h.metrics != nil {
			h.metrics.RecordFailoverProcessing(
				fmt.Sprintf("%d", req.SourceChainID),
				fmt.Sprintf("%d", req.DestinationChainID),
				time.Since(startTime).Seconds(),
				false,
				"receipt_timeout",
			)
		}
		return
	}

	// Record confirmation time
	confirmDuration := time.Since(confirmStartTime).Seconds()
	if h.metrics != nil {
		h.metrics.RecordTransactionConfirmation(
			fmt.Sprintf("%d", destConfig.ChainID),
			"PushOracleReceiver",
			confirmDuration,
		)

		// Record timeline phases
		h.metrics.RecordTimelinePhase("bridge_processing", time.Since(startTime).Seconds(),
			fmt.Sprintf("%d", destConfig.ChainID),
			fmt.Sprintf("%d", req.SourceChainID),
			fmt.Sprintf("%d", req.DestinationChainID))

		h.metrics.RecordTimelinePhase("confirmation", confirmDuration,
			fmt.Sprintf("%d", destConfig.ChainID),
			fmt.Sprintf("%d", req.SourceChainID),
			fmt.Sprintf("%d", req.DestinationChainID))
	}

	if receipt.Status == 1 {
		h.updateStatus(requestID, "completed", txHash, "")

		// Record successful failover
		if h.metrics != nil {
			h.metrics.RecordFailoverProcessing(
				fmt.Sprintf("%d", req.SourceChainID),
				fmt.Sprintf("%d", req.DestinationChainID),
				time.Since(startTime).Seconds(),
				true,
				"",
			)

			// Record total delivery time (if we have the original dispatch time)
			// This would need to be passed from hyperlane-monitor
			h.metrics.RecordTotalDeliveryTime(
				time.Since(startTime).Seconds(), // This is just bridge processing time
				fmt.Sprintf("%d", destConfig.ChainID),
				fmt.Sprintf("%d", req.SourceChainID),
				fmt.Sprintf("%d", req.DestinationChainID),
				"failover",
			)
		}

		// Record bridge processing phase duration
		if h.metrics != nil && req.ReceiverKey != "" {
			bridgeProcessingDuration := time.Since(startTime).Seconds()
			h.metrics.RecordTimelinePhaseDuration("bridge_processing", bridgeProcessingDuration, req.ReceiverKey)
		}

		// Record intent confirmation in lifecycle
		if h.intentMetrics != nil && intentData.Timestamp != nil {
			intentTime := time.Unix(intentData.Timestamp.Int64(), 0)
			lifecycle := &metrics.IntentLifecycle{
				IntentHash:       req.IntentHash,
				Symbol:           intentData.Symbol,
				SourceChain:      fmt.Sprintf("%d", req.SourceChainID),
				DestinationChain: fmt.Sprintf("%d", destConfig.ChainID),
				IntentTime:       intentTime,                                                             // Set the original intent time for end-to-end calculation
				SubmissionTime:   time.Now().Add(-time.Duration(confirmDuration * float64(time.Second))), // Approximate submission time
				ConfirmationTime: time.Now(),
				TxHash:           txHash,
			}
			h.intentMetrics.RecordIntentConfirmed(lifecycle, receipt.GasUsed)
		}

		logger.WithFields(logger.Fields{
			"request_id": requestID,
			"tx_hash":    txHash,
			"gas_used":   receipt.GasUsed,
		}).Info("Failover transaction confirmed")
	} else {
		h.updateStatus(requestID, "failed", txHash, "Transaction reverted")
		if h.metrics != nil {
			h.metrics.RecordFailoverProcessing(
				fmt.Sprintf("%d", req.SourceChainID),
				fmt.Sprintf("%d", req.DestinationChainID),
				time.Since(startTime).Seconds(),
				false,
				"transaction_reverted",
			)
		}
	}
}

// waitForReceipt waits for a transaction receipt
func (h *FailoverHandler) waitForReceipt(ctx context.Context, client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for i := 0; i < 30; i++ { // Wait up to 60 seconds
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			receipt, err := client.TransactionReceipt(ctx, txHash)
			if err == nil {
				return receipt, nil
			}
		}
	}
	return nil, fmt.Errorf("timeout waiting for receipt")
}

// updateStatus updates the status of a failover request
func (h *FailoverHandler) updateStatus(requestID, status, txHash, errorMsg string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if fs, exists := h.requestStatus[requestID]; exists {
		fs.Status = status
		fs.UpdatedAt = time.Now()
		if txHash != "" {
			fs.TransactionHash = txHash
		}
		if errorMsg != "" {
			fs.Error = errorMsg
		}
	}
}

// ProcessFailoverRequest processes a failover request (used by both REST and gRPC)
func (h *FailoverHandler) ProcessFailoverRequest(requestID string, req FailoverRequest) {
	// Get destination config
	destConfig, exists := h.destinations[int64(req.DestinationChainID)]
	if !exists {
		h.updateStatus(requestID, "failed", "", fmt.Sprintf("Destination chain %d not configured", req.DestinationChainID))
		return
	}

	// Get client for destination chain
	client, exists := h.clients[int64(req.DestinationChainID)]
	if !exists {
		h.updateStatus(requestID, "failed", "", fmt.Sprintf("No client for destination chain %d", req.DestinationChainID))
		return
	}

	// Process the failover
	h.processFailover(requestID, req, destConfig, client)
}

// GetStatus returns the status of a failover request
func (h *FailoverHandler) GetStatus(requestID string) *FailoverStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status, exists := h.requestStatus[requestID]
	if !exists {
		return nil
	}

	// Return a copy to avoid race conditions
	statusCopy := *status
	return &statusCopy
}

// GetFailoverStatus handles GET /api/v1/failover/status/{requestId}
func (h *FailoverHandler) GetFailoverStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	requestID := vars["requestId"]

	h.mu.RLock()
	status, exists := h.requestStatus[requestID]
	h.mu.RUnlock()

	if !exists {
		h.sendError(w, http.StatusNotFound, "Request not found")
		return
	}

	response := FailoverResponse{
		RequestID:       status.RequestID,
		Status:          status.Status,
		TransactionHash: status.TransactionHash,
		Error:           status.Error,
		Timestamp:       status.UpdatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetFailoverStats handles GET /api/v1/failover/stats
func (h *FailoverHandler) GetFailoverStats(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := map[string]interface{}{
		"total_requests": len(h.requestStatus),
		"status_breakdown": map[string]int{
			"pending":    0,
			"processing": 0,
			"sent":       0,
			"completed":  0,
			"failed":     0,
		},
		"destinations": make([]map[string]interface{}, 0),
	}

	// Count statuses
	statusBreakdown := stats["status_breakdown"].(map[string]int)
	for _, fs := range h.requestStatus {
		statusBreakdown[fs.Status]++
	}

	// Add destination info
	destinations := []map[string]interface{}{}
	for chainID, config := range h.destinations {
		dest := map[string]interface{}{
			"chain_id":  chainID,
			"receiver":  config.ReceiverAddress.Hex(),
			"connected": h.clients[chainID] != nil,
		}
		destinations = append(destinations, dest)
	}
	stats["destinations"] = destinations

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// sendError sends an error response
func (h *FailoverHandler) sendError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// Close cleanly shuts down the failover handler
func (h *FailoverHandler) Close() error {
	// Close all client connections
	for _, client := range h.clients {
		client.Close()
	}
	return nil
}
