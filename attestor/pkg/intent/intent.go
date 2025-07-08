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
	gethmath "github.com/ethereum/go-ethereum/common/math"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// AttestValue creates a signed intent for cross-chain oracle updates using EIP-712
func AttestValue(ctx context.Context, privateKey string, fromAddress string, price *big.Int, volume *big.Int, symbol string) (string, error) {
	if privateKey == "" {
		return "", fmt.Errorf("private key not provided")
	}

	// Get the current timestamp
	now := time.Now().Unix()
	nowBig := big.NewInt(now)

	utils.DebugLog("Creating intent for %s: price=%s, timestamp=%s", symbol, price.String(), nowBig.String())

	// Generate a unique nonce (using nanoseconds)
	nonceVal := time.Now().UnixNano()
	nonce := big.NewInt(nonceVal)

	// Set expiry to 1 hour from now
	expiry := big.NewInt(now + 3600)

	// Get the chain ID from the environment or use default
	rpcURL := utils.GetEnv("RPC_URL", "https://testnet-rpc.diadata.org")
	ethClient, err := ethclient.Dial(rpcURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to RPC: %v", err)
	}

	// Get chain ID from the connected network
	chainID, err := ethClient.ChainID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	// Use the chain ID from the network
	rpcChainId := chainID.Uint64()

	// Get contract address for EIP-712 domain
	contractAddr := utils.GetEnv("L2_INTENT_REGISTRY_EIP712", "")
	if contractAddr == "" {
		contractAddr = utils.GetEnv("L2_INTENT_REGISTRY_EIP712", "")
		if contractAddr == "" {
			utils.DebugLog("Warning: No contract address found, using zero address")
			contractAddr = "0x0000000000000000000000000000000000000000"
		}
	}

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

	// Create the intent
	intent := types.OracleIntent{
		IntentType: "OracleUpdate",
		Version:    "1.0",
		ChainId:    chainID,
		Nonce:      nonce,
		Expiry:     expiry,
		Symbol:     symbol,
		Price:      price,
		Timestamp:  nowBig,
		Source:     "DIA Oracle",
	}

	// Convert intent to JSON
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal intent: %v", err)
	}

	utils.DebugLog("Intent data: %s", string(intentJSON))

	// Create EIP-712 typed data for signing
	// Define the domain separator and type data
	domain := apitypes.TypedDataDomain{
		Name:              "DIA Oracle Intent",
		Version:           "1",
		ChainId:           gethmath.NewHexOrDecimal256(int64(rpcChainId)),
		VerifyingContract: contractAddr,
		Salt:              "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

	utils.DebugLog("EIP-712 Domain: Name=%s, Version=%s, ChainId=%d, Contract=%s",
		domain.Name, domain.Version, rpcChainId, contractAddr)

	// Create the typed data for EIP-712 signing
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
				{Name: "salt", Type: "bytes32"},
			},
			"OracleIntent": []apitypes.Type{
				{Name: "intentType", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "nonce", Type: "uint256"},
				{Name: "expiry", Type: "uint256"},
				{Name: "symbol", Type: "string"},
				{Name: "price", Type: "uint256"},
				{Name: "timestamp", Type: "uint256"},
				{Name: "source", Type: "string"},
			},
		},
		PrimaryType: "OracleIntent",
		Domain:      domain,
		Message: map[string]interface{}{
			"intentType": intent.IntentType,
			"version":    intent.Version,
			"chainId":    intent.ChainId,
			"nonce":      intent.Nonce,
			"expiry":     intent.Expiry,
			"symbol":     intent.Symbol,
			"price":      intent.Price,
			"timestamp":  intent.Timestamp,
			"source":     intent.Source,
		},
	}

	// Hash the typed data
	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return "", fmt.Errorf("failed to hash domain separator: %v", err)
	}

	fmt.Println("typedData", typedData)

	typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return "", fmt.Errorf("failed to hash typed data: %v", err)
	}

	// Create the final hash to sign
	// rawData := []byte(fmt.Sprintf("\x19\x01%s%s", string(domainSeparator), string(typedDataHash)))
	// hash := crypto.Keccak256Hash(rawData)

	dataToSign := append([]byte{0x19, 0x01}, domainSeparator[:]...)
	dataToSign = append(dataToSign, typedDataHash[:]...)
	hash := crypto.Keccak256Hash(dataToSign)

	utils.DebugLog("Domain separator: 0x%x", domainSeparator)
	utils.DebugLog("Message hash: 0x%x", typedDataHash)
	utils.DebugLog("EIP-712 hash for verification: %s", hash.Hex())

	// Sign the hash
	signature, err := crypto.Sign(hash.Bytes(), privKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign message: %v", err)
	}

	// Adjust the V value (recovery ID) for Ethereum compatibility
	// Ethereum expects V to be 27 or 28, but the signature algorithm gives values 0 or 1
	if signature[64] == 0 || signature[64] == 1 {
		signature[64] += 27
	}

	// Extract R, S, V values for debugging
	r := signature[:32]
	s := signature[32:64]
	v := signature[64]

	utils.DebugLog("Signature components - R: 0x%x, S: 0x%x, V: %d", r, s, v)

	// Convert signature to hex
	signatureHex := "0x" + hex.EncodeToString(signature)
	utils.DebugLog("Signature: %s", signatureHex)

	// Verify the signature ourselves as a sanity check
	recoveredPub, err := crypto.Ecrecover(hash.Bytes(), signature)
	if err != nil {
		utils.DebugLog("Warning: Failed to recover pubkey from signature: %v", err)
	} else {
		pubKey, err := crypto.UnmarshalPubkey(recoveredPub)
		if err == nil {
			recoveredAddr := crypto.PubkeyToAddress(*pubKey)
			utils.DebugLog("Recovered address: %s, expected: %s, match: %v",
				recoveredAddr.Hex(), signerAddress.Hex(), recoveredAddr == signerAddress)
		}
	}

	// Create the final intent message that can be used across chains
	type SignedIntent struct {
		Intent struct {
			IntentType string   `json:"intentType"`
			Version    string   `json:"version"`
			ChainId    *big.Int `json:"chainId"`
			Nonce      *big.Int `json:"nonce"`
			Expiry     *big.Int `json:"expiry"`
			Symbol     string   `json:"symbol"`
			Price      *big.Int `json:"price"`
			Timestamp  *big.Int `json:"timestamp"`
			Source     string   `json:"source"`
		} `json:"intent"`
		Signature string `json:"signature"`
		Signer    string `json:"signer"`
	}

	signedIntent := SignedIntent{}
	signedIntent.Intent.IntentType = intent.IntentType
	signedIntent.Intent.Version = intent.Version
	signedIntent.Intent.ChainId = intent.ChainId
	signedIntent.Intent.Nonce = nonce
	signedIntent.Intent.Expiry = expiry
	signedIntent.Intent.Symbol = intent.Symbol
	signedIntent.Intent.Price = intent.Price
	signedIntent.Intent.Timestamp = intent.Timestamp
	signedIntent.Intent.Source = intent.Source
	signedIntent.Signature = signatureHex
	signedIntent.Signer = signerAddress.Hex()

	// Convert the signed intent to JSON
	signedIntentJSON, err := json.Marshal(signedIntent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal signed intent: %v", err)
	}

	// Log the intent details
	log.Printf("Created EIP-712 signed intent for %s", symbol)
	log.Printf("Price: %s", price.String())
	log.Printf("Timestamp: %s (%s)", nowBig.String(), time.Unix(now, 0).Format(time.RFC3339))
	log.Printf("EIP-712 Hash: %s", hash.Hex())
	log.Printf("Signature: %s", signatureHex)
	log.Printf("Signer: %s", signerAddress.Hex())

	// Return the signed intent JSON
	return string(signedIntentJSON), nil
}

