package interfaces

import (
	"context"
)

// RegistryClient defines the interface for interacting with the intent registry
type RegistryClient interface {
	// PublishIntent publishes a signed intent to the registry
	PublishIntent(ctx context.Context, signedIntent []byte) (string, error)
	
	// PublishBatchIntents publishes multiple signed intents in a single transaction
	PublishBatchIntents(ctx context.Context, signedIntents []byte) (string, error)
	
	// GetIntentByHash retrieves an intent by its hash
	GetIntentByHash(ctx context.Context, intentHash string) (*Intent, error)
	
	// GetLatestIntent retrieves the latest intent for a symbol
	GetLatestIntent(ctx context.Context, symbol string) (*Intent, error)
}

// Intent represents a published intent in the registry
type Intent struct {
	Hash      string
	Symbol    string
	Price     string
	Volume    string
	Timestamp uint64
	Signer    string
	Signature string
}