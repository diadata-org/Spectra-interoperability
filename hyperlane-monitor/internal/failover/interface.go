package failover

import (
	"context"

	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

// BridgeClientInterface defines the interface for bridge clients
type BridgeClientInterface interface {
	CheckHealth(ctx context.Context) error
	TriggerFailover(ctx context.Context, req *types.FailoverRequest) (*types.FailoverResponse, error)
}