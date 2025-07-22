package database

import (
	"fmt"
	"time"
)

// InitializeChainState ensures chain state exists for a given chain
func (db *DB) InitializeChainState(chainID int64, chainName string, startBlock uint64) error {
	query := `
		INSERT INTO chain_state (
			chain_id, chain_name, last_processed_block, last_scan_block,
			is_healthy, error_count, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (chain_id) DO UPDATE SET
			chain_name = COALESCE(EXCLUDED.chain_name, chain_state.chain_name),
			updated_at = EXCLUDED.updated_at`
	
	_, err := db.Exec(query,
		chainID,
		chainName,
		startBlock,
		startBlock,
		true,
		0,
		time.Now(),
	)
	
	if err != nil {
		return fmt.Errorf("failed to initialize chain state: %w", err)
	}
	
	return nil
}