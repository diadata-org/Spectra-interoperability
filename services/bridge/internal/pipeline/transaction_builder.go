package pipeline

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// TransactionBuilder builds transactions from configuration and data
type TransactionBuilder struct {
	destinationConfigs map[int64]*config.DestinationConfig
	clients            map[int64]*ethclient.Client
	abiCache           map[string]abi.Method
	nonceManager       *NonceManager
}

// NewTransactionBuilder creates a new transaction builder
func NewTransactionBuilder(clients map[int64]*ethclient.Client) (*TransactionBuilder, error) {
	return &TransactionBuilder{
		clients:      clients,
		abiCache:     make(map[string]abi.Method),
		nonceManager: NewNonceManager(clients),
	}, nil
}

// BuildTransaction builds a transaction for a specific destination and method
func (tb *TransactionBuilder) BuildTransaction(
	data *config.ExtractedData,
	dest config.RouterDestination,
	routerPrivateKey *ecdsa.PrivateKey,
) (*Transaction, error) {
	_, exists := tb.destinationConfigs[dest.ChainID]
	if !exists {
		return nil, fmt.Errorf("destination config not found for chain %d", dest.ChainID)
	}

	contractAddr := common.HexToAddress(dest.Contract)

	method, err := tb.getMethodABI(dest.Method.Name, dest.Method.ABI)
	if err != nil {
		return nil, fmt.Errorf("failed to get method ABI: %w", err)
	}

	params, err := tb.buildMethodParams(data, dest.Method.Params, method)
	if err != nil {
		return nil, fmt.Errorf("failed to build method parameters: %w", err)
	}

	callData, err := method.Inputs.Pack(params...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack method call: %w", err)
	}

	fullCallData := append(method.ID, callData...)

	value := big.NewInt(0)
	if dest.Method.Value != "" && dest.Method.Value != "0" {
		value = new(big.Int)
		if _, ok := value.SetString(dest.Method.Value, 10); !ok {
			return nil, fmt.Errorf("invalid value: %s", dest.Method.Value)
		}
	}

	gasLimit := dest.Method.GasLimit
	if gasLimit == 0 {
		gasLimit = 300000
	}

	var signerAddress common.Address
	if routerPrivateKey != nil {
		signerAddress = crypto.PubkeyToAddress(routerPrivateKey.PublicKey)
	}

	transaction := &Transaction{
		ChainID:         dest.ChainID,
		To:              contractAddr,
		Value:           value,
		Data:            fullCallData,
		GasLimit:        gasLimit,
		GasMultiplier:   dest.Method.GasMultiplier,
		PrivateKey:      routerPrivateKey,
		SignerAddress:   signerAddress,
		MethodName:      dest.Method.Name,
		ContractAddress: contractAddr,
	}

	logger.Debugf("Built transaction for %s on chain %d", dest.Method.Name, dest.ChainID)

	return transaction, nil
}

// getMethodABI parses and caches method ABI
func (tb *TransactionBuilder) getMethodABI(methodName, abiStr string) (abi.Method, error) {
	cacheKey := fmt.Sprintf("%s:%s", methodName, abiStr)
	if cached, exists := tb.abiCache[cacheKey]; exists {
		return cached, nil
	}

	contractABI := fmt.Sprintf(`[{"type":"function","name":"%s",%s}]`,
		methodName,
		strings.TrimPrefix(abiStr, "function "+methodName))

	contractABI = strings.ReplaceAll(contractABI, "function ", "")
	contractABI = strings.ReplaceAll(contractABI, methodName+"(", `"inputs":[{`)

	parsedABI, err := abi.JSON(strings.NewReader(abiStr))
	if err != nil {
		return tb.parseMethodSignature(methodName, abiStr)
	}

	method, exists := parsedABI.Methods[methodName]
	if !exists {
		return abi.Method{}, fmt.Errorf("method %s not found in ABI", methodName)
	}

	tb.abiCache[cacheKey] = method

	return method, nil
}

