//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/sentinel"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// MockPublisher implements broker.Publisher for integration testing
type MockPublisher struct {
	publishedEvents []*cloudevents.Event
	publishedTopics []string
	publishError    error
}

func (m *MockPublisher) Publish(topic string, event *cloudevents.Event) error {
	if m.publishError != nil {
		return m.publishError
	}
	m.publishedEvents = append(m.publishedEvents, event)
	m.publishedTopics = append(m.publishedTopics, topic)
	return nil
}

func (m *MockPublisher) Close() error {
	return nil
}

// createMockCluster creates a mock cluster response
func createMockCluster(id string, generation int, observedGeneration int, phase string, lastUpdated time.Time) map[string]interface{} {
	return createMockClusterWithLabels(id, generation, observedGeneration, phase, lastUpdated, nil)
}

// createMockClusterWithLabels creates a mock cluster response with labels
func createMockClusterWithLabels(id string, generation int, observedGeneration int, phase string, lastUpdated time.Time, labels map[string]string) map[string]interface{} {
	cluster := map[string]interface{}{
		"id":         id,
		"href":       "/api/hyperfleet/v1/clusters/" + id,
		"kind":       "Cluster",
		"name":       id,
		"generation": generation,
		"created_at": "2025-01-01T09:00:00Z",
		"updated_at": "2025-01-01T10:00:00Z",
		"created_by": "test-user",
		"updated_by": "test-user",
		"spec":       map[string]interface{}{},
		"status": map[string]interface{}{
			"phase":                phase,
			"last_transition_time": "2025-01-01T10:00:00Z",
			"updated_at":           lastUpdated.Format(time.RFC3339),
			"observed_generation":  observedGeneration,
			"adapters":             []interface{}{},
		},
	}

	if labels != nil && len(labels) > 0 {
		cluster["labels"] = labels
	}

	return cluster
}

// createMockClusterList creates a mock ClusterList response
func createMockClusterList(clusters []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"kind":  "ClusterList",
		"page":  1,
		"size":  len(clusters),
		"total": len(clusters),
		"items": clusters,
	}
}

// TestIntegration_EndToEnd tests the full Sentinel workflow end-to-end
func TestIntegration_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now()

	// Create mock HyperFleet API server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// First call: Return clusters with one needing reconciliation
		if callCount == 1 {
			clusters := []map[string]interface{}{
				createMockCluster("cluster-1", 2, 2, "Ready", now.Add(-31*time.Minute)), // Max age exceeded
				createMockCluster("cluster-2", 1, 1, "Ready", now.Add(-5*time.Minute)),  // Within max age
			}
			response := createMockClusterList(clusters)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		// Subsequent calls: Empty list
		response := createMockClusterList([]map[string]interface{}{})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger(ctx)

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		PollInterval:   100 * time.Millisecond, // Short interval for testing
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := sentinel.NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, mockPublisher, log)

	// Run Sentinel in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start(ctx)
	}()

	// Wait for at least 2 polling cycles
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Wait for Sentinel to stop
	select {
	case err := <-errChan:
		if err != nil && err != context.Canceled {
			t.Fatalf("Sentinel failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sentinel did not stop within timeout")
	}

	// Verify events were published
	if len(mockPublisher.publishedEvents) == 0 {
		t.Fatal("Expected at least 1 event to be published")
	}

	// Verify first event properties
	event := mockPublisher.publishedEvents[0]
	if event.Type() != "com.redhat.hyperfleet.Cluster.reconcile" {
		t.Errorf("Expected event type 'com.redhat.hyperfleet.Cluster.reconcile', got '%s'", event.Type())
	}
	if event.Source() != "hyperfleet-sentinel" {
		t.Errorf("Expected source 'hyperfleet-sentinel', got '%s'", event.Source())
	}

	// Verify metrics were collected
	metricCount := testutil.CollectAndCount(m.EventsPublished)
	if metricCount == 0 {
		t.Error("Expected EventsPublished metric to be collected")
	}

	metricCount = testutil.CollectAndCount(m.PollDuration)
	if metricCount == 0 {
		t.Error("Expected PollDuration metric to be collected")
	}
}

// TestIntegration_LabelSelectorFiltering tests resource filtering with label selectors
func TestIntegration_LabelSelectorFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	now := time.Now()

	// Create mock server that returns 2 clusters: one with shard:1, one with shard:2
	// Server implements server-side filtering based on search parameter
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// All available clusters
		allClusters := []map[string]interface{}{
			// This cluster has shard:1 - SHOULD match selector and trigger event
			createMockClusterWithLabels(
				"cluster-shard-1",
				2,
				2,
				"Ready",
				now.Add(-31*time.Minute), // Exceeds max_age_ready (30m)
				map[string]string{"shard": "1"},
			),
			// This cluster has shard:2 - should NOT match selector
			createMockClusterWithLabels(
				"cluster-shard-2",
				2,
				2,
				"Ready",
				now.Add(-31*time.Minute), // Also exceeds max_age, but should be filtered
				map[string]string{"shard": "2"},
			),
		}

		// Server-side filtering: check for search parameter
		searchParam := r.URL.Query().Get("search")
		filteredClusters := allClusters

		if searchParam != "" {
			// Parse search parameter (format: "key1=value1,key2=value2")
			filteredClusters = []map[string]interface{}{}
			for _, cluster := range allClusters {
				labels, ok := cluster["labels"].(map[string]string)
				if !ok {
					continue
				}

				// Simple matching: if search contains "shard=1", only include clusters with shard=1
				if searchParam == "shard=1" && labels["shard"] == "1" {
					filteredClusters = append(filteredClusters, cluster)
				}
			}
		}

		response := createMockClusterList(filteredClusters)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger(ctx)

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		PollInterval:   100 * time.Millisecond,
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		ResourceSelector: []config.LabelSelector{
			{Label: "shard", Value: "1"},
		},
	}

	s := sentinel.NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, mockPublisher, log)

	// Run sentinel in goroutine and capture error
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start(ctx)
	}()

	// Wait for a few polling cycles
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Check Start error
	startErr := <-errChan
	if startErr != nil && startErr != context.Canceled {
		t.Errorf("Expected Start to return nil or context.Canceled, got: %v", startErr)
	}

	// Verify at least 1 event was published
	// (multiple events are ok due to multiple polling cycles)
	if len(mockPublisher.publishedEvents) < 1 {
		t.Error("Expected at least 1 event to be published for cluster-shard-1")
	}

	// Verify ALL published events are for cluster-shard-1 (not cluster-shard-2)
	for i, event := range mockPublisher.publishedEvents {
		var eventData map[string]interface{}
		if err := event.DataAs(&eventData); err != nil {
			t.Fatalf("Failed to parse event data for event %d: %v", i, err)
		}

		resourceID, ok := eventData["resourceId"].(string)
		if !ok {
			t.Errorf("Event %d: Expected resourceId in event data", i)
		} else if resourceID != "cluster-shard-1" {
			t.Errorf("Event %d: Expected event for cluster-shard-1, got %s (label selector filtering failed)", i, resourceID)
		}
	}

	// Verify metrics were collected with correct labels
	metricCount := testutil.CollectAndCount(m.EventsPublished)
	if metricCount == 0 {
		t.Error("Expected EventsPublished metric to be collected")
	}
}
