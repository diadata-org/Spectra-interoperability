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

const guardedOracleABIJSON = `[
  {
    "inputs": [
      {
        "internalType": "address",
        "name": "newBaseDIAContractAddress",
        "type": "address"
      },
      {
        "internalType": "address",
        "name": "newAssetRegistryAddress",
        "type": "address"
      }
    ],
    "stateMutability": "nonpayable",
    "type": "constructor"
  },
  {
    "inputs": [
      {
        "internalType": "uint256",
        "name": "value",
        "type": "uint256"
      },
      {
        "internalType": "uint256",
        "name": "length",
        "type": "uint256"
      }
    ],
    "name": "StringsInsufficientHexLength",
    "type": "error"
  },
  {
    "anonymous": false,
    "inputs": [
      {
        "indexed": false,
        "internalType": "address",
        "name": "guardian",
        "type": "address"
      },
      {
        "indexed": false,
        "internalType": "string",
        "name": "guardianName",
        "type": "string"
      }
    ],
    "name": "GuardianAdded",
    "type": "event"
  },
  {
    "anonymous": false,
    "inputs": [
      {
        "indexed": false,
        "internalType": "address",
        "name": "guardian",
        "type": "address"
      },
      {
        "indexed": false,
        "internalType": "string",
        "name": "guardianName",
        "type": "string"
      }
    ],
    "name": "GuardianRemoved",
    "type": "event"
  },
  {
    "anonymous": false,
    "inputs": [
      {
        "indexed": true,
        "internalType": "address",
        "name": "previousOwner",
        "type": "address"
      },
      {
        "indexed": true,
        "internalType": "address",
        "name": "newOwner",
        "type": "address"
      }
    ],
    "name": "OwnershipTransferred",
    "type": "event"
  },
  {
    "inputs": [
      {
        "internalType": "uint256",
        "name": "",
        "type": "uint256"
      }
    ],
    "name": "activeGuardians",
    "outputs": [
      {
        "internalType": "address",
        "name": "",
        "type": "address"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {
        "internalType": "address",
        "name": "newGuardianAddress",
        "type": "address"
      },
      {
        "internalType": "string",
        "name": "guardianName",
        "type": "string"
      }
    ],
    "name": "addGuardian",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "assetRegistryAddress",
    "outputs": [
      {
        "internalType": "address",
        "name": "",
        "type": "address"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "baseDIAContractAddress",
    "outputs": [
      {
        "internalType": "address",
        "name": "",
        "type": "address"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {
        "internalType": "address",
        "name": "newAssetRegistryAddress",
        "type": "address"
      }
    ],
    "name": "changeAssetRegistry",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {
        "internalType": "address",
        "name": "newBaseDIAContractAddress",
        "type": "address"
      }
    ],
    "name": "changeBaseDIAContract",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {
        "internalType": "string",
        "name": "key",
        "type": "string"
      },
      {
        "internalType": "uint256",
        "name": "maxDeviationBips",
        "type": "uint256"
      },
      {
        "internalType": "uint256",
        "name": "maxTimestampAge",
        "type": "uint256"
      },
      {
        "internalType": "uint256",
        "name": "numMinGuardianMatches",
        "type": "uint256"
      }
    ],
    "name": "getGuardedValue",
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
    "inputs": [
      {
        "internalType": "address",
        "name": "",
        "type": "address"
      }
    ],
    "name": "guardianNames",
    "outputs": [
      {
        "internalType": "string",
        "name": "",
        "type": "string"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "owner",
    "outputs": [
      {
        "internalType": "address",
        "name": "",
        "type": "address"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {
        "internalType": "address",
        "name": "guardianToRemove",
        "type": "address"
      }
    ],
    "name": "removeGuardian",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "renounceOwnership",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {
        "internalType": "address",
        "name": "newOwner",
        "type": "address"
      }
    ],
    "name": "transferOwnership",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  }
]`

// GuardedOracleClient wraps access to the on-chain oracle with RPC failover.
type GuardedOracleClient struct {
	primaryRPC  string
	multiClient *multirpc.MultiClient
	oracleAddr  common.Address
	signedAddr  string
	privateKey  string
	fromAddress common.Address
	oracleABI   abi.ABI
}

