package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/diadata.org/Spectra-interoperability/bridge/config"
)

// OracleIntent represents an oracle intent from the registry
type OracleIntent struct {
	IntentType string         `json:"intentType"`
	Version    string         `json:"version"`
	ChainID    *big.Int       `json:"chainId"`
	Nonce      *big.Int       `json:"nonce"`
	Expiry     *big.Int       `json:"expiry"`
	Symbol     string         `json:"symbol"`
	Price      *big.Int       `json:"price"`
	Timestamp  *big.Int       `json:"timestamp"`
	Source     string         `json:"source"`
	Signature  HexBytes       `json:"signature"`
	Signer     common.Address `json:"signer"`
}

// HexBytes is a byte slice that marshals/unmarshals as hex string
type HexBytes []byte

// MarshalJSON implements json.Marshaler
func (h HexBytes) MarshalJSON() ([]byte, error) {
	if h == nil {
		return []byte("null"), nil
	}
	return json.Marshal("0x" + hex.EncodeToString(h))
}

// UnmarshalJSON implements json.Unmarshaler
func (h *HexBytes) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*h = nil
		return nil
	}
	
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		// Try as base64 byte array for backward compatibility
		var b []byte
		if err := json.Unmarshal(data, &b); err != nil {
			return err
		}
		*h = HexBytes(b)
		return nil
	}
	
	str = strings.TrimPrefix(str, "0x")
	b, err := hex.DecodeString(str)
	if err != nil {
		return err
	}
	*h = HexBytes(b)
	return nil
}

// UnmarshalJSON implements custom JSON unmarshaling to handle big integers properly
func (oi *OracleIntent) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid recursion
	type Alias OracleIntent
	aux := &struct {
		ChainID   json.Number `json:"chainId"`
		Nonce     json.Number `json:"nonce"`
		Expiry    json.Number `json:"expiry"`
		Price     json.Number `json:"price"`
		Timestamp json.Number `json:"timestamp"`
		*Alias
	}{
		Alias: (*Alias)(oi),
	}
	
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	
	// Convert json.Number to *big.Int
	if aux.ChainID != "" {
		val, ok := new(big.Int).SetString(string(aux.ChainID), 10)
		if !ok {
			return fmt.Errorf("invalid chainId: %s", aux.ChainID)
		}
		oi.ChainID = val
	}
	
	if aux.Nonce != "" {
		val, ok := new(big.Int).SetString(string(aux.Nonce), 10)
		if !ok {
			return fmt.Errorf("invalid nonce: %s", aux.Nonce)
		}
		oi.Nonce = val
	}
	
	if aux.Expiry != "" {
		val, ok := new(big.Int).SetString(string(aux.Expiry), 10)
		if !ok {
			return fmt.Errorf("invalid expiry: %s", aux.Expiry)
		}
		oi.Expiry = val
	}
	
	if aux.Price != "" {
		val, ok := new(big.Int).SetString(string(aux.Price), 10)
		if !ok {
			return fmt.Errorf("invalid price: %s", aux.Price)
		}
		oi.Price = val
	}
	
	if aux.Timestamp != "" {
		val, ok := new(big.Int).SetString(string(aux.Timestamp), 10)
		if !ok {
			return fmt.Errorf("invalid timestamp: %s", aux.Timestamp)
		}
		oi.Timestamp = val
	}
	
	return nil
}

// IsExpired checks if the intent has expired
// NOTE: This method is kept for compatibility but always returns false
// The bridge processes all intents regardless of expiry
func (oi *OracleIntent) IsExpired() bool {
	return false // Bridge processes all intents regardless of expiry
}

// GetPriceFloat returns the price as a float64 with 18 decimal places
func (oi *OracleIntent) GetPriceFloat() float64 {
	if oi.Price == nil {
		return 0
	}
	
	// Convert from wei (18 decimals) to float
	priceFloat := new(big.Float).SetInt(oi.Price)
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	priceFloat.Quo(priceFloat, divisor)
	
	result, _ := priceFloat.Float64()
	return result
}

