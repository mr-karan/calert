// Package metrics contains wrapper around VictoriaMetrics
// functions to interact with counters, gauges etc. It also has functions
// to write the metrics output to to an `io.Writer` interface.
package metrics

import (
	"fmt"
	"io"
	"time"

	"github.com/VictoriaMetrics/metrics"
)

// Manager represents options for storing metrics.
type Manager struct {
	metrics   *metrics.Set
	namespace string // Optional string to prepend all label names.
	startTime time.Time
}

// New returns a new configured instance of Manager.
func New(ns string) *Manager {
	return &Manager{
		metrics:   metrics.NewSet(),
		namespace: ns,
		startTime: time.Now(),
	}
}

// Increment the counter for the corresponding key.
// This is used for Counter metric type.
func (s *Manager) Increment(label string) {
	s.metrics.GetOrCreateCounter(s.getFormattedLabel(label)).Inc()
}

// Decrement the counter for the corresponding key.
// This is used for Counter metric type.
func (s *Manager) Decrement(label string) {
	s.metrics.GetOrCreateCounter(s.getFormattedLabel(label)).Dec()
}

// Duration updates the key with time delta value of `startTime`.
// This is used for Histogram metric type.
func (s *Manager) Duration(label string, startTime time.Time) {
	s.metrics.GetOrCreateHistogram(s.getFormattedLabel(label)).UpdateDuration(startTime)
}

// Set updates the key with a float64 value.
// This is used for Gauge metric type.
func (s *Manager) Set(label string, val float64) {
	s.metrics.GetOrCreateFloatCounter(s.getFormattedLabel(label)).Set(val)
}

// FlushMetrics writes the metrics data from the internal store
// to the buffer.
func (s *Manager) FlushMetrics(buf io.Writer) {
	metrics.WriteProcessMetrics(buf)
	s.metrics.WritePrometheus(buf)

	// Export start time and uptime in seconds
	fmt.Fprintf(buf, "calert_start_timestamp %d\n", s.startTime.Unix())
	fmt.Fprintf(buf, "calert_uptime_seconds %d\n", int(time.Since(s.startTime).Seconds()))
}

// getFormattedLabel prefixes the label with namespace (if non empty).
func (s *Manager) getFormattedLabel(l string) string {
	if s.namespace != "" {
		return s.namespace + "_" + l
	}
	return l
}
