package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	_ "github.com/lib/pq"
)

// DB wraps the SQL database connection
type DB struct {
	*sql.DB
	driver string
}

// ProcessedEvent represents a processed oracle intent event
type ProcessedEvent struct {
	ID              int64
	IntentHash      string
	BlockNumber     uint64
	TransactionHash string
	LogIndex        uint
	Symbol          string
	Price           string // Store as string for big numbers
	Timestamp       uint64
	Signer          common.Address
	ProcessedAt     time.Time
}

// ChainState tracks synchronization state for each chain
type ChainState struct {
	ChainID            int64
	ChainName          string
	LastProcessedBlock uint64
	LastScanBlock      uint64
	LastEventBlock     *uint64
	LastHealthCheck    *time.Time
	IsHealthy          bool
	ErrorCount         int
	LastError          *string
	Metadata           *string // JSON metadata
	UpdatedAt          time.Time
}

// TransactionLog records all bridge transactions
type TransactionLog struct {
	ID                     int64
	IntentHash             string
	DestinationChainID     int64
	DestinationChainName   string
	ContractAddress        string
	ContractName           string
	ContractType           string
	TransactionHash        *string
	Status                 string // 'pending', 'submitted', 'confirmed', 'failed'
	FromAddress            string
	ToAddress              string
	Symbol                 string
	Price                  string
	GasLimit               *uint64
	GasUsed                *uint64
	GasPrice               *string
	MaxFeePerGas           *string
	MaxPriorityFeePerGas   *string
	TransactionCost        *string
	ErrorMessage           *string
	ErrorCode              *string
	RetryCount             int
	MaxRetries             int
	CreatedAt              time.Time
	SubmittedAt            *time.Time
	ConfirmedAt            *time.Time
	FailedAt               *time.Time
	UpdatedAt              time.Time
}

// ContractSymbolUpdate tracks last update per symbol per contract
type ContractSymbolUpdate struct {
	ID                   int64
	ChainID              int64
	ContractAddress      string
	Symbol               string
	LastIntentHash       string
	LastPrice            string
	LastUpdateTimestamp  time.Time
	UpdateCount          int64
	TotalGasUsed         string
	AverageGasPrice      string
}

// NewDB creates a new database connection
func NewDB(driver, dsn string) (*DB, error) {
	db, err := sql.Open(driver, dsn)
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

	return &DB{db, driver}, nil
}

