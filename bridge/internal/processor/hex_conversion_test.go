package processor

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestHexPriceConversion(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	tests := []struct {
		name           string
		priceValue     interface{}
		expectedPrice  string
		expectedType   string
	}{
		{
			name:           "hex string price",
			priceValue:     "0x00000000000000000000000000000000000000000000000000000a7477cac135",
			expectedPrice:  "11495342260533",
			expectedType:   "string",
		},
		{
			name:           "big.Int price",
			priceValue:     big.NewInt(11495342260533),
			expectedPrice:  "11495342260533",
			expectedType:   "*big.Int",
		},
		{
			name:           "decimal string price",
			priceValue:     "11495342260533",
			expectedPrice:  "11495342260533",
			expectedType:   "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test ExtractedData like in the actual system
			extractedData := &config.ExtractedData{
				Event: map[string]interface{}{
					"price": tt.priceValue,
				},
			}

			// Create processedEvent like in the actual code
			processedEvent := &database.ProcessedEvent{
				EventID:         "test-event",
				EventName:       "IntentRegistered",
				BlockNumber:     12345,
				TransactionHash: "0xtest",
				LogIndex:        0,
				ProcessedAt:     time.Now(),
			}

			// Test the exact code path from generic_event_processor.go
			if priceValue, ok := extractedData.Event["price"]; ok && priceValue != nil {
				logger.Infof("Processing price value: %v (type: %T)", priceValue, priceValue)
				switch v := priceValue.(type) {
				case *big.Int:
					processedEvent.Price = v.String()
					logger.Infof("Processed as *big.Int: %s", processedEvent.Price)
				case string:
					// Handle hex strings by converting to big.Int first, then to decimal string
					if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
						logger.Infof("Converting hex price value %s to decimal", v)
						if bigInt, success := new(big.Int).SetString(v, 0); success {
							processedEvent.Price = bigInt.String()
							logger.Infof("Successfully converted hex %s to decimal %s", v, processedEvent.Price)
						} else {
							logger.Warnf("Failed to parse hex price value: %s", v)
							processedEvent.Price = "0"
						}
					} else {
						processedEvent.Price = v
						logger.Infof("Processed as decimal string: %s", processedEvent.Price)
					}
				default:
					processedEvent.Price = fmt.Sprintf("%v", v)
					logger.Infof("Processed as default type %T: %s", v, processedEvent.Price)
				}
			} else {
				processedEvent.Price = "0"
			}

			// Verify the result
			assert.Equal(t, tt.expectedPrice, processedEvent.Price, "Price conversion should match expected value")
			
			// Also verify the type detection worked as expected
			actualType := fmt.Sprintf("%T", tt.priceValue)
			assert.Equal(t, tt.expectedType, actualType, "Type detection should match")
		})
	}
}