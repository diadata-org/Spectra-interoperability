package arch

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type pdaEntry struct {
	Pubkey string `json:"pubkey"`
	Bump   uint8  `json:"bump"`
}

func loadPdaVectors(t *testing.T) map[string]pdaEntry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "pda_vectors.json"))
	if err != nil {
		t.Fatalf("read pda_vectors.json: %v", err)
	}
	var m map[string]pdaEntry
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal pda_vectors.json: %v", err)
	}
	return m
}

// testProgramID matches the program id used by scripts/gen-pda-vectors.sh in
// sub-project 2.
func testProgramID() Pubkey {
	b, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	var p Pubkey
	copy(p[:], b)
	return p
}

func TestPDAParity(t *testing.T) {
	vectors := loadPdaVectors(t)
	pid := testProgramID()

	assert := func(t *testing.T, key string, gotPubkey Pubkey, gotBump uint8) {
		t.Helper()
		want, ok := vectors[key]
		if !ok {
			t.Fatalf("no fixture entry for %q", key)
		}
		if hex.EncodeToString(gotPubkey[:]) != want.Pubkey {
			t.Fatalf("%s: pubkey mismatch:\n got %s\nwant %s", key, hex.EncodeToString(gotPubkey[:]), want.Pubkey)
		}
		if gotBump != want.Bump {
			t.Fatalf("%s: bump mismatch: got %d want %d", key, gotBump, want.Bump)
		}
	}

	pk, bump := ConfigPDA(pid)
	assert(t, "636f6e666967", pk, bump) // hex("config")

	pk, bump = DedupPDA(pid)
	assert(t, "6465647570", pk, bump) // hex("dedup")

	pk, bump = PricePDA(pid, "BTC/USD")
	assert(t, "7072696365|sha256:BTC/USD", pk, bump)

	pk, bump = FeeConfigPDA(pid)
	assert(t, "6665655f636f6e666967", pk, bump) // hex("fee_config")

	pk, bump = FeeVaultPDA(pid)
	assert(t, "6665655f7661756c74", pk, bump) // hex("fee_vault")
}
