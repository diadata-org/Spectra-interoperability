package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
)

// Parameter parsing helpers

func (s *Server) parseIntParam(r *http.Request, name string, defaultValue int) int {
	valueStr := r.URL.Query().Get(name)
	if valueStr == "" {
		return defaultValue
	}
	
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	
	return value
}

func (s *Server) parseInt64Param(r *http.Request, name string, defaultValue int64) int64 {
	valueStr := r.URL.Query().Get(name)
	if valueStr == "" {
		return defaultValue
	}
	
	value, err := strconv.ParseInt(valueStr, 10, 64)
	if err != nil {
		return defaultValue
	}
	
	return value
}

func (s *Server) parseUint64Param(r *http.Request, name string, defaultValue uint64) uint64 {
	valueStr := r.URL.Query().Get(name)
	if valueStr == "" {
		return defaultValue
	}
	
	value, err := strconv.ParseUint(valueStr, 10, 64)
	if err != nil {
		return defaultValue
	}
	
	return value
}

func (s *Server) parseChainID(idStr string) int64 {
	chainID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0
	}
	return chainID
}

// Database query helpers

func (s *Server) getSystemStats() (map[string]interface{}, error) {
	// Get event count
	var eventCount int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM processed_events").Scan(&eventCount)
	if err != nil {
		return nil, err
	}
	
	// Get transaction count
	var txCount int64
	err = s.db.QueryRow("SELECT COUNT(*) FROM transaction_log").Scan(&txCount)
	if err != nil {
		return nil, err
	}
	
	// Get success rate
	var successCount, totalCount int64
	err = s.db.QueryRow(`
		SELECT 
			COUNT(CASE WHEN status = 'confirmed' THEN 1 END),
			COUNT(*)
		FROM transaction_log
		WHERE status IN ('confirmed', 'failed')
	`).Scan(&successCount, &totalCount)
	if err != nil {
		return nil, err
	}
	
	successRate := 0.0
	if totalCount > 0 {
		successRate = float64(successCount) / float64(totalCount)
	}
	
	return map[string]interface{}{
		"events_processed": eventCount,
		"transactions_sent": txCount,
		"success_rate": successRate,
	}, nil
}

func (s *Server) queryEvents(startBlock, endBlock uint64, limit, offset int) ([]*database.ProcessedEvent, error) {
	query := `
		SELECT id, intent_hash, block_number, transaction_hash, log_index,
		       symbol, price, timestamp, signer, processed_at
		FROM processed_events
		WHERE 1=1
	`
	args := []interface{}{}
	argCount := 0
	
	if startBlock > 0 {
		argCount++
		query += " AND block_number >= $" + strconv.Itoa(argCount)
		args = append(args, startBlock)
	}
	
	if endBlock > 0 {
		argCount++
		query += " AND block_number <= $" + strconv.Itoa(argCount)
		args = append(args, endBlock)
	}
	
	query += " ORDER BY block_number DESC, log_index DESC"
	
	argCount++
	query += " LIMIT $" + strconv.Itoa(argCount)
	args = append(args, limit)
	
	argCount++
	query += " OFFSET $" + strconv.Itoa(argCount)
	args = append(args, offset)
	
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var events []*database.ProcessedEvent
	for rows.Next() {
		event := &database.ProcessedEvent{}
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
		events = append(events, event)
	}
	
	return events, nil
}

func (s *Server) getEventByHash(hash string) (*database.ProcessedEvent, error) {
	query := `
		SELECT id, intent_hash, block_number, transaction_hash, log_index,
		       symbol, price, timestamp, signer, processed_at
		FROM processed_events
		WHERE intent_hash = $1
	`
	
	event := &database.ProcessedEvent{}
	var signerHex string
	err := s.db.QueryRow(query, hash).Scan(
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
	
	return event, nil
}

func (s *Server) queryTransactions(chainID int64, status string, limit, offset int) ([]*database.TransactionLog, error) {
	query := `
		SELECT id, intent_hash, destination_chain_id, destination_chain_name,
		       contract_address, contract_name, contract_type, transaction_hash,
		       status, symbol, price, gas_used, gas_price, retry_count, max_retries,
		       created_at, submitted_at, confirmed_at, failed_at
		FROM transaction_log
		WHERE 1=1
	`
	args := []interface{}{}
	argCount := 0
	
	if chainID > 0 {
		argCount++
		query += " AND destination_chain_id = $" + strconv.Itoa(argCount)
		args = append(args, chainID)
	}
	
	if status != "" {
		argCount++
		query += " AND status = $" + strconv.Itoa(argCount)
		args = append(args, status)
	}
	
	query += " ORDER BY created_at DESC"
	
	argCount++
	query += " LIMIT $" + strconv.Itoa(argCount)
	args = append(args, limit)
	
	argCount++
	query += " OFFSET $" + strconv.Itoa(argCount)
	args = append(args, offset)
	
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var transactions []*database.TransactionLog
	for rows.Next() {
		tx := &database.TransactionLog{}
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
			&tx.GasUsed,
			&tx.GasPrice,
			&tx.RetryCount,
			&tx.MaxRetries,
			&tx.CreatedAt,
			&tx.SubmittedAt,
			&tx.ConfirmedAt,
			&tx.FailedAt,
		)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, tx)
	}
	
	return transactions, nil
}

