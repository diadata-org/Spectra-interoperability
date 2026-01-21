package processor

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/pkg/router"
)

// ParallelPipelineConfig configures parallel processing behavior
type ParallelPipelineConfig struct {
	EnableParallelEnrichment bool          `json:"enable_parallel_enrichment"`
	EnrichmentTimeout        time.Duration `json:"enrichment_timeout"`
	GasEstimationTimeout     time.Duration `json:"gas_estimation_timeout"`
	EnableGasPreEstimation   bool          `json:"enable_gas_pre_estimation"`
}

// DefaultParallelPipelineConfig returns sensible defaults
func DefaultParallelPipelineConfig() *ParallelPipelineConfig {
	return &ParallelPipelineConfig{
		EnableParallelEnrichment: true,
		EnrichmentTimeout:        20 * time.Second, // Shorter timeout for parallel
		GasEstimationTimeout:     10 * time.Second, // Shorter timeout for parallel
		EnableGasPreEstimation:   true,             // Pre-estimate gas for common operations
	}
}

// ParallelPipelineResult contains the results of parallel processing
type ParallelPipelineResult struct {
	ExtractedData     *config.ExtractedData
	EnrichmentError   error
	RoutingResults    []router.RoutingResult
	GasEstimates      map[string]uint64 // destination -> gas estimate
	GasEstimateErrors map[string]error  // destination -> estimation error
	ProcessingTime    time.Duration
}

// ParallelPipeline processes events with parallel enrichment and gas estimation
type ParallelPipeline struct {
	config       *ParallelPipelineConfig
	enricher     EnrichmentService
	gasEstimator GasEstimationService
	router       RoutingService

	// Statistics
	stats *ParallelPipelineStats
	mutex sync.RWMutex
}

// ParallelPipelineStats tracks parallel processing statistics
type ParallelPipelineStats struct {
	EventsProcessed      uint64
	EnrichmentSuccesses  uint64
	EnrichmentFailures   uint64
	GasEstimateSuccesses uint64
	GasEstimateFailures  uint64
	AverageEnrichTime    float64 // milliseconds
	AverageGasEstTime    float64 // milliseconds
	AverageParallelTime  float64 // milliseconds
	TotalTimesSaved      float64 // milliseconds saved by parallel processing
}

// Services interfaces for dependency injection
type EnrichmentService interface {
	EnrichEventData(ctx context.Context, eventName string, extractedData *config.ExtractedData) error
}

type GasEstimationService interface {
	EstimateGasForDestinations(ctx context.Context, event *types.EventData, destinations []config.RouterDestination) (map[string]uint64, map[string]error)
}

type RoutingService interface {
	RouteEvent(eventName string, extractedData *config.ExtractedData) []router.RoutingResult
}

// NewParallelPipeline creates a new parallel pipeline processor
func NewParallelPipeline(
	config *ParallelPipelineConfig,
	enricher EnrichmentService,
	gasEstimator GasEstimationService,
	router RoutingService,
) *ParallelPipeline {
	if config == nil {
		config = DefaultParallelPipelineConfig()
	}

	return &ParallelPipeline{
		config:       config,
		enricher:     enricher,
		gasEstimator: gasEstimator,
		router:       router,
		stats:        &ParallelPipelineStats{},
	}
}

// ProcessEventParallel processes an event with parallel enrichment and gas estimation
func (pp *ParallelPipeline) ProcessEventParallel(
	ctx context.Context,
	event *types.EventData,
	extractedData *config.ExtractedData,
) (*ParallelPipelineResult, error) {

	startTime := time.Now()

	result := &ParallelPipelineResult{
		ExtractedData:     extractedData,
		GasEstimates:      make(map[string]uint64),
		GasEstimateErrors: make(map[string]error),
	}

	// Step 1: Route events first to determine destinations (fast operation)
	routingResults := pp.router.RouteEvent(event.EventName, extractedData)
	result.RoutingResults = routingResults

	// Collect all destinations for gas estimation
	var allDestinations []config.RouterDestination
	for _, routeResult := range routingResults {
		if routeResult.Routed {
			allDestinations = append(allDestinations, routeResult.Destinations...)
		}
	}

	// Step 2: Run enrichment and gas estimation in parallel
	if pp.config.EnableParallelEnrichment && len(allDestinations) > 0 {
		err := pp.runParallelOperations(ctx, event, extractedData, allDestinations, result)
		if err != nil {
			return result, err
		}
	} else {
		// Fall back to sequential processing
		err := pp.runSequentialOperations(ctx, event, extractedData, allDestinations, result)
		if err != nil {
			return result, err
		}
	}

	result.ProcessingTime = time.Since(startTime)
	pp.updateStats(result)

	return result, nil
}

