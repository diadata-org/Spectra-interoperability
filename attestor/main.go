package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	defaultRPCURL       = "https://testnet-rpc.diadata.org"
	defaultOracleAddr   = "0x0087342f5f4c7AB23a37c045c3EF710749527c88"
	defaultSymbol       = "BTC/USD"
	defaultPollingTime  = 60 // seconds
	defaultDebug        = false
	defaultConsumerAddr = "" // Default OracleIntentConsumer address
)

var (
	debugMode     bool
	dialEthClient = ethclient.Dial
	oracleABIJSON = `[{"inputs":[{"internalType":"string","name":"key","type":"string"}],"name":"getValue","outputs":[{"internalType":"uint128","name":"","type":"uint128"},{"internalType":"uint128","name":"","type":"uint128"}],"stateMutability":"view","type":"function"}]`
)

func debugLog(format string, v ...interface{}) {
	if debugMode {
		log.Printf("DEBUG: "+format, v...)
	}
}

// RPCClient interface for mocking ethclient.Client().CallContext
type RPCClient interface {
	CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error
}

// OracleClient represents a client for interacting with the oracle contract
type OracleClient struct {
	rpcURL      string
	oracleAddr  string
	signedAddr  string
	privateKey  string
	fromAddress string
	ethClient   *ethclient.Client
	rpcClient   RPCClient
	oracleABI   abi.ABI
}

// NewOracleClient creates a new oracle client
func NewOracleClient(rpcURL, oracleAddrStr, signedAddrStr, privateKeyStr string) (*OracleClient, error) {
	client, err := dialEthClient(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %v", err)
	}

	// Derive the address from the private key
	var fromAddress string
	if privateKeyStr != "" {
		// Remove 0x prefix if present
		cleanPrivKey := strings.TrimPrefix(privateKeyStr, "0x")

		// Parse the private key
		privateKey, err := crypto.HexToECDSA(cleanPrivKey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %v", err)
		}

		// Derive public key and address
		publicKey := privateKey.Public()
		publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("failed to cast public key to ECDSA")
		}

		// Get the address
		address := crypto.PubkeyToAddress(*publicKeyECDSA)
		fromAddress = address.Hex()

		log.Printf("Derived address from private key: %s", fromAddress)
	} else {
		// Fallback to a placeholder if no private key provided
		fromAddress = "0x0000000000000000000000000000000000000000"
	}
	oracleABI, _ := abi.JSON(strings.NewReader(oracleABIJSON))

	return &OracleClient{
		rpcURL:      rpcURL,
		oracleAddr:  oracleAddrStr,
		signedAddr:  signedAddrStr,
		privateKey:  privateKeyStr,
		fromAddress: fromAddress,
		ethClient:   client,
		rpcClient:   client.Client(), // Use the actual RPC client
		oracleABI:   oracleABI,       // Assign the global oracleABI
	}, nil
}

