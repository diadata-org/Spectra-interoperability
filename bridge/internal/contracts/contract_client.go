package contracts

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// ContractClient interface for interacting with destination contracts
type ContractClient interface {
	UpdateOracle(ctx context.Context, request *bridgeTypes.UpdateRequest) *bridgeTypes.UpdateResult
	GetContractInfo() *ContractInfo
	EstimateGas(ctx context.Context, request *bridgeTypes.UpdateRequest) (uint64, error)
	GetNonce(ctx context.Context) (uint64, error)
	Close() error
}

// ContractInfo holds basic contract information
type ContractInfo struct {
	ChainID         int64
	ChainName       string
	ContractAddress common.Address
	ContractName    string
	ContractType    string
}

// BaseContractClient implements common functionality
type BaseContractClient struct {
	chainID         int64
	chainName       string
	client          *ethclient.Client
	contractAddress common.Address
	contractABI     abi.ABI
	contractConfig  *config.ContractConfig
	privateKey      *ecdsa.PrivateKey
	fromAddress     common.Address
	
	// Dynamic method mappings
	methods         map[string]*MethodConfig
	
	// Transaction options
	gasLimit        uint64
	gasMultiplier   float64
	maxGasPrice     *big.Int
	nonce           uint64
	nonceManager    *NonceManager
}

// MethodConfig holds configuration for a contract method
type MethodConfig struct {
	MethodName     string
	FieldsMapping  map[string]string
	GasLimit       uint64
	RequiredFields []string
}

// NewContractClient creates a new contract client
func NewContractClient(
	chainConfig *config.DestinationConfig,
	contractConfig *config.ContractConfig,
	client *ethclient.Client,
	privateKey string,
) (ContractClient, error) {
	// Parse private key
	key, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	// Get from address
	publicKey := key.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error casting public key to ECDSA")
	}
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	// Parse contract ABI
	contractABI, err := abi.JSON(strings.NewReader(contractConfig.ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse contract ABI: %w", err)
	}

	// Create base client
	baseClient := &BaseContractClient{
		chainID:         chainConfig.ChainID,
		chainName:       chainConfig.Name,
		client:          client,
		contractAddress: common.HexToAddress(contractConfig.Address),
		contractABI:     contractABI,
		contractConfig:  contractConfig,
		privateKey:      key,
		fromAddress:     fromAddress,
		methods:         make(map[string]*MethodConfig),
		gasLimit:        contractConfig.GasLimit,
		gasMultiplier:   contractConfig.GasMultiplier,
		nonceManager:    NewNonceManager(client, fromAddress),
	}

	// Parse max gas price
	if contractConfig.MaxGasPrice != "" {
		maxGasPrice, ok := new(big.Int).SetString(contractConfig.MaxGasPrice, 10)
		if !ok {
			return nil, fmt.Errorf("invalid max gas price: %s", contractConfig.MaxGasPrice)
		}
		baseClient.maxGasPrice = maxGasPrice
	}

	// Configure methods
	if err := baseClient.configureMethods(); err != nil {
		return nil, fmt.Errorf("failed to configure methods: %w", err)
	}

	// Create specific client based on contract type
	switch strings.ToLower(contractConfig.Type) {
	case "pushoracle":
		return NewPushOracleClient(baseClient), nil
	case "eip712oracle":
		return NewEIP712OracleClient(baseClient), nil
	case "generic":
		return NewGenericClient(baseClient), nil
	default:
		// Default to generic client
		return NewGenericClient(baseClient), nil
	}
}

// configureMethods sets up dynamic method configurations
func (c *BaseContractClient) configureMethods() error {
	for methodType, methodConfig := range c.contractConfig.Methods {
		// Validate method exists in ABI
		method, exists := c.contractABI.Methods[methodConfig.MethodName]
		if !exists {
			return fmt.Errorf("method %s not found in contract ABI", methodConfig.MethodName)
		}

		// Extract required fields from method inputs
		requiredFields := make([]string, 0)
		for _, input := range method.Inputs {
			requiredFields = append(requiredFields, input.Name)
		}

		c.methods[methodType] = &MethodConfig{
			MethodName:     methodConfig.MethodName,
			FieldsMapping:  methodConfig.FieldsMapping,
			GasLimit:       methodConfig.GasLimit,
			RequiredFields: requiredFields,
		}
	}

	return nil
}

