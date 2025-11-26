package metrics

import (
	"strings"
	"sync"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
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

// MetricsLabelsConfigLoads - Array of labels for config loads metric
var MetricsLabelsConfigLoads = []string{
	metricsStatusLabel,
}

// Names of the metrics
const (
	pendingResourcesMetric    = "pending_resources"
	eventsPublishedMetric     = "events_published_total"
	resourcesSkippedMetric    = "resources_skipped_total"
	pollDurationMetric        = "poll_duration_seconds"
	apiErrorsMetric           = "api_errors_total"
	brokerErrorsMetric        = "broker_errors_total"
	configLoadsMetric         = "config_loads_total"
)

// MetricsNames - Array of names of the metrics
var MetricsNames = []string{
	pendingResourcesMetric,
	eventsPublishedMetric,
	resourcesSkippedMetric,
	pollDurationMetric,
	apiErrorsMetric,
	brokerErrorsMetric,
	configLoadsMetric,
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

// Description of the config loads metric
var configLoadsCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: metricsSubsystem,
		Name:      configLoadsMetric,
		Help:      "Total number of configuration load attempts",
	},
	MetricsLabelsConfigLoads,
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

	// ConfigLoads tracks configuration load attempts
	ConfigLoads *prometheus.CounterVec
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
		registry.MustRegister(configLoadsCounter)

		metricsInstance = &SentinelMetrics{
			PendingResources: pendingResourcesGauge,
			EventsPublished:  eventsPublishedCounter,
			ResourcesSkipped: resourcesSkippedCounter,
			PollDuration:     pollDurationHistogram,
			APIErrors:        apiErrorsCounter,
			BrokerErrors:     brokerErrorsCounter,
			ConfigLoads:      configLoadsCounter,
		}
	})

	return metricsInstance
}

// ResetSentinelMetrics resets all metric collectors for testing purposes
func ResetSentinelMetrics() {
	pendingResourcesGauge.Reset()
	eventsPublishedCounter.Reset()
	resourcesSkippedCounter.Reset()
	pollDurationHistogram.Reset()
	apiErrorsCounter.Reset()
	brokerErrorsCounter.Reset()
	configLoadsCounter.Reset()
}

// UpdatePendingResourcesMetric sets the number of pending resources
func UpdatePendingResourcesMetric(resourceType, resourceSelector string, count int) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" {
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

// UpdateEventsPublishedMetric increments the events published counter
func UpdateEventsPublishedMetric(resourceType, resourceSelector, reason string) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" || reason == "" {
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
		metricsReasonLabel:           reason,
	}
	eventsPublishedCounter.With(labels).Inc()
}

// UpdateResourcesSkippedMetric increments the resources skipped counter
func UpdateResourcesSkippedMetric(resourceType, resourceSelector, reason string) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" || reason == "" {
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
		metricsReasonLabel:           reason,
	}
	resourcesSkippedCounter.With(labels).Inc()
}

// UpdatePollDurationMetric records the poll duration
func UpdatePollDurationMetric(resourceType, resourceSelector string, durationSeconds float64) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" {
		return
	}
	if durationSeconds < 0 {
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
	}
	pollDurationHistogram.With(labels).Observe(durationSeconds)
}

// UpdateAPIErrorsMetric increments the API errors counter
func UpdateAPIErrorsMetric(resourceType, resourceSelector, errorType string) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" || errorType == "" {
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
		metricsErrorTypeLabel:        errorType,
	}
	apiErrorsCounter.With(labels).Inc()
}

// UpdateBrokerErrorsMetric increments the broker errors counter
func UpdateBrokerErrorsMetric(resourceType, resourceSelector, errorType string) {
	// Validate inputs
	if resourceType == "" || resourceSelector == "" || errorType == "" {
		return
	}

	labels := prometheus.Labels{
		metricsResourceTypeLabel:     resourceType,
		metricsResourceSelectorLabel: resourceSelector,
		metricsErrorTypeLabel:        errorType,
	}
	brokerErrorsCounter.With(labels).Inc()
}

// UpdateConfigLoadsMetric increments the config loads counter
func UpdateConfigLoadsMetric(status string) {
	// Validate inputs
	if status == "" {
		return
	}

	labels := prometheus.Labels{
		metricsStatusLabel: status,
	}
	configLoadsCounter.With(labels).Inc()
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
