package processor

import (
	"testing"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestDedupCacheBehavior(t *testing.T) {
	// Test the dedup cache with IntArraySet-specific keys
	cache := NewDedupCache(100, time.Minute)

	// Test IntentHash-based dedup key
	intentHashKey := "0x00000000000000000000000000000000000000000000000000000000000001d0"

	// First check - should not exist
	assert.False(t, cache.Has(intentHashKey), "Cache should not contain key initially")

	// Add key
	cache.Add(intentHashKey)

	// Second check - should exist
	assert.True(t, cache.Has(intentHashKey), "Cache should contain key after adding")

	// Test with different transaction but same IntentHash
	differentTxKey := "0xabcdef123456789abcdef123456789abcdef123456789abcdef123456789abc-12345-0"

	// This should NOT prevent the IntentHash key from working
	assert.False(t, cache.Has(differentTxKey), "Different transaction key should not exist")
	assert.True(t, cache.Has(intentHashKey), "IntentHash key should still exist")
}

func TestRealWorldIntArraySetScenario(t *testing.T) {
	// Simulate the real-world scenario where the same RequestId appears in different transactions
	cache := NewDedupCache(100, time.Minute)

	// RequestId 464 → IntentHash 0x00000000000000000000000000000000000000000000000000000000000001d0
	intentHash := [32]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xd0}

	// First transaction
	event1 := &types.EventData{
		EventName:   "IntArraySet",
		TxHash:      common.HexToHash("0xdf5e0cefcbecaa5fb4878a9f3c7ec0df6b036a1f948e4947a8c6d7ddb9a9900b"),
		BlockNumber: 26598137,
		LogIndex:    0,
		IntentHash:  intentHash,
	}

	// Second transaction (different tx, same RequestId)
	event2 := &types.EventData{
		EventName:   "IntArraySet",
		TxHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		BlockNumber: 26598138,
		LogIndex:    0,
		IntentHash:  intentHash,
	}

	// Calculate dedup keys using the same logic as processEvent
	dedupKey1 := "0x" + common.Bytes2Hex(event1.IntentHash[:])
	dedupKey2 := "0x" + common.Bytes2Hex(event2.IntentHash[:])

	// Both should have the same dedup key (based on IntentHash, not transaction)
	assert.Equal(t, dedupKey1, dedupKey2, "Same IntentHash should produce same dedup key")
	assert.Equal(t, "0x00000000000000000000000000000000000000000000000000000000000001d0", dedupKey1)

	// First event should not be in cache
	assert.False(t, cache.Has(dedupKey1), "First event should not be in cache initially")

	// Process first event
	cache.Add(dedupKey1)

	// Second event should be detected as duplicate
	assert.True(t, cache.Has(dedupKey2), "Second event should be detected as duplicate")
}
