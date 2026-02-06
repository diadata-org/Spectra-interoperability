package bridge

import (
	"context"
	"fmt"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/processor"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// manages the scanner -> processor -> updates pipeline.
type EventSource struct {
	scanner    BlockScanner
	processor  *processor.GenericEventProcessor
	eventChan  chan *bridgetypes.EventData
	updateChan chan *bridgetypes.UpdateRequest
	errorChan  chan error
}

// NewEventSource creates a new event source
func NewEventSource(
	scanner BlockScanner,
	processor *processor.GenericEventProcessor,
	eventChan chan *bridgetypes.EventData,
	updateChan chan *bridgetypes.UpdateRequest,
	errorChan chan error,
) *EventSource {
	return &EventSource{
		scanner:    scanner,
		processor:  processor,
		eventChan:  eventChan,
		updateChan: updateChan,
		errorChan:  errorChan,
	}
}

// Start starts the event source components
func (s *EventSource) Start(ctx context.Context) error {
	// Start scanner if available
	if s.scanner != nil {
		if err := s.scanner.Start(ctx); err != nil {
			return fmt.Errorf("failed to start scanner: %w", err)
		}
		logger.Info("Event scanner started")
	}

	if s.processor != nil {
		if err := s.processor.Start(ctx); err != nil {
			if s.scanner != nil {
				s.scanner.Stop()
			}
			return fmt.Errorf("failed to start processor: %w", err)
		}
		logger.Info("Event processor started")
	}

	logger.Info("EventSource started successfully")
	return nil
}

// Stop stops the event source components
func (s *EventSource) Stop(ctx context.Context) error {
	logger.Info("Stopping EventSource...")

	if s.scanner != nil {
		if err := s.scanner.Stop(); err != nil {
			logger.Errorf("Error stopping scanner: %v", err)
			return err
		}
		logger.Info("Event scanner stopped")
	}

	logger.Info("EventSource stopped successfully")
	return nil
}

// GetUpdateChan returns the update channel for consuming processed events
func (s *EventSource) GetUpdateChan() <-chan *bridgetypes.UpdateRequest {
	return s.updateChan
}

// GetQueueSize returns the current size of the update queue
func (s *EventSource) GetQueueSize() int {
	return len(s.updateChan)
}
