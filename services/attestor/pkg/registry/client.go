package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	multirpc "github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/errors"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/intent"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/interfaces"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Client implements the RegistryClient interface
type Client struct {
	privateKey     string
	registryAddr   common.Address
	fromAddress    common.Address
	registryClient *multirpc.MultiClient
}

// NewClient creates a new registry client
func NewClient(privateKey string, registryAddr string, registryRPCURLs []string) (*Client, error) {
	cleanKey := strings.TrimPrefix(privateKey, "0x")
	privKey, err := crypto.HexToECDSA(cleanKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)

	registryClient, err := multirpc.NewMultiClient(registryRPCURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to registry RPC: %w", err)
	}

	logger.Infof("Connected to registry RPC: %s", registryClient.GetCurrentRPCURL())

	return &Client{
		privateKey:     privateKey,
		registryAddr:   common.HexToAddress(registryAddr),
		fromAddress:    fromAddr,
		registryClient: registryClient,
	}, nil
}

// PublishIntent publishes a signed intent to the registry
func (c *Client) PublishIntent(ctx context.Context, signedIntent []byte) (string, error) {
	// Parse the signed intent
	var signedData map[string]interface{}
	if err := json.Unmarshal(signedIntent, &signedData); err != nil {
		return "", errors.NewRegistryError("publish", "", fmt.Errorf("failed to parse signed intent: %w", err))
	}

	// Use the intent package's publish functionality with shared registry client
	txHash, err := intent.PublishIntent(ctx, c.registryClient, c.privateKey, string(signedIntent))
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

	// Use the intent package's batch publish functionality with shared registry client
	txHash, err := intent.PublishMultipleIntents(ctx, c.registryClient, c.privateKey, string(signedIntents))
	if err != nil {
		return "", errors.NewRegistryError("publish batch", "", err)
	}

	logger.WithFields(map[string]interface{}{
		"tx_hash": txHash,
		"from":    c.fromAddress.Hex(),
	}).Debug("Published batch intent to registry")

	return txHash, nil
}

// GetLatestIntentByType gets
func (c *Client) GetLatestIntentByType(ctx context.Context, intentType, symbol string) (*interfaces.LatestIntent, error) {
	const registryABI = `[{"inputs":[{"internalType":"string","name":"intentType","type":"string"},{"internalType":"string","name":"symbol","type":"string"}],"name":"getLatestIntentByType","outputs":[{"components":[{"internalType":"string","name":"intentType","type":"string"},{"internalType":"string","name":"version","type":"string"},{"internalType":"uint256","name":"chainId","type":"uint256"},{"internalType":"uint256","name":"nonce","type":"uint256"},{"internalType":"uint256","name":"expiry","type":"uint256"},{"internalType":"string","name":"symbol","type":"string"},{"internalType":"uint256","name":"price","type":"uint256"},{"internalType":"uint256","name":"timestamp","type":"uint256"},{"internalType":"string","name":"source","type":"string"},{"internalType":"bytes","name":"signature","type":"bytes"},{"internalType":"address","name":"signer","type":"address"}],"internalType":"struct OracleIntentUtils.OracleIntent","name":"intent","type":"tuple"}],"stateMutability":"view","type":"function"}]`

	parsedABI, err := abi.JSON(strings.NewReader(registryABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	data, err := parsedABI.Pack("getLatestIntentByType", intentType, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to pack input data: %w", err)
	}

	msg := ethereum.CallMsg{
		To:   &c.registryAddr,
		Data: data,
	}

	result, err := c.registryClient.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, errors.NewRegistryError("get latest intent", symbol, fmt.Errorf("contract call failed: %w", err))
	}

	// Define the struct to receive the unpacked data
	var out struct {
		Intent struct {
			IntentType string
			Version    string
			ChainId    *big.Int
			Nonce      *big.Int
			Expiry     *big.Int
			Symbol     string
			Price      *big.Int
			Timestamp  *big.Int
			Source     string
			Signature  []byte
			Signer     common.Address
		}
	}

	err = parsedABI.UnpackIntoInterface(&out, "getLatestIntentByType", result)
	if err != nil {
		return nil, errors.NewRegistryError("get latest intent", symbol, fmt.Errorf("failed to unpack result: %w", err))
	}

	if out.Intent.Price == nil || out.Intent.Timestamp == nil {
		return nil, errors.NewRegistryError("get latest intent", symbol, fmt.Errorf("price or timestamp is nil"))
	}

	logger.WithFields(map[string]interface{}{
		"symbol":    symbol,
		"price":     out.Intent.Price.String(),
		"timestamp": out.Intent.Timestamp.String(),
	}).Debug("Retrieved latest intent from registry")

	return &interfaces.LatestIntent{
		Symbol:    symbol,
		Price:     out.Intent.Price,
		Timestamp: out.Intent.Timestamp,
	}, nil
}

// Close closes the registry client and cleans up resources
func (c *Client) Close() {
	if c.registryClient != nil {
		c.registryClient.Close()
	}
}