// NewGuardedOracleClient creates a new guarded oracle client backed by the multi-RPC failover helper.
func NewGuardedOracleClient(rpcURLs []string, oracleAddrStr, signedAddrStr, privateKeyStr string) (*GuardedOracleClient, error) {
	if len(rpcURLs) == 0 {
		return nil, fmt.Errorf("no RPC URLs provided for oracle client")
	}

	multi, err := multirpc.NewMultiClient(rpcURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to guarded oracle RPC endpoints: %w", err)
	}

	oracleAddr := common.HexToAddress(oracleAddrStr)
	oracleABI, _ := abi.JSON(strings.NewReader(guardedOracleABIJSON))

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

	return &GuardedOracleClient{
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
func (oc *GuardedOracleClient) Close() {
	if oc.multiClient != nil {
		oc.multiClient.Close()
	}
}

// GetGuardedValue fetches the latest oracle value with guardian validation.
func (oc *GuardedOracleClient) GetGuardedValue(ctx context.Context, symbol string, params config.GuardianParams) (*big.Int, *big.Int, error) {
	maxDeviationBips := big.NewInt(int64(params.MaxDeviationBips))
	maxTimestampAge := big.NewInt(int64(params.MaxTimestampAge))
	numMinGuardianMatches := big.NewInt(int64(params.MinGuardianMatches))

	logger.WithFields(map[string]interface{}{
		"symbol":             symbol,
		"oracle_address":     oc.oracleAddr.Hex(),
		"client_type":        "guarded",
		"contract_function":  "getGuardedValue(string,uint256,uint256,uint256)",
		"maxDeviationBips":   params.MaxDeviationBips,
		"maxTimestampAge":    params.MaxTimestampAge,
		"minGuardianMatches": params.MinGuardianMatches,
	}).Info("GuardedOracleClient: Calling getGuardedValue with guardian validation")

	price, timestamp, err := oc.fetchOracleValue(ctx, symbol, maxDeviationBips, maxTimestampAge, numMinGuardianMatches)
	if err != nil {
		return nil, nil, errors.NewOracleError(symbol, "failed to get value (getGuardedValue)", err)
	}

	if price == nil || price.Sign() <= 0 {
		return nil, nil, errors.NewOracleError(symbol, "invalid price", nil)
	}

	if timestamp == nil || timestamp.Sign() <= 0 {
		return nil, nil, errors.NewOracleError(symbol, "invalid timestamp", nil)
	}

	return price, timestamp, nil
}

func (oc *GuardedOracleClient) GetValue(ctx context.Context, symbol string) (*big.Int, *big.Int, error) {
	defaultParams := config.GuardianParams{
		MaxDeviationBips:   500,  // 5% deviation
		MaxTimestampAge:    3600, // 1 hour
		MinGuardianMatches: 1,    // at least 1 guardian match
	}
	return oc.GetGuardedValue(ctx, symbol, defaultParams)
}

func (oc *GuardedOracleClient) fetchOracleValue(ctx context.Context, symbol string, maxDeviationBips, maxTimestampAge, numMinGuardianMatches *big.Int) (*big.Int, *big.Int, error) {
	logger.WithFields(map[string]interface{}{
		"symbol":         symbol,
		"oracle_address": oc.oracleAddr.Hex(),
		"function":       "getGuardedValue(string,uint256,uint256,uint256)",
	}).Debug("Packing contract call for GuardedOracle.getGuardedValue")

	data, err := oc.oracleABI.Pack("getGuardedValue", symbol, maxDeviationBips, maxTimestampAge, numMinGuardianMatches)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to pack input data for getGuardedValue: %v", err)
	}

	callMsg := ethereum.CallMsg{To: &oc.oracleAddr, Data: data}
	logger.WithFields(map[string]interface{}{
		"symbol":         symbol,
		"oracle_address": oc.oracleAddr.Hex(),
		"function":       "getGuardedValue(string,uint256,uint256,uint256)",
	}).Info("Calling GuardedOracle contract: getGuardedValue")

	resultBytes, err := oc.multiClient.CallContract(ctx, callMsg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("contract call failed for getGuardedValue(%s, %d, %d, %d): %v",
			symbol, maxDeviationBips.Int64(), maxTimestampAge.Int64(), numMinGuardianMatches.Int64(), err)
	}

	outputs, err := oc.oracleABI.Unpack("getGuardedValue", resultBytes)
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
func (oc *GuardedOracleClient) GetRPCURL() string {
	return oc.primaryRPC
}

func (oc *GuardedOracleClient) GetOracleAddr() string {
	return oc.oracleAddr.Hex()
}

func (oc *GuardedOracleClient) GetSignedAddr() string {
	return oc.signedAddr
}

func (oc *GuardedOracleClient) GetPrivateKey() string {
	return oc.privateKey
}

func (oc *GuardedOracleClient) GetFromAddress() string {
	return oc.fromAddress.Hex()
}
