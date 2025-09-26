package utils

import (
	"fmt"
	"strings"
)

func GenerateReceiverKey(chainID int, receiverAddress string, maxDeliveryWait string) string {
	address := strings.ToLower(receiverAddress)
	if strings.HasPrefix(address, "0x") {
		address = address[2:]
	}
	
	last6Chars := address
	if len(address) >= 6 {
		last6Chars = address[len(address)-6:]
	}
	
	return fmt.Sprintf("%d:%s:%s", chainID, last6Chars, maxDeliveryWait)
}