package intent

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	multirpc "github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/types"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	gethmath "github.com/ethereum/go-ethereum/common/math"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

var (
	nonceCounter  atomic.Int64
	nonceInitOnce sync.Once
)

func initNonceCounter() {
	nonceInitOnce.Do(func() {
		nonceCounter.Store(time.Now().UnixNano())
	})
}

func generateNonce() *big.Int {
	initNonceCounter()
	nonce := nonceCounter.Add(1)
	return big.NewInt(nonce)
}

func AttestValue(ctx context.Context, multiClient *multirpc.MultiClient, privateKey string, fromAddress string, price *big.Int, volume *big.Int, symbol string, oracleTimestamp *big.Int) (string, error) {
	if privateKey == "" {
		return "", fmt.Errorf("private key not provided")
	}
	if multiClient == nil {
		return "", fmt.Errorf("multiClient is required")
	}

	// Use oracle timestamp if provided, otherwise fall back to current time
	var nowBig *big.Int
	if oracleTimestamp != nil && oracleTimestamp.Sign() > 0 {
		nowBig = new(big.Int).Set(oracleTimestamp)
	} else {
		nowBig = big.NewInt(time.Now().Unix())
	}

	// Generate unique nonce using atomic counter to prevent collisions
	nonce := generateNonce()
	expiry := big.NewInt(time.Now().Unix() + 3600)

	cfg := config.Get()
	intentType := cfg.Attestor.IntentType
	if intentType == "" {
		intentType = "OracleUpdate"
	}
	intentVersion := cfg.Attestor.IntentVersion
	if intentVersion == "" {
		intentVersion = "1.0"
	}

	chainID, err := multiClient.ChainID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	contractAddr := strings.TrimSpace(cfg.Registry.Address)
	if contractAddr == "" {
		return "", fmt.Errorf("registry address not configured")
	}
	if !common.IsHexAddress(contractAddr) {
		return "", fmt.Errorf("invalid registry address: %s", contractAddr)
	}
	contractAddress := common.HexToAddress(contractAddr)

	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	publicKey := privKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("failed to cast public key to ECDSA")
	}
	signerAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	intent := types.OracleIntent{
		IntentType: intentType,
		Version:    intentVersion,
		ChainId:    chainID,
		Nonce:      nonce,
		Expiry:     expiry,
		Symbol:     symbol,
		Price:      price,
		Timestamp:  nowBig,
		Source:     "DIA Oracle",
	}

	domain := apitypes.TypedDataDomain{
		Name:              "DIA Oracle",
		Version:           intentVersion,
		ChainId:           (*gethmath.HexOrDecimal256)(chainID),
		VerifyingContract: contractAddress.Hex(),
		Salt:              "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

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

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return "", fmt.Errorf("failed to hash domain separator: %v", err)
	}

	typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return "", fmt.Errorf("failed to hash typed data: %v", err)
	}

	dataToSign := append([]byte{0x19, 0x01}, domainSeparator[:]...)
	dataToSign = append(dataToSign, typedDataHash[:]...)
	hash := crypto.Keccak256Hash(dataToSign)

	signature, err := crypto.Sign(hash.Bytes(), privKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign message: %v", err)
	}

	if signature[64] == 0 || signature[64] == 1 {
		signature[64] += 27
	}

	signatureHex := "0x" + hex.EncodeToString(signature)

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

	signedIntentJSON, err := json.Marshal(signedIntent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal signed intent: %v", err)
	}

	logger.WithFields(map[string]interface{}{
		"symbol": symbol,
		"price":  price.String(),
	}).Debug("Created intent")

	return string(signedIntentJSON), nil
}

// SymbolData represents price data for a single symbol
type SymbolData struct {
	Symbol    string
	Price     *big.Int
	Volume    *big.Int
	Timestamp *big.Int // Oracle timestamp; nil means use time.Now() at signing
}

