package router

import (
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// GenericRouterInterface defines the interface for generic event routing
type GenericRouterInterface interface {
	ID() string
	Type() string
	IsEnabled() bool
	ShouldRoute(eventName string, data *config.ExtractedData) (bool, string)
	GetDestinations(data *config.ExtractedData) []config.RouterDestination
	FilterDestinationsByTimeThreshold(destinations []config.RouterDestination, data *config.ExtractedData) []config.RouterDestination
	BuildUpdateRequest(eventName string, data *config.ExtractedData, dest config.RouterDestination) (*types.UpdateRequest, error)
	ProcessingConfig() *config.ProcessingConfig
	OnRouted(eventName string, data *config.ExtractedData)
	GetStats() GenericRouterStats
	UpdateDestinationTime(dest config.RouterDestination, symbol string, data ...*config.ExtractedData)
	GetSymbolFromData(data *config.ExtractedData) string
}
