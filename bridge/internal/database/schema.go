package database

// SQL schema definitions
const (
	createProcessedEventsTable = `
CREATE TABLE IF NOT EXISTS processed_events (
    id SERIAL PRIMARY KEY,
    intent_hash VARCHAR(66) UNIQUE NOT NULL,
    block_number BIGINT NOT NULL,
    transaction_hash VARCHAR(66) NOT NULL,
    log_index INT NOT NULL,
    symbol VARCHAR(20) NOT NULL,
    price NUMERIC(78, 0) NOT NULL,
    timestamp BIGINT NOT NULL,
    signer VARCHAR(42) NOT NULL,
    processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    UNIQUE (transaction_hash, log_index)
);`

	createChainStateTable = `
CREATE TABLE IF NOT EXISTS chain_state (
    chain_id BIGINT PRIMARY KEY,
    chain_name VARCHAR(50),
    last_processed_block BIGINT NOT NULL DEFAULT 0,
    last_scan_block BIGINT NOT NULL DEFAULT 0,
    last_event_block BIGINT,
    last_health_check TIMESTAMP,
    is_healthy BOOLEAN DEFAULT true,
    error_count INT DEFAULT 0,
    last_error TEXT,
    metadata JSONB,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	createTransactionLogTable = `
CREATE TABLE IF NOT EXISTS transaction_log (
    id SERIAL PRIMARY KEY,
    intent_hash VARCHAR(66) NOT NULL,
    destination_chain_id BIGINT NOT NULL,
    destination_chain_name VARCHAR(50),
    contract_address VARCHAR(42) NOT NULL,
    contract_name VARCHAR(100),
    contract_type VARCHAR(50),
    transaction_hash VARCHAR(66),
    status VARCHAR(20) NOT NULL CHECK (status IN ('pending', 'submitted', 'confirmed', 'failed')),
    
    -- Transaction details
    from_address VARCHAR(42),
    to_address VARCHAR(42),
    symbol VARCHAR(20),
    price NUMERIC(78, 0),
    
    -- Gas tracking
    gas_limit BIGINT,
    gas_used BIGINT,
    gas_price NUMERIC(78, 0),
    max_fee_per_gas NUMERIC(78, 0),
    max_priority_fee_per_gas NUMERIC(78, 0),
    transaction_cost NUMERIC(78, 0),
    
    -- Error handling
    error_message TEXT,
    error_code VARCHAR(50),
    retry_count INT DEFAULT 0,
    max_retries INT DEFAULT 3,
    
    -- Timing
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    submitted_at TIMESTAMP,
    confirmed_at TIMESTAMP,
    failed_at TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	createContractSymbolUpdateTable = `
CREATE TABLE IF NOT EXISTS contract_symbol_updates (
    id SERIAL PRIMARY KEY,
    chain_id BIGINT NOT NULL,
    contract_address VARCHAR(42) NOT NULL,
    symbol VARCHAR(20) NOT NULL,
    last_intent_hash VARCHAR(66),
    last_price NUMERIC(78, 0),
    last_update_timestamp TIMESTAMP NOT NULL,
    update_count BIGINT DEFAULT 0,
    total_gas_used NUMERIC(78, 0) DEFAULT 0,
    average_gas_price NUMERIC(78, 0),
    
    UNIQUE (chain_id, contract_address, symbol)
);`

	createPerformanceMetricsTable = `
CREATE TABLE IF NOT EXISTS performance_metrics (
    id SERIAL PRIMARY KEY,
    metric_date DATE NOT NULL,
    metric_hour INT NOT NULL CHECK (metric_hour >= 0 AND metric_hour < 24),
    chain_id BIGINT NOT NULL,
    
    -- Event metrics
    events_detected INT DEFAULT 0,
    events_processed INT DEFAULT 0,
    events_duplicate INT DEFAULT 0,
    events_failed INT DEFAULT 0,
    
    -- Transaction metrics
    transactions_submitted INT DEFAULT 0,
    transactions_confirmed INT DEFAULT 0,
    transactions_failed INT DEFAULT 0,
    
    -- Performance metrics
    avg_processing_time_ms DECIMAL(10, 2),
    avg_confirmation_time_ms DECIMAL(10, 2),
    total_gas_used NUMERIC(78, 0),
    total_gas_cost NUMERIC(78, 0),
    
    -- Health metrics
    websocket_reconnections INT DEFAULT 0,
    block_scan_gaps INT DEFAULT 0,
    health_check_failures INT DEFAULT 0,
    
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    UNIQUE (metric_date, metric_hour, chain_id)
);`

	createAlertLogTable = `
CREATE TABLE IF NOT EXISTS alert_log (
    id SERIAL PRIMARY KEY,
    alert_type VARCHAR(50) NOT NULL,
    severity VARCHAR(20) NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    chain_id BIGINT,
    message TEXT NOT NULL,
    details JSONB,
    notification_sent BOOLEAN DEFAULT false,
    resolved BOOLEAN DEFAULT false,
    resolved_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	createIndices = `
-- Processed events indices
CREATE INDEX IF NOT EXISTS idx_processed_events_block_number ON processed_events(block_number);
CREATE INDEX IF NOT EXISTS idx_processed_events_symbol ON processed_events(symbol);
CREATE INDEX IF NOT EXISTS idx_processed_events_timestamp ON processed_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_processed_events_signer ON processed_events(signer);

-- Transaction log indices
CREATE INDEX IF NOT EXISTS idx_transaction_log_intent_hash ON transaction_log(intent_hash);
CREATE INDEX IF NOT EXISTS idx_transaction_log_status ON transaction_log(status);
CREATE INDEX IF NOT EXISTS idx_transaction_log_created_at ON transaction_log(created_at);
CREATE INDEX IF NOT EXISTS idx_transaction_log_chain_status ON transaction_log(destination_chain_id, status);
CREATE INDEX IF NOT EXISTS idx_transaction_log_contract ON transaction_log(destination_chain_id, contract_address);

-- Contract symbol updates indices
CREATE INDEX IF NOT EXISTS idx_contract_symbol_last_update ON contract_symbol_updates(last_update_timestamp);
CREATE INDEX IF NOT EXISTS idx_contract_symbol_chain ON contract_symbol_updates(chain_id);

-- Performance metrics indices
CREATE INDEX IF NOT EXISTS idx_performance_date_chain ON performance_metrics(metric_date, chain_id);

-- Alert log indices
CREATE INDEX IF NOT EXISTS idx_alert_severity_unresolved ON alert_log(severity, resolved);
CREATE INDEX IF NOT EXISTS idx_alert_created_at ON alert_log(created_at);`
)