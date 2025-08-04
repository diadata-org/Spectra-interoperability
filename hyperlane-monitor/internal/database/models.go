package database

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"time"

	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

// MonitoringPair represents a source-destination monitoring configuration
type MonitoringPair struct {
	PairID               string    `db:"pair_id"`
	SourceChainID        int       `db:"source_chain_id"`
	SourceChainName      string    `db:"source_chain_name"`
	OracleTriggerAddress string    `db:"oracle_trigger_address"`
	OracleRegistryAddress string   `db:"oracle_registry_address"`
	DestinationChainID   int       `db:"destination_chain_id"`
	DestinationChainName string    `db:"destination_chain_name"`
	Enabled              bool      `db:"enabled"`
	LastProcessedBlock   uint64    `db:"last_processed_block"`
	CreatedAt            time.Time `db:"created_at"`
	UpdatedAt            time.Time `db:"updated_at"`
}

// PairReceiver represents a receiver configuration for a monitoring pair
type PairReceiver struct {
	ID                     int64          `db:"id"`
	PairID                 string         `db:"pair_id"`
	ReceiverAddress        string         `db:"receiver_address"`
	ReceiverName           string         `db:"receiver_name"`
	Enabled                bool           `db:"enabled"`
	MonitoringProfile      string         `db:"monitoring_profile"`
	CheckIntervalSeconds   int            `db:"check_interval_seconds"`
	InitialWaitSeconds     int            `db:"initial_wait_seconds"`
	MaxDeliveryWaitSeconds int            `db:"max_delivery_wait_seconds"`
	MaxCheckAttempts       int            `db:"max_check_attempts"`
	Priority               string         `db:"priority"`
	AlertOnFailure         bool           `db:"alert_on_failure"`
	AlertWebhook           string         `db:"alert_webhook"`
	CustomConfig           JSONB          `db:"custom_config"`
	CreatedAt              time.Time      `db:"created_at"`
	UpdatedAt              time.Time      `db:"updated_at"`
}

// HyperlaneMessage represents a tracked Hyperlane message in the database
type HyperlaneMessage struct {
	ID                  int64               `db:"id"`
	MessageID           string              `db:"message_id"`
	IntentHash          string              `db:"intent_hash"`
	PairID              string              `db:"pair_id"`
	SourceChainID       int                 `db:"source_chain_id"`
	SourceTxHash        string              `db:"source_tx_hash"`
	SourceBlockNumber   uint64              `db:"source_block_number"`
	DestinationChainID  int                 `db:"destination_chain_id"`
	ReceiverAddress     string              `db:"receiver_address"`
	ReceiverName        string              `db:"receiver_name"`
	Symbol              string              `db:"symbol"`
	Price               string              `db:"price"` // Stored as string for precision
	Timestamp           int64               `db:"timestamp"`
	IntentData          JSONB               `db:"intent_data"`
	Status              types.MessageStatus `db:"status"`
	Priority            string              `db:"priority"`
	DeliveryChecks      int                 `db:"delivery_checks"`
	FirstCheckAt        *time.Time          `db:"first_check_at"`
	LastCheckAt         *time.Time          `db:"last_check_at"`
	NextCheckAt         *time.Time          `db:"next_check_at"`
	DeliveredAt         *time.Time          `db:"delivered_at"`
	FailoverRequested   bool                `db:"failover_requested"`
	FailoverRequestID   string              `db:"failover_request_id"`
	FailoverRequestedAt *time.Time          `db:"failover_requested_at"`
	FailoverTxHash      string              `db:"failover_tx_hash"`
	CreatedAt           time.Time           `db:"created_at"`
	UpdatedAt           time.Time           `db:"updated_at"`
}

// DeliveryStatistics represents aggregated delivery statistics
type DeliveryStatistics struct {
	PairID              string  `db:"pair_id"`
	ReceiverAddress     string  `db:"receiver_address"`
	Date                time.Time `db:"date"`
	Hour                int     `db:"hour"`
	MessagesDispatched  int     `db:"messages_dispatched"`
	MessagesDelivered   int     `db:"messages_delivered"`
	MessagesFailed      int     `db:"messages_failed"`
	AvgDeliverySeconds  float64 `db:"avg_delivery_seconds"`
	P95DeliverySeconds  float64 `db:"p95_delivery_seconds"`
	P99DeliverySeconds  float64 `db:"p99_delivery_seconds"`
	FailoversTriggered  int     `db:"failovers_triggered"`
}

// JSONB handles JSON data storage in PostgreSQL
type JSONB map[string]interface{}

// Value implements the driver.Valuer interface
func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements the sql.Scanner interface
func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	
	switch v := value.(type) {
	case []byte:
		// Use json.Decoder with UseNumber to preserve numeric precision
		decoder := json.NewDecoder(bytes.NewReader(v))
		decoder.UseNumber()
		return decoder.Decode(j)
	case string:
		// Use json.Decoder with UseNumber to preserve numeric precision
		decoder := json.NewDecoder(strings.NewReader(v))
		decoder.UseNumber()
		return decoder.Decode(j)
	default:
		// If it's already parsed (e.g., by the driver), convert to JSON and back
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return err
		}
		// Use json.Decoder with UseNumber to preserve numeric precision
		decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
		decoder.UseNumber()
		return decoder.Decode(j)
	}
}