// parseMethodSignature parses a method signature like "function transfer(address to, uint256 amount)"
func (tb *TransactionBuilder) parseMethodSignature(methodName, signature string) (abi.Method, error) {

	start := strings.Index(signature, "(")
	end := strings.LastIndex(signature, ")")
	if start == -1 || end == -1 {
		return abi.Method{}, fmt.Errorf("invalid method signature: %s", signature)
	}

	paramStr := signature[start+1 : end]
	method := abi.Method{
		Name: methodName,
	}

	sigHash := crypto.Keccak256([]byte(methodName + "(" + paramStr + ")"))
	copy(method.ID[:], sigHash[:4])

	if paramStr != "" {
		params := strings.Split(paramStr, ",")
		for i, param := range params {
			param = strings.TrimSpace(param)
			parts := strings.Fields(param)
			if len(parts) >= 1 {
				typeStr := parts[0]
				argName := fmt.Sprintf("arg%d", i)
				if len(parts) >= 2 {
					argName = parts[len(parts)-1]
				}

				argType, err := abi.NewType(typeStr, "", nil)
				if err != nil {
					return abi.Method{}, fmt.Errorf("invalid type %s: %w", typeStr, err)
				}

				method.Inputs = append(method.Inputs, abi.Argument{
					Name: argName,
					Type: argType,
				})
			}
		}
	}

	return method, nil
}

// buildMethodParams builds method parameters from data and mapping
func (tb *TransactionBuilder) buildMethodParams(
	data *config.ExtractedData,
	paramMapping map[string]string,
	method abi.Method,
) ([]interface{}, error) {
	params := make([]interface{}, len(method.Inputs))

	for i, input := range method.Inputs {
		mapping, exists := paramMapping[input.Name]
		if !exists {
			mapping, exists = paramMapping[fmt.Sprintf("arg%d", i)]
			if !exists {
				return nil, fmt.Errorf("no mapping for parameter %s", input.Name)
			}
		}

		value, err := tb.resolveParameterValue(mapping, data)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve parameter %s: %w", input.Name, err)
		}

		converted, err := tb.convertToABIType(value, input.Type)
		if err != nil {
			return nil, fmt.Errorf("failed to convert parameter %s: %w", input.Name, err)
		}

		params[i] = converted
	}

	return params, nil
}

// resolveParameterValue resolves a parameter value from data
func (tb *TransactionBuilder) resolveParameterValue(mapping string, data *config.ExtractedData) (interface{}, error) {
	if strings.HasPrefix(mapping, "${") && strings.HasSuffix(mapping, "}") {
		path := mapping[2 : len(mapping)-1]
		return tb.getValueByPath(path, data)
	}

	return mapping, nil
}

// getValueByPath extracts a value from data using a path
func (tb *TransactionBuilder) getValueByPath(path string, data *config.ExtractedData) (interface{}, error) {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid path: %s", path)
	}

	var source map[string]interface{}
	switch parts[0] {
	case "event":
		source = data.Event
	case "enrichment":
		source = data.Enrichment
	case "processed":
		source = data.Processed
	default:
		return nil, fmt.Errorf("unknown source: %s", parts[0])
	}

	var current interface{} = source
	for i := 1; i < len(parts); i++ {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("cannot navigate through non-map at %s", parts[i])
		}
		current, ok = m[parts[i]]
		if !ok {
			return nil, fmt.Errorf("field not found: %s", parts[i])
		}
	}

	return current, nil
}

// convertToABIType converts a value to the appropriate ABI type
func (tb *TransactionBuilder) convertToABIType(value interface{}, abiType abi.Type) (interface{}, error) {
	switch abiType.T {
	case abi.AddressTy:
		return tb.toAddress(value)
	case abi.UintTy, abi.IntTy:
		return tb.toBigInt(value)
	case abi.BoolTy:
		return tb.toBool(value)
	case abi.StringTy:
		return tb.toString(value)
	case abi.BytesTy, abi.FixedBytesTy:
		return tb.toBytes(value)
	case abi.ArrayTy, abi.SliceTy:
		return tb.toArray(value, abiType)
	case abi.TupleTy:
		return tb.toTuple(value, abiType)
	default:
		return value, nil
	}
}

func (tb *TransactionBuilder) toAddress(value interface{}) (common.Address, error) {
	switch v := value.(type) {
	case common.Address:
		return v, nil
	case string:
		if !common.IsHexAddress(v) {
			return common.Address{}, fmt.Errorf("invalid address: %s", v)
		}
		return common.HexToAddress(v), nil
	default:
		return common.Address{}, fmt.Errorf("cannot convert %T to address", value)
	}
}

func (tb *TransactionBuilder) toBigInt(value interface{}) (*big.Int, error) {
	switch v := value.(type) {
	case *big.Int:
		return v, nil
	case string:
		n := new(big.Int)
		if strings.HasPrefix(v, "0x") {
			n.SetString(v[2:], 16)
		} else {
			n.SetString(v, 10)
		}
		return n, nil
	case float64:
		return big.NewInt(int64(v)), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to big.Int", value)
	}
}

func (tb *TransactionBuilder) toBool(value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return strings.ToLower(v) == "true", nil
	default:
		return false, fmt.Errorf("cannot convert %T to bool", value)
	}
}

