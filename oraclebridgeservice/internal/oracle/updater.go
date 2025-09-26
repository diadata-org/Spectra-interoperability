package oracle

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	logger "oracleservice/internal/logging"

	"oracleservice/internal/config"
	"oracleservice/internal/ethclient"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type receiverTarget struct {
	chainID uint32
	address common.Address
}

type OracleUpdater struct {
	config           *config.Configuration
	client           ethclient.EthereumClientProvider
	oracleMetadata   *OracleMetadata
	oracleTriggerABI abi.ABI
	auth             *bind.TransactOpts
	oldPrices        map[string]float64
	receivers        []receiverTarget
	intentType       string
}

const (
	oracleTriggerABIJSON  = `[{"inputs":[{"internalType":"uint32","name":"_destinationDomain","type":"uint32"},{"internalType":"address","name":"_recipientAddress","type":"address"},{"internalType":"string","name":"_intentType","type":"string"},{"internalType":"string","name":"_key","type":"string"}],"name":"dispatch","outputs":[],"stateMutability":"payable","type":"function"}]`
	oracleMetadataABIJSON = `[{"inputs":[{"internalType":"string","name":"key","type":"string"}],"name":"getValue","outputs":[{"internalType":"uint128","name":"","type":"uint128"},{"internalType":"uint128","name":"","type":"uint128"}],"stateMutability":"view","type":"function"}]`
)

func NewOracleUpdater(cfg *config.Configuration, client ethclient.EthereumClientProvider) (*OracleUpdater, error) {
	if len(cfg.Receivers) == 0 {
		return nil, fmt.Errorf("no receivers configured; set RECEIVER_ADDRESS env var")
	}

	parsedABI, err := abi.JSON(strings.NewReader(oracleTriggerABIJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse oracle trigger ABI: %w", err)
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKey, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch network id: %w", err)
	}

	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to create transactor: %w", err)
	}
	auth.GasLimit = 300_000

	metadata, err := NewOracleMetadata(client, oracleMetadataABIJSON, cfg.MetadataAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise metadata reader: %w", err)
	}

	receivers := make([]receiverTarget, 0, len(cfg.Receivers))
	for _, receiver := range cfg.Receivers {
		addr := common.HexToAddress(receiver.Address)
		if addr == (common.Address{}) {
			return nil, fmt.Errorf("receiver address %s resolves to zero address", receiver.Address)
		}
		receivers = append(receivers, receiverTarget{
			chainID: receiver.ChainID,
			address: addr,
		})
	}

	if cfg.IntentType == "" {
		return nil, fmt.Errorf("intent type must be provided")
	}

	return &OracleUpdater{
		config:           cfg,
		client:           client,
		oracleMetadata:   metadata,
		oracleTriggerABI: parsedABI,
		auth:             auth,
		oldPrices:        make(map[string]float64),
		receivers:        receivers,
		intentType:       cfg.IntentType,
	}, nil
}

func (ou *OracleUpdater) Start(ctx context.Context) {
	deviationTicker := time.NewTicker(10 * time.Second)
	mandatoryTicker := time.NewTicker(1 * time.Minute)
	defer deviationTicker.Stop()
	defer mandatoryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Oracle updater stopped: context cancelled")
			return
		case <-deviationTicker.C:
			logger.WithField("receivers", len(ou.receivers)).Debug("Checking deviation across receivers")
			for _, symbol := range ou.config.SupportedAssets {
				ou.updateIfNecessary(ctx, symbol)
			}
		case <-mandatoryTicker.C:
			logger.WithField("receivers", len(ou.receivers)).Info("Performing mandatory oracle update")
			for _, symbol := range ou.config.SupportedAssets {
				ou.updateMandatory(ctx, symbol)
			}
		}
	}
}

func (ou *OracleUpdater) convertToFloat64WithDecimals(value *big.Int, decimals int) float64 {
	floatValue := new(big.Float).SetInt(value)
	scaleFactor := new(big.Float).SetFloat64(math.Pow10(decimals))
	floatValue.Quo(floatValue, scaleFactor)
	result, _ := floatValue.Float64()
	return result
}

