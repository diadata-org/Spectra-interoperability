package database

import (
	"fmt"
)

// MigrateForGenericEvents adds columns needed for generic event processing
func (db *DB) MigrateForGenericEvents() error {
	queries := []string{
		// Add event_id and event_name columns if they don't exist
		`ALTER TABLE processed_events ADD COLUMN IF NOT EXISTS event_id VARCHAR(100)`,
		`ALTER TABLE processed_events ADD COLUMN IF NOT EXISTS event_name VARCHAR(100)`,

		// Create index on event_id for faster lookups
		`CREATE INDEX IF NOT EXISTS idx_processed_events_event_id ON processed_events(event_id)`,

		// Update unique constraint to use event_id instead of intent_hash for generic events
		// Note: This is safe because existing data will have NULL event_id
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_processed_events_event_id_unique ON processed_events(event_id) WHERE event_id IS NOT NULL`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}
