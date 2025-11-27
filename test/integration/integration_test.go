//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/golang/glog"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/sentinel"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
)

// TestMain provides centralized setup/teardown for integration tests
func TestMain(m *testing.M) {
	flag.Parse()
	glog.Infof("Starting integration test using go version %s", runtime.Version())

	// Initialize shared test helper (creates RabbitMQ container once)
	helper := NewHelper()

	// Run all tests
	exitCode := m.Run()

	// Cleanup shared resources
	helper.Teardown()

	glog.Infof("Integration tests completed with exit code %d", exitCode)
	os.Exit(exitCode)
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

	if len(labels) > 0 {
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

// TestIntegration_EndToEnd tests the full Sentinel workflow end-to-end with real RabbitMQ
func TestIntegration_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared RabbitMQ testcontainer from helper
	helper := NewHelper()

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

	// Setup components with real RabbitMQ broker
	hyperfleetClient := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	log := logger.NewHyperFleetLogger(ctx)

	// Create metrics with a test registry
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		PollInterval:   100 * time.Millisecond, // Short interval for testing
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := sentinel.NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, helper.RabbitMQ.Publisher(), log)

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

	// Integration test validates end-to-end workflow without errors
	// Event verification requires subscriber implementation (future enhancement)
	t.Log("Integration test with real RabbitMQ broker completed successfully")
}

// TestIntegration_LabelSelectorFiltering tests resource filtering with label selectors and real RabbitMQ
func TestIntegration_LabelSelectorFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared RabbitMQ testcontainer from helper
	helper := NewHelper()

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

	// Setup components with real RabbitMQ broker
	hyperfleetClient := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	log := logger.NewHyperFleetLogger(ctx)

	// Create metrics with a test registry
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

	s := sentinel.NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, helper.RabbitMQ.Publisher(), log)

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

	// Integration test validates label selector filtering works end-to-end
	// Event verification requires subscriber implementation (future enhancement)
	t.Log("Label selector filtering test with real RabbitMQ broker completed successfully")
}
