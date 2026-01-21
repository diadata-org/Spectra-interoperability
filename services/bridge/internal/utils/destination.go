package utils

import "fmt"

// GenerateDestinationKey creates a unique key for a destination
func GenerateDestinationKey(chainID int64, contract, symbol string) string {
	// "chainID-contract-symbol"
	return fmt.Sprintf("%d-%s-%s", chainID, contract, symbol)
}
