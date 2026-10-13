package intent

import (
	"context"
	"math/big"
	"testing"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/keysigner"
)

func testKeySigner(t *testing.T) keysigner.KeySigner {
	t.Helper()
	ks, err := keysigner.NewHexKeySigner("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if err != nil {
		t.Fatalf("failed to create key signer: %v", err)
	}
	return ks
}

func TestAttestValue_NilKeySigner(t *testing.T) {
	_, err := AttestValue(context.Background(), nil, nil, big.NewInt(1), big.NewInt(1), "BTC/USD", nil)
	if err == nil {
		t.Fatal("expected error when key signer is nil")
	}
}

func TestAttestValue_NilMultiClient(t *testing.T) {
	ks := testKeySigner(t)
	_, err := AttestValue(context.Background(), nil, ks, big.NewInt(1), big.NewInt(1), "BTC/USD", nil)
	if err == nil {
		t.Fatal("expected error when multiClient is nil")
	}
}

func TestAttestMultipleValues_NilKeySigner(t *testing.T) {
	_, err := AttestMultipleValues(context.Background(), nil, nil, []SymbolData{{Symbol: "BTC/USD", Price: big.NewInt(1), Volume: big.NewInt(1)}})
	if err == nil {
		t.Fatal("expected error when key signer is nil")
	}
}

func TestAttestMultipleValues_NilMultiClient(t *testing.T) {
	ks := testKeySigner(t)
	_, err := AttestMultipleValues(context.Background(), nil, ks, []SymbolData{{Symbol: "BTC/USD", Price: big.NewInt(1), Volume: big.NewInt(1)}})
	if err == nil {
		t.Fatal("expected error when multiClient is nil")
	}
}

func TestAttestMultipleValues_NoSymbols(t *testing.T) {
	ks := testKeySigner(t)
	// multiClient is nil here too, but the no-symbols check is expected to
	// fire first since it doesn't require network access.
	_, err := AttestMultipleValues(context.Background(), nil, ks, nil)
	if err == nil {
		t.Fatal("expected error when no symbols are provided")
	}
}

func TestPublishIntent_NilRegistryClient(t *testing.T) {
	ks := testKeySigner(t)
	_, err := PublishIntent(context.Background(), nil, ks, "{}")
	if err == nil {
		t.Fatal("expected error when registryClient is nil")
	}
}

func TestPublishMultipleIntents_NilRegistryClient(t *testing.T) {
	ks := testKeySigner(t)
	_, err := PublishMultipleIntents(context.Background(), nil, ks, "{}")
	if err == nil {
		t.Fatal("expected error when registryClient is nil")
	}
}
