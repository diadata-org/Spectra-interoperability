package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

// Repository handles all database operations
type Repository struct {
	db *sql.DB
}

// NewRepository creates a new database repository
func NewRepository(dsn string) (*Repository, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Repository{db: db}, nil
}

// Close closes the database connection
func (r *Repository) Close() error {
	return r.db.Close()
}

// RunMigrations executes database migrations
func (r *Repository) RunMigrations() error {
	logger.Info("Running database migrations...")
	
	// Execute the migration SQL
	migrationSQL := `
CREATE TABLE IF NOT EXISTS monitoring_pairs (
    pair_id VARCHAR(100) PRIMARY KEY,
    source_chain_id INT NOT NULL,
    source_chain_name VARCHAR(100),
    oracle_trigger_address VARCHAR(42) NOT NULL,
    oracle_registry_address VARCHAR(42) NOT NULL,
    destination_chain_id INT NOT NULL,
    destination_chain_name VARCHAR(100),
    enabled BOOLEAN DEFAULT TRUE,
    last_processed_block BIGINT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source_chain_id, destination_chain_id, oracle_trigger_address)
);

CREATE TABLE IF NOT EXISTS pair_receivers (
    id SERIAL PRIMARY KEY,
    pair_id VARCHAR(100) NOT NULL,
    receiver_address VARCHAR(42) NOT NULL,
    receiver_name VARCHAR(200),
    enabled BOOLEAN DEFAULT TRUE,
    monitoring_profile VARCHAR(50),
    check_interval_seconds INT,
    initial_wait_seconds INT,
    max_delivery_wait_seconds INT,
    max_check_attempts INT,
    priority VARCHAR(20) DEFAULT 'medium',
    alert_on_failure BOOLEAN DEFAULT FALSE,
    alert_webhook VARCHAR(500),
    custom_config JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (pair_id) REFERENCES monitoring_pairs(pair_id) ON DELETE CASCADE,
    UNIQUE(pair_id, receiver_address)
);

CREATE TABLE IF NOT EXISTS hyperlane_messages (
    id SERIAL PRIMARY KEY,
    message_id VARCHAR(66) UNIQUE NOT NULL,
    intent_hash VARCHAR(66) NOT NULL,
    pair_id VARCHAR(100) NOT NULL,
    source_chain_id INT NOT NULL,
    source_tx_hash VARCHAR(66) NOT NULL,
    source_block_number BIGINT NOT NULL,
    destination_chain_id INT NOT NULL,
    receiver_address VARCHAR(42) NOT NULL,
    receiver_name VARCHAR(200),
    symbol VARCHAR(20) NOT NULL,
    price DECIMAL(78, 0) NOT NULL,
    timestamp BIGINT NOT NULL,
    intent_data JSONB NOT NULL,
    status VARCHAR(20) DEFAULT 'dispatched',
    priority VARCHAR(20),
    delivery_checks INT DEFAULT 0,
    first_check_at TIMESTAMP,
    last_check_at TIMESTAMP,
    next_check_at TIMESTAMP,
    delivered_at TIMESTAMP,
    failover_requested BOOLEAN DEFAULT FALSE,
    failover_request_id VARCHAR(66),
    failover_requested_at TIMESTAMP,
    failover_tx_hash VARCHAR(66),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (pair_id) REFERENCES monitoring_pairs(pair_id)
);

CREATE INDEX IF NOT EXISTS idx_pair_status ON hyperlane_messages(pair_id, status);
CREATE INDEX IF NOT EXISTS idx_receiver_status ON hyperlane_messages(receiver_address, status);
CREATE INDEX IF NOT EXISTS idx_next_check ON hyperlane_messages(status, next_check_at);
CREATE INDEX IF NOT EXISTS idx_intent_hash ON hyperlane_messages(intent_hash);

CREATE TABLE IF NOT EXISTS delivery_statistics (
    pair_id VARCHAR(100) NOT NULL,
    receiver_address VARCHAR(42) NOT NULL,
    date DATE NOT NULL,
    hour INT NOT NULL,
    messages_dispatched INT DEFAULT 0,
    messages_delivered INT DEFAULT 0,
    messages_failed INT DEFAULT 0,
    avg_delivery_seconds FLOAT,
    p95_delivery_seconds FLOAT,
    p99_delivery_seconds FLOAT,
    failovers_triggered INT DEFAULT 0,
    PRIMARY KEY (pair_id, receiver_address, date, hour)
);`

	_, err := r.db.Exec(migrationSQL)
	if err != nil {
		return fmt.Errorf("failed to execute migrations: %w", err)
	}

	logger.Info("Database tables created successfully")
	return nil
}

