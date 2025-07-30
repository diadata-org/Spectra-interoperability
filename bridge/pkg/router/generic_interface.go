package router

import (
	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// GenericRouterInterface defines the interface for generic event routing
type GenericRouterInterface interface {
	ID() string
	Type() string
	IsEnabled() bool
	ShouldRoute(eventName string, data *config.ExtractedData) (bool, string)
	GetDestinations(data *config.ExtractedData) []config.RouterDestination
	BuildUpdateRequest(eventName string, data *config.ExtractedData, dest config.RouterDestination) (*types.UpdateRequest, error)
	ProcessingConfig() *config.ProcessingConfig
	OnRouted(eventName string, data *config.ExtractedData)
	GetStats() RouterStats
}