package registry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/errors"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/intent"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/interfaces"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Client implements the RegistryClient interface
type Client struct {
	privateKey   string
	ethClient    *ethclient.Client
	registryAddr common.Address
	fromAddress  common.Address
}

// NewClient creates a new registry client
func NewClient(privateKey string, rpcURL string, registryAddr string) (*Client, error) {
	ethClient, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}

	// Derive address from private key
	privKey, err := crypto.HexToECDSA(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)

	return &Client{
		privateKey:   privateKey,
		ethClient:    ethClient,
		registryAddr: common.HexToAddress(registryAddr),
		fromAddress:  fromAddr,
	}, nil
}

// PublishIntent publishes a signed intent to the registry
func (c *Client) PublishIntent(ctx context.Context, signedIntent []byte) (string, error) {
	// Parse the signed intent
	var signedData map[string]interface{}
	if err := json.Unmarshal(signedIntent, &signedData); err != nil {
		return "", errors.NewRegistryError("publish", "", fmt.Errorf("failed to parse signed intent: %w", err))
	}

	// Use the intent package's publish functionality
	txHash, err := intent.PublishIntent(ctx, c.privateKey, string(signedIntent))
	if err != nil {
		return "", errors.NewRegistryError("publish", "", err)
	}

	logger.WithFields(map[string]interface{}{
		"tx_hash": txHash,
		"from":    c.fromAddress.Hex(),
	}).Debug("Published intent to registry")

	return txHash, nil
}

// PublishBatchIntents publishes multiple signed intents in a single transaction
func (c *Client) PublishBatchIntents(ctx context.Context, signedIntents []byte) (string, error) {
	// Parse the batch intent
	var batchData map[string]interface{}
	if err := json.Unmarshal(signedIntents, &batchData); err != nil {
		return "", errors.NewRegistryError("publish batch", "", fmt.Errorf("failed to parse batch intent: %w", err))
	}

	// Use the intent package's batch publish functionality
	txHash, err := intent.PublishMultipleIntents(ctx, c.privateKey, string(signedIntents))
	if err != nil {
		return "", errors.NewRegistryError("publish batch", "", err)
	}

	logger.WithFields(map[string]interface{}{
		"tx_hash": txHash,
		"from":    c.fromAddress.Hex(),
	}).Debug("Published batch intent to registry")

	return txHash, nil
}

// GetIntentByHash retrieves an intent by its hash
func (c *Client) GetIntentByHash(ctx context.Context, intentHash string) (*interfaces.Intent, error) {
	// This would require implementing contract calls to retrieve intent data
	// For now, return not implemented
	return nil, fmt.Errorf("GetIntentByHash not implemented")
}

// GetLatestIntent retrieves the latest intent for a symbol
func (c *Client) GetLatestIntent(ctx context.Context, symbol string) (*interfaces.Intent, error) {
	// This would require implementing contract calls to retrieve latest intent
	// For now, return not implemented
	return nil, fmt.Errorf("GetLatestIntent not implemented")
}

// Close closes the Ethereum client connection
func (c *Client) Close() {
	if c.ethClient != nil {
		c.ethClient.Close()
	}
}