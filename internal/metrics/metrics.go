package metrics

import (
	"context"
	"strings"
	"sync"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
)

// Subsystem used to define the metrics
const metricsSubsystem = "hyperfleet_sentinel"

// Names of the labels added to metrics
const (
	metricsResourceTypeLabel     = "resource_type"
	metricsResourceSelectorLabel = "resource_selector"
	metricsReasonLabel           = "reason"
	metricsErrorTypeLabel        = "error_type"
	metricsStatusLabel           = "status"
)

// MetricsLabels - Array of common labels added to most metrics
var MetricsLabels = []string{
	metricsResourceTypeLabel,
	metricsResourceSelectorLabel,
}

// MetricsLabelsWithReason - Array of labels for metrics that include reason
var MetricsLabelsWithReason = []string{
	metricsResourceTypeLabel,
	metricsResourceSelectorLabel,
	metricsReasonLabel,
}

// MetricsLabelsWithErrorType - Array of labels for error metrics
var MetricsLabelsWithErrorType = []string{
	metricsResourceTypeLabel,
	metricsResourceSelectorLabel,
	metricsErrorTypeLabel,
}

// Names of the metrics
const (
	pendingResourcesMetric = "pending_resources"
	eventsPublishedMetric  = "events_published_total"
	resourcesSkippedMetric = "resources_skipped_total"
	pollDurationMetric     = "poll_duration_seconds"
	apiErrorsMetric        = "api_errors_total"
	brokerErrorsMetric     = "broker_errors_total"
)

// MetricsNames - Array of names of the metrics
var MetricsNames = []string{
	pendingResourcesMetric,
	eventsPublishedMetric,
	resourcesSkippedMetric,
	pollDurationMetric,
	apiErrorsMetric,
	brokerErrorsMetric,
}

// Description of the pending resources metric
var pendingResourcesGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Subsystem: metricsSubsystem,
		Name:      pendingResourcesMetric,
		Help:      "Number of resources pending reconciliation based on max age or generation change",
	},
	MetricsLabels,
)

// Description of the events published metric
var eventsPublishedCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: metricsSubsystem,
		Name:      eventsPublishedMetric,
		Help:      "Total number of reconciliation events published to the message broker",
	},
	MetricsLabelsWithReason,
)

// Description of the resources skipped metric
var resourcesSkippedCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: metricsSubsystem,
		Name:      resourcesSkippedMetric,
		Help:      "Total number of resources skipped (preconditions not met or already reconciled)",
	},
	MetricsLabelsWithReason,
)

// Description of the poll duration metric
var pollDurationHistogram = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Subsystem: metricsSubsystem,
		Name:      pollDurationMetric,
		Help:      "Duration of each polling cycle in seconds",
		Buckets:   prometheus.DefBuckets,
	},
	MetricsLabels,
)

// Description of the API errors metric
var apiErrorsCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: metricsSubsystem,
		Name:      apiErrorsMetric,
		Help:      "Total number of errors when calling the HyperFleet API",
	},
	MetricsLabelsWithErrorType,
)

// Description of the broker errors metric
var brokerErrorsCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: metricsSubsystem,
		Name:      brokerErrorsMetric,
		Help:      "Total number of errors when publishing events to the message broker",
	},
	MetricsLabelsWithErrorType,
)

// SentinelMetrics holds all Prometheus metrics for the Sentinel service
type SentinelMetrics struct {
	// PendingResources tracks the number of resources pending reconciliation
	PendingResources *prometheus.GaugeVec

	// EventsPublished tracks the total number of events published to the broker
	EventsPublished *prometheus.CounterVec

	// ResourcesSkipped tracks resources that were skipped (preconditions not met)
	ResourcesSkipped *prometheus.CounterVec

	// PollDuration tracks the duration of each polling cycle
	PollDuration *prometheus.HistogramVec

	// APIErrors tracks errors when calling the HyperFleet API
	APIErrors *prometheus.CounterVec

	// BrokerErrors tracks errors when publishing to the message broker
	BrokerErrors *prometheus.CounterVec
}

var (
	metricsInstance *SentinelMetrics
	registerOnce    sync.Once
)