// runParallelOperations executes enrichment and gas estimation concurrently
func (pp *ParallelPipeline) runParallelOperations(
	ctx context.Context,
	event *types.EventData,
	extractedData *config.ExtractedData,
	destinations []config.RouterDestination,
	result *ParallelPipelineResult,
) error {

	// Create error group for parallel execution
	g, gctx := errgroup.WithContext(ctx)

	// Track timing for each operation
	var enrichmentTime, gasEstimationTime time.Duration

	// Parallel operation 1: Event enrichment
	g.Go(func() error {
		enrichStart := time.Now()
		defer func() {
			enrichmentTime = time.Since(enrichStart)
		}()

		// Create timeout context for enrichment
		enrichCtx, cancel := context.WithTimeout(gctx, pp.config.EnrichmentTimeout)
		defer cancel()

		err := pp.enricher.EnrichEventData(enrichCtx, event.EventName, extractedData)
		if err != nil {
			result.EnrichmentError = err
			logger.Warnf("Parallel enrichment failed for event %s: %v", event.EventName, err)
			// Don't return error - enrichment failure shouldn't stop gas estimation
		}
		return nil
	})

	// Parallel operation 2: Gas estimation for all destinations
	if pp.config.EnableGasPreEstimation && len(destinations) > 0 {
		g.Go(func() error {
			gasStart := time.Now()
			defer func() {
				gasEstimationTime = time.Since(gasStart)
			}()

			// Create timeout context for gas estimation
			gasCtx, cancel := context.WithTimeout(gctx, pp.config.GasEstimationTimeout)
			defer cancel()

			estimates, errors := pp.gasEstimator.EstimateGasForDestinations(gasCtx, event, destinations)
			result.GasEstimates = estimates
			result.GasEstimateErrors = errors

			// Log results
			for dest, estimate := range estimates {
				logger.Debugf("Pre-estimated gas for %s: %d", dest, estimate)
			}
			for dest, err := range errors {
				logger.Warnf("Gas estimation failed for %s: %v", dest, err)
			}

			return nil
		})
	}

	// Wait for all parallel operations to complete
	if err := g.Wait(); err != nil {
		return err
	}

	// Calculate time savings
	sequentialTime := enrichmentTime + gasEstimationTime
	parallelTime := max(enrichmentTime, gasEstimationTime)
	timeSaved := sequentialTime - parallelTime

	logger.Debugf("Parallel processing completed - Sequential: %v, Parallel: %v, Saved: %v",
		sequentialTime, parallelTime, timeSaved)

	return nil
}

// runSequentialOperations executes operations sequentially as fallback
func (pp *ParallelPipeline) runSequentialOperations(
	ctx context.Context,
	event *types.EventData,
	extractedData *config.ExtractedData,
	destinations []config.RouterDestination,
	result *ParallelPipelineResult,
) error {

	// Sequential enrichment
	if err := pp.enricher.EnrichEventData(ctx, event.EventName, extractedData); err != nil {
		result.EnrichmentError = err
		logger.Warnf("Sequential enrichment failed for event %s: %v", event.EventName, err)
	}

	// Sequential gas estimation
	if len(destinations) > 0 {
		estimates, errors := pp.gasEstimator.EstimateGasForDestinations(ctx, event, destinations)
		result.GasEstimates = estimates
		result.GasEstimateErrors = errors
	}

	return nil
}

// updateStats updates processing statistics
func (pp *ParallelPipeline) updateStats(result *ParallelPipelineResult) {
	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	pp.stats.EventsProcessed++

	if result.EnrichmentError == nil {
		pp.stats.EnrichmentSuccesses++
	} else {
		pp.stats.EnrichmentFailures++
	}

	gasSuccesses := 0
	gasFailures := 0
	for _, err := range result.GasEstimateErrors {
		if err == nil {
			gasSuccesses++
		} else {
			gasFailures++
		}
	}

	pp.stats.GasEstimateSuccesses += uint64(gasSuccesses)
	pp.stats.GasEstimateFailures += uint64(gasFailures)

	// Update average processing time (rolling average)
	processingTimeMs := float64(result.ProcessingTime) / 1e6
	pp.stats.AverageParallelTime = updateRollingAverage(pp.stats.AverageParallelTime, processingTimeMs, pp.stats.EventsProcessed)
}

// GetStats returns current parallel pipeline statistics
func (pp *ParallelPipeline) GetStats() *ParallelPipelineStats {
	pp.mutex.RLock()
	defer pp.mutex.RUnlock()

	// Return a copy to avoid race conditions
	statsCopy := *pp.stats
	return &statsCopy
}

// Helper functions

// updateRollingAverage calculates a rolling average
func updateRollingAverage(currentAvg, newValue float64, count uint64) float64 {
	if count == 1 {
		return newValue
	}
	// Weighted average with more weight on recent values
	weight := min(1.0/float64(count), 0.1) // Max 10% weight for stability
	return currentAvg*(1-weight) + newValue*weight
}

// max returns the maximum of two durations
func max(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// min returns the minimum of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