func AttestMultipleValues(ctx context.Context, multiClient *multirpc.MultiClient, privateKey string, fromAddress string, symbolsData []SymbolData) (string, error) {
	if privateKey == "" {
		return "", fmt.Errorf("private key not provided")
	}
	if multiClient == nil {
		return "", fmt.Errorf("multiClient is required")
	}

	if len(symbolsData) == 0 {
		return "", fmt.Errorf("no symbols provided")
	}

	now := time.Now().Unix()
	nowBig := big.NewInt(now)

	expiry := big.NewInt(now + 3600)

	cfg := config.Get()
	intentType := cfg.Attestor.IntentType
	if intentType == "" {
		intentType = "OracleUpdate"
	}
	intentVersion := cfg.Attestor.IntentVersion
	if intentVersion == "" {
		intentVersion = "1.0"
	}

	chainID, err := multiClient.ChainID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	contractAddr := strings.TrimSpace(cfg.Registry.Address)
	if contractAddr == "" {
		return "", fmt.Errorf("registry address not configured")
	}
	if !common.IsHexAddress(contractAddr) {
		return "", fmt.Errorf("invalid registry address: %s", contractAddr)
	}
	contractAddress := common.HexToAddress(contractAddr)

	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	publicKey := privKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("failed to cast public key to ECDSA")
	}
	signerAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	signedIntents := make([]types.SignedIntent, len(symbolsData))

	for i, data := range symbolsData {
		// Generate unique nonce for each intent in the batch to prevent collisions
		nonce := generateNonce()

		// Use per-symbol oracle timestamp if provided, otherwise fall back to current time
		ts := nowBig
		if data.Timestamp != nil && data.Timestamp.Sign() > 0 {
			ts = data.Timestamp
		}

		intent := types.OracleIntent{
			IntentType: intentType,
			Version:    intentVersion,
			ChainId:    chainID,
			Nonce:      nonce,
			Expiry:     expiry,
			Symbol:     data.Symbol,
			Price:      data.Price,
			Timestamp:  ts,
			Source:     "DIA Oracle",
		}

		domain := apitypes.TypedDataDomain{
			Name:              "DIA Oracle",
			Version:           intentVersion,
			ChainId:           (*gethmath.HexOrDecimal256)(chainID),
			VerifyingContract: contractAddress.Hex(),
			Salt:              "0x0000000000000000000000000000000000000000000000000000000000000000",
		}

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

		domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
		if err != nil {
			return "", fmt.Errorf("failed to hash domain separator: %v", err)
		}

		typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
		if err != nil {
			return "", fmt.Errorf("failed to hash typed data: %v", err)
		}

		dataToSign := append([]byte{0x19, 0x01}, domainSeparator[:]...)
		dataToSign = append(dataToSign, typedDataHash[:]...)
		hash := crypto.Keccak256Hash(dataToSign)

		signature, err := crypto.Sign(hash.Bytes(), privKey)
		if err != nil {
			return "", fmt.Errorf("failed to sign message: %v", err)
		}

		if signature[64] == 0 || signature[64] == 1 {
			signature[64] += 27
		}

		signatureHex := "0x" + hex.EncodeToString(signature)

		signedIntent := types.SignedIntent{}
		signedIntent.Intent.IntentType = intent.IntentType
		signedIntent.Intent.Version = intent.Version
		signedIntent.Intent.ChainId = intent.ChainId
		signedIntent.Intent.Nonce = intent.Nonce
		signedIntent.Intent.Expiry = intent.Expiry
		signedIntent.Intent.Symbol = intent.Symbol
		signedIntent.Intent.Price = intent.Price
		signedIntent.Intent.Timestamp = intent.Timestamp
		signedIntent.Intent.Source = intent.Source
		signedIntent.Signature = signatureHex
		signedIntent.Signer = signerAddress.Hex()

		signedIntents[i] = signedIntent

		logger.WithFields(map[string]interface{}{
			"symbol": data.Symbol,
			"price":  data.Price.String(),
		}).Debug("Created intent")
	}

	type BatchSignedIntent struct {
		Intents []types.SignedIntent `json:"intents"`
	}

	batchIntent := BatchSignedIntent{
		Intents: signedIntents,
	}

	batchIntentJSON, err := json.Marshal(batchIntent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal batch intent: %v", err)
	}

	return string(batchIntentJSON), nil
}

