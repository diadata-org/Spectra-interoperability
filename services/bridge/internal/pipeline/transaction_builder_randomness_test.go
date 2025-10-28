package pipeline

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
)

// TestTransactionBuilderToArray tests the toArray function which is actually used in production
// The enrichment process using go-ethereum's ABI unpacker already returns []*big.Int for int256[] arrays
func TestTransactionBuilderToArray(t *testing.T) {
	tb := &TransactionBuilder{}

	t.Run("Int256ArrayFromEnrichment", func(t *testing.T) {
		// Enrichment returns []*big.Int directly (from go-ethereum's ABI unpacker)
		// NOT []interface{} as previously assumed
		input := []*big.Int{
			big.NewInt(12345),
			big.NewInt(67890),
			big.NewInt(-11111),
		}

		// Create ABI type for int256[]
		int256Type, err := abi.NewType("int256", "", nil)
		require.NoError(t, err)

		abiType := abi.Type{
			T:    abi.SliceTy,
			Elem: &int256Type,
		}

		// Call toArray - should pass through unchanged
		result, err := tb.toArray(input, abiType)
		require.NoError(t, err)

		// Verify it's the correct type
		converted, ok := result.([]*big.Int)
		require.True(t, ok, "Result should be []*big.Int, got %T", result)
		assert.Equal(t, 3, len(converted))
		assert.Equal(t, big.NewInt(12345), converted[0])
		assert.Equal(t, big.NewInt(67890), converted[1])
		assert.Equal(t, big.NewInt(-11111), converted[2])
	})

	t.Run("Uint256ArrayFromEnrichment", func(t *testing.T) {
		// Test uint256[] arrays - enrichment returns []*big.Int for these as well
		input := []*big.Int{
			big.NewInt(111),
			big.NewInt(222),
		}

		uint256Type, err := abi.NewType("uint256", "", nil)
		require.NoError(t, err)

		abiType := abi.Type{
			T:    abi.SliceTy,
			Elem: &uint256Type,
		}

		result, err := tb.toArray(input, abiType)
		require.NoError(t, err)

		converted, ok := result.([]*big.Int)
		require.True(t, ok, "Result should be []*big.Int for uint256[]")
		assert.Equal(t, 2, len(converted))
		assert.Equal(t, big.NewInt(111), converted[0])
		assert.Equal(t, big.NewInt(222), converted[1])
	})

	t.Run("EmptyArray", func(t *testing.T) {
		input := []*big.Int{}

		int256Type, err := abi.NewType("int256", "", nil)
		require.NoError(t, err)

		abiType := abi.Type{
			T:    abi.SliceTy,
			Elem: &int256Type,
		}

		result, err := tb.toArray(input, abiType)
		require.NoError(t, err)

		converted, ok := result.([]*big.Int)
		require.True(t, ok, "Result should be []*big.Int")
		assert.Equal(t, 0, len(converted))
	})

	t.Run("LargeNumbers", func(t *testing.T) {
		// Test with large RequestID-sized numbers
		largeNum := new(big.Int)
		largeNum.SetString("71119481679945759894353904554849254494546402392496571852073868090432771583328", 10)

		input := []*big.Int{
			largeNum,
			big.NewInt(1),
		}

		int256Type, err := abi.NewType("int256", "", nil)
		require.NoError(t, err)

		abiType := abi.Type{
			T:    abi.SliceTy,
			Elem: &int256Type,
		}

		result, err := tb.toArray(input, abiType)
		require.NoError(t, err)

		converted, ok := result.([]*big.Int)
		require.True(t, ok, "Result should be []*big.Int")
		assert.Equal(t, 2, len(converted))
		assert.Equal(t, largeNum.String(), converted[0].String())
	})

	t.Run("NonBigIntArrayPassesThrough", func(t *testing.T) {
		// Arrays of other types should pass through unchanged
		input := []string{"a", "b", "c"}

		stringType, err := abi.NewType("string", "", nil)
		require.NoError(t, err)

		abiType := abi.Type{
			T:    abi.SliceTy,
			Elem: &stringType,
		}

		result, err := tb.toArray(input, abiType)
		require.NoError(t, err)

		// Should pass through unchanged
		assert.Equal(t, input, result)
	})
}

// TestTransactionBuilderBuildMethodParams tests the full parameter building flow
func TestTransactionBuilderBuildMethodParams(t *testing.T) {
	tb := &TransactionBuilder{}

	t.Run("FulfillRandomIntFromEnrichment", func(t *testing.T) {
		// This replicates the actual production scenario
		// Enrichment returns []*big.Int directly from go-ethereum's ABI unpacker
		requestID := new(big.Int)
		requestID.SetString("71119481679945759894353904554849254494546402392496571852073868090432771583328", 10)

		randomIntsFromContract := []*big.Int{
			big.NewInt(999),
			big.NewInt(-888),
			big.NewInt(777),
		}

		data := &config.ExtractedData{
			Event: map[string]interface{}{
				"requestId": requestID,
			},
			Enrichment: map[string]interface{}{
				"requestId":  requestID,
				"randomInts": randomIntsFromContract,
			},
		}

		// Create method ABI manually (since getMethodABI expects specific format)
		uint256Type, err := abi.NewType("uint256", "", nil)
		require.NoError(t, err)
		int256Type, err := abi.NewType("int256", "", nil)
		require.NoError(t, err)

		method := abi.Method{
			Name: "fulfillRandomInt",
			Inputs: abi.Arguments{
				{Name: "requestId", Type: uint256Type},
				{Name: "randomInts", Type: abi.Type{T: abi.SliceTy, Elem: &int256Type}},
			},
		}

		paramMapping := map[string]string{
			"requestId":  "${event.requestId}",
			"randomInts": "${enrichment.randomInts}",
		}

		// Build parameters - this is the actual production code path
		params, err := tb.buildMethodParams(data, paramMapping, method)
		require.NoError(t, err)
		require.Equal(t, 2, len(params), "Should have 2 parameters")

		// Verify requestId
		reqID, ok := params[0].(*big.Int)
		require.True(t, ok, "First parameter should be *big.Int for requestId")
		assert.Equal(t, requestID.String(), reqID.String())

		// Verify randomInts passes through correctly
		randInts, ok := params[1].([]*big.Int)
		require.True(t, ok, "Second parameter should be []*big.Int for randomInts, got %T", params[1])
		assert.Equal(t, 3, len(randInts))
		assert.Equal(t, big.NewInt(999), randInts[0])
		assert.Equal(t, big.NewInt(-888), randInts[1])
		assert.Equal(t, big.NewInt(777), randInts[2])
	})
}

// TestTransactionBuilderConvertToABIType tests the type conversion dispatcher
func TestTransactionBuilderConvertToABIType(t *testing.T) {
	tb := &TransactionBuilder{}

	t.Run("Int256ArrayPassesThrough", func(t *testing.T) {
		// Input is already []*big.Int from enrichment
		input := []*big.Int{
			big.NewInt(100),
			big.NewInt(200),
		}

		int256Type, err := abi.NewType("int256", "", nil)
		require.NoError(t, err)

		abiType := abi.Type{
			T:    abi.SliceTy,
			Elem: &int256Type,
		}

		result, err := tb.convertToABIType(input, abiType)
		require.NoError(t, err)

		converted, ok := result.([]*big.Int)
		require.True(t, ok, "Should remain []*big.Int")
		assert.Equal(t, 2, len(converted))
		assert.Equal(t, big.NewInt(100), converted[0])
		assert.Equal(t, big.NewInt(200), converted[1])
	})
}