// Migrate creates all required tables
func (db *DB) Migrate() error {
	queries := []string{
		createProcessedEventsTable,
		createChainStateTable,
		createTransactionLogTable,
		createContractSymbolUpdateTable,
		createPerformanceMetricsTable,
		createAlertLogTable,
		createIndices,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}

// Event tracking methods

// IsEventProcessed checks if an event has already been processed
func (db *DB) IsEventProcessed(intentHash string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM processed_events WHERE intent_hash = $1)`
	err := db.QueryRow(query, intentHash).Scan(&exists)
	return exists, err
}

// SaveProcessedEvent saves a processed event to the database
func (db *DB) SaveProcessedEvent(event *ProcessedEvent) error {
	query := `
		INSERT INTO processed_events (
			intent_hash, block_number, transaction_hash, log_index,
			symbol, price, timestamp, signer, processed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (intent_hash) DO NOTHING`

	_, err := db.Exec(query,
		event.IntentHash,
		event.BlockNumber,
		event.TransactionHash,
		event.LogIndex,
		event.Symbol,
		event.Price,
		event.Timestamp,
		event.Signer.Hex(),
		event.ProcessedAt,
	)
	return err
}

// GetProcessedEventsByBlockRange retrieves events in a block range
func (db *DB) GetProcessedEventsByBlockRange(startBlock, endBlock uint64) ([]*ProcessedEvent, error) {
	query := `
		SELECT id, intent_hash, block_number, transaction_hash, log_index,
		       symbol, price, timestamp, signer, processed_at
		FROM processed_events
		WHERE block_number >= $1 AND block_number <= $2
		ORDER BY block_number, log_index`

	rows, err := db.Query(query, startBlock, endBlock)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*ProcessedEvent
	for rows.Next() {
		event := &ProcessedEvent{}
		var signerHex string
		err := rows.Scan(
			&event.ID,
			&event.IntentHash,
			&event.BlockNumber,
			&event.TransactionHash,
			&event.LogIndex,
			&event.Symbol,
			&event.Price,
			&event.Timestamp,
			&signerHex,
			&event.ProcessedAt,
		)
		if err != nil {
			return nil, err
		}
		event.Signer = common.HexToAddress(signerHex)
		events = append(events, event)
	}

	return events, rows.Err()
}

// Chain state methods

// GetChainState retrieves the state for a specific chain
func (db *DB) GetChainState(chainID int64) (*ChainState, error) {
	query := `
		SELECT chain_id, chain_name, last_processed_block, last_scan_block,
		       last_event_block, last_health_check, is_healthy, error_count,
		       last_error, metadata, updated_at
		FROM chain_state
		WHERE chain_id = $1`

	state := &ChainState{}
	err := db.QueryRow(query, chainID).Scan(
		&state.ChainID,
		&state.ChainName,
		&state.LastProcessedBlock,
		&state.LastScanBlock,
		&state.LastEventBlock,
		&state.LastHealthCheck,
		&state.IsHealthy,
		&state.ErrorCount,
		&state.LastError,
		&state.Metadata,
		&state.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		// Return default state if not exists
		return &ChainState{
			ChainID:            chainID,
			LastProcessedBlock: 0,
			LastScanBlock:      0,
			IsHealthy:          true,
			UpdatedAt:          time.Now(),
		}, nil
	}

	return state, err
}

// UpdateLastProcessedBlock updates the last processed block for a chain
func (db *DB) UpdateLastProcessedBlock(chainID int64, blockNumber uint64) error {
	query := `
		INSERT INTO chain_state (chain_id, last_processed_block, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (chain_id) DO UPDATE
		SET last_processed_block = $2, updated_at = $3`

	_, err := db.Exec(query, chainID, blockNumber, time.Now())
	return err
}

// UpdateLastScanBlock updates the last scanned block for a chain
func (db *DB) UpdateLastScanBlock(chainID int64, blockNumber uint64) error {
	query := `
		INSERT INTO chain_state (chain_id, last_scan_block, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (chain_id) DO UPDATE
		SET last_scan_block = $2, updated_at = $3`

	_, err := db.Exec(query, chainID, blockNumber, time.Now())
	return err
}

// UpdateChainHealth updates the health status of a chain
func (db *DB) UpdateChainHealth(chainID int64, isHealthy bool, errorMsg *string) error {
	query := `
		INSERT INTO chain_state (chain_id, is_healthy, error_count, last_error, last_health_check, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chain_id) DO UPDATE
		SET is_healthy = $2,
		    error_count = CASE WHEN $2 = false THEN chain_state.error_count + 1 ELSE 0 END,
		    last_error = $4,
		    last_health_check = $5,
		    updated_at = $6`

	errorCount := 0
	if !isHealthy {
		errorCount = 1
	}

	_, err := db.Exec(query, chainID, isHealthy, errorCount, errorMsg, time.Now(), time.Now())
	return err
}

// Transaction logging methods

// LogTransaction creates a new transaction log entry
func (db *DB) LogTransaction(log *TransactionLog) error {
	query := `
		INSERT INTO transaction_log (
			intent_hash, destination_chain_id, destination_chain_name,
			contract_address, contract_name, contract_type,
			status, from_address, to_address, symbol, price,
			retry_count, max_retries, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id`

	err := db.QueryRow(query,
		log.IntentHash,
		log.DestinationChainID,
		log.DestinationChainName,
		log.ContractAddress,
		log.ContractName,
		log.ContractType,
		log.Status,
		log.FromAddress,
		log.ToAddress,
		log.Symbol,
		log.Price,
		log.RetryCount,
		log.MaxRetries,
		log.CreatedAt,
		log.UpdatedAt,
	).Scan(&log.ID)

	return err
}

// UpdateTransactionStatus updates the status of a transaction
func (db *DB) UpdateTransactionStatus(id int64, status string, txHash *string, gasUsed *uint64, gasPrice *string) error {
	now := time.Now()
	var query string

	switch status {
	case "submitted":
		query = `
			UPDATE transaction_log
			SET status = $2, transaction_hash = $3, submitted_at = $4, updated_at = $4
			WHERE id = $1`
		_, err := db.Exec(query, id, status, txHash, now)
		return err

	case "confirmed":
		query = `
			UPDATE transaction_log
			SET status = $2, gas_used = $3, gas_price = $4, confirmed_at = $5, updated_at = $5
			WHERE id = $1`
		_, err := db.Exec(query, id, status, gasUsed, gasPrice, now)
		return err

	case "failed":
		query = `
			UPDATE transaction_log
			SET status = $2, failed_at = $3, updated_at = $3
			WHERE id = $1`
		_, err := db.Exec(query, id, status, now)
		return err

	default:
		query = `
			UPDATE transaction_log
			SET status = $2, updated_at = $3
			WHERE id = $1`
		_, err := db.Exec(query, id, status, now)
		return err
	}
}

// GetPendingTransactions retrieves pending transactions for a chain
func (db *DB) GetPendingTransactions(chainID int64) ([]*TransactionLog, error) {
	query := `
		SELECT id, intent_hash, destination_chain_id, destination_chain_name,
		       contract_address, contract_name, contract_type, transaction_hash,
		       status, symbol, price, retry_count, max_retries, created_at
		FROM transaction_log
		WHERE destination_chain_id = $1 AND status IN ('pending', 'submitted')
		ORDER BY created_at`

	rows, err := db.Query(query, chainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transactions []*TransactionLog
	for rows.Next() {
		tx := &TransactionLog{}
		err := rows.Scan(
			&tx.ID,
			&tx.IntentHash,
			&tx.DestinationChainID,
			&tx.DestinationChainName,
			&tx.ContractAddress,
			&tx.ContractName,
			&tx.ContractType,
			&tx.TransactionHash,
			&tx.Status,
			&tx.Symbol,
			&tx.Price,
			&tx.RetryCount,
			&tx.MaxRetries,
			&tx.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, tx)
	}

	return transactions, rows.Err()
}

// Contract symbol update methods

// GetLastContractUpdate retrieves the last update for a symbol on a specific contract
func (db *DB) GetLastContractUpdate(chainID int64, contractAddr, symbol string) (*ContractSymbolUpdate, error) {
	query := `
		SELECT id, chain_id, contract_address, symbol, last_intent_hash,
		       last_price, last_update_timestamp, update_count
		FROM contract_symbol_updates
		WHERE chain_id = $1 AND contract_address = $2 AND symbol = $3`

	update := &ContractSymbolUpdate{}
	err := db.QueryRow(query, chainID, contractAddr, symbol).Scan(
		&update.ID,
		&update.ChainID,
		&update.ContractAddress,
		&update.Symbol,
		&update.LastIntentHash,
		&update.LastPrice,
		&update.LastUpdateTimestamp,
		&update.UpdateCount,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return update, err
}

// UpdateContractSymbol updates the last update info for a contract symbol
func (db *DB) UpdateContractSymbol(chainID int64, contractAddr, symbol, intentHash, price string) error {
	query := `
		INSERT INTO contract_symbol_updates (
			chain_id, contract_address, symbol, last_intent_hash,
			last_price, last_update_timestamp, update_count
		) VALUES ($1, $2, $3, $4, $5, $6, 1)
		ON CONFLICT (chain_id, contract_address, symbol) DO UPDATE
		SET last_intent_hash = $4,
		    last_price = $5,
		    last_update_timestamp = $6,
		    update_count = contract_symbol_updates.update_count + 1`

	_, err := db.Exec(query, chainID, contractAddr, symbol, intentHash, price, time.Now())
	return err
}

// Transaction methods

// WithTx executes a function within a database transaction
func (db *DB) WithTx(fn func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// GetLastProcessedEvent returns the most recently processed event
func (db *DB) GetLastProcessedEvent() (*ProcessedEvent, error) {
	query := `
		SELECT id, intent_hash, block_number, transaction_hash, log_index,
		       symbol, price, timestamp, signer, processed_at
		FROM processed_events
		ORDER BY processed_at DESC
		LIMIT 1
	`
	
	event := &ProcessedEvent{}
	var signerHex string
	err := db.QueryRow(query).Scan(
		&event.ID,
		&event.IntentHash,
		&event.BlockNumber,
		&event.TransactionHash,
		&event.LogIndex,
		&event.Symbol,
		&event.Price,
		&event.Timestamp,
		&signerHex,
		&event.ProcessedAt,
	)
	
	if err == sql.ErrNoRows {
		return nil, nil
	}
	
	if err == nil {
		event.Signer = common.HexToAddress(signerHex)
	}
	
	return event, err
}

// WorkerPoolStats represents worker pool statistics
type WorkerPoolStats struct {
	QueueSize     int
	ActiveWorkers int
	SuccessRate   float64
}

// GetWorkerPoolStats retrieves worker pool statistics
func (db *DB) GetWorkerPoolStats() (*WorkerPoolStats, error) {
	// Get queue size (pending transactions)
	var queueSize int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM transaction_log
		WHERE status = 'pending'
	`).Scan(&queueSize)
	if err != nil {
		return nil, err
	}
	
	// Get success rate (last 1000 transactions)
	var totalCount, successCount int
	err = db.QueryRow(`
		SELECT 
			COUNT(*) as total,
			COUNT(CASE WHEN status = 'confirmed' THEN 1 END) as success
		FROM (
			SELECT status FROM transaction_log
			WHERE status IN ('confirmed', 'failed')
			ORDER BY created_at DESC
			LIMIT 1000
		) recent_transactions
	`).Scan(&totalCount, &successCount)
	if err != nil {
		return nil, err
	}
	
	successRate := 0.0
	if totalCount > 0 {
		successRate = float64(successCount) / float64(totalCount)
	}
	
	return &WorkerPoolStats{
		QueueSize:     queueSize,
		ActiveWorkers: 0, // This would come from runtime metrics
		SuccessRate:   successRate,
	}, nil
}

// StoreHealthAlert stores a health alert
func (db *DB) StoreHealthAlert(alert interface{}) error {
	// In a real implementation, this would store the alert in the alert_log table
	// For now, just log it
	return nil
}

// Ping checks database connectivity
func (db *DB) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}