// NewSentinelMetrics creates and registers all Sentinel metrics.
// It uses sync.Once to ensure metrics are only registered once, preventing
// duplicate registration panics when called multiple times (e.g., in tests).
func NewSentinelMetrics(registry prometheus.Registerer) *SentinelMetrics {
	registerOnce.Do(func() {
		if registry == nil {
			registry = prometheus.DefaultRegisterer
		}

		// Register all metrics
		registry.MustRegister(pendingResourcesGauge)
		registry.MustRegister(eventsPublishedCounter)
		registry.MustRegister(resourcesSkippedCounter)
		registry.MustRegister(pollDurationHistogram)
		registry.MustRegister(apiErrorsCounter)
		registry.MustRegister(brokerErrorsCounter)

		metricsInstance = &SentinelMetrics{
			PendingResources: pendingResourcesGauge,
			EventsPublished:  eventsPublishedCounter,
			ResourcesSkipped: resourcesSkippedCounter,
			PollDuration:     pollDurationHistogram,
			APIErrors:        apiErrorsCounter,
			BrokerErrors:     brokerErrorsCounter,
		}
	})

	return metricsInstance
}

// ResetSentinelMetrics resets all metric collectors to their initial state.
//
// This function is intended for testing purposes only. It clears all metric values
// across all collectors (gauges, counters, histograms) without unregistering them.
//
// WARNING: Do not use in production code. This will clear operational metrics.
//
// Thread-safe: Safe to call concurrently, but should only be called from test code.
func ResetSentinelMetrics() {
	pendingResourcesGauge.Reset()
	eventsPublishedCounter.Reset()
	resourcesSkippedCounter.Reset()
	pollDurationHistogram.Reset()
	apiErrorsCounter.Reset()
	brokerErrorsCounter.Reset()
}

// UpdatePendingResourcesMetric sets the current number of resources pending reconciliation.
//
// This gauge metric tracks resources that need reconciliation based on max age intervals
// or generation mismatches. The count is set (not incremented) and represents the current
// snapshot of pending resources.
//
// Parameters:
//   - resourceType: Type of resource (e.g., "clusters", "nodepools")
//   - resourceSelector: Label selector string (e.g., "shard:1" or "all")
//   - count: Number of pending resources (negative values are clamped to 0)
//
// Thread-safe: Can be called concurrently from multiple goroutines.
//
// Validation: Empty resourceType or resourceSelector trigger a warning and are ignored to prevent
// cardinality issues. This should never happen in normal operation and indicates a bug.
func UpdatePendingResourcesMetric(resourceType, resourceSelector string, count int) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" {
		log := logger.NewHyperFleetLogger()
		log.Warningf(context.Background(), "Attempted to update pending_resources metric with empty parameters: resourceType=%q resourceSelector=%q", resourceType, resourceSelector)
		return
	}
	if count < 0 {
		count = 0
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
	}
	pendingResourcesGauge.With(labels).Set(float64(count))
}

// UpdateEventsPublishedMetric increments the counter of reconciliation events published to the broker.
//
// This counter tracks successful event publications, labeled by resource type, selector, and reason.
// Common reasons include "max_age_exceeded" and "generation_mismatch".
//
// Parameters:
//   - resourceType: Type of resource (e.g., "clusters", "nodepools")
//   - resourceSelector: Label selector string (e.g., "shard:1" or "all")
//   - reason: Reason for publishing (e.g., "max_age_exceeded", "generation_mismatch")
//
// Thread-safe: Can be called concurrently from multiple goroutines.
//
// Validation: Empty parameters trigger a warning and are ignored to prevent cardinality issues.
// This should never happen in normal operation and indicates a bug.
func UpdateEventsPublishedMetric(resourceType, resourceSelector, reason string) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" || reason == "" {
		log := logger.NewHyperFleetLogger()
		log.Warningf(context.Background(), "Attempted to update events_published metric with empty parameters: resourceType=%q resourceSelector=%q reason=%q", resourceType, resourceSelector, reason)
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
		metricsReasonLabel:           reason,
	}
	eventsPublishedCounter.With(labels).Inc()
}

// UpdateResourcesSkippedMetric increments the counter of resources that were skipped during evaluation.
//
// Resources are skipped when they don't meet the criteria for publishing events, such as
// being recently updated (within max age) or having matching observed generation.
// Common reasons include "within_max_age" and "generation_match".
//
// Parameters:
//   - resourceType: Type of resource (e.g., "clusters", "nodepools")
//   - resourceSelector: Label selector string (e.g., "shard:1" or "all")
//   - reason: Reason for skipping (e.g., "within_max_age", "generation_match")
//
// Thread-safe: Can be called concurrently from multiple goroutines.
//
// Validation: Empty parameters trigger a warning and are ignored to prevent cardinality issues.
// This should never happen in normal operation and indicates a bug.
func UpdateResourcesSkippedMetric(resourceType, resourceSelector, reason string) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" || reason == "" {
		log := logger.NewHyperFleetLogger()
		log.Warningf(context.Background(), "Attempted to update resources_skipped metric with empty parameters: resourceType=%q resourceSelector=%q reason=%q", resourceType, resourceSelector, reason)
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
		metricsReasonLabel:           reason,
	}
	resourcesSkippedCounter.With(labels).Inc()
}