// UpdateOracle implements the main update functionality
func (c *BaseContractClient) UpdateOracle(ctx context.Context, request *bridgeTypes.UpdateRequest) *bridgeTypes.UpdateResult {
	startTime := time.Now()
	result := &bridgeTypes.UpdateResult{
		ChainID:         c.chainID,
		ContractAddress: c.contractAddress,
	}

	// Determine which method to use
	methodConfig, err := c.selectMethod(request)
	if err != nil {
		result.Error = fmt.Errorf("failed to select method: %w", err)
		return result
	}

	// Build transaction data
	data, err := c.buildTransactionData(methodConfig, request)
	if err != nil {
		result.Error = fmt.Errorf("failed to build transaction data: %w", err)
		return result
	}

	// Get nonce
	nonce, err := c.nonceManager.GetNextNonce(ctx)
	if err != nil {
		result.Error = fmt.Errorf("failed to get nonce: %w", err)
		return result
	}

	// Get gas price
	gasPrice, err := c.getGasPrice(ctx)
	if err != nil {
		result.Error = fmt.Errorf("failed to get gas price: %w", err)
		return result
	}
	result.GasPrice = gasPrice

	// Estimate gas if not specified
	gasLimit := methodConfig.GasLimit
	if gasLimit == 0 {
		gasLimit = c.gasLimit
	}
	if gasLimit == 0 {
		estimatedGas, err := c.estimateGasForData(ctx, data)
		if err != nil {
			result.Error = fmt.Errorf("failed to estimate gas: %w", err)
			return result
		}
		gasLimit = uint64(float64(estimatedGas) * c.gasMultiplier)
	}

	// Check balance before creating transaction
	balance, requiredBalance, err := c.checkBalance(ctx, gasLimit, gasPrice)
	if err != nil {
		c.nonceManager.ReturnNonce(nonce)
		result.Error = fmt.Errorf("failed to check balance: %w", err)
		return result
	}

	// Check if balance is sufficient
	if balance.Cmp(requiredBalance) < 0 {
		c.nonceManager.ReturnNonce(nonce)
		result.Error = fmt.Errorf("insufficient balance: have %s, need %s (gas: %d, gasPrice: %s)", 
			balance.String(), requiredBalance.String(), gasLimit, gasPrice.String())
		logger.Errorf("Insufficient balance for address %s on chain %s: have %s, need %s",
			c.fromAddress.Hex(), c.chainName, balance.String(), requiredBalance.String())
		return result
	}

	// Create transaction
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &c.contractAddress,
		Value:    big.NewInt(0),
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     data,
	})

	// Sign transaction
	chainID := big.NewInt(c.chainID)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), c.privateKey)
	if err != nil {
		c.nonceManager.ReturnNonce(nonce)
		result.Error = fmt.Errorf("failed to sign transaction: %w", err)
		return result
	}

	// Send transaction
	err = c.client.SendTransaction(ctx, signedTx)
	if err != nil {
		c.nonceManager.ReturnNonce(nonce)
		result.Error = fmt.Errorf("failed to send transaction: %w", err)
		return result
	}

	result.TxHash = signedTx.Hash().Hex()
	logger.Infof("Transaction sent: %s on %s", result.TxHash, c.chainName)

	// Wait for confirmation
	receipt, err := c.waitForConfirmation(ctx, signedTx.Hash())
	if err != nil {
		result.Error = fmt.Errorf("failed to get receipt: %w", err)
		return result
	}

	if receipt.Status == 0 {
		result.Error = fmt.Errorf("transaction failed")
		return result
	}

	result.GasUsed = receipt.GasUsed
	result.BlockNumber = receipt.BlockNumber.Uint64()
	result.Duration = time.Since(startTime)

	logger.Infof("Transaction confirmed: %s, gas used: %d", result.TxHash, result.GasUsed)
	return result
}

