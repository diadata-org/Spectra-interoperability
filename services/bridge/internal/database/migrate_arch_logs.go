package database

import (
	"fmt"
)

// MigrateForArchLogs adds the arch_logs column to processed_events.
func (db *DB) MigrateForArchLogs() error {
	queries := []string{
		`ALTER TABLE processed_events ADD COLUMN IF NOT EXISTS arch_logs JSONB`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}
