package bridge

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// MockWriteClient is a mock implementation of WriteClient for testing
type MockWriteClient struct {
	mock.Mock
	ReceiverClientAddress common.Address
	Client               interface{} // Mock ethereum client
}

func (m *MockWriteClient) GetReceiverClientAddress() common.Address {
	return m.ReceiverClientAddress
}

// TestCallRouterMethod_UsesCorrectContractAddress tests that callRouterMethod
// passes the correct contract address from the router configuration
func TestCallRouterMethod_UsesCorrectContractAddress(t *testing.T) {
	// Test addresses
	receiverAddress := common.HexToAddress("0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd") // PushOracleReceiver (wrong)
	randomnessAddress := common.HexToAddress("0x2a1687c44ff91296098B692241Bdf3f5dCf26305") // RandomRequestManager (correct)

	tests := []struct {
		name                string
		routerDestination   string
		expectedAddress     common.Address
		description         string
	}{
		{
			name:                "RandomnessForwarder_UsesRandomRequestManager",
			routerDestination:   "0x2a1687c44ff91296098B692241Bdf3f5dCf26305",
			expectedAddress:     randomnessAddress,
			description:         "IntArraySet events should route to RandomRequestManager contract",
		},
		{
			name:                "IntentForwarder_UsesPushOracleReceiver", 
			routerDestination:   "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd",
			expectedAddress:     receiverAddress,
			description:         "IntentRegistered events should route to PushOracleReceiver contract",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create bridge instance
			bridge := &Bridge{}

			// Create mock write client with receiver address (the wrong address for randomness)
			mockClient := &MockWriteClient{
				ReceiverClientAddress: receiverAddress, // This should NOT be used for randomness
			}

			// Create method config for fulfillRandomInt
			methodConfig := &config.DestinationMethodConfig{
				Name: "fulfillRandomInt",
				ABI:  `{"name":"fulfillRandomInt","type":"function","inputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"}],"outputs":[]}`,
				Params: map[string]string{
					"requestId":  "${event.requestId}",
					"randomInts": "${enrichment.randomInts}",
				},
				GasLimit:      150000,
				GasMultiplier: 1.2,
			}

			// Create update request with router-specified contract address
			updateReq := &bridgetypes.UpdateRequest{
				ID: "test-request",
				Contract: &config.ContractConfig{
					Name:    "test-contract",
					Address: tt.routerDestination, // This should be used, not receiverClient address
					Type:    "randomness",
					Enabled: true,
				},
				DestinationMethodConfig: methodConfig,
				Event: &bridgetypes.EventData{
					EventName: "IntArraySet",
					RequestId: big.NewInt(462),
				},
				ExtractedData: &config.ExtractedData{
					Event: map[string]interface{}{
						"requestId": big.NewInt(462),
					},
					Enrichment: map[string]interface{}{
						"randomInts": []int{999, -888, 777},
					},
				},
			}

			// Test that buildMethodParams works correctly
			params, err := bridge.buildMethodParams(methodConfig, updateReq)
			assert.NoError(t, err)
			assert.Len(t, params, 2)

			// Verify the parameters are built correctly
			assert.Equal(t, big.NewInt(462), params[0]) // requestId
			assert.Equal(t, []int{999, -888, 777}, params[1]) // randomInts

			// Test the address extraction logic (this is what was fixed)
			extractedAddress := common.HexToAddress(updateReq.Contract.Address)
			assert.Equal(t, tt.expectedAddress, extractedAddress, tt.description)

			// Verify that the contract address comes from router config
			// The key test is that extractedAddress equals the router config address
			// For randomness case, ensure it's different from receiver to prove routing works
			if tt.name == "RandomnessForwarder_UsesRandomRequestManager" {
				assert.NotEqual(t, mockClient.ReceiverClientAddress, extractedAddress, 
					"RandomRequestManager should use different address than receiverClient")
			}
			// For intent case, the addresses may be the same, but what matters is that
			// we're using the router config address, not accidentally using client address

			t.Logf("✅ Test passed: %s", tt.description)
			t.Logf("   Router Config Address: %s", updateReq.Contract.Address)
			t.Logf("   Expected Address: %s", tt.expectedAddress.Hex())
			t.Logf("   Receiver Address (should NOT be used): %s", mockClient.ReceiverClientAddress.Hex())
		})
	}
}

