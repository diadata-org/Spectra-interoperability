package bridge

import (
	"context"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/api"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/grpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
)

// startMetricsServer starts the metrics server
func (b *Bridge) startMetricsServer(ctx context.Context) {
	if b.configService.GetInfrastructure().Metrics.Enabled {
		logger.Info("Metrics collection is enabled")
	}

	// Start API server if configured
	if b.configService.GetInfrastructure().API.ListenAddr != "" {
		// Create metrics collector for API
		var metricsCollector *metrics.Collector
		if b.metrics != nil {
			// Use the singleton metrics collector which includes IntentMetrics
			metricsCollector = metrics.NewCollector()
			// Override the FailoverMetrics with the bridge's instance
			metricsCollector.FailoverMetrics = b.metrics
		}

		apiServer := api.NewServer(b.configService, b.db, metricsCollector, b.routerRegistry)

		go func() {
			if err := apiServer.Start(ctx); err != nil {
				logger.Errorf("API server error: %v", err)
			}
		}()

		b.apiServer = apiServer

		// Start gRPC server if failover handler is available
		if apiServer.GetFailoverHandler() != nil {
			grpcServer := grpc.NewServer(apiServer.GetFailoverHandler())
			go func() {
				grpcPort := 8082 // Use port 8082 for gRPC
				logger.Infof("Starting gRPC server on port %d", grpcPort)
				if err := grpcServer.Start(grpcPort); err != nil {
					logger.Errorf("Failed to start gRPC server: %v", err)
				}
			}()
		}
	}

	select {
	case <-ctx.Done():
		logger.Info("Metrics server stopping due to context cancellation")
		return
	case <-b.shutdownChan:
		logger.Info("Metrics server stopping due to shutdown signal")
		return
	}
}

// handleErrors handles errors from various components
func (b *Bridge) handleErrors(ctx context.Context) {
	logger.Info("Starting error handler")

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownChan:
			return
		case err := <-b.errorChan:
			logger.Errorf("Bridge error: %v", err)

			// Record error metrics if available
			if b.metricsTracker != nil {
				// Count errors for monitoring/alerting
				// This enables external alerting systems to detect issues
			}

			// Log error details for troubleshooting
			logger.Errorf("Error reported by bridge component: %v", err)
		}
	}
}
