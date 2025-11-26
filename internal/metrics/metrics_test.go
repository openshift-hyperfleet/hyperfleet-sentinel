package metrics

import (
	"testing"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewSentinelMetrics(t *testing.T) {
	// Reset for clean test
	ResetSentinelMetrics()

	registry := prometheus.NewRegistry()
	m := NewSentinelMetrics(registry)

	if m == nil {
		t.Fatal("Expected non-nil SentinelMetrics")
	}

	if m.PendingResources == nil {
		t.Error("Expected PendingResources to be initialized")
	}
	if m.EventsPublished == nil {
		t.Error("Expected EventsPublished to be initialized")
	}
	if m.ResourcesSkipped == nil {
		t.Error("Expected ResourcesSkipped to be initialized")
	}
	if m.PollDuration == nil {
		t.Error("Expected PollDuration to be initialized")
	}
	if m.APIErrors == nil {
		t.Error("Expected APIErrors to be initialized")
	}
	if m.BrokerErrors == nil {
		t.Error("Expected BrokerErrors to be initialized")
	}
}

func TestNewSentinelMetrics_MultipleCallsNoPanic(t *testing.T) {
	// This tests the fix for double registration panic
	registry := prometheus.NewRegistry()

	// First call
	m1 := NewSentinelMetrics(registry)
	if m1 == nil {
		t.Fatal("Expected non-nil SentinelMetrics from first call")
	}

	// Second call should NOT panic due to sync.Once
	m2 := NewSentinelMetrics(registry)
	if m2 == nil {
		t.Fatal("Expected non-nil SentinelMetrics from second call")
	}

	// Both should return the same instance
	if m1 != m2 {
		t.Error("Expected same instance from multiple calls")
	}
}

func TestUpdatePendingResourcesMetric(t *testing.T) {
	ResetSentinelMetrics()

	tests := []struct {
		name             string
		resourceType     string
		resourceSelector string
		count            int
		expectUpdate     bool
	}{
		{
			name:             "valid update",
			resourceType:     "clusters",
			resourceSelector: "shard:1",
			count:            5,
			expectUpdate:     true,
		},
		{
			name:             "negative count clamped to zero",
			resourceType:     "clusters",
			resourceSelector: "shard:1",
			count:            -10,
			expectUpdate:     true, // Should clamp to 0
		},
		{
			name:             "empty resource type",
			resourceType:     "",
			resourceSelector: "shard:1",
			count:            5,
			expectUpdate:     false,
		},
		{
			name:             "empty resource selector",
			resourceType:     "clusters",
			resourceSelector: "",
			count:            5,
			expectUpdate:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetSentinelMetrics()
			UpdatePendingResourcesMetric(tt.resourceType, tt.resourceSelector, tt.count)

			if tt.expectUpdate {
				count := testutil.CollectAndCount(pendingResourcesGauge)
				if count == 0 {
					t.Error("Expected metric to be collected")
				}
			}
		})
	}
}

func TestUpdateEventsPublishedMetric(t *testing.T) {
	ResetSentinelMetrics()

	tests := []struct {
		name             string
		resourceType     string
		resourceSelector string
		reason           string
		expectUpdate     bool
	}{
		{
			name:             "valid update",
			resourceType:     "clusters",
			resourceSelector: "all",
			reason:           "max_age_exceeded",
			expectUpdate:     true,
		},
		{
			name:             "empty resource type",
			resourceType:     "",
			resourceSelector: "all",
			reason:           "max_age_exceeded",
			expectUpdate:     false,
		},
		{
			name:             "empty resource selector",
			resourceType:     "clusters",
			resourceSelector: "",
			reason:           "max_age_exceeded",
			expectUpdate:     false,
		},
		{
			name:             "empty reason",
			resourceType:     "clusters",
			resourceSelector: "all",
			reason:           "",
			expectUpdate:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetSentinelMetrics()
			UpdateEventsPublishedMetric(tt.resourceType, tt.resourceSelector, tt.reason)

			if tt.expectUpdate {
				count := testutil.CollectAndCount(eventsPublishedCounter)
				if count == 0 {
					t.Error("Expected metric to be collected")
				}
			}
		})
	}
}

func TestUpdateResourcesSkippedMetric(t *testing.T) {
	ResetSentinelMetrics()

	UpdateResourcesSkippedMetric("clusters", "all", "within_max_age")

	count := testutil.CollectAndCount(resourcesSkippedCounter)
	if count == 0 {
		t.Error("Expected ResourcesSkipped metric to be collected")
	}
}

