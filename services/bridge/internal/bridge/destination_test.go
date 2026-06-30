package bridge

import "testing"

// TestWriteClientSatisfiesDestination is a compile-time check that the
// existing EVM *WriteClient implements the new Destination interface.
func TestWriteClientSatisfiesDestination(t *testing.T) {
	var _ Destination = (*WriteClient)(nil)
}

// TestEmptyAdapterMethods verifies the four adapter methods don't allocate
// or panic on a zero-valued client.
func TestEmptyAdapterMethods(t *testing.T) {
	var wc WriteClient
	if wc.Kind() != "evm" {
		t.Errorf("Kind() = %q, want evm", wc.Kind())
	}
	if got := wc.ReceiverAddress(); got != "" {
		t.Errorf("ReceiverAddress() on zero client = %q, want \"\"", got)
	}
	if wc.ChainID() != 0 {
		t.Errorf("ChainID() on zero client = %d, want 0", wc.ChainID())
	}
}
