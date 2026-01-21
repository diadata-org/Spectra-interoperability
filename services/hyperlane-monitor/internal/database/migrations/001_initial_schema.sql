-- Initial schema for Hyperlane Monitor

-- Source-Destination pair configuration
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

-- Receiver configurations per pair
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

-- Messages tracked by pair and receiver
CREATE TABLE IF NOT EXISTS hyperlane_messages (
    id SERIAL PRIMARY KEY,
    message_id VARCHAR(66) UNIQUE NOT NULL,
    intent_hash VARCHAR(66) NOT NULL,
    
    -- Source-Destination pair info
    pair_id VARCHAR(100) NOT NULL,
    source_chain_id INT NOT NULL,
    source_tx_hash VARCHAR(66) NOT NULL,
    source_block_number BIGINT NOT NULL,
    
    -- Specific receiver info
    destination_chain_id INT NOT NULL,
    receiver_address VARCHAR(42) NOT NULL,
    receiver_name VARCHAR(200),
    
    -- Intent data
    symbol VARCHAR(20) NOT NULL,
    price DECIMAL(78, 0) NOT NULL,
    timestamp BIGINT NOT NULL,
    intent_data JSONB NOT NULL,
    
    -- Monitoring status
    status VARCHAR(20) DEFAULT 'dispatched',
    priority VARCHAR(20),
    delivery_checks INT DEFAULT 0,
    first_check_at TIMESTAMP,
    last_check_at TIMESTAMP,
    next_check_at TIMESTAMP,
    delivered_at TIMESTAMP,
    
    -- Failover info
    failover_requested BOOLEAN DEFAULT FALSE,
    failover_request_id VARCHAR(66),
    failover_requested_at TIMESTAMP,
    failover_tx_hash VARCHAR(66),
    
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    FOREIGN KEY (pair_id) REFERENCES monitoring_pairs(pair_id)
);

-- Indexes for efficient querying
CREATE INDEX idx_pair_status ON hyperlane_messages(pair_id, status);
CREATE INDEX idx_receiver_status ON hyperlane_messages(receiver_address, status);
CREATE INDEX idx_next_check ON hyperlane_messages(status, next_check_at);
CREATE INDEX idx_intent_hash ON hyperlane_messages(intent_hash);

-- Statistics per pair and receiver
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
);

-- Create update timestamp trigger
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Apply trigger to tables
CREATE TRIGGER update_monitoring_pairs_updated_at BEFORE UPDATE
    ON monitoring_pairs FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_pair_receivers_updated_at BEFORE UPDATE
    ON pair_receivers FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_hyperlane_messages_updated_at BEFORE UPDATE
    ON hyperlane_messages FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();