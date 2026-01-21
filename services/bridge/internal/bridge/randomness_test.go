package bridge

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// TestRandomnessTransactionFlow tests the complete flow of a successful randomness transaction
// Based on the successful transaction 0x1024bbeacebcaa85fe762ef25d919879a8e645583411a9556d21ae6917688dca
// Request ID: 52996656424404026260937221846395723626337550432718030897205122978920084893552
func TestRandomnessTransactionFlow(t *testing.T) {
	// Setup: Create test configuration for Shannon Network (chain 50312)
	requestID := new(big.Int)
	requestID.SetString("52996656424404026260937221846395723626337550432718030897205122978920084893552", 10)

	// Simulate enrichment data as it would come from the getIntArray contract call
	// The successful transaction had randomInts as []*big.Int
	randomInts := []*big.Int{
		big.NewInt(12345),
		big.NewInt(67890),
		big.NewInt(-11111),
	}

	// Create extracted data with enrichment
	extractedData := &config.ExtractedData{
		Event: map[string]interface{}{
			"requestId": requestID,
			"round":     big.NewInt(1),
			"seed":      "test-seed",
			"signature": "test-signature",
		},
		Enrichment: map[string]interface{}{
			"requestId":     requestID,
			"randomInts":    randomInts, // This is the key - it should be []*big.Int
			"fullRound":     big.NewInt(1),
			"fullSeed":      "enriched-seed",
			"fullSignature": "enriched-signature",
		},
	}

	// Create UpdateRequest as it would be created by the event processor
	updateReq := &bridgetypes.UpdateRequest{
		Event: &bridgetypes.EventData{
			EventName: "IntArraySet",
			RequestId: requestID,
			TxHash:    common.HexToHash("0xtest"),
		},
		ExtractedData: extractedData,
		DestinationChain: &config.DestinationConfig{
			ChainID: 50312,
			Name:    "Shannon Network",
		},
		Contract: &config.ContractConfig{
			Address: "0xbFaE1AdD2182cf5008497bf6580061F81ffD74cb",
			Type:    "randomness",
		},
		DestinationMethodConfig: &config.DestinationMethodConfig{
			Name:     "fulfillRandomInt",
			ABI:      `{"name":"fulfillRandomInt","type":"function","inputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"}]}`,
			GasLimit: 3504118,
			Params: map[string]string{
				"requestId":  "${event.requestId}",
				"randomInts": "${enrichment.randomInts}",
			},
		},
		RouterID: "randomness_router_001",
	}

	// Test parameter resolution
	t.Run("ResolveRequestID", func(t *testing.T) {
		// Mock bridge instance
		b := &Bridge{}

		// Resolve requestId parameter
		value, err := b.resolveParameterValue("${event.requestId}", updateReq)
		require.NoError(t, err)
		assert.Equal(t, requestID, value)
	})

	t.Run("ResolveRandomInts", func(t *testing.T) {
		// Mock bridge instance
		b := &Bridge{}

		// Resolve randomInts parameter
		value, err := b.resolveParameterValue("${enrichment.randomInts}", updateReq)
		require.NoError(t, err)

		// Assert it's the correct type - this is critical for ABI packing
		randomIntsResult, ok := value.([]*big.Int)
		require.True(t, ok, "randomInts should be []*big.Int, got %T", value)
		assert.Equal(t, 3, len(randomIntsResult))
		assert.Equal(t, big.NewInt(12345), randomIntsResult[0])
	})

	t.Run("BuildMethodParams", func(t *testing.T) {
		// Mock bridge instance
		b := &Bridge{}

		// Build all parameters
		params, err := b.buildMethodParams(updateReq.DestinationMethodConfig, updateReq)
		require.NoError(t, err)
		require.Equal(t, 2, len(params), "Should have 2 parameters: requestId and randomInts")

		// Verify parameter types
		reqID, ok := params[0].(*big.Int)
		require.True(t, ok, "First parameter should be *big.Int for requestId")
		assert.Equal(t, requestID.String(), reqID.String())

		randInts, ok := params[1].([]*big.Int)
		require.True(t, ok, "Second parameter should be []*big.Int for randomInts, got %T", params[1])
		assert.Equal(t, 3, len(randInts))
	})
}

// NOTE: Removed ALL obsolete conversion tests.
//
// Investigation revealed that go-ethereum's ABI unpacker returns []*big.Int directly
// for int256[] arrays (NOT []interface{} as we initially assumed).
//
// We created test program /tmp/test_abi_unpack.go which proved:
//   results[1] type: []*big.Int  ← ABI unpacker returns typed slice!
//
// Therefore:
// 1. The convertToInt256Array() function was removed as unnecessary
// 2. All tests testing the conversion logic were removed
// 3. The enrichment process already provides correctly typed data
//
// The original "abi: cannot use slice as type ptr as argument" error was caused by:
// - Parameter ordering bug (Go map iteration is non-deterministic)
// - NOT by type conversion issues
//
// Fix: v1.2.7 fixed parameter ordering by using ABI-defined order instead of map iteration
