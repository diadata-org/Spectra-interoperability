package contracts

import (
	"context"
	"fmt"

	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// GenericClient implements a generic contract client for custom contracts
type GenericClient struct {
	*BaseContractClient
}

// NewGenericClient creates a new generic client
func NewGenericClient(base *BaseContractClient) *GenericClient {
	return &GenericClient{
		BaseContractClient: base,
	}
}

// UpdateOracle implements generic update logic
func (c *GenericClient) UpdateOracle(ctx context.Context, request *bridgeTypes.UpdateRequest) *bridgeTypes.UpdateResult {
	// Minimal validation for generic contracts
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

// validateRequest performs minimal validation for generic contracts
func (c *GenericClient) validateRequest(request *bridgeTypes.UpdateRequest) error {
	// Check that we have an intent
	if request.Intent == nil {
		return fmt.Errorf("intent is required")
	}

	// Generic contracts have minimal requirements
	// The actual requirements depend on the contract's method signature
	
	return nil
}