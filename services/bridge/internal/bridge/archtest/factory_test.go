package archtest

import (
	"testing"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/bridge"
)

// TestDestinationFactory_ArchKind verifies that buildDestination routes to the
// Arch backend when chain.Kind == "arch" and all required fields are present.
// It calls through BuildDestinationForTest, the thin exported wrapper around
// the unexported buildDestination — keeping the production API unexported.
func TestDestinationFactory_ArchKind(t *testing.T) {
	chain := config.ChainConfig{
		ChainID: -1,
		Name:    "arch-testnet",
		Kind:    "arch",
		RPCURLs: []string{"http://127.0.0.1:9002"},
	}
	contract := config.ContractConfig{
		ChainID:          -1,
		Address:          "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		FeeHookProgramID: "1112131415161718191a1b1c1d1e1f2021222324252627282a2b2c2d2e2f3031",
		Type:             "arch-oracle-receiver",
	}
	secret := "1111111111111111111111111111111111111111111111111111111111111111"

	dest, err := bridge.BuildDestinationForTest(chain, contract, secret)
	if err != nil {
		t.Fatalf("BuildDestinationForTest: %v", err)
	}
	if dest.Kind() != "arch" {
		t.Errorf("Kind() = %q, want arch", dest.Kind())
	}
	if dest.ChainID() != -1 {
		t.Errorf("ChainID() = %d, want -1", dest.ChainID())
	}
}

// TestDestinationFactory_ArchKind_MissingSigner ensures a missing signer secret
// returns an error for arch destinations.
func TestDestinationFactory_ArchKind_MissingSigner(t *testing.T) {
	chain := config.ChainConfig{
		ChainID: -1,
		Name:    "arch-testnet",
		Kind:    "arch",
		RPCURLs: []string{"http://127.0.0.1:9002"},
	}
	contract := config.ContractConfig{
		ChainID:          -1,
		Address:          "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		FeeHookProgramID: "1112131415161718191a1b1c1d1e1f2021222324252627282a2b2c2d2e2f3031",
		Type:             "arch-oracle-receiver",
	}

	_, err := bridge.BuildDestinationForTest(chain, contract, "")
	if err == nil {
		t.Fatal("expected error for missing signer, got nil")
	}
}

// TestDestinationFactory_ArchKind_MissingFeeHook ensures a missing
// fee_hook_program_id returns an error for arch destinations.
func TestDestinationFactory_ArchKind_MissingFeeHook(t *testing.T) {
	chain := config.ChainConfig{
		ChainID: -1,
		Name:    "arch-testnet",
		Kind:    "arch",
		RPCURLs: []string{"http://127.0.0.1:9002"},
	}
	contract := config.ContractConfig{
		ChainID: -1,
		Address: "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		// FeeHookProgramID deliberately absent
		Type: "arch-oracle-receiver",
	}
	secret := "1111111111111111111111111111111111111111111111111111111111111111"

	_, err := bridge.BuildDestinationForTest(chain, contract, secret)
	if err == nil {
		t.Fatal("expected error for missing fee_hook_program_id, got nil")
	}
}
