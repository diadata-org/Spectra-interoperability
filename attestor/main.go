package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/client"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/intent"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/utils"
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

	symbols := strings.Split(symbolsStr, ",")
	for i, s := range symbols {
		symbols[i] = strings.TrimSpace(s)
	}

	log.Println("Using EIP-712 signatures for intents")

	eip712Contract := utils.GetEnv("L2_INTENT_REGISTRY_EIP712", "")
	if eip712Contract == "" {
		log.Printf("Warning: L2_INTENT_REGISTRY_EIP712 environment variable not set")
	} else {
		log.Printf("Using EIP-712 contract: %s", eip712Contract)
	}


	if privateKey == "" {
		log.Fatal("PRIVATE_KEY environment variable is required")
	}

	pollingTimeFloat, err := strconv.ParseFloat(pollingTimeStr, 64)
	if err != nil {
		log.Fatalf("Invalid polling time: %v", err)
	}
	pollingTime := time.Duration(pollingTimeFloat * float64(time.Second))

	oracleClient, err := client.NewOracleClient(rpcURL, oracleAddr, "", privateKey)
	if err != nil {
		log.Fatalf("Failed to create oracle client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("Starting attestation service for symbols: %s", strings.Join(symbols, ", "))
	log.Printf("Oracle address: %s", oracleAddr)
	if pollingTime < time.Second {
		log.Printf("Polling interval: %dms", pollingTime.Milliseconds())
	} else {
		log.Printf("Polling interval: %s", pollingTime)
	}

	if *useBatchMode {
		log.Printf("Using batch mode: All symbols will be processed in a single transaction")
	} else {
		log.Printf("Using individual mode: Each symbol will be processed separately")
	}

	if *useBatchMode {
		processMultipleAttestations(ctx, oracleClient, symbols)
	} else {
		for _, symbol := range symbols {
			processAttestation(ctx, oracleClient, symbol)
		}
	}

	if err := utils.CreateEnvTemplate(); err != nil {
		log.Printf("Warning: Failed to create .env.example file: %v", err)
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
		case <-sigCh:
			log.Println("Received shutdown signal, exiting...")
			return
		}
	}
}

func processAttestation(ctx context.Context, oracleClient *client.OracleClient, symbol string) {
	startTime := time.Now()
	log.Printf("Processing attestation for %s", symbol)

	price, timestamp, err := oracleClient.GetOracleValue(ctx, symbol)
	if err != nil {
		log.Printf("Failed to get oracle value: %v", err)
		return
	}

	volume := big.NewInt(1)

	log.Printf("Retrieved price: %s, timestamp: %s", price.String(), timestamp.String())

	signedIntentJSON, err := intent.AttestValue(ctx, oracleClient.GetPrivateKey(), oracleClient.GetFromAddress(), price, volume, symbol)
	if err != nil {
		log.Printf("Failed to create intent: %v", err)
		return
	}

	txHash, err := intent.PublishIntent(ctx, oracleClient.GetPrivateKey(), signedIntentJSON)
	if err != nil {
		log.Printf("Failed to publish intent: %v", err)
		return
	}

	log.Printf("Successfully published intent for %s, transaction hash: %s", symbol, txHash)
	log.Printf("Completed in %s", time.Since(startTime))
}

func processMultipleAttestations(ctx context.Context, oracleClient *client.OracleClient, symbols []string) {
	startTime := time.Now()
	log.Printf("Processing batch attestation for %d symbols", len(symbols))

	symbolsData := make([]intent.SymbolData, 0, len(symbols))

	for _, symbol := range symbols {
		price, timestamp, err := oracleClient.GetOracleValue(ctx, symbol)
		if err != nil {
			log.Printf("Failed to get oracle value for %s: %v", symbol, err)
			continue
		}

		volume := big.NewInt(1)

		log.Printf("Retrieved price for %s: %s, timestamp: %s", symbol, price.String(), timestamp.String())

		symbolsData = append(symbolsData, intent.SymbolData{
			Symbol: symbol,
			Price:  price,
			Volume: volume,
		})
	}

	if len(symbolsData) == 0 {
		log.Printf("No valid symbol data found")
		return
	}

	batchIntentJSON, err := intent.AttestMultipleValues(ctx, oracleClient.GetPrivateKey(),
		oracleClient.GetFromAddress(), symbolsData)
	if err != nil {
		log.Printf("Failed to create batch intent: %v", err)
		return
	}

	txHash, err := intent.PublishMultipleIntents(ctx, oracleClient.GetPrivateKey(), batchIntentJSON)
	if err != nil {
		log.Printf("Failed to publish batch intent: %v", err)
		return
	}

	log.Printf("Successfully published batch intent for %d symbols, transaction hash: %s", len(symbolsData), txHash)
	log.Printf("Completed in %s", time.Since(startTime))
}
