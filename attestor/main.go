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
	"syscall"
	"time"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/client"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/consumer"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/intent"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/utils"
)

const (
	defaultRPCURL       = "https://testnet-rpc.diadata.org"
	defaultOracleAddr   = "0x0087342f5f4c7AB23a37c045c3EF710749527c88"
	defaultSymbol       = "BTC/USD"
	defaultPollingTime  = 0.3 // seconds (300ms)
	defaultDebug        = false
	defaultConsumerAddr = "" // Default OracleIntentConsumer address
)

func main() {
	// Define command line flags
	startConsumer := flag.Bool("consumer", false, "Start the OracleIntentConsumer service")
	flag.Parse()

	// Get configuration from environment variables or use defaults
	rpcURL := utils.GetEnv("RPC_URL", defaultRPCURL)
	oracleAddr := utils.GetEnv("ORACLE_ADDRESS", defaultOracleAddr)
	signedAddr := utils.GetEnv("SIGNED_ORACLE_ADDRESS", "")
	privateKey := utils.GetEnv("PRIVATE_KEY", "")
	symbol := utils.GetEnv("SYMBOL", defaultSymbol)
	pollingTimeStr := utils.GetEnv("POLLING_TIME", fmt.Sprintf("%g", defaultPollingTime))
	consumerAddr := utils.GetEnv("CONSUMER_ADDRESS", defaultConsumerAddr)

	// Always use EIP-712 signatures
	log.Println("Using EIP-712 signatures for intents")

	// Check if the EIP-712 contract address is set
	eip712Contract := utils.GetEnv("L2_INTENT_REGISTRY_EIP712", "")
	if eip712Contract == "" {
		log.Printf("Warning: L2_INTENT_REGISTRY_EIP712 environment variable not set, signature verification may fail")
	} else {
		log.Printf("Using EIP-712 contract: %s", eip712Contract)
	}

	// Set debug mode
	debugModeStr := utils.GetEnv("DEBUG", fmt.Sprintf("%t", defaultDebug))
	utils.DebugMode, _ = strconv.ParseBool(debugModeStr)
	if utils.DebugMode {
		log.Println("Debug mode enabled")
	}

	// Set the debug log function for the client package
	client.DebugLog = utils.DebugLog

	// Validate required environment variables
	if privateKey == "" {
		log.Fatal("PRIVATE_KEY environment variable is required")
	}

	// Parse polling time
	pollingTimeFloat, err := strconv.ParseFloat(pollingTimeStr, 64)
	if err != nil {
		log.Fatalf("Invalid polling time: %v", err)
	}
	pollingTime := time.Duration(pollingTimeFloat * float64(time.Second))

	// Create oracle client
	oracleClient, err := client.NewOracleClient(rpcURL, oracleAddr, signedAddr, privateKey)
	if err != nil {
		log.Fatalf("Failed to create oracle client: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Check if we should start the consumer service
	if *startConsumer {
		if consumerAddr == "" {
			log.Fatal("CONSUMER_ADDRESS environment variable is required for consumer service")
		}
		log.Printf("Starting OracleIntentConsumer service for symbol %s", symbol)
		log.Printf("Consumer address: %s", consumerAddr)
		if pollingTime < time.Second {
			log.Printf("Polling interval: %dms", pollingTime.Milliseconds())
		} else {
			log.Printf("Polling interval: %s", pollingTime)
		}

		// Start consumer service
		go consumer.StartConsumerService(ctx, oracleClient, consumerAddr, symbol, pollingTime, consumer.ModeEIP712)
	} else {
		// Start attestation loop
		log.Printf("Starting attestation service for symbol %s", symbol)
		log.Printf("Oracle address: %s", oracleAddr)
		log.Printf("Signed oracle address: %s", signedAddr)
		if pollingTime < time.Second {
			log.Printf("Polling interval: %dms", pollingTime.Milliseconds())
		} else {
			log.Printf("Polling interval: %s", pollingTime)
		}

		// Process once immediately
		processAttestation(ctx, oracleClient, symbol)
	}

	// Create .env.example file
	if err := utils.CreateEnvTemplate(); err != nil {
		log.Printf("Warning: Failed to create .env.example file: %v", err)
	}

	// Set up ticker for regular processing
	ticker := time.NewTicker(pollingTime)
	defer ticker.Stop()

	// Main loop
	for {
		select {
		case <-ticker.C:
			if *startConsumer {
				// Consumer service is handled in its own goroutine
			} else {
				processAttestation(ctx, oracleClient, symbol)
			}
		case <-sigCh:
			log.Println("Received shutdown signal, exiting...")
			return
		}
	}
}

// processAttestation handles the attestation process
func processAttestation(ctx context.Context, oracleClient *client.OracleClient, symbol string) {
	// Get the current time
	startTime := time.Now()
	log.Printf("Processing attestation for %s at %s", symbol, startTime.Format(time.RFC3339))

	// Get oracle value
	price, timestamp, err := oracleClient.GetOracleValue(ctx, symbol)
	if err != nil {
		log.Printf("Failed to get oracle value: %v", err)
		return
	}

	// Use a default volume of 1 for simplicity
	volume := big.NewInt(1)

	// Log the retrieved values
	log.Printf("Retrieved price: %s, timestamp: %s", price.String(), timestamp.String())

	// Create the intent
	signedIntentJSON, err := intent.AttestValue(ctx, oracleClient.GetPrivateKey(), oracleClient.GetFromAddress(), price, volume, symbol)
	if err != nil {
		log.Printf("Failed to create intent: %v", err)
		return
	}
	log.Printf("Signing intent process completed in %s", time.Since(startTime))

	// Publish the intent to the L2 chain
	txHash, err := intent.PublishIntent(ctx, oracleClient.GetPrivateKey(), signedIntentJSON, intent.ModeEIP712)
	if err != nil {
		log.Printf("Failed to publish intent: %v", err)
		return
	}

	// Log success
	log.Printf("Successfully published intent for %s, transaction hash: %s", symbol, txHash)
	log.Printf("Attestation process completed in %s", time.Since(startTime))
}
