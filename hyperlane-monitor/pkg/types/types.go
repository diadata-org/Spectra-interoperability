package types

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// OracleIntent represents the intent data structure
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

// HyperlaneMessage represents a tracked Hyperlane message
type HyperlaneMessage struct {
	ID                   int64
	MessageID            string
	IntentHash           string
	PairID               string
	SourceChainID        int
	SourceTxHash         string
	SourceBlockNumber    uint64
	DestinationChainID   int
	ReceiverAddress      string
	ReceiverName         string
	Symbol               string
	Price                *big.Int
	Timestamp            int64
	IntentData           *OracleIntent
	Status               MessageStatus
	Priority             string
	DeliveryChecks       int
	FirstCheckAt         *time.Time
	LastCheckAt          *time.Time
	NextCheckAt          *time.Time
	DeliveredAt          *time.Time
	FailoverRequested    bool
	FailoverRequestID    string
	FailoverRequestedAt  *time.Time
	FailoverTxHash       string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// MessageStatus represents the delivery status
type MessageStatus string

const (
	StatusDispatched       MessageStatus = "dispatched"
	StatusDelivered        MessageStatus = "delivered"
	StatusFailoverTriggered MessageStatus = "failover_triggered"
	StatusFailed           MessageStatus = "failed"
)

// MonitoringPair represents a source-destination monitoring configuration
type MonitoringPair struct {
	PairID               string
	SourceChainID        int
	SourceChainName      string
	OracleTriggerAddress string
	OracleRegistryAddress string
	DestinationChainID   int
	DestinationChainName string
	Enabled              bool
	LastProcessedBlock   uint64
	Receivers            []ReceiverConfig
}

// ReceiverConfig represents monitoring configuration for a specific receiver
type ReceiverConfig struct {
	Address          string
	Name             string
	Enabled          bool
	Profile          string
	CheckInterval    time.Duration
	InitialWait      time.Duration
	MaxDeliveryWait  time.Duration
	MaxCheckAttempts int
	Priority         string
	AlertOnFailure   bool
	AlertWebhook     string
	CustomConfig     map[string]interface{}
}

// FailoverRequest represents a request to trigger failover via Bridge
type FailoverRequest struct {
	MessageID          string       `json:"message_id"`
	IntentHash         string       `json:"intent_hash"`
	PairID             string       `json:"pair_id"`
	SourceChainID      int          `json:"source_chain_id"`
	DestinationChainID int          `json:"destination_chain_id"`
	ReceiverAddress    string       `json:"receiver_address"`
	IntentData         *OracleIntent `json:"intent_data"`
	Reason             string       `json:"reason"`
}

// FailoverResponse represents the Bridge API response
type FailoverResponse struct {
	RequestID     string    `json:"request_id"`
	Status        string    `json:"status"`
	TransactionHash string  `json:"transaction_hash,omitempty"`
	Error         string    `json:"error,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}