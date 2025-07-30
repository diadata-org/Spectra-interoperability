package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/client"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/intent"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/metrics"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultRPCURL      = "https://testnet-rpc.diadata.org"
	defaultOracleAddr  = "0x0087342f5f4c7AB23a37c045c3EF710749527c88"
	defaultPollingTime = 0.3
	defaultSymbols     = "BTC/USD,ETH/USD"
)

func main() {
	useBatchMode := flag.Bool("batch", true, "Use batch mode to process all symbols in one transaction")
	flag.Parse()

	rpcURL := utils.GetEnv("RPC_URL", defaultRPCURL)
	oracleAddr := utils.GetEnv("ORACLE_ADDRESS", defaultOracleAddr)
	privateKey := utils.GetEnv("PRIVATE_KEY", "")
	symbolsStr := utils.GetEnv("SYMBOLS", defaultSymbols)
	pollingTimeStr := utils.GetEnv("POLLING_TIME", fmt.Sprintf("%g", defaultPollingTime))

	// Initialize logger
	logLevel := utils.GetEnv("LOG_LEVEL", "info")
	if err := logger.Init(logLevel); err != nil {
		logger.Warnf("Invalid log level %s, using default: %v", logLevel, err)
	}

	symbols := strings.Split(symbolsStr, ",")
	for i, s := range symbols {
		symbols[i] = strings.TrimSpace(s)
	}

	logger.Info("Using EIP-712 signatures for intents")

	eip712Contract := utils.GetEnv("L2_INTENT_REGISTRY_EIP712", "")
	if eip712Contract == "" {
		logger.Warn("L2_INTENT_REGISTRY_EIP712 environment variable not set")
	} else {
		logger.WithField("contract", eip712Contract).Info("Using EIP-712 contract")
	}


	if privateKey == "" {
		logger.Fatal("PRIVATE_KEY environment variable is required")
	}

	pollingTimeFloat, err := strconv.ParseFloat(pollingTimeStr, 64)
	if err != nil {
		logger.Fatalf("Invalid polling time: %v", err)
	}
	pollingTime := time.Duration(pollingTimeFloat * float64(time.Second))

	oracleClient, err := client.NewOracleClient(rpcURL, oracleAddr, "", privateKey)
	if err != nil {
		logger.Fatalf("Failed to create oracle client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start metrics server in background
	metricsPort := utils.GetEnv("METRICS_PORT", "8080")
	go metrics.StartMetricsServer(metricsPort)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	logger.WithFields(map[string]interface{}{
		"symbols":      strings.Join(symbols, ", "),
		"oracle":       oracleAddr,
		"polling_time": pollingTime.String(),
	}).Info("Starting attestation service")

	if *useBatchMode {
		logger.Info("Using batch mode: All symbols will be processed in a single transaction")
	} else {
		logger.Info("Using individual mode: Each symbol will be processed separately")
	}

	if *useBatchMode {
		processMultipleAttestations(ctx, oracleClient, symbols)
	} else {
		for _, symbol := range symbols {
			processAttestation(ctx, oracleClient, symbol)
		}
	}

	if err := utils.CreateEnvTemplate(); err != nil {
		logger.Warnf("Failed to create .env.example file: %v", err)
	}

	ticker := time.NewTicker(pollingTime)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if *useBatchMode {
				processMultipleAttestations(ctx, oracleClient, symbols)
			} else {
				for _, symbol := range symbols {
					processAttestation(ctx, oracleClient, symbol)
				}
			}
		case sig := <-sigCh:
			logger.WithField("signal", sig).Info("Received shutdown signal, exiting gracefully...")
			return
		}
	}
}

