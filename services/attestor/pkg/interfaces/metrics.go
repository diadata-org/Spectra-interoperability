package interfaces

import "time"

// MetricsCollector defines the interface for collecting metrics
type MetricsCollector interface {
	// RecordIntentCreated records when an intent is created
	RecordIntentCreated(symbol string, success bool)

	// RecordIntentPublished records when an intent is published
	RecordIntentPublished(symbol string, success bool)

	// RecordProcessingDuration records the duration of processing
	RecordProcessingDuration(symbol string, mode string, duration time.Duration)

	// RecordOracleFetchDuration records the duration of oracle fetches
	RecordOracleFetchDuration(symbol string, duration time.Duration)
}
