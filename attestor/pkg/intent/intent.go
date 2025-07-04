package intent

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/types"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/utils"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// AttestValue creates a signed intent for cross-chain oracle updates
func AttestValue(ctx context.Context, privateKey string, fromAddress string, price *big.Int, volume *big.Int, symbol string) (string, error) {
	if privateKey == "" {
		return "", fmt.Errorf("private key not provided")
	}

	// Get the current timestamp
	timestamp := big.NewInt(time.Now().Unix())

	utils.DebugLog("Creating intent for %s: price=%s, timestamp=%s", symbol, price.String(), timestamp.String())

	// Generate a nonce based on timestamp and random component
	nonce := uint64(time.Now().UnixNano())

	// Create intent expiry (current time + 1 hour)
	expiry := big.NewInt(time.Now().Add(1 * time.Hour).Unix())

	// Extract chain ID from RPC URL (simplified)
	// In a real implementation, you might want to get this from the provider
	chainId := int64(1) // Default to Ethereum mainnet

	// Create the intent
	intent := types.OracleIntent{
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

	utils.DebugLog("Intent data: %s", string(intentJSON))

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
	utils.DebugLog("Intent hash (ABI encoded): %s", intentHash.Hex())

	// Parse private key
	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	// Get the signer address from the private key
	publicKey := privKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("failed to cast public key to ECDSA")
	}
	signerAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	// Use the same method as the contract to create the hash for signing
	// The contract uses "\x19Ethereum Signed Message:\n32" + intentHash
	prefix := []byte("\x19Ethereum Signed Message:\n32")
	msgHash := crypto.Keccak256(append(prefix, intentHash.Bytes()...))
	utils.DebugLog("Message to sign: %s", hex.EncodeToString(msgHash))

	// Sign the message hash
	signature, err := crypto.Sign(msgHash, privKey)
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
	utils.DebugLog("Signature: %s", signatureHex)

	// Use the contract's verification method to verify the signature
	// This matches how the contract creates the hash for ecrecover
	prefixVerify := []byte("\x19Ethereum Signed Message:\n32")
	hash := crypto.Keccak256Hash(append(prefixVerify, intentHash.Bytes()...))
	utils.DebugLog("Hash for verification: %s", hash.Hex())

	// Create the final intent message that can be used across chains
	signedIntent := types.SignedIntent{
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
func PublishIntent(ctx context.Context, privateKey string, signedIntentJSON string) (string, error) {
	utils.DebugLog("Publishing intent to L2 chain")

	// Parse the L2 chain RPC URL from environment variable or use a default
	l2RpcURL := utils.GetEnv("L2_RPC_URL", "https://testnet-rpc.diadata.org")
	l2ContractAddr := utils.GetEnv("L2_INTENT_CONTRACT", "0x405485B4d4ED05bBD2D5249A9ed564556Cb7A13d")

	utils.DebugLog("L2 RPC URL: %s", l2RpcURL)
	utils.DebugLog("L2 Intent Contract: %s", l2ContractAddr)

	// Connect to the L2 chain
	l2Client, err := ethclient.Dial(l2RpcURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to L2 chain: %v", err)
	}

	// Parse the signed intent JSON
	var signedIntent types.SignedIntent
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
	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	// Get the chain ID
	chainID, err := l2Client.ChainID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	// Get the sender's nonce
	fromAddress := crypto.PubkeyToAddress(privKey.PublicKey)
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
	tx := ethTypes.NewTransaction(
		nonce,
		common.HexToAddress(l2ContractAddr),
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
	err = l2Client.SendTransaction(ctx, signedTx)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %v", err)
	}

	// Return the transaction hash
	return signedTx.Hash().Hex(), nil
}
