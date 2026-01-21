package metrics

// Queue metric methods

func (c *Collector) SetQueueLength(queueKey string, length int) {
	c.queueLength.WithLabelValues(queueKey).Set(float64(length))
}

func (c *Collector) ObserveQueueWaitDuration(queueKey string, duration float64) {
	c.queueWaitDuration.WithLabelValues(queueKey).Observe(duration)
}

func (c *Collector) ObserveQueueProcessingDuration(queueKey string, duration float64) {
	c.queueProcessingDuration.WithLabelValues(queueKey).Observe(duration)
}
