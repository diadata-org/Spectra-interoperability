package arch

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type fixture struct {
	IntentHex            string `json:"intent_hex"`
	DomainSeparatorHex   string `json:"domain_separator_hex"`
	DigestHex            string `json:"digest_hex"`
	SignerHex            string `json:"signer_hex"`
}

func loadFixture(t *testing.T, name string) fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	return f
}

func TestOracleIntentRoundTrip(t *testing.T) {
	for _, name := range []string{"intent_a.json", "intent_b.json"} {
		t.Run(name, func(t *testing.T) {
			f := loadFixture(t, name)
			raw, err := hex.DecodeString(f.IntentHex)
			if err != nil {
				t.Fatalf("decode intent_hex: %v", err)
			}
			intent, err := UnmarshalOracleIntent(raw)
			if err != nil {
				t.Fatalf("UnmarshalOracleIntent: %v", err)
			}
			reser, err := MarshalOracleIntent(intent)
			if err != nil {
				t.Fatalf("MarshalOracleIntent: %v", err)
			}
			if !bytes.Equal(reser, raw) {
				t.Fatalf("round-trip mismatch:\n got %s\nwant %s", hex.EncodeToString(reser), f.IntentHex)
			}
			// Cross-check: the signer field decodes to fixture.signer_hex.
			gotSigner := hex.EncodeToString(intent.Signer[:])
			if gotSigner != f.SignerHex {
				t.Fatalf("signer mismatch: got %s want %s", gotSigner, f.SignerHex)
			}
		})
	}
}
