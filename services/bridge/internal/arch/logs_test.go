package arch

import (
	"encoding/hex"
	"math/big"
	"testing"
)

func TestParseIntentEvents(t *testing.T) {
	logs := []string{
		`Program log: DIA_ORACLE.DISPATCH ix=HandleIntentUpdate`,
		`Program log: DIA_ORACLE.INTENT_UPDATE intent_hash=0xaabbccddeeff00112233445566778899aabbccddeeff00112233445566778899 symbol="BTC/USD" price=65000000000000 timestamp=1700000017 signer=0x19e7e376e7c213b7e7e7e46cc70a5dd086daff2a`,
		`Program log: irrelevant noise`,
		`Program log: DIA_ORACLE.INTENT_STALE intent_hash=0x1111111111111111111111111111111111111111111111111111111111111111 symbol="ETH/USD" price=2000000000000 timestamp=1700000016 existing_timestamp=1700000020 signer=0x19e7e376e7c213b7e7e7e46cc70a5dd086daff2a`,
		`Program log: DIA_ORACLE.INTENT_REJECTED intent_hash=0x2222222222222222222222222222222222222222222222222222222222222222 symbol="USDC/USD" signer=0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa reason=UnauthorizedSigner`,
	}

	events := ParseIntentEvents(logs)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	// Event 0: update.
	e := events[0]
	if e.Kind != "update" {
		t.Errorf("event 0 kind = %q, want update", e.Kind)
	}
	if e.Symbol != "BTC/USD" {
		t.Errorf("event 0 symbol = %q", e.Symbol)
	}
	wantPrice := new(big.Int).SetUint64(65000000000000)
	if e.Price.Cmp(wantPrice) != 0 {
		t.Errorf("event 0 price = %s, want %s", e.Price, wantPrice)
	}
	if e.Timestamp != 1700000017 {
		t.Errorf("event 0 timestamp = %d", e.Timestamp)
	}
	wantHash, _ := hex.DecodeString("aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")
	if hex.EncodeToString(e.IntentHash[:]) != hex.EncodeToString(wantHash) {
		t.Errorf("event 0 intent hash mismatch")
	}

	// Event 1: stale.
	if events[1].Kind != "stale" {
		t.Errorf("event 1 kind = %q, want stale", events[1].Kind)
	}
	if events[1].StaleAgainst != 1700000020 {
		t.Errorf("event 1 stale_against = %d, want 1700000020", events[1].StaleAgainst)
	}

	// Event 2: rejection.
	if events[2].Kind != "rejected" {
		t.Errorf("event 2 kind = %q, want rejected", events[2].Kind)
	}
	if events[2].Reason != "UnauthorizedSigner" {
		t.Errorf("event 2 reason = %q", events[2].Reason)
	}
}

func TestParseIntentEvents_NoEvents(t *testing.T) {
	events := ParseIntentEvents([]string{"Program log: hello", "Program 1234 invoke [1]"})
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0", len(events))
	}
}
