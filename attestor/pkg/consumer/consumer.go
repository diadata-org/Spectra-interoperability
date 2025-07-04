package consumer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/client"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/intent"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/utils"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// StartConsumerService starts the OracleIntentConsumer service
func StartConsumerService(ctx context.Context, client *client.OracleClient, consumerAddr string, symbol string, pollingTime time.Duration) {
	log.Printf("OracleIntentConsumer service started for %s", symbol)

	// Create a ticker for regular processing
	ticker := time.NewTicker(pollingTime)
	defer ticker.Stop()

	// Process once immediately
	ProcessConsumerUpdate(ctx, client, consumerAddr, symbol)

	// Main loop for consumer service
	for {
		select {
		case <-ticker.C:
			ProcessConsumerUpdate(ctx, client, consumerAddr, symbol)
		case <-ctx.Done():
			log.Println("Consumer service shutting down...")
			return
		}
	}
}

// ProcessConsumerUpdate handles the consumer update process
func ProcessConsumerUpdate(ctx context.Context, client *client.OracleClient, consumerAddr string, symbol string) {
	// Get the current time
	startTime := time.Now()
	log.Printf("Processing consumer update for %s at %s", symbol, startTime.Format(time.RFC3339))

	// Get oracle value
	price, timestamp, err := client.GetOracleValue(ctx, symbol)
	if err != nil {
		log.Printf("Failed to get oracle value: %v", err)
		return
	}

	// Log the retrieved values
	log.Printf("Retrieved price: %s, timestamp: %s", price.String(), timestamp.String())

	// Create the intent
	signedIntentJSON, err := intent.AttestValue(ctx, client.GetPrivateKey(), client.GetFromAddress(), price, big.NewInt(1), symbol)
	if err != nil {
		log.Printf("Failed to create intent: %v", err)
		return
	}

	// Parse the signed intent
	var signedIntent struct {
		Intent struct {
			IntentType string   `json:"intentType"`
			Version    string   `json:"version"`
			ChainId    int64    `json:"chainId"`
			Nonce      uint64   `json:"nonce"`
			Expiry     *big.Int `json:"expiry"`
			Symbol     string   `json:"symbol"`
			Price      *big.Int `json:"price"`
			Timestamp  *big.Int `json:"timestamp"`
			Source     string   `json:"source"`
		} `json:"intent"`
		Signature string `json:"signature"`
		Signer    string `json:"signer"`
	}

	err = json.Unmarshal([]byte(signedIntentJSON), &signedIntent)
	if err != nil {
		log.Printf("Failed to parse signed intent: %v", err)
		return
	}

	// Update the consumer contract
	txHash, err := UpdateConsumerContract(ctx, client.GetRPCURL(), client.GetPrivateKey(), consumerAddr, signedIntent)
	if err != nil {
		log.Printf("Failed to update consumer contract: %v", err)
		return
	}

	// Log success
	log.Printf("Successfully updated consumer contract for %s, transaction hash: %s", symbol, txHash)
	log.Printf("Consumer update process completed in %s", time.Since(startTime))
}

// UpdateConsumerContract updates the OracleIntentConsumer contract with the latest price
func UpdateConsumerContract(ctx context.Context, rpcURL string, privateKey string, consumerAddr string, signedIntent struct {
	Intent struct {
		IntentType string   `json:"intentType"`
		Version    string   `json:"version"`
		ChainId    int64    `json:"chainId"`
		Nonce      uint64   `json:"nonce"`
		Expiry     *big.Int `json:"expiry"`
		Symbol     string   `json:"symbol"`
		Price      *big.Int `json:"price"`
		Timestamp  *big.Int `json:"timestamp"`
		Source     string   `json:"source"`
	} `json:"intent"`
	Signature string `json:"signature"`
	Signer    string `json:"signer"`
}) (string, error) {
	utils.DebugLog("Updating consumer contract at %s", consumerAddr)

	// Connect to the chain
	ethClient, err := ethclient.Dial(rpcURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to chain: %v", err)
	}

	// Define the ABI for the OracleIntentConsumer contract
	const consumerABI = `[{"inputs":[{"internalType":"string","name":"intentType","type":"string"},{"internalType":"string","name":"version","type":"string"},{"internalType":"uint64","name":"chainId","type":"uint64"},{"internalType":"uint64","name":"nonce","type":"uint64"},{"internalType":"uint256","name":"expiry","type":"uint256"},{"internalType":"string","name":"symbol","type":"string"},{"internalType":"uint256","name":"price","type":"uint256"},{"internalType":"uint256","name":"timestamp","type":"uint256"},{"internalType":"string","name":"source","type":"string"},{"internalType":"bytes","name":"signature","type":"bytes"},{"internalType":"address","name":"signer","type":"address"}],"name":"updatePrice","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

	// Parse the ABI
	parsedABI, err := abi.JSON(strings.NewReader(consumerABI))
	if err != nil {
		return "", fmt.Errorf("failed to parse ABI: %v", err)
	}

	// Convert signature from hex to bytes
	signatureStr := signedIntent.Signature
	if strings.HasPrefix(signatureStr, "0x") {
		signatureStr = signatureStr[2:]
	}
	signatureBytes, err := hex.DecodeString(signatureStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode signature: %v", err)
	}

	// Convert signer from hex to address
	signerAddr := common.HexToAddress(signedIntent.Signer)

	// Pack the input data for the updatePrice function
	data, err := parsedABI.Pack(
		"updatePrice",
		signedIntent.Intent.IntentType,
		signedIntent.Intent.Version,
		uint64(signedIntent.Intent.ChainId),
		signedIntent.Intent.Nonce,
		signedIntent.Intent.Expiry,
		signedIntent.Intent.Symbol,
		signedIntent.Intent.Price,
		signedIntent.Intent.Timestamp,
		signedIntent.Intent.Source,
		signatureBytes,
		signerAddr,
	)
	if err != nil {
		return "", fmt.Errorf("failed to pack input data: %v", err)
	}

	// Parse private key
	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	// Get the chain ID
	chainID, err := ethClient.ChainID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	// Get the sender's nonce
	fromAddress := crypto.PubkeyToAddress(privKey.PublicKey)
	nonce, err := ethClient.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %v", err)
	}

	// Get gas price
	gasPrice, err := ethClient.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get gas price: %v", err)
	}

	// Create transaction
	tx := ethTypes.NewTransaction(
		nonce,
		common.HexToAddress(consumerAddr),
		big.NewInt(0),
		3000000, // Gas limit
		gasPrice,
		data,
	)

	// Sign the transaction
	signedTx, err := ethTypes.SignTx(tx, ethTypes.NewEIP155Signer(chainID), privKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %v", err)
	}

	// Send the transaction
	err = ethClient.SendTransaction(ctx, signedTx)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %v", err)
	}

	// Return the transaction hash
	return signedTx.Hash().Hex(), nil
}
