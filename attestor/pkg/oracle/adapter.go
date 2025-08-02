package oracle

import (
	"context"
	"math/big"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/client"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/errors"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/interfaces"
)

// ClientAdapter adapts the existing OracleClient to implement the OracleReader interface
type ClientAdapter struct {
	client *client.OracleClient
}

// NewClientAdapter creates a new adapter for the existing oracle client
func NewClientAdapter(client *client.OracleClient) *ClientAdapter {
	return &ClientAdapter{
		client: client,
	}
}

// GetValue implements the OracleReader interface
func (a *ClientAdapter) GetValue(ctx context.Context, symbol string) (*big.Int, *big.Int, error) {
	price, timestamp, err := a.client.GetOracleValue(ctx, symbol)
	if err != nil {
		return nil, nil, errors.NewOracleError(symbol, "failed to get value", err)
	}
	
	// Validate the returned values
	if price == nil || price.Sign() <= 0 {
		return nil, nil, errors.NewOracleError(symbol, "invalid price", nil)
	}
	
	if timestamp == nil || timestamp.Sign() <= 0 {
		return nil, nil, errors.NewOracleError(symbol, "invalid timestamp", nil)
	}
	
	return price, timestamp, nil
}

// GetMultipleValues implements the OracleReader interface
func (a *ClientAdapter) GetMultipleValues(ctx context.Context, symbols []string) (map[string]interfaces.OracleValue, error) {
	results := make(map[string]interfaces.OracleValue)
	
	// For now, we fetch values sequentially
	// This could be optimized with concurrent fetches
	for _, symbol := range symbols {
		price, timestamp, err := a.GetValue(ctx, symbol)
		if err != nil {
			// Log the error but continue with other symbols
			continue
		}
		
		results[symbol] = interfaces.OracleValue{
			Symbol:    symbol,
			Price:     price,
			Timestamp: timestamp,
			Volume:    big.NewInt(1), // Default volume
		}
	}
	
	if len(results) == 0 {
		return nil, errors.ErrOracleValueNotFound
	}
	
	return results, nil
}

// GetClient returns the underlying client (for legacy compatibility)
func (a *ClientAdapter) GetClient() *client.OracleClient {
	return a.client
}