func PublishMultipleIntents(ctx context.Context, registryClient *multirpc.MultiClient, privateKey string, batchIntentJSON string) (string, error) {
	startTime := time.Now()

	if registryClient == nil {
		return "", fmt.Errorf("registryClient is required")
	}

	cfg := config.Get()
	registryContract := cfg.Registry.Address

	if registryContract == "" {
		return "", fmt.Errorf("registry address not configured")
	}

	// Parse the batch intent
	var batchIntent struct {
		Intents []types.SignedIntent `json:"intents"`
	}

	err := json.Unmarshal([]byte(batchIntentJSON), &batchIntent)
	if err != nil {
		return "", fmt.Errorf("failed to parse batch intent: %v", err)
	}

	intentCount := len(batchIntent.Intents)
	if intentCount == 0 {
		return "", fmt.Errorf("no intents found in batch")
	}

	logger.WithField("intent_count", intentCount).Info("Processing batch transaction")

	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	chainID, err := registryClient.ChainID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	gasPrice, err := registryClient.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get gas price: %v", err)
	}

	const batchRegistryABI = `[{"inputs":[{"components":[{"internalType":"string","name":"intentType","type":"string"},{"internalType":"string","name":"version","type":"string"},{"internalType":"uint256","name":"chainId","type":"uint256"},{"internalType":"uint256","name":"nonce","type":"uint256"},{"internalType":"uint256","name":"expiry","type":"uint256"},{"internalType":"string","name":"symbol","type":"string"},{"internalType":"uint256","name":"price","type":"uint256"},{"internalType":"uint256","name":"timestamp","type":"uint256"},{"internalType":"string","name":"source","type":"string"},{"internalType":"bytes","name":"signature","type":"bytes"},{"internalType":"address","name":"signer","type":"address"}],"internalType":"struct OracleIntentRegistryEIP712.IntentData[]","name":"intents","type":"tuple[]"}],"name":"registerMultipleIntents","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

	parsedABI, err := abi.JSON(strings.NewReader(batchRegistryABI))
	if err != nil {
		return "", fmt.Errorf("failed to parse ABI: %v", err)
	}

	// Prepare the intents for the batch transaction
	intentData := make([]struct {
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
	}, len(batchIntent.Intents))

	for i, intent := range batchIntent.Intents {
		signatureStr := intent.Signature
		if strings.HasPrefix(signatureStr, "0x") {
			signatureStr = signatureStr[2:]
		}
		signatureBytes, err := hex.DecodeString(signatureStr)
		if err != nil {
			return "", fmt.Errorf("failed to decode signature for %s: %v", intent.Intent.Symbol, err)
		}

		intentData[i] = struct {
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
		}{
			IntentType: intent.Intent.IntentType,
			Version:    intent.Intent.Version,
			ChainId:    intent.Intent.ChainId,
			Nonce:      intent.Intent.Nonce,
			Expiry:     intent.Intent.Expiry,
			Symbol:     intent.Intent.Symbol,
			Price:      intent.Intent.Price,
			Timestamp:  intent.Intent.Timestamp,
			Source:     intent.Intent.Source,
			Signature:  signatureBytes,
			Signer:     common.HexToAddress(intent.Signer),
		}
	}

	data, err := parsedABI.Pack("registerMultipleIntents", intentData)
	if err != nil {
		return "", fmt.Errorf("failed to pack input data: %v", err)
	}

	gasLimit := uint64(3000000 + (intentCount * 200000))
	fromAddress := crypto.PubkeyToAddress(privKey.PublicKey)
	currentGasPrice := new(big.Int).Set(gasPrice)

	var lastErr error
	const maxNonceAttempts = 5
	for attempt := 0; attempt < maxNonceAttempts; attempt++ {
		nonce, err := registryClient.PendingNonceAt(ctx, fromAddress)
		if err != nil {
			return "", fmt.Errorf("failed to get nonce: %v", err)
		}

		tx := ethTypes.NewTransaction(
			nonce,
			common.HexToAddress(registryContract),
			big.NewInt(0),
			gasLimit,
			currentGasPrice,
			data,
		)

		signedTx, err := ethTypes.SignTx(tx, ethTypes.NewEIP155Signer(chainID), privKey)
		if err != nil {
			return "", fmt.Errorf("failed to sign transaction: %v", err)
		}

		if err := registryClient.SendTransaction(ctx, signedTx); err != nil {
			retry, bumpGas, known := classifyTxError(err)
			if known {
				logger.WithField("info", "transaction already known").Debug("Batch transaction reported as already known")
				logger.WithField("duration", time.Since(startTime).String()).Info("Batch transaction completed")
				return signedTx.Hash().Hex(), nil
			}

			if retry {
				lastErr = err
				if bumpGas {
					currentGasPrice = bumpGasPrice(currentGasPrice)
				}
				time.Sleep(200 * time.Millisecond)
				continue
			}

			return "", fmt.Errorf("failed to send transaction: %v", err)
		}

		logger.WithField("duration", time.Since(startTime).String()).Info("Batch transaction completed")
		return signedTx.Hash().Hex(), nil
	}

	return "", fmt.Errorf("failed to send transaction after %d attempts: %v", maxNonceAttempts, lastErr)
}

func PublishIntent(ctx context.Context, registryClient *multirpc.MultiClient, privateKey string, signedIntentJSON string) (string, error) {
	if registryClient == nil {
		return "", fmt.Errorf("registryClient is required")
	}

	cfg := config.Get()
	registryContract := cfg.Registry.Address

	if registryContract == "" {
		return "", fmt.Errorf("registry address not configured")
	}

	// Parse the signed intent
	var signedIntent types.SignedIntent
	err := json.Unmarshal([]byte(signedIntentJSON), &signedIntent)
	if err != nil {
		return "", fmt.Errorf("failed to parse signed intent: %v", err)
	}

	const registryABI = `[{"inputs":[{"internalType":"string","name":"intentType","type":"string"},{"internalType":"string","name":"version","type":"string"},{"internalType":"uint256","name":"chainId","type":"uint256"},{"internalType":"uint256","name":"nonce","type":"uint256"},{"internalType":"uint256","name":"expiry","type":"uint256"},{"internalType":"string","name":"symbol","type":"string"},{"internalType":"uint256","name":"price","type":"uint256"},{"internalType":"uint256","name":"timestamp","type":"uint256"},{"internalType":"string","name":"source","type":"string"},{"internalType":"bytes","name":"signature","type":"bytes"},{"internalType":"address","name":"signer","type":"address"}],"name":"registerIntent","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

	parsedABI, err := abi.JSON(strings.NewReader(registryABI))
	if err != nil {
		return "", fmt.Errorf("failed to parse ABI: %v", err)
	}

	signatureStr := signedIntent.Signature
	if strings.HasPrefix(signatureStr, "0x") {
		signatureStr = signatureStr[2:]
	}
	signatureBytes, err := hex.DecodeString(signatureStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode signature: %v", err)
	}

	signerAddr := common.HexToAddress(signedIntent.Signer)

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

	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	chainID, err := registryClient.ChainID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	fromAddress := crypto.PubkeyToAddress(privKey.PublicKey)
	gasPrice, err := registryClient.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get gas price: %v", err)
	}

	currentGasPrice := new(big.Int).Set(gasPrice)

	var lastErr error
	const maxNonceAttempts = 5
	for attempt := 0; attempt < maxNonceAttempts; attempt++ {
		nonce, err := registryClient.PendingNonceAt(ctx, fromAddress)
		if err != nil {
			return "", fmt.Errorf("failed to get nonce: %v", err)
		}

		tx := ethTypes.NewTransaction(
			nonce,
			common.HexToAddress(registryContract),
			big.NewInt(0),
			3000000,
			currentGasPrice,
			data,
		)

		signedTx, err := ethTypes.SignTx(tx, ethTypes.NewEIP155Signer(chainID), privKey)
		if err != nil {
			return "", fmt.Errorf("failed to sign transaction: %v", err)
		}

		if err := registryClient.SendTransaction(ctx, signedTx); err != nil {
			retry, bumpGas, known := classifyTxError(err)
			if known {
				logger.WithField("info", "transaction already known").Debug("Transaction reported as already known")
				return signedTx.Hash().Hex(), nil
			}

			if retry {
				lastErr = err
				if bumpGas {
					currentGasPrice = bumpGasPrice(currentGasPrice)
				}
				time.Sleep(200 * time.Millisecond)
				continue
			}

			return "", fmt.Errorf("failed to send transaction: %v", err)
		}

		return signedTx.Hash().Hex(), nil
	}

	return "", fmt.Errorf("failed to send transaction after %d attempts: %v", maxNonceAttempts, lastErr)
}

func classifyTxError(err error) (retry bool, bumpGas bool, alreadyKnown bool) {
	if err == nil {
		return false, false, false
	}

	msg := strings.ToLower(err.Error())

	if strings.Contains(msg, "already known") {
		return false, false, true
	}

	if strings.Contains(msg, "replacement transaction underpriced") || strings.Contains(msg, "transaction underpriced") {
		return true, true, false
	}

	if strings.Contains(msg, "nonce too low") {
		return true, false, false
	}

	return false, false, false
}

func bumpGasPrice(current *big.Int) *big.Int {
	bumped := new(big.Int).Mul(current, big.NewInt(110))
	bumped.Div(bumped, big.NewInt(100))

	if bumped.Cmp(current) <= 0 {
		bumped = new(big.Int).Add(current, big.NewInt(1_000_000_000))
	}

	return bumped
}
