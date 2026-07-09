package client

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	multirpc "github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/errors"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// DIAOracleV2 ABI JSON - based on DIAOracleV2.sol contract
const diaOracleV2ABIJSON = `[
  {
    "inputs": [
      {
        "internalType": "string",
        "name": "key",
        "type": "string"
      }
    ],
    "name": "getValue",
    "outputs": [
      {
        "internalType": "uint128",
        "name": "",
        "type": "uint128"
      },
      {
        "internalType": "uint128",
        "name": "",
        "type": "uint128"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "getThreshold",
    "outputs": [
      {
        "internalType": "uint256",
        "name": "",
        "type": "uint256"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "getWindowSize",
    "outputs": [
      {
        "internalType": "uint256",
        "name": "",
        "type": "uint256"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "priceMethodology",
    "outputs": [
      {
        "internalType": "address",
        "name": "",
        "type": "address"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  }
]`

// DIAOracleV2Client wraps access to the DIAOracleV2 on-chain oracle with RPC failover.
type DIAOracleV2Client struct {
	primaryRPC  string
	multiClient *multirpc.MultiClient
	oracleAddr  common.Address
	signedAddr  string
	privateKey  string
	fromAddress common.Address
	oracleABI   abi.ABI
}

// NewDIAOracleV2Client creates a new DIAOracleV2 client backed by the multi-RPC failover helper.
func NewDIAOracleV2Client(rpcURLs []string, oracleAddrStr, signedAddrStr, privateKeyStr string) (*DIAOracleV2Client, error) {
	if len(rpcURLs) == 0 {
		return nil, fmt.Errorf("no RPC URLs provided for oracle client")
	}

	multi, err := multirpc.NewMultiClient(rpcURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to DIAOracleV2 RPC endpoints: %w", err)
	}

	oracleAddr := common.HexToAddress(oracleAddrStr)
	oracleABI, err := abi.JSON(strings.NewReader(diaOracleV2ABIJSON))
	if err != nil {
		multi.Close()
		return nil, fmt.Errorf("failed to parse DIAOracleV2 ABI: %w", err)
	}

	var fromAddress common.Address
	if privateKeyStr != "" {
		cleanPrivKey := strings.TrimPrefix(privateKeyStr, "0x")
		privateKey, err := crypto.HexToECDSA(cleanPrivKey)
		if err != nil {
			multi.Close()
			return nil, fmt.Errorf("failed to parse private key: %v", err)
		}

		publicKey := privateKey.Public()
		publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
		if !ok {
			multi.Close()
			return nil, fmt.Errorf("failed to cast public key to ECDSA")
		}

		fromAddress = crypto.PubkeyToAddress(*publicKeyECDSA)
		logger.WithField("address", fromAddress.Hex()).Debug("Derived DIAOracleV2 client address from private key")
	} else {
		fromAddress = common.Address{}
	}

	return &DIAOracleV2Client{
		primaryRPC:  rpcURLs[0],
		multiClient: multi,
		oracleAddr:  oracleAddr,
		signedAddr:  signedAddrStr,
		privateKey:  privateKeyStr,
		fromAddress: fromAddress,
		oracleABI:   oracleABI,
	}, nil
}

// Close releases the underlying RPC connections.
func (oc *DIAOracleV2Client) Close() {
	if oc.multiClient != nil {
		oc.multiClient.Close()
	}
}

// GetValue fetches the latest oracle value from DIAOracleV2 contract.
func (oc *DIAOracleV2Client) GetValue(ctx context.Context, symbol string) (*big.Int, *big.Int, error) {
	logger.WithFields(map[string]interface{}{
		"symbol":         symbol,
		"oracle_address": oc.oracleAddr.Hex(),
		"function":       "getValue",
	}).Debug("Calling DIAOracleV2 contract function: getValue")

	price, timestamp, err := oc.fetchOracleValue(ctx, symbol)
	if err != nil {
		return nil, nil, errors.NewOracleError(symbol, "failed to get value (getValue)", err)
	}

	if price == nil || price.Sign() <= 0 {
		return nil, nil, errors.NewOracleError(symbol, "invalid price", nil)
	}

	if timestamp == nil || timestamp.Sign() <= 0 {
		return nil, nil, errors.NewOracleError(symbol, "invalid timestamp", nil)
	}

	return price, timestamp, nil
}

// GetGuardedValue fetches the latest oracle value from DIAOracleV2 contract.
// Note: DIAOracleV2 doesn't support guardian validation, so this method calls GetValue internally
// and ignores the guardian parameters.
func (oc *DIAOracleV2Client) GetGuardedValue(ctx context.Context, symbol string, params config.GuardianParams) (*big.Int, *big.Int, error) {
	logger.WithFields(map[string]interface{}{
		"symbol":             symbol,
		"oracle_address":     oc.oracleAddr.Hex(),
		"client_type":        "dia_v2",
		"contract_function":  "getValue(string)",
		"MaxDeviationBips":   params.MaxDeviationBips,
		"MaxTimestampAge":    params.MaxTimestampAge,
		"MinGuardianMatches": params.MinGuardianMatches,
	}).Info("DIAOracleV2Client: Guardian parameters ignored (not supported), calling getValue(string) instead of getGuardedValue")
	return oc.GetValue(ctx, symbol)
}

