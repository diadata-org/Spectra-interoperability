package client

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	multirpc "github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/errors"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const oracleABIJSON = `[{"inputs":[{"internalType":"string","name":"key","type":"string"}],"name":"getValue","outputs":[{"internalType":"uint128","name":"","type":"uint128"},{"internalType":"uint128","name":"","type":"uint128"}],"stateMutability":"view","type":"function"}]`

// OracleClient wraps access to the on-chain oracle with RPC failover.
type OracleClient struct {
	primaryRPC  string
	multiClient *multirpc.MultiClient
	oracleAddr  common.Address
	signedAddr  string
	privateKey  string
	fromAddress common.Address
	oracleABI   abi.ABI
}

// NewOracleClient creates a new oracle client backed by the multi-RPC failover helper.
func NewOracleClient(rpcURLs []string, oracleAddrStr, signedAddrStr, privateKeyStr string) (*OracleClient, error) {
	if len(rpcURLs) == 0 {
		return nil, fmt.Errorf("no RPC URLs provided for oracle client")
	}

	multi, err := multirpc.NewMultiClient(rpcURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to oracle RPC endpoints: %w", err)
	}

	oracleAddr := common.HexToAddress(oracleAddrStr)
	oracleABI, _ := abi.JSON(strings.NewReader(oracleABIJSON))

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
		logger.WithField("address", fromAddress.Hex()).Debug("Derived oracle client address from private key")
	} else {
		fromAddress = common.Address{}
	}

	return &OracleClient{
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
func (oc *OracleClient) Close() {
	if oc.multiClient != nil {
		oc.multiClient.Close()
	}
}

// GetValue fetches the latest oracle value with validation.
func (oc *OracleClient) GetValue(ctx context.Context, symbol string) (*big.Int, *big.Int, error) {
	price, timestamp, err := oc.fetchOracleValue(ctx, symbol)
	if err != nil {
		return nil, nil, errors.NewOracleError(symbol, "failed to get value", err)
	}

	if price == nil || price.Sign() <= 0 {
		return nil, nil, errors.NewOracleError(symbol, "invalid price", nil)
	}

	if timestamp == nil || timestamp.Sign() <= 0 {
		return nil, nil, errors.NewOracleError(symbol, "invalid timestamp", nil)
	}

	return price, timestamp, nil
}

func (oc *OracleClient) fetchOracleValue(ctx context.Context, symbol string) (*big.Int, *big.Int, error) {
	data, err := oc.oracleABI.Pack("getValue", symbol)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to pack input data: %v", err)
	}

	callMsg := ethereum.CallMsg{To: &oc.oracleAddr, Data: data}
	resultBytes, err := oc.multiClient.CallContract(ctx, callMsg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("contract call failed: %v", err)
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

// Accessors retained for compatibility.
func (oc *OracleClient) GetRPCURL() string {
	return oc.primaryRPC
}

func (oc *OracleClient) GetOracleAddr() string {
	return oc.oracleAddr.Hex()
}

func (oc *OracleClient) GetSignedAddr() string {
	return oc.signedAddr
}

func (oc *OracleClient) GetPrivateKey() string {
	return oc.privateKey
}

func (oc *OracleClient) GetFromAddress() string {
	return oc.fromAddress.Hex()
}
