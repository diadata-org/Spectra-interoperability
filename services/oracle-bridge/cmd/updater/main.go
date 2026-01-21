package main

import (
	"context"
	"log"
	"github.com/diadata.org/Spectra-interoperability/services/oracle-bridge/internal/config"
	"github.com/diadata.org/Spectra-interoperability/services/oracle-bridge/internal/ethclient"
	"github.com/diadata.org/Spectra-interoperability/services/oracle-bridge/internal/oracle"
)

func main() {
	ctx := context.Background()
	config, err := config.LoadConfiguration()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	client, err := ethclient.NewRealEthereumClient(config.RPCURL)
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	updater, err := oracle.NewOracleUpdater(config, client)
	if err != nil {
		log.Fatalf("Failed to initialize updater: %v", err)
	}

	updater.Start(ctx)

}