// selectMethod determines which method to use based on the request
func (c *BaseContractClient) selectMethod(request *bridgeTypes.UpdateRequest) (*MethodConfig, error) {
	// For now, use single_update method
	// In future, could select based on batch size, urgency, etc.
	methodConfig, exists := c.methods["single_update"]
	if !exists {
		return nil, fmt.Errorf("no single_update method configured")
	}
	return methodConfig, nil
}

// buildTransactionData builds the transaction data for the method call
func (c *BaseContractClient) buildTransactionData(methodConfig *MethodConfig, request *bridgeTypes.UpdateRequest) ([]byte, error) {
	method, exists := c.contractABI.Methods[methodConfig.MethodName]
	if !exists {
		return nil, fmt.Errorf("method %s not found in ABI", methodConfig.MethodName)
	}

	// Map intent fields to method parameters
	args := make([]interface{}, len(method.Inputs))
	
	for i, input := range method.Inputs {
		// Check field mapping
		mappedField, _ := methodConfig.FieldsMapping[input.Name]
		
		// Get value based on mapping or default field name
		value, err := c.getFieldValue(request, mappedField, input.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get value for %s: %w", input.Name, err)
		}
		
		// Convert value to appropriate type
		convertedValue, err := c.convertValue(value, input.Type)
		if err != nil {
			return nil, fmt.Errorf("failed to convert value for %s: %w", input.Name, err)
		}
		
		args[i] = convertedValue
	}

	// Pack the method call
	data, err := c.contractABI.Pack(methodConfig.MethodName, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack method call: %w", err)
	}

	return data, nil
}

// getFieldValue extracts a field value from the update request
func (c *BaseContractClient) getFieldValue(request *bridgeTypes.UpdateRequest, mappedField, defaultField string) (interface{}, error) {
	// Check mapped field first
	fieldName := defaultField
	if mappedField != "" {
		fieldName = mappedField
	}

	// Extract value based on field name
	switch strings.ToLower(fieldName) {
	case "symbol":
		return request.Intent.Symbol, nil
	case "price":
		return request.Intent.Price, nil
	case "timestamp":
		return request.Intent.Timestamp, nil
	case "signer":
		return request.Intent.Signer, nil
	case "signature":
		return request.Intent.Signature, nil
	case "expiry":
		return request.Intent.Expiry, nil
	case "intenthash":
		return request.IntentHash, nil
	// IntArraySet event fields
	case "requestid":
		if request.Event != nil && request.Event.RequestId != nil {
			return request.Event.RequestId, nil
		}
		return nil, fmt.Errorf("requestId not available in event data")
	case "randomints":
		if request.Event != nil && request.Event.RandomInts != nil {
			return request.Event.RandomInts, nil
		}
		return nil, fmt.Errorf("randomInts not available in event data")
	case "round":
		if request.Event != nil && request.Event.Round != nil {
			return request.Event.Round, nil
		}
		return nil, fmt.Errorf("round not available in event data")
	case "seed":
		if request.Event != nil {
			return request.Event.Seed, nil
		}
		return nil, fmt.Errorf("seed not available in event data")
	default:
		return nil, fmt.Errorf("field %s not found", fieldName)
	}
}

// convertValue converts a value to the appropriate type for the contract
func (c *BaseContractClient) convertValue(value interface{}, targetType abi.Type) (interface{}, error) {
	switch targetType.T {
	case abi.StringTy:
		return fmt.Sprintf("%v", value), nil
	case abi.AddressTy:
		switch v := value.(type) {
		case common.Address:
			return v, nil
		case string:
			return common.HexToAddress(v), nil
		default:
			return nil, fmt.Errorf("cannot convert %T to address", value)
		}
	case abi.UintTy, abi.IntTy:
		switch v := value.(type) {
		case *big.Int:
			return v, nil
		case string:
			n, ok := new(big.Int).SetString(v, 10)
			if !ok {
				return nil, fmt.Errorf("cannot convert %s to big.Int", v)
			}
			return n, nil
		case int64:
			return big.NewInt(v), nil
		case uint64:
			return new(big.Int).SetUint64(v), nil
		default:
			return nil, fmt.Errorf("cannot convert %T to big.Int", value)
		}
	case abi.BytesTy, abi.FixedBytesTy:
		switch v := value.(type) {
		case []byte:
			return v, nil
		case [32]byte:
			return v[:], nil
		case string:
			return common.FromHex(v), nil
		default:
			return nil, fmt.Errorf("cannot convert %T to bytes", value)
		}
	case abi.SliceTy:
		// Handle arrays like int256[]
		switch v := value.(type) {
		case []*big.Int:
			return v, nil
		case []interface{}:
			// Convert slice of interfaces to slice of big.Int
			result := make([]*big.Int, len(v))
			for i, item := range v {
				if bigInt, ok := item.(*big.Int); ok {
					result[i] = bigInt
				} else if str, ok := item.(string); ok {
					n, ok := new(big.Int).SetString(str, 10)
					if !ok {
						return nil, fmt.Errorf("cannot convert %s to big.Int", str)
					}
					result[i] = n
				} else {
					return nil, fmt.Errorf("cannot convert array element %T to big.Int", item)
				}
			}
			return result, nil
		default:
			return nil, fmt.Errorf("cannot convert %T to slice", value)
		}
	default:
		return value, nil
	}
}

