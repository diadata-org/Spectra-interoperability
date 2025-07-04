package main

import (
	"os"
	"testing"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/utils"
)

// TestEnvironmentVariableHelper tests the GetEnv helper function
func TestEnvironmentVariableHelper(t *testing.T) {
	// Test with default value
	value := utils.GetEnv("NON_EXISTENT_ENV_VAR", "default_value")
	if value != "default_value" {
		t.Errorf("Expected default value 'default_value', got %s", value)
	}

	// Test with environment variable set
	os.Setenv("TEST_ENV_VAR", "test_value")
	defer os.Unsetenv("TEST_ENV_VAR")

	value = utils.GetEnv("TEST_ENV_VAR", "default_value")
	if value != "test_value" {
		t.Errorf("Expected value 'test_value', got %s", value)
	}
}

// TestDebugLogFunction tests the DebugLog function
func TestDebugLogFunction(t *testing.T) {
	// Enable debug mode
	utils.DebugMode = true

	// This is just to ensure the function doesn't panic
	utils.DebugLog("Test debug log message: %s", "hello world")

	// Disable debug mode and ensure it still works
	utils.DebugMode = false
	utils.DebugLog("This should not cause any issues")
}