// RegistryMode represents the type of registry contract to interact with
// Now only EIP-712 is supported
type RegistryMode string

const (
	// ModeEIP712 uses EIP-712 structured data for signatures
	ModeEIP712 RegistryMode = "eip712"
)

// PublishIntent publishes a signed intent to the blockchain
func PublishIntent(ctx context.Context, privateKey string, signedIntentJSON string, mode RegistryMode) (string, error) {
	// Get L2 RPC URL and contract address from environment variables
	l2RpcURL := utils.GetEnv("L2_RPC_URL", "https://testnet-rpc.diadata.org")
	l2IntentContract := utils.GetEnv("L2_INTENT_REGISTRY_EIP712", "")

	if l2IntentContract == "" {
		return "", fmt.Errorf("L2_INTENT_REGISTRY_EIP712 environment variable not set")
	}

	utils.DebugLog("Publishing intent to %s using EIP-712", l2IntentContract)

	// For debugging purposes
	utils.DebugLog("Intent JSON: %s", signedIntentJSON)

	// Parse the signed intent
	var signedIntent types.SignedIntent
	err := json.Unmarshal([]byte(signedIntentJSON), &signedIntent)
	if err != nil {
		return "", fmt.Errorf("failed to parse signed intent: %v", err)
	}

	// Connect to the L2 chain
	ethClient, err := ethclient.Dial(l2RpcURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to L2 chain: %v", err)
	}

	// Define the ABI for the OracleIntentRegistry contract
	const registryABI = `[{"inputs":[{"internalType":"string","name":"intentType","type":"string"},{"internalType":"string","name":"version","type":"string"},{"internalType":"uint256","name":"chainId","type":"uint256"},{"internalType":"uint256","name":"nonce","type":"uint256"},{"internalType":"uint256","name":"expiry","type":"uint256"},{"internalType":"string","name":"symbol","type":"string"},{"internalType":"uint256","name":"price","type":"uint256"},{"internalType":"uint256","name":"timestamp","type":"uint256"},{"internalType":"string","name":"source","type":"string"},{"internalType":"bytes","name":"signature","type":"bytes"},{"internalType":"address","name":"signer","type":"address"}],"name":"registerIntent","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

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

	// Pack the input data for the registerIntent function
	data, err := parsedABI.Pack(
		"registerIntent",
		signedIntent.Intent.IntentType,
		signedIntent.Intent.Version,
		signedIntent.Intent.ChainId,
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
		common.HexToAddress(l2IntentContract),
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
