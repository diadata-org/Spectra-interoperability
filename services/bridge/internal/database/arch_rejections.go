package database

import (
	"context"
	"database/sql"
	"time"
)

// ArchRejection mirrors one row of dia_arch_rejections.
type ArchRejection struct {
	ID         int64
	EventID    sql.NullInt64
	IntentHash []byte
	Symbol     string
	Signer     []byte
	Reason     string
	TxHash     string
	CreatedAt  time.Time
}

// InsertArchRejection persists one parsed rejection. eventID == 0 inserts NULL.
func InsertArchRejection(
	ctx context.Context,
	db *sql.DB,
	eventID int64,
	intentHash [32]byte,
	symbol string,
	signer [20]byte,
	reason, txHash string,
) error {
	const q = `
		INSERT INTO dia_arch_rejections (event_id, intent_hash, symbol, signer, reason, tx_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	var evtArg interface{}
	if eventID > 0 {
		evtArg = eventID
	} else {
		evtArg = nil
	}
	_, err := db.ExecContext(ctx, q, evtArg, intentHash[:], symbol, signer[:], reason, txHash)
	return err
}

// ListArchRejections returns rejections newer than since, newest first.
func ListArchRejections(ctx context.Context, db *sql.DB, since time.Time) ([]ArchRejection, error) {
	const q = `
		SELECT id, event_id, intent_hash, symbol, signer, reason, tx_hash, created_at
		FROM dia_arch_rejections
		WHERE created_at >= $1
		ORDER BY created_at DESC
	`
	rows, err := db.QueryContext(ctx, q, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchRejection
	for rows.Next() {
		var r ArchRejection
		if err := rows.Scan(&r.ID, &r.EventID, &r.IntentHash, &r.Symbol, &r.Signer, &r.Reason, &r.TxHash, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
