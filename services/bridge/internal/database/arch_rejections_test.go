package database

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// dbForTest returns an open *sql.DB if TEST_DATABASE_URL is set, else skips.
func dbForTest(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

func TestInsertAndListArchRejection(t *testing.T) {
	db := dbForTest(t)
	defer db.Close()
	// Seed: needs a processed_events row to reference. Use NULL eventID via
	// the helper accepting 0 to skip the FK ref (modify the helper to accept
	// a nullable event ID for tests).
	var hash [32]byte
	copy(hash[:], []byte("00112233445566778899aabbccddeeff"))
	var signer [20]byte
	copy(signer[:], []byte("aaaaaaaaaaaaaaaaaaaa"))

	err := InsertArchRejection(context.Background(), db, 0, hash, "BTC/USD", signer, "UnauthorizedSigner", "abcd")
	if err != nil {
		t.Fatalf("InsertArchRejection: %v", err)
	}
	rows, err := ListArchRejections(context.Background(), db, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("ListArchRejections: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("got 0 rows, want at least 1")
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM dia_arch_rejections WHERE created_at > NOW() - INTERVAL '1 hour'")
	})
}
