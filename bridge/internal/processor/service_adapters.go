package processor

import (
	"context"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/pipeline"
	"github.com/diadata.org/Spectra-interoperability/bridge/pkg/router"
)

// EnrichmentServiceAdapter adapts pipeline.DataEnricher to EnrichmentService interface
type EnrichmentServiceAdapter struct {
	enricher *pipeline.DataEnricher
}

// NewEnrichmentServiceAdapter creates a new adapter for the data enricher
func NewEnrichmentServiceAdapter(enricher *pipeline.DataEnricher) *EnrichmentServiceAdapter {
	return &EnrichmentServiceAdapter{
		enricher: enricher,
	}
}

// EnrichEventData implements EnrichmentService interface
func (esa *EnrichmentServiceAdapter) EnrichEventData(ctx context.Context, eventName string, extractedData *config.ExtractedData) error {
	return esa.enricher.EnrichEventData(ctx, eventName, extractedData)
}

// RoutingServiceAdapter adapts router.GenericRegistry to RoutingService interface
type RoutingServiceAdapter struct {
	registry *router.GenericRegistry
}

// NewRoutingServiceAdapter creates a new adapter for the router registry
func NewRoutingServiceAdapter(registry *router.GenericRegistry) *RoutingServiceAdapter {
	return &RoutingServiceAdapter{
		registry: registry,
	}
}

// RouteEvent implements RoutingService interface
func (rsa *RoutingServiceAdapter) RouteEvent(eventName string, extractedData *config.ExtractedData) []router.RoutingResult {
	// Get routing results from the actual router
	return rsa.registry.RouteEvent(eventName, extractedData)
}