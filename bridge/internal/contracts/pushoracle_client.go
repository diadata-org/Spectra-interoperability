package contracts

import (
	"context"
	"fmt"

	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// PushOracleClient implements specific logic for PushOracle contracts
type PushOracleClient struct {
	*BaseContractClient
}

// NewPushOracleClient creates a new PushOracle client
func NewPushOracleClient(base *BaseContractClient) *PushOracleClient {
	return &PushOracleClient{
		BaseContractClient: base,
	}
}

// UpdateOracle implements PushOracle-specific update logic
func (c *PushOracleClient) UpdateOracle(ctx context.Context, request *bridgeTypes.UpdateRequest) *bridgeTypes.UpdateResult {
	// Validate PushOracle-specific requirements
	if err := c.validateRequest(request); err != nil {
		return &bridgeTypes.UpdateResult{
			ChainID:         c.chainID,
			ContractAddress: c.contractAddress,
			Error:           fmt.Errorf("validation failed: %w", err),
		}
	}

	// Use base implementation
	return c.BaseContractClient.UpdateOracle(ctx, request)
}

// validateRequest validates PushOracle-specific requirements
func (c *PushOracleClient) validateRequest(request *bridgeTypes.UpdateRequest) error {
	// Check required fields for PushOracle
	if request.Intent.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if request.Intent.Price == nil || request.Intent.Price.Sign() <= 0 {
		return fmt.Errorf("valid price is required")
	}
	if request.Intent.Timestamp == nil || request.Intent.Timestamp.Uint64() == 0 {
		return fmt.Errorf("valid timestamp is required")
	}

	// Additional PushOracle-specific validations can be added here
	
	return nil
}