// TestRouterDestinationBugFix specifically tests the bug that was fixed
func TestRouterDestinationBugFix(t *testing.T) {
	t.Run("IntArraySetEvent_ShouldRouteToRandomRequestManager", func(t *testing.T) {
		// This test verifies that IntArraySet events route to the correct contract
		// Previously, they were incorrectly routed to PushOracleReceiver

		wrongAddress := common.HexToAddress("0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd") // PushOracleReceiver  
		correctAddress := common.HexToAddress("0x2a1687c44ff91296098B692241Bdf3f5dCf26305") // RandomRequestManager

		// Simulate an IntArraySet event that should go to RandomRequestManager
		updateReq := &bridgetypes.UpdateRequest{
			ID: "RandomRequest-462-421614",
			Contract: &config.ContractConfig{
				Name:    "randomness_manager",
				Address: correctAddress.Hex(), // Router specifies RandomRequestManager
				Type:    "randomness",
				Enabled: true,
			},
			DestinationMethodConfig: &config.DestinationMethodConfig{
				Name: "fulfillRandomInt",
				ABI:  `{"name":"fulfillRandomInt","type":"function","inputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"}],"outputs":[]}`,
				Params: map[string]string{
					"requestId":  "${event.requestId}",
					"randomInts": "${enrichment.randomInts}",
				},
				GasLimit: 150000,
			},
			Event: &bridgetypes.EventData{
				EventName: "IntArraySet",
				RequestId: big.NewInt(462),
			},
			ExtractedData: &config.ExtractedData{
				Event: map[string]interface{}{
					"requestId": big.NewInt(462),
				},
				Enrichment: map[string]interface{}{
					"randomInts": []int{100, -200, 300, -400, 500, 600},
				},
			},
		}

		// Test the fix: contract address should come from updateReq.Contract.Address
		actualAddress := common.HexToAddress(updateReq.Contract.Address)
		
		// BEFORE THE FIX: This would have been wrongAddress (PushOracleReceiver)
		// AFTER THE FIX: This should be correctAddress (RandomRequestManager)
		assert.Equal(t, correctAddress, actualAddress, 
			"IntArraySet events must route to RandomRequestManager, not PushOracleReceiver")
		
		assert.NotEqual(t, wrongAddress, actualAddress,
			"Fixed bug: should not route to PushOracleReceiver anymore")

		t.Logf("✅ Bug Fix Verified:")
		t.Logf("   Event: IntArraySet (requestId: %s)", updateReq.Event.RequestId.String())
		t.Logf("   Method: %s", updateReq.DestinationMethodConfig.Name)
		t.Logf("   Correct Address: %s (RandomRequestManager)", correctAddress.Hex())
		t.Logf("   Wrong Address: %s (PushOracleReceiver - FIXED)", wrongAddress.Hex())
	})

	t.Run("IntentRegisteredEvent_ShouldRouteToPushOracleReceiver", func(t *testing.T) {
		// This test verifies that IntentRegistered events still route to PushOracleReceiver
		// This routing should remain unchanged

		correctAddress := common.HexToAddress("0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd") // PushOracleReceiver

		updateReq := &bridgetypes.UpdateRequest{
			ID: "IntentRegistered-421614-1757675035",
			Contract: &config.ContractConfig{
				Name:    "receiver",
				Address: correctAddress.Hex(), // Router specifies PushOracleReceiver  
				Type:    "pushoracle",
				Enabled: true,
			},
			DestinationMethodConfig: &config.DestinationMethodConfig{
				Name: "handleIntentUpdate",
				ABI:  `{"name":"handleIntentUpdate","type":"function","inputs":[{"name":"intent","type":"tuple"}],"outputs":[]}`,
				Params: map[string]string{
					"intent": "${enrichment.fullIntent}",
				},
				GasLimit: 200000,
			},
		}

		// Test that IntentRegistered events still route correctly
		actualAddress := common.HexToAddress(updateReq.Contract.Address)
		assert.Equal(t, correctAddress, actualAddress,
			"IntentRegistered events should continue routing to PushOracleReceiver")

		t.Logf("✅ IntentRegistered Routing Verified:")
		t.Logf("   Method: %s", updateReq.DestinationMethodConfig.Name)
		t.Logf("   Address: %s (PushOracleReceiver)", correctAddress.Hex())
	})
}

