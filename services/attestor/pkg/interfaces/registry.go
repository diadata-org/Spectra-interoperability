package interfaces

import (
	"context"
	"math/big"
)

type LatestIntent struct {
	Symbol    string
	Price     *big.Int
	Timestamp *big.Int
}

// RegistryClient defines the interface for interacting with the intent registry
type RegistryClient interface {
	// PublishIntent publishes a signed intent to the registry
	PublishIntent(ctx context.Context, signedIntent []byte) (string, error)

	// PublishBatchIntents publishes multiple signed intents in a single transaction
	PublishBatchIntents(ctx context.Context, signedIntents []byte) (string, error)

	GetLatestIntentByType(ctx context.Context, intentType, symbol string) (*LatestIntent, error)
}