func (oc *DIAOracleV2Client) fetchOracleValue(ctx context.Context, symbol string) (*big.Int, *big.Int, error) {
	logger.WithFields(map[string]interface{}{
		"symbol":         symbol,
		"oracle_address": oc.oracleAddr.Hex(),
		"function":       "getValue(string)",
	}).Debug("Packing contract call for DIAOracleV2.getValue")

	data, err := oc.oracleABI.Pack("getValue", symbol)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to pack input data for getValue: %v", err)
	}

	callMsg := ethereum.CallMsg{To: &oc.oracleAddr, Data: data}
	logger.WithFields(map[string]interface{}{
		"symbol":         symbol,
		"oracle_address": oc.oracleAddr.Hex(),
		"function":       "getValue(string)",
	}).Info("Calling DIAOracleV2 contract: getValue")

	resultBytes, err := oc.multiClient.CallContract(ctx, callMsg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("contract call failed for getValue(%s): %v", symbol, err)
	}

	outputs, err := oc.oracleABI.Unpack("getValue", resultBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unpack result: %v", err)
	}

	if len(outputs) != 2 {
		return nil, nil, fmt.Errorf("unexpected number of outputs: got %d, want 2", len(outputs))
	}

	price, ok := outputs[0].(*big.Int)
	if !ok {
		return nil, nil, fmt.Errorf("failed to convert price to big.Int")
	}

	timestamp, ok := outputs[1].(*big.Int)
	if !ok {
		return nil, nil, fmt.Errorf("failed to convert timestamp to big.Int")
	}

	return price, timestamp, nil
}

// GetThreshold fetches the current threshold from the DIAOracleV2 contract.
func (oc *DIAOracleV2Client) GetThreshold(ctx context.Context) (*big.Int, error) {
	logger.WithFields(map[string]interface{}{
		"oracle_address": oc.oracleAddr.Hex(),
		"function":       "getThreshold",
	}).Info("Calling DIAOracleV2 contract: getThreshold")

	data, err := oc.oracleABI.Pack("getThreshold")
	if err != nil {
		return nil, fmt.Errorf("failed to pack input data for getThreshold: %v", err)
	}

	callMsg := ethereum.CallMsg{To: &oc.oracleAddr, Data: data}
	resultBytes, err := oc.multiClient.CallContract(ctx, callMsg, nil)
	if err != nil {
		return nil, fmt.Errorf("contract call failed for getThreshold: %v", err)
	}

	outputs, err := oc.oracleABI.Unpack("getThreshold", resultBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getThreshold result: %v", err)
	}

	if len(outputs) != 1 {
		return nil, fmt.Errorf("unexpected number of outputs for getThreshold: got %d, want 1", len(outputs))
	}

	threshold, ok := outputs[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("failed to convert getThreshold result to big.Int")
	}

	return threshold, nil
}

// GetWindowSize fetches the current window size from the DIAOracleV2 contract.
func (oc *DIAOracleV2Client) GetWindowSize(ctx context.Context) (*big.Int, error) {
	logger.WithFields(map[string]interface{}{
		"oracle_address": oc.oracleAddr.Hex(),
		"function":       "getWindowSize",
	}).Info("Calling DIAOracleV2 contract: getWindowSize")

	data, err := oc.oracleABI.Pack("getWindowSize")
	if err != nil {
		return nil, fmt.Errorf("failed to pack input data for getWindowSize: %v", err)
	}

	callMsg := ethereum.CallMsg{To: &oc.oracleAddr, Data: data}
	resultBytes, err := oc.multiClient.CallContract(ctx, callMsg, nil)
	if err != nil {
		return nil, fmt.Errorf("contract call failed for getWindowSize: %v", err)
	}

	outputs, err := oc.oracleABI.Unpack("getWindowSize", resultBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getWindowSize result: %v", err)
	}

	if len(outputs) != 1 {
		return nil, fmt.Errorf("unexpected number of outputs for getWindowSize: got %d, want 1", len(outputs))
	}

	windowSize, ok := outputs[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("failed to convert getWindowSize result to big.Int")
	}

	return windowSize, nil
}

// GetPriceMethodology fetches the price methodology contract address from the DIAOracleV2 contract.
func (oc *DIAOracleV2Client) GetPriceMethodology(ctx context.Context) (common.Address, error) {
	logger.WithFields(map[string]interface{}{
		"oracle_address": oc.oracleAddr.Hex(),
		"function":       "priceMethodology",
	}).Info("Calling DIAOracleV2 contract: priceMethodology")

	data, err := oc.oracleABI.Pack("priceMethodology")
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to pack input data for priceMethodology: %v", err)
	}

	callMsg := ethereum.CallMsg{To: &oc.oracleAddr, Data: data}
	resultBytes, err := oc.multiClient.CallContract(ctx, callMsg, nil)
	if err != nil {
		return common.Address{}, fmt.Errorf("contract call failed for priceMethodology: %v", err)
	}

	outputs, err := oc.oracleABI.Unpack("priceMethodology", resultBytes)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to unpack priceMethodology result: %v", err)
	}

	if len(outputs) != 1 {
		return common.Address{}, fmt.Errorf("unexpected number of outputs for priceMethodology: got %d, want 1", len(outputs))
	}

	addr, ok := outputs[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("failed to convert priceMethodology result to common.Address")
	}

	return addr, nil
}

// Accessors retained for compatibility.
func (oc *DIAOracleV2Client) GetRPCURL() string {
	return oc.primaryRPC
}

func (oc *DIAOracleV2Client) GetOracleAddr() string {
	return oc.oracleAddr.Hex()
}

func (oc *DIAOracleV2Client) GetSignedAddr() string {
	return oc.signedAddr
}

func (oc *DIAOracleV2Client) GetPrivateKey() string {
	return oc.privateKey
}

func (oc *DIAOracleV2Client) GetFromAddress() string {
	return oc.fromAddress.Hex()
}
