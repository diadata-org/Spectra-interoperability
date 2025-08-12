package router

import (
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// GenericRouterWrapper wraps a GenericRouter to implement the legacy Router interface
type GenericRouterWrapper struct {
	*BaseRouter
	genericRouter *GenericRouter
}

// NewGenericRouterWrapper creates a new wrapper for GenericRouter
func NewGenericRouterWrapper(genericRouter *GenericRouter, destinations []Destination) *GenericRouterWrapper {
	baseRouter := NewBaseRouter(
		genericRouter.ID(),
		genericRouter.config.Name,
		destinations,
	)
	
	return &GenericRouterWrapper{
		BaseRouter:    baseRouter,
		genericRouter: genericRouter,
	}
}

// ShouldRoute determines if an intent should be routed using the generic router logic
func (grw *GenericRouterWrapper) ShouldRoute(intent *types.OracleIntent) (bool, string) {
	if !grw.IsEnabled() {
		return false, "router disabled"
	}

	grw.IncrementChecked()

	// For now, since we're dealing with intent-based routing, always route if enabled
	// This is a compatibility layer - the actual routing logic would be in the event processor
	grw.IncrementRouted()
	return true, "generic router accepts all intents when enabled"
}

// OnRouted is called after an intent has been successfully routed
func (grw *GenericRouterWrapper) OnRouted(intent *types.OracleIntent) {
	// Update last routed timestamp
	grw.stats.LastRouted = time.Now().Unix()
}