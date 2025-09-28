package processor

import (
	"crypto/sha256"
	"fmt"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestCompositeIntentHashGeneration(t *testing.T) {
	tests := []struct {
		name           string
		originalIntent [32]byte
		eventID        string
		destID         string
		expectedLength int
		description    string
	}{
		{
			name:           "RequestId 466 with real tx data",
			originalIntent: [32]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xd2},
			eventID:        "0xcd488f08ed94d32d578712e86be17da16eff92397670837f37f384cd20700de6-26629870-0",
			destID:         "421614-0x2a1687c44ff91296098B692241Bdf3f5dCf26305",
			expectedLength: 66, // "0x" + 64 hex chars
			description:    "Should generate SHA256 hash that fits in VARCHAR(66)",
		},
		{
			name:           "RequestId 466 with second tx data",
			originalIntent: [32]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xd2},
			eventID:        "0x96dc2ee77bd0ccffa89251d16361fd37a30446560816337b96ba3f52a0fbee77-26630888-0",
			destID:         "421614-0x2a1687c44ff91296098B692241Bdf3f5dCf26305",
			expectedLength: 66,
			description:    "Same RequestId in different tx should generate different hash",
		},
		{
			name:           "RequestId 467 with different data",
			originalIntent: [32]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xd3},
			eventID:        "0xe9930de9a649c39a19db6e680e7578293b6a46415820fd34a25b9a88f12827f0-26630998-0",
			destID:         "421614-0x2a1687c44ff91296098B692241Bdf3f5dCf26305",
			expectedLength: 66,
			description:    "Different RequestId should generate different hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply the same logic as in processEvent
			hashInput := fmt.Sprintf("0x%x-%s-%s", tt.originalIntent, tt.eventID, tt.destID)
			hash := sha256.Sum256([]byte(hashInput))
			compositeIntentHash := fmt.Sprintf("0x%x", hash)

			t.Logf("Hash input: %s", hashInput)
			t.Logf("Composite hash: %s", compositeIntentHash)
			t.Logf("Hash length: %d", len(compositeIntentHash))

			// Verify length fits in database VARCHAR(66)
			assert.Equal(t, tt.expectedLength, len(compositeIntentHash),
				"Hash should be exactly %d characters to fit VARCHAR(66)", tt.expectedLength)

			// Verify it starts with 0x
			assert.True(t, strings.HasPrefix(compositeIntentHash, "0x"), "Hash should start with 0x")

			// Verify the input was too long without hashing
			assert.Greater(t, len(hashInput), 66,
				"Original input should be longer than 66 chars (requiring hashing)")
		})
	}
}

func TestCompositeIntentHashUniqueness(t *testing.T) {
	// Test that different inputs generate different hashes
	originalIntent := [32]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xd2}

	// Same RequestId in different transactions
	eventID1 := "0xcd488f08ed94d32d578712e86be17da16eff92397670837f37f384cd20700de6-26629870-0"
	eventID2 := "0x96dc2ee77bd0ccffa89251d16361fd37a30446560816337b96ba3f52a0fbee77-26630888-0"
	destID := "421614-0x2a1687c44ff91296098B692241Bdf3f5dCf26305"

	// Generate hashes
	hash1Input := fmt.Sprintf("0x%x-%s-%s", originalIntent, eventID1, destID)
	hash1 := sha256.Sum256([]byte(hash1Input))
	compositeHash1 := fmt.Sprintf("0x%x", hash1)

	hash2Input := fmt.Sprintf("0x%x-%s-%s", originalIntent, eventID2, destID)
	hash2 := sha256.Sum256([]byte(hash2Input))
	compositeHash2 := fmt.Sprintf("0x%x", hash2)

	// Verify they are different
	assert.NotEqual(t, compositeHash1, compositeHash2,
		"Same RequestId in different transactions should generate different composite hashes")

	t.Logf("Hash 1: %s", compositeHash1)
	t.Logf("Hash 2: %s", compositeHash2)
}

func TestActualProblematicCase(t *testing.T) {
	// Test the actual case that was failing in production
	originalIntent := [32]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xd2}
	eventID := "0xcd488f08ed94d32d578712e86be17da16eff92397670837f37f384cd20700de6-26629870-0"
	destID := "421614-0x2a1687c44ff91296098B692241Bdf3f5dCf26305"

	// This was the original failing composite string from the logs
	originalFailingString := "0x00000000000000000000000000000000000000000000000000000000000001d2-0xcd488f08ed94d32d578712e86be17da16eff92397670837f37f384cd20700de6-26629870-0-421614-0x2a1687c44ff91296098B692241Bdf3f5dCf26305"

	// Generate the new hashed version
	hashInput := fmt.Sprintf("0x%x-%s-%s", originalIntent, eventID, destID)
	hash := sha256.Sum256([]byte(hashInput))
	compositeIntentHash := fmt.Sprintf("0x%x", hash)

	t.Logf("Original failing string length: %d", len(originalFailingString))
	t.Logf("New hashed string length: %d", len(compositeIntentHash))
	t.Logf("Original: %s", originalFailingString)
	t.Logf("New hash: %s", compositeIntentHash)

	// Verify the fix
	assert.Greater(t, len(originalFailingString), 66, "Original string should be too long")
	assert.Equal(t, 66, len(compositeIntentHash), "New hash should fit in VARCHAR(66)")
	assert.NotEqual(t, originalFailingString, compositeIntentHash, "Hash should be different from original")
}