func (tb *TransactionBuilder) toString(value interface{}) (string, error) {
	return fmt.Sprintf("%v", value), nil
}

func (tb *TransactionBuilder) toBytes(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		if strings.HasPrefix(v, "0x") {
			return common.FromHex(v), nil
		}
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to bytes", value)
	}
}

func (tb *TransactionBuilder) toArray(value interface{}, abiType abi.Type) (interface{}, error) {
	return value, nil
}

func (tb *TransactionBuilder) toTuple(value interface{}, abiType abi.Type) (interface{}, error) {
	// Handle OracleIntent tuple conversion
	if intent, ok := value.(*bridgetypes.OracleIntent); ok {
		tuple := struct {
			IntentType string         `abi:"intentType"`
			Version    string         `abi:"version"`
			ChainId    *big.Int       `abi:"chainId"`
			Nonce      *big.Int       `abi:"nonce"`
			Expiry     *big.Int       `abi:"expiry"`
			Symbol     string         `abi:"symbol"`
			Price      *big.Int       `abi:"price"`
			Timestamp  *big.Int       `abi:"timestamp"`
			Source     string         `abi:"source"`
			Signature  []byte         `abi:"signature"`
			Signer     common.Address `abi:"signer"`
		}{
			IntentType: intent.IntentType,
			Version:    intent.Version,
			ChainId:    intent.ChainID,
			Nonce:      intent.Nonce,
			Expiry:     intent.Expiry,
			Symbol:     intent.Symbol,
			Price:      intent.Price,
			Timestamp:  intent.Timestamp,
			Source:     intent.Source,
			Signature:  []byte(intent.Signature),
			Signer:     intent.Signer,
		}
		return tuple, nil
	}
	return value, nil
}

// Transaction represents a built transaction
type Transaction struct {
	ChainID         int64
	To              common.Address
	Value           *big.Int
	Data            []byte
	GasLimit        uint64
	GasMultiplier   float64
	PrivateKey      *ecdsa.PrivateKey
	SignerAddress   common.Address
	MethodName      string
	ContractAddress common.Address
}

// TransactionData represents data needed to build and send a transaction
type TransactionData struct {
	RouterID      string
	ChainID       int64
	Contract      common.Address
	Method        config.DestinationMethodConfig
	ExtractedData *config.ExtractedData
	SourceData    interface{}
	Nonce         uint64
}

// BuildAndSendTransaction builds and sends a transaction
func (tb *TransactionBuilder) BuildAndSendTransaction(ctx context.Context, privateKey *ecdsa.PrivateKey, txData *TransactionData) (common.Hash, error) {
	client, exists := tb.clients[txData.ChainID]
	if !exists {
		return common.Hash{}, fmt.Errorf("no client for chain %d", txData.ChainID)
	}

	dest := config.RouterDestination{
		ChainID:  txData.ChainID,
		Contract: txData.Contract.Hex(),
		Method:   txData.Method,
	}

	tx, err := tb.BuildTransaction(txData.ExtractedData, dest, privateKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to build transaction: %w", err)
	}

	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	nonce, err := tb.nonceManager.GetNextNonce(ctx, txData.ChainID, fromAddress)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get nonce: %w", err)
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get gas price: %w", err)
	}

	if tx.GasMultiplier > 0 {
		gasPrice = new(big.Int).Mul(gasPrice, big.NewInt(int64(tx.GasMultiplier*100)))
		gasPrice = new(big.Int).Div(gasPrice, big.NewInt(100))
	}

	ethTx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &tx.To,
		Value:    tx.Value,
		Gas:      tx.GasLimit,
		GasPrice: gasPrice,
		Data:     tx.Data,
	})

	chainID := big.NewInt(tx.ChainID)
	signedTx, err := types.SignTx(ethTx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to sign transaction: %w", err)
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		// Check if this is a nonce-related error and reset if needed
		errStr := err.Error()
		if strings.Contains(errStr, "nonce too low") || strings.Contains(errStr, "nonce too high") {
			logger.Warnf("Nonce error detected (%s), resetting nonce for address %s on chain %d",
				errStr, fromAddress.Hex(), txData.ChainID)
			if resetErr := tb.nonceManager.ResetNonce(ctx, txData.ChainID, fromAddress); resetErr != nil {
				logger.Errorf("Failed to reset nonce: %v", resetErr)
			}
		}
		return common.Hash{}, fmt.Errorf("failed to send transaction: %w", err)
	}

	logger.Infof("Sent transaction %s to chain %d, contract %s, method %s",
		signedTx.Hash().Hex(), tx.ChainID, tx.To.Hex(), tx.MethodName)

	return signedTx.Hash(), nil
}
