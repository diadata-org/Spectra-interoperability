package oracle

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"
	"oracleservice/internal/config"
	"oracleservice/internal/ethclient"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// import statements

type OracleUpdater struct {
	config           *config.Configuration
	client           ethclient.EthereumClientProvider
	oracleMetadata   *OracleMetadata
	oracleTriggerABI abi.ABI
	auth             *bind.TransactOpts
	oldPrices        map[string]float64
}

const (
	oracleTriggerABI  = `[{"inputs":[{"internalType":"uint32","name":"_destinationDomain","type":"uint32"},{"internalType":"string","name":"key","type":"string"}],"name":"dispatchToChain","outputs":[],"stateMutability":"payable","type":"function"}]`
	oracleMetadataABI = `[
  {
    "inputs": [],
    "stateMutability": "nonpayable",
    "type": "constructor"
  },
  {
    "inputs": [
      {
        "internalType": "address",
        "name": "newOracleAddress",
        "type": "address"
      }
    ],
    "name": "addOracle",
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
    "name": "numOracles",
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
    "inputs": [
      {
        "internalType": "address",
        "name": "oracleToRemove",
        "type": "address"
      }
    ],
    "name": "removeOracle",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {
        "internalType": "uint256",
        "name": "newThreshold",
        "type": "uint256"
      }
    ],
    "name": "setThreshold",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {
        "internalType": "uint256",
        "name": "newTimeoutSeconds",
        "type": "uint256"
      }
    ],
    "name": "setTimeoutSeconds",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "threshold",
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
    "name": "timeoutSeconds",
    "outputs": [
      {
        "internalType": "uint256",
        "name": "",
        "type": "uint256"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  }
]`
)

var (
	oracleMetadataAddress = "0x90857994FA959d77728067A71EFA8dc154D89813"
)

func NewOracleUpdater(config *config.Configuration, client ethclient.EthereumClientProvider) (*OracleUpdater, error) {
	parsedOracleTriggerABI, err := abi.JSON(strings.NewReader(oracleTriggerABI))
	if err != nil {
		return nil, err
	}

	privateKey, err := crypto.HexToECDSA(config.PrivateKey)
	if err != nil {
		return nil, err
	}

	chainID, err := client.NetworkID(context.Background())

	if err != nil {
		return nil, err
	}
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		return nil, err
	}
	auth.GasLimit = uint64(500000) // in units

	oracleMetadata, err := NewOracleMetadata(client, oracleMetadataABI, oracleMetadataAddress)
	if err != nil {
		return nil, err
	}

	return &OracleUpdater{
		config:           config,
		client:           client,
		oracleMetadata:   oracleMetadata,
		oracleTriggerABI: parsedOracleTriggerABI,
		auth:             auth,
		oldPrices:        make(map[string]float64),
	}, nil
}

func (ou *OracleUpdater) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	tickerH := time.NewTicker(1 * time.Minute)

	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ticker.C:

				fmt.Println("total chains", len(ou.config.DestinationChains))

				for _, symbol := range ou.config.SupportedAssets {
					ou.updateIfNecessary(ctx, ou.config.DestinationChains, symbol)
				}

			case <-tickerH.C:
				{
					fmt.Println("mandatory update total chains", len(ou.config.DestinationChains))

					for _, symbol := range ou.config.SupportedAssets {
						ou.updateNecessary(ctx, ou.config.DestinationChains, symbol)
					}

				}

			}
		}
	}()

	select {}
}

func (ou *OracleUpdater) convertToFloat64WithDecimals(value *big.Int, decimals int) float64 {
	floatValue := new(big.Float).SetInt(value)

	scaleFactor := new(big.Float).SetFloat64(math.Pow10(decimals))

	floatValue.Quo(floatValue, scaleFactor)

	result, _ := floatValue.Float64()
	return result
}

func (ou *OracleUpdater) updateIfNecessary(ctx context.Context, chainIDs []string, symbol string) {

	price, err := ou.oracleMetadata.GetLatestValue(ctx, symbol)
	if err != nil {
		log.Printf("Failed to get latest value for %s:  Metadata Address %s, %v", symbol, ou.oracleMetadata.address, err)
		return
	}

	newPrice := ou.convertToFloat64WithDecimals(price, 8)
	oldPrice, exists := ou.oldPrices[symbol]
	if !exists || math.Abs(newPrice-oldPrice)/oldPrice >= float64(ou.config.DeviationPermille)/1000 {
		log.Printf("Deviation threshold met, triggering update Old price %f new price %f", oldPrice, newPrice)
		for _, chainID := range chainIDs {
			ou.sendTransaction(ctx, chainID, symbol)
		}
		ou.oldPrices[symbol] = newPrice
	} else {
		log.Printf("Deviation threshold not  met, Old price %f new price %f", oldPrice, newPrice)

	}
}

func (ou *OracleUpdater) updateNecessary(ctx context.Context, chainIDs []string, symbol string) {

	price, err := ou.oracleMetadata.GetLatestValue(ctx, symbol)
	if err != nil {
		log.Printf("Failed to get latest value for %s: %v", symbol, err)
		return
	}

	newPrice := ou.convertToFloat64WithDecimals(price, 8)
	log.Printf("mandatory, triggering update Old price   new price %f", newPrice)
	for _, chainID := range chainIDs {
		ou.sendTransaction(ctx, chainID, symbol)
	}
	ou.oldPrices[symbol] = newPrice

}

// sendTransaction prepares and sends a transaction to update the oracle on-chain
func (ou *OracleUpdater) sendTransaction(ctx context.Context, chainID, symbol string) {
	nonce, err := ou.client.PendingNonceAt(context.Background(), ou.auth.From)
	if err != nil {
		log.Fatalf("Failed to get nonce: %v", err)
	}

	gasPrice, err := ou.client.SuggestGasPrice(context.Background())
	if err != nil {
		log.Fatalf("Failed to get gas price: %v", err)
	}

	// chainID, err := conn.NetworkID(context.Background())
	// if err != nil {
	// 	log.Fatalf("Failed to get network ID: %v", err)
	// }

	cid, _ := strconv.ParseUint(chainID, 10, 32)

	txData, err := ou.oracleTriggerABI.Pack("dispatchToChain", uint32(cid), symbol)
	if err != nil {
		log.Printf("Failed to pack the transaction data: %v", err)
		return
	}

	fmt.Println("gasPrice", gasPrice)
	fmt.Println("nonce", nonce)
	fmt.Println("chainID", chainID)
	fmt.Println("symbol", symbol)
	fmt.Println("ou.config.OracleTriggerAddress", ou.config.OracleTriggerAddress)
	fmt.Println("ou.auth.GasLimit", ou.auth.GasLimit)

	tx := types.NewTransaction(nonce, common.HexToAddress(ou.config.OracleTriggerAddress), big.NewInt(0), ou.auth.GasLimit, gasPrice, txData)

	signedTx, err := ou.auth.Signer(ou.auth.From, tx)
	if err != nil {
		log.Printf("Failed to sign the transaction: %v", err)
		return
	}

	err = ou.client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		log.Printf("Failed to send the transaction: %v", err)
		return
	}

	fmt.Printf("Transaction sent: %s\n", signedTx.Hash().Hex())

	nonce++
}