// estimateGasForData estimates gas for transaction data
func (c *BaseContractClient) estimateGasForData(ctx context.Context, data []byte) (uint64, error) {
	msg := ethereum.CallMsg{
		From:  c.fromAddress,
		To:    &c.contractAddress,
		Data:  data,
		Value: big.NewInt(0),
	}

	gas, err := c.client.EstimateGas(ctx, msg)
	if err != nil {
		return 0, fmt.Errorf("gas estimation failed: %w", err)
	}

	return gas, nil
}

// getGasPrice gets the current gas price with limits
func (c *BaseContractClient) getGasPrice(ctx context.Context) (*big.Int, error) {
	gasPrice, err := c.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, err
	}

	// Apply max gas price limit
	if c.maxGasPrice != nil && gasPrice.Cmp(c.maxGasPrice) > 0 {
		logger.Warnf("Gas price %s exceeds max %s, using max", gasPrice, c.maxGasPrice)
		gasPrice = new(big.Int).Set(c.maxGasPrice)
	}

	return gasPrice, nil
}

// waitForConfirmation waits for transaction confirmation
func (c *BaseContractClient) waitForConfirmation(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	// Create a context with timeout
	confirmCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-confirmCtx.Done():
			return nil, fmt.Errorf("confirmation timeout")
		case <-ticker.C:
			receipt, err := c.client.TransactionReceipt(confirmCtx, txHash)
			if err == nil {
				return receipt, nil
			}
			// Continue waiting if transaction not found
		}
	}
}

// GetContractInfo returns contract information
func (c *BaseContractClient) GetContractInfo() *ContractInfo {
	return &ContractInfo{
		ChainID:         c.chainID,
		ChainName:       c.chainName,
		ContractAddress: c.contractAddress,
		ContractName:    c.contractConfig.Name,
		ContractType:    c.contractConfig.Type,
	}
}

// EstimateGas estimates gas for an update request
func (c *BaseContractClient) EstimateGas(ctx context.Context, request *bridgeTypes.UpdateRequest) (uint64, error) {
	methodConfig, err := c.selectMethod(request)
	if err != nil {
		return 0, err
	}

	data, err := c.buildTransactionData(methodConfig, request)
	if err != nil {
		return 0, err
	}

	return c.estimateGasForData(ctx, data)
}

// GetNonce returns the current nonce
func (c *BaseContractClient) GetNonce(ctx context.Context) (uint64, error) {
	return c.client.PendingNonceAt(ctx, c.fromAddress)
}

// Close closes the client
func (c *BaseContractClient) Close() error {
	// Any cleanup needed
	return nil
}

// checkBalance checks if the account has sufficient balance for the transaction
func (c *BaseContractClient) checkBalance(ctx context.Context, gasLimit uint64, gasPrice *big.Int) (*big.Int, *big.Int, error) {
	// Get current balance
	balance, err := c.client.BalanceAt(ctx, c.fromAddress, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get balance: %w", err)
	}

	// Calculate required balance (gas * gasPrice)
	requiredBalance := new(big.Int).Mul(new(big.Int).SetUint64(gasLimit), gasPrice)

	return balance, requiredBalance, nil
}