func (ou *OracleUpdater) updateIfNecessary(ctx context.Context, symbol string) {
	price, err := ou.oracleMetadata.GetLatestValue(ctx, symbol)
	if err != nil {
		logger.WithError(err).Warnf("Failed to get latest value for %s", symbol)
		return
	}

	newPrice := ou.convertToFloat64WithDecimals(price, 8)
	oldPrice, exists := ou.oldPrices[symbol]

	if !exists || oldPrice == 0 {
		logger.WithFields(logger.Fields{
			"symbol": symbol,
			"price":  newPrice,
		}).Info("Initialized price cache")
		ou.dispatchToAll(ctx, symbol)
		ou.oldPrices[symbol] = newPrice
		return
	}

	deviation := math.Abs(newPrice-oldPrice) / oldPrice
	threshold := float64(ou.config.DeviationPermille) / 1000

	if deviation >= threshold {
		logger.WithFields(logger.Fields{
			"symbol":    symbol,
			"old_price": oldPrice,
			"new_price": newPrice,
			"threshold": threshold,
		}).Info("Deviation threshold met; dispatching update")
		ou.dispatchToAll(ctx, symbol)
		ou.oldPrices[symbol] = newPrice
		return
	}

	logger.WithFields(logger.Fields{
		"symbol":    symbol,
		"old_price": oldPrice,
		"new_price": newPrice,
		"threshold": threshold,
		"delta":     deviation,
	}).Debug("Deviation threshold not met; skipping dispatch")
}

func (ou *OracleUpdater) updateMandatory(ctx context.Context, symbol string) {
	price, err := ou.oracleMetadata.GetLatestValue(ctx, symbol)
	if err != nil {
		logger.WithError(err).Warnf("Failed to get latest value for %s during mandatory update", symbol)
		return
	}

	newPrice := ou.convertToFloat64WithDecimals(price, 8)
	logger.WithFields(logger.Fields{
		"symbol": symbol,
		"price":  newPrice,
	}).Info("Performing mandatory dispatch")
	ou.dispatchToAll(ctx, symbol)
	ou.oldPrices[symbol] = newPrice
}

func (ou *OracleUpdater) dispatchToAll(ctx context.Context, symbol string) {
	for _, target := range ou.receivers {
		if err := ou.sendTransaction(ctx, target, symbol); err != nil {
			logger.WithError(err).WithFields(logger.Fields{
				"symbol":   symbol,
				"chain_id": target.chainID,
				"address":  target.address.Hex(),
			}).Error("Failed to dispatch update")
		}
	}
}

func (ou *OracleUpdater) sendTransaction(ctx context.Context, target receiverTarget, symbol string) error {
	nonce, err := ou.client.PendingNonceAt(ctx, ou.auth.From)
	if err != nil {
		return fmt.Errorf("failed to get nonce: %w", err)
	}

	gasPrice, err := ou.client.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get gas price: %w", err)
	}

	txData, err := ou.oracleTriggerABI.Pack("dispatch", target.chainID, target.address, ou.intentType, symbol)
	if err != nil {
		return fmt.Errorf("failed to pack dispatch calldata: %w", err)
	}

	destination := common.HexToAddress(ou.config.OracleTriggerAddress)
	tx := types.NewTransaction(nonce, destination, big.NewInt(0), ou.auth.GasLimit, gasPrice, txData)

	signedTx, err := ou.auth.Signer(ou.auth.From, tx)
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}

	if err := ou.client.SendTransaction(ctx, signedTx); err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}

	logger.WithFields(logger.Fields{
		"symbol":   symbol,
		"chain_id": target.chainID,
		"address":  target.address.Hex(),
		"tx_hash":  signedTx.Hash().Hex(),
	}).Info("Dispatch transaction sent")
	return nil
}