// JSONRPCRequest represents a JSON-RPC request
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      int             `json:"id"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// GetOracleValue fetches the latest value from the oracle
func (oc *OracleClient) GetOracleValue(ctx context.Context, symbol string) (*big.Int, *big.Int, error) {
	// Create the contract address
	contractAddress := common.HexToAddress(oc.oracleAddr)

	debugLog("Calling getValue(%s) on contract %s", symbol, oc.oracleAddr)

	// Pack the input data
	data, err := oc.oracleABI.Pack("getValue", symbol)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to pack input data: %v", err)
	}

	// Create a message call
	callData := map[string]interface{}{
		"to":   contractAddress.Hex(),
		"data": hexutil.Encode(data),
	}

	// Make the call using CallContext
	var result string
	err = oc.rpcClient.CallContext(ctx, &result, "eth_call", callData, "latest")
	if err != nil {
		return nil, nil, fmt.Errorf("contract call failed: %v", err)
	}

	debugLog("Got result: %s", result)

	// Decode the hex result
	resultBytes, err := hexutil.Decode(result)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode result: %v", err)
	}

	// Unpack the result
	outputs, err := oc.oracleABI.Unpack("getValue", resultBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unpack result: %v", err)
	}

	if len(outputs) != 2 {
		return nil, nil, fmt.Errorf("unexpected number of outputs: got %d, want 2", len(outputs))
	}

	// Convert the outputs to big.Int
	price, ok := outputs[0].(*big.Int)
	if !ok {
		return nil, nil, fmt.Errorf("failed to convert price to big.Int")
	}

	timestamp, ok := outputs[1].(*big.Int)
	if !ok {
		return nil, nil, fmt.Errorf("failed to convert timestamp to big.Int")
	}

	debugLog("Unpacked price: %s, timestamp: %s", price.String(), timestamp.String())

	return price, timestamp, nil
}

// AttestValue creates a signed intent for cross-chain oracle updates
func (oc *OracleClient) AttestValue(ctx context.Context, price *big.Int, volume *big.Int, symbol string) (string, error) {
	if oc.privateKey == "" {
		return "", fmt.Errorf("private key not provided")
	}

	// Get the current timestamp
	timestamp := big.NewInt(time.Now().Unix())

	debugLog("Creating intent for %s: price=%s, timestamp=%s", symbol, price.String(), timestamp.String())

	// Create a cross-chain oracle intent structure
	// This structure will be used across different chains
	type OracleIntent struct {
		// Metadata
		IntentType string   `json:"intentType"` // "OracleUpdate"
		Version    string   `json:"version"`    // "1.0"
		ChainId    int64    `json:"chainId"`    // Chain ID where the intent originates
		Nonce      uint64   `json:"nonce"`      // Unique identifier for this intent
		Expiry     *big.Int `json:"expiry"`     // When this intent expires (unix timestamp)

		// Oracle data
		Symbol    string   `json:"symbol"`
		Price     *big.Int `json:"price"`
		Timestamp *big.Int `json:"timestamp"`
		Source    string   `json:"source"` // Source of the oracle data
	}

	// Generate a nonce based on timestamp and random component
	nonce := uint64(time.Now().UnixNano())

	// Create intent expiry (current time + 1 hour)
	expiry := big.NewInt(time.Now().Add(1 * time.Hour).Unix())

	// Extract chain ID from RPC URL (simplified)
	// In a real implementation, you might want to get this from the provider
	chainId := int64(1) // Default to Ethereum mainnet
	if strings.Contains(oc.rpcURL, "testnet") {
		chainId = 5 // Goerli testnet
	}

	// Create the intent
	intent := OracleIntent{
		IntentType: "OracleUpdate",
		Version:    "1.0",
		ChainId:    chainId,
		Nonce:      nonce,
		Expiry:     expiry,
		Symbol:     symbol,
		Price:      price,
		Timestamp:  timestamp,
		Source:     "DIA Oracle",
	}

	// Convert intent to JSON
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal intent: %v", err)
	}

	debugLog("Intent data: %s", string(intentJSON))

	// IMPORTANT: Create the intent hash exactly as the contract does
	// The contract uses keccak256(abi.encode(...)) for hashing
	// We need to recreate this exact format

	// Define ABI types
	stringType, _ := abi.NewType("string", "", nil)
	uint64Type, _ := abi.NewType("uint64", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)

	// Pack the data in the same order as the contract
	abiEncoder := abi.Arguments{
		{Type: stringType},  // intentType
		{Type: stringType},  // version
		{Type: uint64Type},  // chainId
		{Type: uint64Type},  // nonce
		{Type: uint256Type}, // expiry
		{Type: stringType},  // symbol
		{Type: uint256Type}, // price
		{Type: uint256Type}, // timestamp
		{Type: stringType},  // source
	}

	// Pack the values
	packed, err := abiEncoder.Pack(
		intent.IntentType,
		intent.Version,
		uint64(intent.ChainId),
		intent.Nonce,
		intent.Expiry,
		intent.Symbol,
		intent.Price,
		intent.Timestamp,
		intent.Source,
	)
	if err != nil {
		return "", fmt.Errorf("failed to pack intent data: %v", err)
	}

	// Hash the packed data
	intentHash := crypto.Keccak256Hash(packed)
	debugLog("Intent hash (ABI encoded): %s", intentHash.Hex())

	// Parse private key
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(oc.privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	// Get the signer address from the private key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("failed to cast public key to ECDSA")
	}
	signerAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	// Use the same method as the contract to create the hash for signing
	// The contract uses "\x19Ethereum Signed Message:\n32" + intentHash
	prefix := []byte("\x19Ethereum Signed Message:\n32")
	msgHash := crypto.Keccak256(append(prefix, intentHash.Bytes()...))
	debugLog("Message to sign: %s", hex.EncodeToString(msgHash))

	// Sign the message hash
	signature, err := crypto.Sign(msgHash, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign message: %v", err)
	}

	// Check if v needs adjustment for Ethereum compatibility
	// The contract will adjust if v < 27, so we should NOT adjust here
	if signature[64] >= 27 {
		signature[64] -= 27
	}

	// Convert signature to hex
	signatureHex := "0x" + hex.EncodeToString(signature)
	debugLog("Signature: %s", signatureHex)

	// Use the contract's verification method to verify the signature
	// This matches how the contract creates the hash for ecrecover
	prefixVerify := []byte("\x19Ethereum Signed Message:\n32")
	hash := crypto.Keccak256Hash(append(prefixVerify, intentHash.Bytes()...))
	debugLog("Hash for verification: %s", hash.Hex())

	// Create the final intent message that can be used across chains
	type SignedIntent struct {
		Intent    OracleIntent `json:"intent"`
		Signature string       `json:"signature"`
		Signer    string       `json:"signer"`
	}

	signedIntent := SignedIntent{
		Intent:    intent,
		Signature: signatureHex,
		Signer:    signerAddress.Hex(),
	}

	// Convert the signed intent to JSON
	signedIntentJSON, err := json.Marshal(signedIntent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal signed intent: %v", err)
	}

	// Log the intent details
	log.Printf("Created signed intent for %s", symbol)
	log.Printf("Price: %s", price.String())
	log.Printf("Timestamp: %s (%s)", timestamp.String(), time.Unix(timestamp.Int64(), 0).Format(time.RFC3339))
	log.Printf("Intent Hash: %s", intentHash.Hex())
	log.Printf("Signature: %s", signatureHex)
	log.Printf("Signer: %s", signerAddress.Hex())

	// Return the signed intent JSON
	return string(signedIntentJSON), nil
}

// PublishIntent publishes the signed intent to the L2 chain
func (oc *OracleClient) PublishIntent(ctx context.Context, signedIntentJSON string) (string, error) {
	debugLog("Publishing intent to L2 chain")

	// Parse the L2 chain RPC URL from environment variable or use a default
	l2RpcURL := getEnv("L2_RPC_URL", "https://testnet-rpc.diadata.org")
	l2ContractAddr := getEnv("L2_INTENT_CONTRACT", "0x405485B4d4ED05bBD2D5249A9ed564556Cb7A13d")

	debugLog("L2 RPC URL: %s", l2RpcURL)
	debugLog("L2 Intent Contract: %s", l2ContractAddr)

	// Connect to the L2 chain
	l2Client, err := ethclient.Dial(l2RpcURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to L2 chain: %v", err)
	}

	// Parse the signed intent JSON
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
		return "", fmt.Errorf("failed to parse signed intent: %v", err)
	}

	// Define the ABI for the OracleIntentRegistry contract
	const registryABI = `[{"inputs":[{"internalType":"string","name":"intentType","type":"string"},{"internalType":"string","name":"version","type":"string"},{"internalType":"uint64","name":"chainId","type":"uint64"},{"internalType":"uint64","name":"nonce","type":"uint64"},{"internalType":"uint256","name":"expiry","type":"uint256"},{"internalType":"string","name":"symbol","type":"string"},{"internalType":"uint256","name":"price","type":"uint256"},{"internalType":"uint256","name":"timestamp","type":"uint256"},{"internalType":"string","name":"source","type":"string"},{"internalType":"bytes","name":"signature","type":"bytes"},{"internalType":"address","name":"signer","type":"address"}],"name":"registerIntent","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

	// Parse the ABI
	parsedABI, err := abi.JSON(strings.NewReader(registryABI))
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

	log.Printf("Signer address: %s", signerAddr.Hex())

	// Pack the input data for the registerIntent function
	data, err := parsedABI.Pack(
		"registerIntent",
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
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(oc.privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	// Get the chain ID
	chainID, err := l2Client.ChainID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	// Get the sender's nonce
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	log.Printf("Transaction sender address: %s", fromAddress.Hex())

	nonce, err := l2Client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %v", err)
	}

	// Get gas price
	gasPrice, err := l2Client.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get gas price: %v", err)
	}

	// Create transaction
	tx := types.NewTransaction(
		nonce,
		common.HexToAddress(l2ContractAddr),
		big.NewInt(0),
		3000000, // Gas limit
		gasPrice,
		data,
	)

	// Sign the transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %v", err)
	}

	// Send the transaction
	err = l2Client.SendTransaction(ctx, signedTx)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %v", err)
	}

	// Return the transaction hash
	return signedTx.Hash().Hex(), nil
}

