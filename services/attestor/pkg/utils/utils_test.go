package utils

import (
	"os"
	"testing"
)

// TestGetEnv tests the GetEnv function
func TestGetEnv(t *testing.T) {
	// Test with default value
	value := GetEnv("NON_EXISTENT_ENV_VAR", "default_value")
	if value != "default_value" {
		t.Errorf("Expected default value 'default_value', got %s", value)
	}

	// Test with environment variable set
	os.Setenv("TEST_ENV_VAR", "test_value")
	defer os.Unsetenv("TEST_ENV_VAR")

	value = GetEnv("TEST_ENV_VAR", "default_value")
	if value != "test_value" {
		t.Errorf("Expected value 'test_value', got %s", value)
	}
}

// TestCreateEnvTemplate tests the CreateEnvTemplate function
func TestCreateEnvTemplate(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "env-template-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to the temporary directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}
	defer os.Chdir(origDir)

	// Test creating the .env.example file
	err = CreateEnvTemplate()
	if err != nil {
		t.Fatalf("Failed to create .env.example: %v", err)
	}

	// Check if the file exists
	_, err = os.Stat(".env.example")
	if os.IsNotExist(err) {
		t.Error("Expected .env.example file to exist, but it doesn't")
	} else if err != nil {
		t.Fatalf("Failed to check if .env.example exists: %v", err)
	}
}
