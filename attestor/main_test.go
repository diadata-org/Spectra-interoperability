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