func processAttestation(ctx context.Context, oracleClient *client.OracleClient, symbol string) {
	startTime := time.Now()
	timer := prometheus.NewTimer(metrics.ProcessingDuration.WithLabelValues(symbol, "single"))
	defer timer.ObserveDuration()

	logger.WithField("symbol", symbol).Info("Processing attestation")

	// Time oracle fetch
	fetchTimer := prometheus.NewTimer(metrics.OracleValueFetchDuration.WithLabelValues(symbol))
	price, timestamp, err := oracleClient.GetOracleValue(ctx, symbol)
	fetchTimer.ObserveDuration()

	if err != nil {
		logger.WithFields(map[string]interface{}{
			"symbol": symbol,
			"error":  err,
		}).Error("Failed to get oracle value")
		metrics.IntentsCreated.WithLabelValues(symbol, "error").Inc()
		return
	}

	volume := big.NewInt(1)

	logger.WithFields(map[string]interface{}{
		"symbol":    symbol,
		"price":     price.String(),
		"timestamp": timestamp.String(),
	}).Debug("Retrieved oracle value")

	signedIntentJSON, err := intent.AttestValue(ctx, oracleClient.GetPrivateKey(), oracleClient.GetFromAddress(), price, volume, symbol)
	if err != nil {
		logger.WithFields(map[string]interface{}{
			"symbol": symbol,
			"error":  err,
		}).Error("Failed to create intent")
		metrics.IntentsCreated.WithLabelValues(symbol, "error").Inc()
		return
	}
	metrics.IntentsCreated.WithLabelValues(symbol, "success").Inc()

	txHash, err := intent.PublishIntent(ctx, oracleClient.GetPrivateKey(), signedIntentJSON)
	if err != nil {
		logger.WithFields(map[string]interface{}{
			"symbol": symbol,
			"error":  err,
		}).Error("Failed to publish intent")
		metrics.IntentsPublished.WithLabelValues(symbol, "error").Inc()
		return
	}
	metrics.IntentsPublished.WithLabelValues(symbol, "success").Inc()

	logger.WithFields(map[string]interface{}{
		"symbol":   symbol,
		"tx_hash":  txHash,
		"duration": time.Since(startTime).String(),
	}).Info("Successfully published intent")
}

func processMultipleAttestations(ctx context.Context, oracleClient *client.OracleClient, symbols []string) {
	startTime := time.Now()
	timer := prometheus.NewTimer(metrics.ProcessingDuration.WithLabelValues("batch", "batch"))
	defer timer.ObserveDuration()

	logger.WithField("symbol_count", len(symbols)).Info("Processing batch attestation")

	symbolsData := make([]intent.SymbolData, 0, len(symbols))

	for _, symbol := range symbols {
		fetchTimer := prometheus.NewTimer(metrics.OracleValueFetchDuration.WithLabelValues(symbol))
		price, timestamp, err := oracleClient.GetOracleValue(ctx, symbol)
		fetchTimer.ObserveDuration()

		if err != nil {
			logger.WithFields(map[string]interface{}{
				"symbol": symbol,
				"error":  err,
			}).Error("Failed to get oracle value")
			metrics.IntentsCreated.WithLabelValues(symbol, "error").Inc()
			continue
		}

		volume := big.NewInt(1)

		logger.WithFields(map[string]interface{}{
			"symbol":    symbol,
			"price":     price.String(),
			"timestamp": timestamp.String(),
		}).Debug("Retrieved oracle value")

		symbolsData = append(symbolsData, intent.SymbolData{
			Symbol: symbol,
			Price:  price,
			Volume: volume,
		})
		metrics.IntentsCreated.WithLabelValues(symbol, "success").Inc()
	}

	if len(symbolsData) == 0 {
		logger.Warn("No valid symbol data found")
		return
	}

	batchIntentJSON, err := intent.AttestMultipleValues(ctx, oracleClient.GetPrivateKey(),
		oracleClient.GetFromAddress(), symbolsData)
	if err != nil {
		logger.WithField("error", err).Error("Failed to create batch intent")
		return
	}

	txHash, err := intent.PublishMultipleIntents(ctx, oracleClient.GetPrivateKey(), batchIntentJSON)
	if err != nil {
		logger.WithField("error", err).Error("Failed to publish batch intent")
		for _, data := range symbolsData {
			metrics.IntentsPublished.WithLabelValues(data.Symbol, "error").Inc()
		}
		return
	}
	for _, data := range symbolsData {
		metrics.IntentsPublished.WithLabelValues(data.Symbol, "success").Inc()
	}

	logger.WithFields(map[string]interface{}{
		"symbol_count": len(symbolsData),
		"tx_hash":      txHash,
		"duration":     time.Since(startTime).String(),
	}).Info("Successfully published batch intent")
}