func (s *Server) getTransactionByHash(hash string) (*database.TransactionLog, error) {
	query := `
		SELECT id, intent_hash, destination_chain_id, destination_chain_name,
		       contract_address, contract_name, contract_type, transaction_hash,
		       status, symbol, price, gas_used, gas_price, retry_count, max_retries,
		       created_at, submitted_at, confirmed_at, failed_at
		FROM transaction_log
		WHERE transaction_hash = $1 OR intent_hash = $1
		LIMIT 1
	`
	
	tx := &database.TransactionLog{}
	err := s.db.QueryRow(query, hash).Scan(
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
		&tx.GasUsed,
		&tx.GasPrice,
		&tx.RetryCount,
		&tx.MaxRetries,
		&tx.CreatedAt,
		&tx.SubmittedAt,
		&tx.ConfirmedAt,
		&tx.FailedAt,
	)
	
	if err != nil {
		return nil, err
	}
	
	return tx, nil
}

func (s *Server) getConfiguredChains() ([]map[string]interface{}, error) {
	// This would come from configuration
	// For now, return a placeholder
	return []map[string]interface{}{
		{
			"id": 1,
			"name": "DIA Chain",
			"type": "source",
		},
	}, nil
}

func (s *Server) getSupportedSymbols() ([]string, error) {
	query := `
		SELECT DISTINCT symbol
		FROM processed_events
		ORDER BY symbol
	`
	
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var symbols []string
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			return nil, err
		}
		symbols = append(symbols, symbol)
	}
	
	return symbols, nil
}

func (s *Server) getSymbolUpdates(symbol string, chainID int64, contractAddr string, limit int) ([]map[string]interface{}, error) {
	query := `
		SELECT tl.intent_hash, tl.destination_chain_id, tl.contract_address,
		       tl.price, tl.gas_used, tl.status, tl.confirmed_at
		FROM transaction_log tl
		WHERE tl.symbol = $1
		  AND tl.status = 'confirmed'
	`
	args := []interface{}{symbol}
	argCount := 1
	
	if chainID > 0 {
		argCount++
		query += " AND tl.destination_chain_id = $" + strconv.Itoa(argCount)
		args = append(args, chainID)
	}
	
	if contractAddr != "" {
		argCount++
		query += " AND tl.contract_address = $" + strconv.Itoa(argCount)
		args = append(args, contractAddr)
	}
	
	query += " ORDER BY tl.confirmed_at DESC"
	
	argCount++
	query += " LIMIT $" + strconv.Itoa(argCount)
	args = append(args, limit)
	
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var updates []map[string]interface{}
	for rows.Next() {
		var intentHash string
		var chainID int64
		var contractAddress string
		var price string
		var gasUsed *uint64
		var status string
		var confirmedAt *time.Time
		
		err := rows.Scan(
			&intentHash,
			&chainID,
			&contractAddress,
			&price,
			&gasUsed,
			&status,
			&confirmedAt,
		)
		if err != nil {
			return nil, err
		}
		
		update := map[string]interface{}{
			"intent_hash": intentHash,
			"chain_id": chainID,
			"contract_address": contractAddress,
			"price": price,
			"status": status,
		}
		
		if gasUsed != nil {
			update["gas_used"] = *gasUsed
		}
		if confirmedAt != nil {
			update["confirmed_at"] = confirmedAt
		}
		
		updates = append(updates, update)
	}
	
	return updates, nil
}

var startTime = time.Now()

func (s *Server) getUptime() string {
	return time.Since(startTime).String()
}