// GetMonitoringPairs returns all monitoring pairs
func (r *Repository) GetMonitoringPairs() ([]MonitoringPair, error) {
	query := `
		SELECT pair_id, source_chain_id, source_chain_name, oracle_trigger_address,
		       oracle_registry_address, destination_chain_id, destination_chain_name,
		       enabled, last_processed_block, created_at, updated_at
		FROM monitoring_pairs
		WHERE enabled = true
	`
	
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pairs []MonitoringPair
	for rows.Next() {
		var pair MonitoringPair
		err := rows.Scan(
			&pair.PairID, &pair.SourceChainID, &pair.SourceChainName,
			&pair.OracleTriggerAddress, &pair.OracleRegistryAddress,
			&pair.DestinationChainID, &pair.DestinationChainName,
			&pair.Enabled, &pair.LastProcessedBlock,
			&pair.CreatedAt, &pair.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, pair)
	}

	return pairs, rows.Err()
}

// GetPairReceivers returns all receivers for a monitoring pair
func (r *Repository) GetPairReceivers(pairID string) ([]PairReceiver, error) {
	query := `
		SELECT id, pair_id, receiver_address, receiver_name, enabled,
		       monitoring_profile, check_interval_seconds, initial_wait_seconds,
		       max_delivery_wait_seconds, max_check_attempts, priority,
		       alert_on_failure, alert_webhook, custom_config,
		       created_at, updated_at
		FROM pair_receivers
		WHERE pair_id = $1 AND enabled = true
	`
	
	rows, err := r.db.Query(query, pairID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var receivers []PairReceiver
	for rows.Next() {
		var receiver PairReceiver
		err := rows.Scan(
			&receiver.ID, &receiver.PairID, &receiver.ReceiverAddress,
			&receiver.ReceiverName, &receiver.Enabled, &receiver.MonitoringProfile,
			&receiver.CheckIntervalSeconds, &receiver.InitialWaitSeconds,
			&receiver.MaxDeliveryWaitSeconds, &receiver.MaxCheckAttempts,
			&receiver.Priority, &receiver.AlertOnFailure, &receiver.AlertWebhook,
			&receiver.CustomConfig, &receiver.CreatedAt, &receiver.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		receivers = append(receivers, receiver)
	}

	return receivers, rows.Err()
}

// SaveMessage saves a new Hyperlane message
func (r *Repository) SaveMessage(msg *HyperlaneMessage) error {
	query := `
		INSERT INTO hyperlane_messages (
			message_id, intent_hash, pair_id, source_chain_id, source_tx_hash,
			source_block_number, destination_chain_id, receiver_address,
			receiver_name, symbol, price, timestamp, intent_data,
			status, priority, next_check_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (message_id) DO NOTHING
	`
	
	_, err := r.db.Exec(query,
		msg.MessageID, msg.IntentHash, msg.PairID, msg.SourceChainID,
		msg.SourceTxHash, msg.SourceBlockNumber, msg.DestinationChainID,
		msg.ReceiverAddress, msg.ReceiverName, msg.Symbol, msg.Price,
		msg.Timestamp, msg.IntentData, msg.Status, msg.Priority,
		msg.NextCheckAt,
	)
	
	return err
}

// GetPendingMessages returns messages that need delivery checking
func (r *Repository) GetPendingMessages(limit int) ([]HyperlaneMessage, error) {
	query := `
		SELECT id, message_id, intent_hash, pair_id, source_chain_id,
		       source_tx_hash, source_block_number, destination_chain_id,
		       receiver_address, receiver_name, symbol, price, timestamp,
		       intent_data, status, priority, delivery_checks,
		       first_check_at, last_check_at, next_check_at,
		       created_at, updated_at
		FROM hyperlane_messages
		WHERE status = $1 
		  AND (next_check_at IS NULL OR next_check_at <= NOW())
		ORDER BY priority DESC, next_check_at ASC
		LIMIT $2
	`
	
	rows, err := r.db.Query(query, types.StatusDispatched, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []HyperlaneMessage
	for rows.Next() {
		var msg HyperlaneMessage
		err := rows.Scan(
			&msg.ID, &msg.MessageID, &msg.IntentHash, &msg.PairID,
			&msg.SourceChainID, &msg.SourceTxHash, &msg.SourceBlockNumber,
			&msg.DestinationChainID, &msg.ReceiverAddress, &msg.ReceiverName,
			&msg.Symbol, &msg.Price, &msg.Timestamp, &msg.IntentData,
			&msg.Status, &msg.Priority, &msg.DeliveryChecks,
			&msg.FirstCheckAt, &msg.LastCheckAt, &msg.NextCheckAt,
			&msg.CreatedAt, &msg.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// UpdateMessageDelivered marks a message as delivered
func (r *Repository) UpdateMessageDelivered(messageID string) error {
	query := `
		UPDATE hyperlane_messages
		SET status = $1, delivered_at = NOW(), updated_at = NOW()
		WHERE message_id = $2
	`
	
	_, err := r.db.Exec(query, types.StatusDelivered, messageID)
	return err
}

// UpdateMessageCheck updates the check status of a message
func (r *Repository) UpdateMessageCheck(messageID string, nextCheckAt time.Time) error {
	query := `
		UPDATE hyperlane_messages
		SET delivery_checks = delivery_checks + 1,
		    last_check_at = NOW(),
		    next_check_at = $1,
		    first_check_at = COALESCE(first_check_at, NOW()),
		    updated_at = NOW()
		WHERE message_id = $2
	`
	
	_, err := r.db.Exec(query, nextCheckAt, messageID)
	return err
}

// UpdateMessageFailover marks a message as having triggered failover
func (r *Repository) UpdateMessageFailover(messageID, requestID string) error {
	query := `
		UPDATE hyperlane_messages
		SET status = $1,
		    failover_requested = true,
		    failover_request_id = $2,
		    failover_requested_at = NOW(),
		    updated_at = NOW()
		WHERE message_id = $3
	`
	
	_, err := r.db.Exec(query, types.StatusFailoverTriggered, requestID, messageID)
	return err
}

// UpdateLastProcessedBlock updates the last processed block for a pair
func (r *Repository) UpdateLastProcessedBlock(pairID string, blockNumber uint64) error {
	query := `
		UPDATE monitoring_pairs
		SET last_processed_block = $1, updated_at = NOW()
		WHERE pair_id = $2
	`
	
	_, err := r.db.Exec(query, blockNumber, pairID)
	return err
}

// SaveOrUpdatePair saves or updates a monitoring pair
func (r *Repository) SaveOrUpdatePair(pair *MonitoringPair) error {
	query := `
		INSERT INTO monitoring_pairs (
			pair_id, source_chain_id, source_chain_name,
			oracle_trigger_address, oracle_registry_address,
			destination_chain_id, destination_chain_name,
			enabled, last_processed_block
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (pair_id) DO UPDATE SET
			source_chain_name = EXCLUDED.source_chain_name,
			destination_chain_name = EXCLUDED.destination_chain_name,
			enabled = EXCLUDED.enabled,
			updated_at = NOW()
	`
	
	_, err := r.db.Exec(query,
		pair.PairID, pair.SourceChainID, pair.SourceChainName,
		pair.OracleTriggerAddress, pair.OracleRegistryAddress,
		pair.DestinationChainID, pair.DestinationChainName,
		pair.Enabled, pair.LastProcessedBlock,
	)
	
	return err
}

// SaveOrUpdateReceiver saves or updates a pair receiver
func (r *Repository) SaveOrUpdateReceiver(receiver *PairReceiver) error {
	query := `
		INSERT INTO pair_receivers (
			pair_id, receiver_address, receiver_name, enabled,
			monitoring_profile, check_interval_seconds, initial_wait_seconds,
			max_delivery_wait_seconds, max_check_attempts, priority,
			alert_on_failure, alert_webhook, custom_config
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (pair_id, receiver_address) DO UPDATE SET
			receiver_name = EXCLUDED.receiver_name,
			enabled = EXCLUDED.enabled,
			monitoring_profile = EXCLUDED.monitoring_profile,
			check_interval_seconds = EXCLUDED.check_interval_seconds,
			initial_wait_seconds = EXCLUDED.initial_wait_seconds,
			max_delivery_wait_seconds = EXCLUDED.max_delivery_wait_seconds,
			max_check_attempts = EXCLUDED.max_check_attempts,
			priority = EXCLUDED.priority,
			alert_on_failure = EXCLUDED.alert_on_failure,
			alert_webhook = EXCLUDED.alert_webhook,
			custom_config = EXCLUDED.custom_config,
			updated_at = NOW()
	`
	
	_, err := r.db.Exec(query,
		receiver.PairID, receiver.ReceiverAddress, receiver.ReceiverName,
		receiver.Enabled, receiver.MonitoringProfile, receiver.CheckIntervalSeconds,
		receiver.InitialWaitSeconds, receiver.MaxDeliveryWaitSeconds,
		receiver.MaxCheckAttempts, receiver.Priority, receiver.AlertOnFailure,
		receiver.AlertWebhook, receiver.CustomConfig,
	)
	
	return err
}

// QueueStats represents message queue statistics
type QueueStats struct {
	PendingMessages   int
	CheckingMessages  int
	DeliveredMessages int
	FailedMessages    int
}

// GetQueueStats returns current message queue statistics
func (r *Repository) GetQueueStats() (*QueueStats, error) {
	query := `
		SELECT 
			COUNT(CASE WHEN status = 'pending' THEN 1 END) as pending,
			COUNT(CASE WHEN status = 'checking' THEN 1 END) as checking,
			COUNT(CASE WHEN status = 'delivered' THEN 1 END) as delivered,
			COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed
		FROM hyperlane_messages
		WHERE created_at > NOW() - INTERVAL '24 hours'
	`
	
	var stats QueueStats
	err := r.db.QueryRow(query).Scan(
		&stats.PendingMessages,
		&stats.CheckingMessages,
		&stats.DeliveredMessages,
		&stats.FailedMessages,
	)
	
	return &stats, err
}

// Ping checks database connectivity
func (r *Repository) Ping() error {
	return r.db.Ping()
}