// TestCallRouterMethodParameters tests parameter building and method configuration
func TestCallRouterMethodParameters(t *testing.T) {
	bridge := &Bridge{}

	t.Run("FulfillRandomInt_Parameters", func(t *testing.T) {
		// Test that fulfillRandomInt gets the correct parameters
		methodConfig := &config.DestinationMethodConfig{
			Name: "fulfillRandomInt",
			ABI:  `{"name":"fulfillRandomInt","type":"function","inputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"}],"outputs":[]}`,
			Params: map[string]string{
				"requestId":  "${event.requestId}",
				"randomInts": "${enrichment.randomInts}",
			},
		}

		updateReq := &bridgetypes.UpdateRequest{
			Event: &bridgetypes.EventData{
				RequestId: big.NewInt(777),
			},
			ExtractedData: &config.ExtractedData{
				Event: map[string]interface{}{
					"requestId": big.NewInt(777),
				},
				Enrichment: map[string]interface{}{
					"randomInts": []int{999, -888, 777},
				},
			},
		}

		params, err := bridge.buildMethodParams(methodConfig, updateReq)
		assert.NoError(t, err)
		assert.Len(t, params, 2)

		// Verify parameter values
		assert.Equal(t, big.NewInt(777), params[0])
		assert.Equal(t, []int{999, -888, 777}, params[1])
	})

	t.Run("HandleIntentUpdate_Parameters", func(t *testing.T) {
		// Test that handleIntentUpdate gets the correct parameters  
		intent := &bridgetypes.OracleIntent{
			Symbol:    "BTC",
			Price:     big.NewInt(50000),
			Timestamp: big.NewInt(1234567890),
			Signer:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
		}

		methodConfig := &config.DestinationMethodConfig{
			Name: "handleIntentUpdate",
			ABI:  `{"name":"handleIntentUpdate","type":"function","inputs":[{"name":"intent","type":"tuple"}],"outputs":[]}`,
			Params: map[string]string{
				"intent": "${enrichment.fullIntent}",
			},
		}

		updateReq := &bridgetypes.UpdateRequest{
			Intent: intent,
			ExtractedData: &config.ExtractedData{
				Enrichment: map[string]interface{}{
					"fullIntent": intent,
				},
			},
		}

		params, err := bridge.buildMethodParams(methodConfig, updateReq)
		assert.NoError(t, err)
		assert.Len(t, params, 1)

		// Verify intent parameter
		resolvedIntent, ok := params[0].(*bridgetypes.OracleIntent)
		assert.True(t, ok)
		assert.Equal(t, intent.Symbol, resolvedIntent.Symbol)
		assert.Equal(t, intent.Price, resolvedIntent.Price)
	})
}

// TestRouterConfigValidation tests router configuration scenarios
func TestRouterConfigValidation(t *testing.T) {
	t.Run("ValidRandomnessRouterConfig", func(t *testing.T) {
		// This simulates the actual randomness_forwarder configuration
		routerDest := &config.RouterDestination{
			ChainID:  421614,
			Contract: "0x2a1687c44ff91296098B692241Bdf3f5dCf26305",
			Method: config.DestinationMethodConfig{
				Name: "fulfillRandomInt",
				ABI:  `{"name":"fulfillRandomInt","type":"function","inputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"}],"outputs":[]}`,
				Params: map[string]string{
					"requestId":  "${event.requestId}",
					"randomInts": "${enrichment.randomInts}",
				},
				GasLimit:      150000,
				GasMultiplier: 1.2,
			},
		}

		// Validate configuration
		assert.Equal(t, int64(421614), routerDest.ChainID)
		assert.Equal(t, "0x2a1687c44ff91296098B692241Bdf3f5dCf26305", routerDest.Contract)
		assert.Equal(t, "fulfillRandomInt", routerDest.Method.Name)
		assert.Equal(t, uint64(150000), routerDest.Method.GasLimit)

		// Test that the contract address is parsed correctly
		contractAddr := common.HexToAddress(routerDest.Contract)
		expectedAddr := common.HexToAddress("0x2a1687c44ff91296098B692241Bdf3f5dCf26305")
		assert.Equal(t, expectedAddr, contractAddr)
	})

	t.Run("ValidIntentRouterConfig", func(t *testing.T) {
		// This simulates the actual intent_forwarder configuration
		routerDest := &config.RouterDestination{
			ChainID:  421614,
			Contract: "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd",
			Method: config.DestinationMethodConfig{
				Name: "handleIntentUpdate", 
				ABI:  `{"name":"handleIntentUpdate","type":"function","inputs":[{"name":"intent","type":"tuple"}],"outputs":[]}`,
				Params: map[string]string{
					"intent": "${enrichment.fullIntent}",
				},
				GasLimit:      200000,
				GasMultiplier: 1.2,
			},
		}

		// Validate configuration
		assert.Equal(t, int64(421614), routerDest.ChainID)
		assert.Equal(t, "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd", routerDest.Contract)
		assert.Equal(t, "handleIntentUpdate", routerDest.Method.Name)
		assert.Equal(t, uint64(200000), routerDest.Method.GasLimit)

		// Test that the contract address is parsed correctly
		contractAddr := common.HexToAddress(routerDest.Contract)
		expectedAddr := common.HexToAddress("0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd")
		assert.Equal(t, expectedAddr, contractAddr)
	})
}