// GetTimestamp returns the timestamp as time.Time
func (oi *OracleIntent) GetTimestamp() time.Time {
	if oi.Timestamp == nil {
		return time.Time{}
	}
	return time.Unix(oi.Timestamp.Int64(), 0)
}

// IntentRegisteredEvent represents the IntentRegistered event from the registry
type IntentRegisteredEvent struct {
	IntentHash common.Hash    `json:"intent_hash"`
	Symbol     string         `json:"symbol"`
	Price      *big.Int       `json:"price"`
	Timestamp  *big.Int       `json:"timestamp"`
	Signer     common.Address `json:"signer"`
	BlockNumber uint64        `json:"block_number"`
	TxHash     common.Hash    `json:"tx_hash"`
}

// BridgeStatus represents the status of a bridge operation
type BridgeStatus int

const (
	StatusPending BridgeStatus = iota
	StatusProcessing
	StatusSuccess
	StatusFailed
	StatusRetrying
)

func (s BridgeStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusProcessing:
		return "processing"
	case StatusSuccess:
		return "success"
	case StatusFailed:
		return "failed"
	case StatusRetrying:
		return "retrying"
	default:
		return "unknown"
	}
}

// BridgeOperation represents a bridge operation
type BridgeOperation struct {
	ID              string            `json:"id"`
	SourceChainID   int64            `json:"source_chain_id"`
	DestChainID     int64            `json:"dest_chain_id"`
	IntentHash      common.Hash      `json:"intent_hash"`
	Symbol          string           `json:"symbol"`
	Price           *big.Int         `json:"price"`
	Timestamp       *big.Int         `json:"timestamp"`
	Signer          common.Address   `json:"signer"`
	Status          BridgeStatus     `json:"status"`
	TxHash          common.Hash      `json:"tx_hash"`
	GasUsed         uint64           `json:"gas_used"`
	GasPrice        *big.Int         `json:"gas_price"`
	RetryCount      int              `json:"retry_count"`
	LastError       string           `json:"last_error"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	ProcessedAt     *time.Time       `json:"processed_at"`
}

// ChainStatus represents the status of a blockchain connection
type ChainStatus struct {
	ChainID          int64     `json:"chain_id"`
	Name             string    `json:"name"`
	Connected        bool      `json:"connected"`
	LatestBlock      uint64    `json:"latest_block"`
	SyncedBlock      uint64    `json:"synced_block"`
	LastHealthCheck  time.Time `json:"last_health_check"`
	LastError        string    `json:"last_error"`
	PendingTxCount   int       `json:"pending_tx_count"`
	SuccessfulTxCount int      `json:"successful_tx_count"`
	FailedTxCount    int       `json:"failed_tx_count"`
}

// BridgeStats represents bridge statistics
type BridgeStats struct {
	TotalOperations    int64              `json:"total_operations"`
	SuccessfulOps      int64              `json:"successful_ops"`
	FailedOps          int64              `json:"failed_ops"`
	PendingOps         int64              `json:"pending_ops"`
	ProcessingOps      int64              `json:"processing_ops"`
	RetryingOps        int64              `json:"retrying_ops"`
	ChainStats         map[int64]*ChainStatus `json:"chain_stats"`
	LastProcessedBlock uint64             `json:"last_processed_block"`
	StartTime          time.Time          `json:"start_time"`
	Uptime             time.Duration      `json:"uptime"`
	UptimeFormatted    string             `json:"uptime_formatted"`
	ScannerStats       *ScannerStats      `json:"scanner_stats,omitempty"`
}

// UpdateRequest represents a request to update an oracle value
type UpdateRequest struct {
	ID               string                    `json:"id"`
	IntentHash       common.Hash               `json:"intent_hash"`
	Intent           *OracleIntent             `json:"intent"`
	Event            *EventData                `json:"event"`
	DestinationChain *config.DestinationConfig `json:"destination_chain"`
	Contract         *config.ContractConfig    `json:"contract"`
	Priority         int                       `json:"priority"`
	Retries          int                       `json:"retries"`
	CreatedAt        time.Time                 `json:"created_at"`
	
	// New router system fields
	RouterID                string                           `json:"router_id,omitempty"`
	DestinationMethodConfig *config.DestinationMethodConfig `json:"destination_method_config,omitempty"`
	ExtractedData           *config.ExtractedData           `json:"extracted_data,omitempty"`
}

// UpdateResult represents the result of an update operation
type UpdateResult struct {
	ChainID         int64          `json:"chain_id"`
	ContractAddress common.Address `json:"contract_address"`
	TxHash          string         `json:"tx_hash"`
	BlockNumber     uint64         `json:"block_number"`
	GasUsed         uint64         `json:"gas_used"`
	GasPrice        *big.Int       `json:"gas_price"`
	Duration        time.Duration  `json:"duration"`
	Error           error          `json:"error,omitempty"`
}

// EventData represents a blockchain event
type EventData struct {
	EventName       string         `json:"event_name"`
	ContractAddress common.Address `json:"contract_address"`
	BlockNumber     uint64         `json:"block_number"`
	TxHash          common.Hash    `json:"tx_hash"`
	LogIndex        uint           `json:"log_index"`
	IntentHash      [32]byte       `json:"intent_hash"`
	Symbol          string         `json:"symbol"`
	Price           *big.Int       `json:"price"`
	Timestamp       *big.Int       `json:"timestamp"`
	Signer          common.Address `json:"signer"`
	
	// IntArraySet event specific fields
	RequestId       *big.Int       `json:"request_id,omitempty"`
	Round           *big.Int       `json:"round,omitempty"`
	Seed            string         `json:"seed,omitempty"`
	Signature       string         `json:"signature,omitempty"`
	RandomInts      []*big.Int     `json:"random_ints,omitempty"`
	RawData         []byte         `json:"raw_data,omitempty"`
	
	Data            map[string]interface{} `json:"data"`
	Raw             interface{}    `json:"raw"`
	IsGapFill       bool           `json:"is_gap_fill"`
	IsBackwardScan  bool           `json:"is_backward_scan"`
	Priority        int            `json:"priority"`
}

// WorkerStats represents worker pool statistics
type WorkerStats struct {
	TasksReceived  uint64 `json:"tasks_received"`
	TasksProcessed uint64 `json:"tasks_processed"`
	TasksSucceeded uint64 `json:"tasks_succeeded"`
	TasksFailed    uint64 `json:"tasks_failed"`
	TasksRetried   uint64 `json:"tasks_retried"`
	ActiveWorkers  int32  `json:"active_workers"`
	QueueSize      int32  `json:"queue_size"`
	TotalGasUsed   uint64 `json:"total_gas_used"`
}

// ProcessorStats represents event processor statistics
type ProcessorStats struct {
	EventsReceived    uint64    `json:"events_received"`
	EventsProcessed   uint64    `json:"events_processed"`
	EventsDuplicate   uint64    `json:"events_duplicate"`
	EventsInvalid     uint64    `json:"events_invalid"`
	EventsFailed      uint64    `json:"events_failed"`
	UpdatesCreated    uint64    `json:"updates_created"`
	LastProcessedTime time.Time `json:"last_processed_time"`
	CacheSize         int       `json:"cache_size"`
}

// ScannerStats represents block scanner statistics
type ScannerStats struct {
	LastScanBlock       uint64    `json:"last_scan_block"`
	CurrentBlock        uint64    `json:"current_block"`
	BlocksBehind        uint64    `json:"blocks_behind"`
	IsScanning          bool      `json:"is_scanning"`
	BackwardScanning    bool      `json:"backward_scanning"`
	Converged           bool      `json:"converged"`
	ForwardBlock        uint64    `json:"forward_block"`
	BackwardBlock       uint64    `json:"backward_block"`
	HeadBlock           uint64    `json:"head_block"`
	ForwardEventsFound  uint64    `json:"forward_events_found"`
	BackwardEventsFound uint64    `json:"backward_events_found"`
	HeadEventsFound     uint64    `json:"head_events_found"`
	TotalBlocksScanned  uint64    `json:"total_blocks_scanned"`
	LastHeadUpdate      time.Time `json:"last_head_update"`
}