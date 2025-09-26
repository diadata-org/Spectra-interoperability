
package processor

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePrice(t *testing.T) {
	testCases := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"nil_input", nil, "0"},
		{"big_int", big.NewInt(12345), "12345"},
		{"decimal_string", "98765", "98765"},
		{"hex_string", "0x10", "16"},
		{"large_hex_string", "0x1A2B3C", "1715004"},
		{"invalid_hex_string", "0xG", "0"},
		{"integer", 100, "100"},
		{"float", 123.45, "123.45"},
		{"non_numeric_string", "abc", "abc"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parsePrice(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseTimestamp(t *testing.T) {
	testCases := []struct {
		name     string
		input    interface{}
		expected uint64
	}{
		{"nil_input", nil, 0},
		{"uint64_val", uint64(1678886400), 1678886400},
		{"big_int", big.NewInt(1678886401), 1678886401},
		{"decimal_string", "1678886402", 1678886402},
		{"hex_string", "0x6411B343", 1678881603},
		{"invalid_hex_string", "0xG", 0},
		{"invalid_decimal_string", "abc", 0},
		{"integer", 100, 100},
		{"float64", 123.45, 123},
		{"float32", float32(123.45), 123},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseTimestamp(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