// UpdatePollDurationMetric records the duration of a polling cycle in seconds.
//
// This histogram metric tracks how long each polling cycle takes, including API calls,
// decision evaluation, and event publishing. Useful for identifying performance issues
// and API latency.
//
// Parameters:
//   - resourceType: Type of resource (e.g., "clusters", "nodepools")
//   - resourceSelector: Label selector string (e.g., "shard:1" or "all")
//   - durationSeconds: Duration in seconds (negative values trigger a warning and are ignored)
//
// Thread-safe: Can be called concurrently from multiple goroutines.
//
// Validation: Empty resourceType/resourceSelector or negative duration trigger a warning and are
// ignored to prevent invalid metrics. This should never happen in normal operation and indicates a bug.
func UpdatePollDurationMetric(resourceType, resourceSelector string, durationSeconds float64) {
	log := logger.NewHyperFleetLogger()
	// Validate inputs
	if resourceType == "" || resourceSelector == "" {
		log.Warningf(context.Background(), "Attempted to update poll_duration metric with empty parameters: resourceType=%q resourceSelector=%q", resourceType, resourceSelector)
		return
	}
	if durationSeconds < 0 {
		log.Warningf(context.Background(), "Attempted to update poll_duration metric with negative duration: %f", durationSeconds)
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
	}
	pollDurationHistogram.With(labels).Observe(durationSeconds)
}

// UpdateAPIErrorsMetric increments the counter of errors when calling the HyperFleet API.
//
// Tracks API errors by type to help diagnose connectivity and availability issues.
// Common error types include "fetch_error", "timeout", "auth_error".
//
// Parameters:
//   - resourceType: Type of resource (e.g., "clusters", "nodepools")
//   - resourceSelector: Label selector string (e.g., "shard:1" or "all")
//   - errorType: Type of error (e.g., "fetch_error", "timeout", "auth_error")
//
// Thread-safe: Can be called concurrently from multiple goroutines.
//
// Validation: Empty parameters trigger a warning and are ignored to prevent cardinality issues.
// This should never happen in normal operation and indicates a bug.
func UpdateAPIErrorsMetric(resourceType, resourceSelector, errorType string) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" || errorType == "" {
		log := logger.NewHyperFleetLogger()
		log.Warningf(context.Background(), "Attempted to update api_errors metric with empty parameters: resourceType=%q resourceSelector=%q errorType=%q", resourceType, resourceSelector, errorType)
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
		metricsErrorTypeLabel:        errorType,
	}
	apiErrorsCounter.With(labels).Inc()
}

// UpdateBrokerErrorsMetric increments the counter of errors when publishing events to the message broker.
//
// Tracks broker errors by type to help diagnose message delivery and broker connectivity issues.
// Common error types include "publish_error", "connection_error", "timeout".
//
// Parameters:
//   - resourceType: Type of resource (e.g., "clusters", "nodepools")
//   - resourceSelector: Label selector string (e.g., "shard:1" or "all")
//   - errorType: Type of error (e.g., "publish_error", "connection_error")
//
// Thread-safe: Can be called concurrently from multiple goroutines.
//
// Validation: Empty parameters trigger a warning and are ignored to prevent cardinality issues.
// This should never happen in normal operation and indicates a bug.
func UpdateBrokerErrorsMetric(resourceType, resourceSelector, errorType string) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" || errorType == "" {
		log := logger.NewHyperFleetLogger()
		log.Warningf(context.Background(), "Attempted to update broker_errors metric with empty parameters: resourceType=%q resourceSelector=%q errorType=%q", resourceType, resourceSelector, errorType)
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
		metricsErrorTypeLabel:        errorType,
	}
	brokerErrorsCounter.With(labels).Inc()
}

// GetResourceSelectorLabel converts resource selector to a single label value.
// Empty selector returns "all", otherwise returns comma-separated label:value pairs.
// Uses strings.Builder for efficient string concatenation.
func GetResourceSelectorLabel(selectors config.LabelSelectorList) string {
	if len(selectors) == 0 {
		return "all"
	}

	var builder strings.Builder
	for i, selector := range selectors {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(selector.Label)
		builder.WriteString(":")
		builder.WriteString(selector.Value)
	}
	return builder.String()
}
