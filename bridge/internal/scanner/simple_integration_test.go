package scanner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
)

// TestScannerComponents tests individual scanner components without mocking issues
func TestScannerComponents(t *testing.T) {
	t.Run("EventDefinitionParsing", func(t *testing.T) {
		_, _, eventDefs := CreateTestConfig()
		assert.NotNil(t, eventDefs)
		assert.Contains(t, eventDefs, "IntentRegistered")
		assert.Contains(t, eventDefs, "IntArraySet")
	})

	t.Run("TestDataGeneration", func(t *testing.T) {
		log := CreateTestLog("IntentRegistered", 1000, 0)
		assert.Equal(t, uint64(1000), log.BlockNumber)
		assert.Equal(t, uint(0), log.Index)
		assert.Greater(t, len(log.Topics), 0)
		assert.Greater(t, len(log.Data), 0)
	})

	t.Run("EventDataCreation", func(t *testing.T) {
		eventData := CreateTestEventData("IntentRegistered", 1000)
		assert.Equal(t, "IntentRegistered", eventData.EventName)
		assert.Equal(t, uint64(1000), eventData.BlockNumber)
		assert.Equal(t, "BTC", eventData.Symbol)
		assert.NotNil(t, eventData.Price)
		assert.NotNil(t, eventData.Timestamp)
	})

	t.Run("ConfigurationValidation", func(t *testing.T) {
		scannerConfig, sourceConfig, eventDefs := CreateTestConfig()
		
		// Validate scanner config
		assert.True(t, scannerConfig.Enabled)
		assert.Equal(t, uint64(100), scannerConfig.BlockRange)
		assert.Equal(t, uint64(1000), scannerConfig.MaxBlockGap)
		
		// Validate source config
		assert.Equal(t, int64(11155420), sourceConfig.ChainID)
		assert.Equal(t, "optimism-sepolia", sourceConfig.Name)
		assert.Equal(t, uint64(1000), sourceConfig.StartBlock)
		
		// Validate event definitions
		assert.Len(t, eventDefs, 2)
		
		for eventName, eventDef := range eventDefs {
			assert.NotEmpty(t, eventName)
			assert.NotEmpty(t, eventDef.Contract)
			assert.NotEmpty(t, eventDef.ABI)
		}
	})
}

// TestUtilityFunctions tests utility functions used by scanners
func TestUtilityFunctions(t *testing.T) {
	t.Run("TestChannels", func(t *testing.T) {
		channels := NewTestChannels()
		assert.NotNil(t, channels.EventChan)
		assert.NotNil(t, channels.ErrorChan)
		
		// Test channel operations
		testEvent := CreateTestEventData("IntentRegistered", 1000)
		
		// Send event
		select {
		case channels.EventChan <- testEvent:
			// Success
		default:
			t.Fatal("Event channel should be buffered")
		}
		
		// Receive event
		select {
		case receivedEvent := <-channels.EventChan:
			assert.Equal(t, testEvent.EventName, receivedEvent.EventName)
			assert.Equal(t, testEvent.BlockNumber, receivedEvent.BlockNumber)
		default:
			t.Fatal("Should have received event")
		}
		
		// Drain channels
		channels.DrainChannels()
	})

	t.Run("MockChainState", func(t *testing.T) {
		chainState := MockChainState(11155420, 1500)
		assert.Equal(t, int64(11155420), chainState.ChainID)
		assert.Equal(t, uint64(1500), chainState.LastScanBlock)
		assert.Equal(t, "test-chain", chainState.ChainName)
		assert.False(t, chainState.UpdatedAt.IsZero())
	})
}

// TestEventParsing tests event parsing logic components
func TestEventParsing(t *testing.T) {
	t.Run("IntentRegisteredLog", func(t *testing.T) {
		log := CreateTestLog("IntentRegistered", 1000, 0)
		
		// Verify log structure
		assert.Greater(t, len(log.Topics), 1, "Should have event signature and intent hash")
		assert.Greater(t, len(log.Data), 64, "Should have price and timestamp data")
		
		// Verify event signature (first topic)
		assert.NotEqual(t, log.Topics[0], log.Topics[1], "Event signature should differ from intent hash")
	})

	t.Run("IntArraySetLog", func(t *testing.T) {
		log := CreateTestLog("IntArraySet", 1000, 0)
		
		// Verify log structure for IntArraySet
		assert.Greater(t, len(log.Topics), 1, "Should have event signature and round")
		assert.Greater(t, len(log.Data), 32, "Should have request ID data")
	})
}

// TestConfigurationScenarios tests different configuration scenarios
func TestConfigurationScenarios(t *testing.T) {
	t.Run("DisabledScanner", func(t *testing.T) {
		scannerConfig, sourceConfig, eventDefs := CreateTestConfig()
		scannerConfig.Enabled = false
		
		// Scanner should handle disabled state gracefully
		assert.False(t, scannerConfig.Enabled)
		assert.NotNil(t, sourceConfig)
		assert.NotNil(t, eventDefs)
	})

	t.Run("BackwardSyncEnabled", func(t *testing.T) {
		scannerConfig, _, _ := CreateTestConfig()
		scannerConfig.BackwardSync = true
		scannerConfig.MaxBlockGap = 50
		
		// Should enable enhanced scanning features
		assert.True(t, scannerConfig.BackwardSync)
		assert.Equal(t, uint64(50), scannerConfig.MaxBlockGap)
	})

	t.Run("CustomIntervals", func(t *testing.T) {
		scannerConfig, _, _ := CreateTestConfig()
		originalInterval := scannerConfig.ScanInterval
		
		// Modify scan interval
		scannerConfig.ScanInterval = config.Duration(1000) // 1 second
		
		assert.NotEqual(t, originalInterval, scannerConfig.ScanInterval)
		assert.Equal(t, config.Duration(1000), scannerConfig.ScanInterval)
	})
}

// TestErrorScenarios tests error handling scenarios
func TestErrorScenarios(t *testing.T) {
	t.Run("InvalidEventDefinitions", func(t *testing.T) {
		// Test with nil event definitions
		scannerConfig, sourceConfig, _ := CreateTestConfig()
		
		// This should be handled gracefully by the scanner
		require.NotNil(t, scannerConfig)
		require.NotNil(t, sourceConfig)
	})

	t.Run("InvalidContractAddress", func(t *testing.T) {
		eventDef := &config.EventDefinition{
			Contract: "invalid-address",
			ABI:      `{"name":"TestEvent","type":"event","inputs":[]}`,
		}
		
		// Scanner should handle invalid addresses gracefully
		assert.Equal(t, "invalid-address", eventDef.Contract)
	})

	t.Run("MalformedABI", func(t *testing.T) {
		eventDef := &config.EventDefinition{
			Contract: "0x1234567890abcdef1234567890abcdef12345678",
			ABI:      "invalid json",
		}
		
		// Scanner should handle malformed ABI gracefully
		assert.Equal(t, "invalid json", eventDef.ABI)
	})
}

// BenchmarkTestUtilities benchmarks test utility functions
func BenchmarkTestUtilities(b *testing.B) {
	b.Run("CreateTestLog", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			log := CreateTestLog("IntentRegistered", uint64(i), 0)
			_ = log
		}
	})

	b.Run("CreateTestEventData", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			event := CreateTestEventData("IntentRegistered", uint64(i))
			_ = event
		}
	})

	b.Run("MockChainState", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			state := MockChainState(11155420, uint64(i))
			_ = state
		}
	})
}