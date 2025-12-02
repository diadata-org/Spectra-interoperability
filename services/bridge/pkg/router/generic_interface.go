package router

import (
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
)

// GenericRouterInterface defines the interface for generic event routing
type GenericRouterInterface interface {
	ID() string
	Type() string
	IsEnabled() bool
	ShouldRoute(eventName string, data *config.ExtractedData) (bool, string)
	GetDestinations(data *config.ExtractedData) []config.RouterDestination
	FilterDestinationsByTimeThreshold(destinations []config.RouterDestination, data *config.ExtractedData, intentHash string) []config.RouterDestination
	ProcessingConfig() *config.ProcessingConfig
	OnRouted(eventName string, data *config.ExtractedData)
	GetStats() GenericRouterStats
	UpdateDestinationTime(dest config.RouterDestination, symbol string, data ...*config.ExtractedData)
	GetSymbolFromData(data *config.ExtractedData) string
}
