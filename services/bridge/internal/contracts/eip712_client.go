package contracts

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// EIP712OracleClient implements specific logic for EIP-712 Oracle contracts
type EIP712OracleClient struct {
	*BaseContractClient
}

// NewEIP712OracleClient creates a new EIP-712 Oracle client
func NewEIP712OracleClient(base *BaseContractClient) *EIP712OracleClient {
	return &EIP712OracleClient{
		BaseContractClient: base,
	}
}

// UpdateOracle implements EIP-712-specific update logic
func (c *EIP712OracleClient) UpdateOracle(ctx context.Context, request *bridgeTypes.UpdateRequest) *bridgeTypes.UpdateResult {
	// Validate EIP-712-specific requirements
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

// validateRequest validates EIP-712-specific requirements
func (c *EIP712OracleClient) validateRequest(request *bridgeTypes.UpdateRequest) error {
	// Check required fields for EIP-712
	if request.Intent.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if request.Intent.Price == nil || request.Intent.Price.Sign() <= 0 {
		return fmt.Errorf("valid price is required")
	}
	if request.Intent.Timestamp == nil || request.Intent.Timestamp.Uint64() == 0 {
		return fmt.Errorf("valid timestamp is required")
	}
	if request.Intent.Signer == (common.Address{}) {
		return fmt.Errorf("signer is required")
	}
	if len(request.Intent.Signature) == 0 {
		return fmt.Errorf("signature is required")
	}

	// Verify EIP-712 signature format
	if len(request.Intent.Signature) != 65 {
		return fmt.Errorf("invalid signature length: expected 65 bytes, got %d", len(request.Intent.Signature))
	}

	// Additional EIP-712-specific validations can be added here
	// For example, verifying the signature against the signer

	return nil
}