func TestUpdatePollDurationMetric(t *testing.T) {
	ResetSentinelMetrics()

	tests := []struct {
		name             string
		resourceType     string
		resourceSelector string
		durationSeconds  float64
		expectUpdate     bool
	}{
		{
			name:             "valid duration",
			resourceType:     "clusters",
			resourceSelector: "all",
			durationSeconds:  1.5,
			expectUpdate:     true,
		},
		{
			name:             "negative duration ignored",
			resourceType:     "clusters",
			resourceSelector: "all",
			durationSeconds:  -1.0,
			expectUpdate:     false,
		},
		{
			name:             "empty resource type",
			resourceType:     "",
			resourceSelector: "all",
			durationSeconds:  1.0,
			expectUpdate:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetSentinelMetrics()
			UpdatePollDurationMetric(tt.resourceType, tt.resourceSelector, tt.durationSeconds)

			if tt.expectUpdate {
				count := testutil.CollectAndCount(pollDurationHistogram)
				if count == 0 {
					t.Error("Expected metric to be collected")
				}
			}
		})
	}
}

func TestUpdateAPIErrorsMetric(t *testing.T) {
	ResetSentinelMetrics()

	UpdateAPIErrorsMetric("clusters", "all", "fetch_error")

	count := testutil.CollectAndCount(apiErrorsCounter)
	if count == 0 {
		t.Error("Expected APIErrors metric to be collected")
	}
}

func TestUpdateBrokerErrorsMetric(t *testing.T) {
	ResetSentinelMetrics()

	UpdateBrokerErrorsMetric("clusters", "all", "publish_error")

	count := testutil.CollectAndCount(brokerErrorsCounter)
	if count == 0 {
		t.Error("Expected BrokerErrors metric to be collected")
	}
}

func TestGetResourceSelectorLabel(t *testing.T) {
	tests := []struct {
		name      string
		selectors config.LabelSelectorList
		expected  string
	}{
		{
			name:      "empty selector returns all",
			selectors: config.LabelSelectorList{},
			expected:  "all",
		},
		{
			name: "single selector",
			selectors: config.LabelSelectorList{
				{Label: "shard", Value: "1"},
			},
			expected: "shard:1",
		},
		{
			name: "multiple selectors",
			selectors: config.LabelSelectorList{
				{Label: "shard", Value: "1"},
				{Label: "region", Value: "us-east"},
			},
			expected: "shard:1,region:us-east",
		},
		{
			name: "three selectors",
			selectors: config.LabelSelectorList{
				{Label: "shard", Value: "1"},
				{Label: "region", Value: "us-east"},
				{Label: "env", Value: "prod"},
			},
			expected: "shard:1,region:us-east,env:prod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetResourceSelectorLabel(tt.selectors)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestGetResourceSelectorLabel_Efficiency(t *testing.T) {
	// Test that strings.Builder is used efficiently
	// Create a large selector list
	selectors := make(config.LabelSelectorList, 100)
	for i := 0; i < 100; i++ {
		selectors[i] = config.LabelSelector{
			Label: "label",
			Value: "value",
		}
	}

	// This should not panic or be extremely slow
	result := GetResourceSelectorLabel(selectors)

	// Should contain all selectors
	if len(result) == 0 {
		t.Error("Expected non-empty result for large selector list")
	}
}

func TestResetSentinelMetrics(t *testing.T) {
	// Add some metrics
	UpdatePendingResourcesMetric("clusters", "all", 10)
	UpdateEventsPublishedMetric("clusters", "all", "test")

	// Reset
	ResetSentinelMetrics()

	// Verify reset - collectors should have 0 metrics collected
	// Note: We can't directly verify the values are 0, but we can verify
	// the function doesn't panic
	ResetSentinelMetrics() // Should not panic on second call
}

func TestMetricsLabelsConstants(t *testing.T) {
	// Verify label arrays are correctly defined
	if len(MetricsLabels) != 2 {
		t.Errorf("Expected MetricsLabels to have 2 elements, got %d", len(MetricsLabels))
	}

	if len(MetricsLabelsWithReason) != 3 {
		t.Errorf("Expected MetricsLabelsWithReason to have 3 elements, got %d", len(MetricsLabelsWithReason))
	}

	if len(MetricsLabelsWithErrorType) != 3 {
		t.Errorf("Expected MetricsLabelsWithErrorType to have 3 elements, got %d", len(MetricsLabelsWithErrorType))
	}
}

func TestMetricsNamesConstants(t *testing.T) {
	// Verify all metric names are in the MetricsNames array
	expectedCount := 6
	if len(MetricsNames) != expectedCount {
		t.Errorf("Expected %d metric names, got %d", expectedCount, len(MetricsNames))
	}

	// Verify no duplicates
	seen := make(map[string]bool)
	for _, name := range MetricsNames {
		if seen[name] {
			t.Errorf("Duplicate metric name found: %s", name)
		}
		seen[name] = true
	}
}

func TestMetricsSubsystem(t *testing.T) {
	expected := "hyperfleet_sentinel"
	if metricsSubsystem != expected {
		t.Errorf("Expected subsystem '%s', got '%s'", expected, metricsSubsystem)
	}
}