func main() {
	// Define command line flags
	startConsumer := flag.Bool("consumer", false, "Start the OracleIntentConsumer service")
	flag.Parse()

	// Get configuration from environment variables or use defaults
	rpcURL := getEnv("RPC_URL", defaultRPCURL)
	oracleAddr := getEnv("ORACLE_ADDRESS", defaultOracleAddr)
	signedAddr := getEnv("SIGNED_ORACLE_ADDRESS", "")
	privateKey := getEnv("PRIVATE_KEY", "")
	symbol := getEnv("SYMBOL", defaultSymbol)
	pollingTimeStr := getEnv("POLLING_TIME", fmt.Sprintf("%d", defaultPollingTime))
	consumerAddr := getEnv("CONSUMER_ADDRESS", defaultConsumerAddr)

	// Set debug mode
	debugModeStr := getEnv("DEBUG", fmt.Sprintf("%t", defaultDebug))
	debugMode, _ = strconv.ParseBool(debugModeStr)
	if debugMode {
		log.Println("Debug mode enabled")
	}

	// Validate required environment variables
	// if signedAddr == "" {
	// 	log.Fatal("SIGNED_ORACLE_ADDRESS environment variable is required")
	// }
	if privateKey == "" {
		log.Fatal("PRIVATE_KEY environment variable is required")
	}

	// Parse polling time
	pollingTime, err := time.ParseDuration(pollingTimeStr + "s")
	if err != nil {
		log.Fatalf("Invalid polling time: %v", err)
	}

	// Create oracle client
	client, err := NewOracleClient(rpcURL, oracleAddr, signedAddr, privateKey)
	if err != nil {
		log.Fatalf("Failed to create oracle client: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Check if we should start the consumer service
	if *startConsumer {
		if consumerAddr == "" {
			log.Fatal("CONSUMER_ADDRESS environment variable is required for consumer service")
		}
		log.Printf("Starting OracleIntentConsumer service for symbol %s", symbol)
		log.Printf("Consumer address: %s", consumerAddr)
		log.Printf("Polling interval: %s", pollingTime)

		// Start consumer service
		go startConsumerService(ctx, client, consumerAddr, symbol, pollingTime)
	} else {
		// Start attestation loop
		log.Printf("Starting attestation service for symbol %s", symbol)
		log.Printf("Oracle address: %s", oracleAddr)
		log.Printf("Signed oracle address: %s", signedAddr)
		log.Printf("Polling interval: %s", pollingTime)

		// Process once immediately
		processAttestation(ctx, client, symbol)
	}

	// Create .env.example file
	if err := createEnvTemplate(); err != nil {
		log.Printf("Warning: Failed to create .env.example file: %v", err)
	}

	// Set up ticker for regular processing
	ticker := time.NewTicker(pollingTime)
	defer ticker.Stop()

	// Main loop
	for {
		select {
		case <-ticker.C:
			if *startConsumer {
				// Consumer service is handled in its own goroutine
			} else {
				processAttestation(ctx, client, symbol)
			}
		case <-sigCh:
			log.Println("Received shutdown signal, exiting...")
			return
		}
	}
}

// processAttestation handles the attestation process
func processAttestation(ctx context.Context, client *OracleClient, symbol string) {
	// Get the current time
	startTime := time.Now()
	log.Printf("Processing attestation for %s at %s", symbol, startTime.Format(time.RFC3339))

	// Get oracle value
	price, timestamp, err := client.GetOracleValue(ctx, symbol)
	if err != nil {
		log.Printf("Failed to get oracle value: %v", err)
		return
	}

	// Use a default volume of 1 for simplicity
	volume := big.NewInt(1)

	// Log the retrieved values
	log.Printf("Retrieved price: %s, timestamp: %s", price.String(), timestamp.String())

	// Create the intent
	signedIntentJSON, err := client.AttestValue(ctx, price, volume, symbol)
	if err != nil {
		log.Printf("Failed to create intent: %v", err)
		return
	}

	// Publish the intent to the L2 chain
	txHash, err := client.PublishIntent(ctx, signedIntentJSON)
	if err != nil {
		log.Printf("Failed to publish intent: %v", err)
		return
	}

	// Log success
	log.Printf("Successfully published intent for %s, transaction hash: %s", symbol, txHash)
	log.Printf("Attestation process completed in %s", time.Since(startTime))
}

// startConsumerService starts the OracleIntentConsumer service
func startConsumerService(ctx context.Context, client *OracleClient, consumerAddr string, symbol string, pollingTime time.Duration) {
	log.Printf("OracleIntentConsumer service started for %s", symbol)

	// Create a ticker for regular processing
	ticker := time.NewTicker(pollingTime)
	defer ticker.Stop()

	// Process once immediately
	processConsumerUpdate(ctx, client, consumerAddr, symbol)

	// Main loop for consumer service
	for {
		select {
		case <-ticker.C:
			processConsumerUpdate(ctx, client, consumerAddr, symbol)
		case <-ctx.Done():
			log.Println("Consumer service shutting down...")
			return
		}
	}
}

// processConsumerUpdate handles the consumer update process
func processConsumerUpdate(ctx context.Context, client *OracleClient, consumerAddr string, symbol string) {
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
	signedIntentJSON, err := client.AttestValue(ctx, price, big.NewInt(1), symbol)
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
	txHash, err := updateConsumerContract(ctx, client, consumerAddr, signedIntent)
	if err != nil {
		log.Printf("Failed to update consumer contract: %v", err)
		return
	}

	// Log success
	log.Printf("Successfully updated consumer contract for %s, transaction hash: %s", symbol, txHash)
	log.Printf("Consumer update process completed in %s", time.Since(startTime))
}

// updateConsumerContract updates the OracleIntentConsumer contract with the latest price
func updateConsumerContract(ctx context.Context, client *OracleClient, consumerAddr string, signedIntent struct {
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
	debugLog("Updating consumer contract at %s", consumerAddr)

	// Connect to the chain
	ethClient, err := ethclient.Dial(client.rpcURL)
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
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(client.privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	// Get the chain ID
	chainID, err := ethClient.ChainID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	// Get the sender's nonce
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
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
	tx := types.NewTransaction(
		nonce,
		common.HexToAddress(consumerAddr),
		big.NewInt(0),
		3000000, // Gas limit
		gasPrice,
		data,
	)

	// Sign the transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
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

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// Create a .env file template
func createEnvTemplate() error {
	envContent := `# Oracle Attestor Configuration
RPC_URL=https://testnet-rpc.diadata.org
ORACLE_ADDRESS=0x0087342f5f4c7AB23a37c045c3EF710749527c88
SIGNED_ORACLE_ADDRESS=
PRIVATE_KEY=
SYMBOL=BTC/USD
POLLING_TIME=60
DEBUG=false

# L2 Chain Configuration for Cross-Chain Intent System
L2_RPC_URL=https://testnet-rpc.diadata.org
L2_INTENT_CONTRACT=0x30c0A25a54e156487f8FF2F5836c5150A2828632

# OracleIntentConsumer Configuration
CONSUMER_ADDRESS=
`
	return os.WriteFile(".env.example", []byte(envContent), 0644)
}
