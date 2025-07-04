package client

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// RPCClient interface for mocking ethclient.Client().CallContext
type RPCClient interface {
	CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error
}

// Constants for the oracle client
const (
	oracleABIJSON = `[{"inputs":[{"internalType":"string","name":"key","type":"string"}],"name":"getValue","outputs":[{"internalType":"uint128","name":"","type":"uint128"},{"internalType":"uint128","name":"","type":"uint128"}],"stateMutability":"view","type":"function"}]`
)

// DebugLog logs a message if debug mode is enabled
var DebugLog func(format string, v ...interface{})

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

// DialEthClient is a variable to allow mocking in tests
var DialEthClient = ethclient.Dial

// NewOracleClient creates a new oracle client
func NewOracleClient(rpcURL, oracleAddrStr, signedAddrStr, privateKeyStr string) (*OracleClient, error) {
	client, err := DialEthClient(rpcURL)
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
		oracleABI:   oracleABI,
	}, nil
}

// GetOracleValue fetches the latest value from the oracle
func (oc *OracleClient) GetOracleValue(ctx context.Context, symbol string) (*big.Int, *big.Int, error) {
	// Create the contract address
	contractAddress := common.HexToAddress(oc.oracleAddr)

	if DebugLog != nil {
		DebugLog("Calling getValue(%s) on contract %s", symbol, oc.oracleAddr)
	}

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

	if DebugLog != nil {
		DebugLog("Got result: %s", result)
	}

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

	if DebugLog != nil {
		DebugLog("Unpacked price: %s, timestamp: %s", price.String(), timestamp.String())
	}

	return price, timestamp, nil
}

// GetRPCURL returns the RPC URL
func (oc *OracleClient) GetRPCURL() string {
	return oc.rpcURL
}

// GetOracleAddr returns the oracle address
func (oc *OracleClient) GetOracleAddr() string {
	return oc.oracleAddr
}

// GetSignedAddr returns the signed oracle address
func (oc *OracleClient) GetSignedAddr() string {
	return oc.signedAddr
}

// GetPrivateKey returns the private key
func (oc *OracleClient) GetPrivateKey() string {
	return oc.privateKey
}

// GetFromAddress returns the from address
func (oc *OracleClient) GetFromAddress() string {
	return oc.fromAddress
}
