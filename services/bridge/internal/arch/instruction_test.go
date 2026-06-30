package arch

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestBuildHandleIntentUpdateData(t *testing.T) {
	for _, name := range []string{"intent_a.json", "intent_b.json"} {
		t.Run(name, func(t *testing.T) {
			f := loadFixture(t, name)
			intentBytes, err := hex.DecodeString(f.IntentHex)
			if err != nil {
				t.Fatalf("decode intent_hex: %v", err)
			}
			intent, err := UnmarshalOracleIntent(intentBytes)
			if err != nil {
				t.Fatalf("UnmarshalOracleIntent: %v", err)
			}
			data, err := BuildHandleIntentUpdateData(intent)
			if err != nil {
				t.Fatalf("BuildHandleIntentUpdateData: %v", err)
			}
			// Expected: discriminator (0x01) + intent bytes.
			expected := append([]byte{0x01}, intentBytes...)
			if !bytes.Equal(data, expected) {
				t.Fatalf("mismatch:\n got %s\nwant %s", hex.EncodeToString(data), hex.EncodeToString(expected))
			}
		})
	}
}
