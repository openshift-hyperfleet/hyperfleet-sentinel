//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

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
	log := logger.NewHyperFleetLogger()
	ctx := context.Background()
	log.Infof(ctx, "Starting integration test using go version %s", runtime.Version())

	// Initialize shared test helper (creates RabbitMQ container once)
	helper := NewHelper()

	// Run all tests
	exitCode := m.Run()

	// Cleanup shared resources
	helper.Teardown()

	log.Infof(ctx, "Integration tests completed with exit code %d", exitCode)
	os.Exit(exitCode)
}

// createMockCluster creates a mock cluster response
func createMockCluster(id string, generation int, observedGeneration int, ready bool, lastUpdated time.Time) map[string]interface{} {
	return createMockClusterWithLabels(id, generation, observedGeneration, ready, lastUpdated, nil)
}

// createMockClusterWithLabels creates a mock cluster response with labels
func createMockClusterWithLabels(id string, generation int, observedGeneration int, ready bool, lastUpdated time.Time, labels map[string]string) map[string]interface{} {
	// Map ready bool to condition status
	readyStatus := "False"
	if ready {
		readyStatus = "True"
	}

	readyCondition := map[string]interface{}{
		"type":                 "Ready",
		"status":               readyStatus,
		"created_time":         "2025-01-01T09:00:00Z",
		"last_transition_time": "2025-01-01T10:00:00Z",
		"last_updated_time":    lastUpdated.Format(time.RFC3339),
		"observed_generation":  observedGeneration,
	}

	cluster := map[string]interface{}{
		"id":         id,
		"href":       "/api/hyperfleet/v1/clusters/" + id,
		"kind":       "Cluster",
		"name":       id,
		"generation": generation,
		"created_at": "2025-01-01T09:00:00Z",
		"updated_at": "2025-01-01T10:00:00Z",
		"created_by": "test-user@example.com",
		"updated_by": "test-user@example.com",
		"spec":       map[string]interface{}{},
		"status": map[string]interface{}{
			"conditions": []map[string]interface{}{
				readyCondition,
				{
					"type":                 "Available",
					"status":               readyStatus,
					"created_time":         "2025-01-01T09:00:00Z",
					"last_transition_time": "2025-01-01T10:00:00Z",
					"last_updated_time":    lastUpdated.Format(time.RFC3339),
					"observed_generation":  observedGeneration,
				},
			},
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
	pollCycleCount := 0

	// Create mock HyperFleet API server that handles condition-based queries
	// Each poll cycle makes 2 queries: not-ready + stale
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")

		var clusters []map[string]interface{}

		// Track poll cycles (stale query marks end of a cycle)
		if strings.Contains(search, "Ready='True'") {
			pollCycleCount++
		}

		switch {
		case strings.Contains(search, "Ready='False'"):
			// Not-ready query: no not-ready clusters
			clusters = nil
		case strings.Contains(search, "Ready='True'"):
			// Stale query: return stale cluster only on first cycle
			if pollCycleCount == 1 {
				clusters = []map[string]interface{}{
					createMockCluster("cluster-1", 2, 2, true, now.Add(-31*time.Minute)), // Max age exceeded
				}
			}
		}

		response := createMockClusterList(clusters)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup components with real RabbitMQ broker
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		PollInterval:   100 * time.Millisecond, // Short interval for testing
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := sentinel.NewSentinel(cfg, hyperfleetClient, decisionEngine, helper.RabbitMQ.Publisher(), log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Run Sentinel in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start(ctx)
	}()

	// Wait for at least 2 polling cycles
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Verify that Sentinel actually polled the API at least twice
	if pollCycleCount < 2 {
		t.Errorf("Expected at least 2 polling cycles, got %d", pollCycleCount)
	}

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

	// Create mock server that returns clusters filtered by label selector and conditions.
	// With condition-based queries, Sentinel sends:
	// - Not-ready query: "labels.shard='1' and status.conditions.Ready='False'"
	// - Stale query: "labels.shard='1' and status.conditions.Ready='True' and ..."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchParam := r.URL.Query().Get("search")
		var filteredClusters []map[string]interface{}

		// Only return clusters matching shard:1 label AND condition filters
		if strings.Contains(searchParam, "labels.shard='1'") {
			if strings.Contains(searchParam, "Ready='False'") {
				// Not-ready query: no not-ready clusters with shard:1
				filteredClusters = nil
			} else if strings.Contains(searchParam, "Ready='True'") {
				// Stale query: return stale cluster with shard:1
				filteredClusters = []map[string]interface{}{
					createMockClusterWithLabels(
						"cluster-shard-1",
						2, 2, true,
						now.Add(-31*time.Minute), // Exceeds max_age_ready (30m)
						map[string]string{"shard": "1"},
					),
				}
			}
		}
		// shard:2 clusters are never returned since label selector doesn't match

		response := createMockClusterList(filteredClusters)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup components with real RabbitMQ broker
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		PollInterval:   100 * time.Millisecond,
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		ResourceSelector: []config.LabelSelector{
			{Label: "shard", Value: "1"},
		},
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := sentinel.NewSentinel(cfg, hyperfleetClient, decisionEngine, helper.RabbitMQ.Publisher(), log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

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

	// Integration test validates label selector + condition filtering works end-to-end
	t.Log("Label selector filtering test with real RabbitMQ broker completed successfully")
}

// TestIntegration_TSLSyntaxMultipleLabels validates TSL syntax with multiple label selectors
// and condition-based filtering
func TestIntegration_TSLSyntaxMultipleLabels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared RabbitMQ testcontainer from helper
	helper := NewHelper()

	now := time.Now()

	// Track the search parameters received by the mock server
	var receivedSearchParams []string

	// Create mock server that validates TSL syntax with conditions
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")
		receivedSearchParams = append(receivedSearchParams, search)

		var filteredClusters []map[string]interface{}

		// With condition-based queries, labels are combined with condition filters
		labelPrefix := "labels.env='production' and labels.region='us-east'"
		if strings.HasPrefix(search, labelPrefix) {
			if strings.Contains(search, "Ready='False'") {
				// Not-ready query
				filteredClusters = nil
			} else if strings.Contains(search, "Ready='True'") {
				// Stale query: return matching cluster
				filteredClusters = []map[string]interface{}{
					createMockClusterWithLabels(
						"cluster-region-env-match",
						2, 2, true,
						now.Add(-31*time.Minute),
						map[string]string{"region": "us-east", "env": "production"},
					),
				}
			}
		}

		response := createMockClusterList(filteredClusters)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		PollInterval:   100 * time.Millisecond,
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		ResourceSelector: []config.LabelSelector{
			{Label: "region", Value: "us-east"},
			{Label: "env", Value: "production"},
		},
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := sentinel.NewSentinel(cfg, hyperfleetClient, decisionEngine, helper.RabbitMQ.Publisher(), log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Run sentinel
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start(ctx)
	}()

	// Wait for polling
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Validate that queries include both label selectors and condition filters
	labelPrefix := "labels.env='production' and labels.region='us-east'"
	for _, search := range receivedSearchParams {
		if !strings.HasPrefix(search, labelPrefix) {
			t.Errorf("Expected search to start with label selectors %q, got %q", labelPrefix, search)
		}
	}

	// Wait for sentinel to stop
	startErr := <-errChan
	if startErr != nil && startErr != context.Canceled {
		t.Errorf("Expected Start to return nil or context.Canceled, got: %v", startErr)
	}

	t.Logf("TSL syntax validation completed with %d queries", len(receivedSearchParams))
}

func TestIntegration_BrokerLoggerContext(t *testing.T) {
	const SENTINEL_COMPONENT = "sentinel"
	const TEST_VERSION = "test"
	const TEST_HOST = "testhost"
	const TEST_TOPIC = "test-topic"

	// Buffer to observe logs
	var logBuffer bytes.Buffer
	now := time.Now()
	callCount := 0
	readyChan := make(chan bool, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get globalConfig and assign multiWriter to observe output logs
	globalConfig := logger.GetGlobalConfig()
	multiWriter := io.MultiWriter(globalConfig.Output, &logBuffer)

	helper := NewHelper()
	cfg := &logger.LogConfig{
		Level:     logger.LevelInfo,
		Format:    logger.FormatJSON, // JSON for easy parsing
		Output:    multiWriter,
		Component: SENTINEL_COMPONENT,
		Version:   TEST_VERSION,
		Hostname:  TEST_HOST,
	}
	log := logger.NewHyperFleetLoggerWithConfig(cfg)

	// Mock server returns clusters that will trigger event publishing
	// With condition-based queries, each poll cycle makes 2 queries
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")

		var clusters []map[string]interface{}
		switch {
		case strings.Contains(search, "Ready='False'"):
			callCount++
			if callCount >= 2 {
				select {
				case readyChan <- true:
				default:
				}
			}
			// Not-ready query: return not-ready cluster
			clusters = []map[string]interface{}{
				createMockCluster("cluster-not-ready", 1, 1, false, now.Add(-15*time.Second)),
			}
		case strings.Contains(search, "Ready='True'"):
			// Stale query: return stale ready cluster
			clusters = []map[string]interface{}{
				createMockCluster("cluster-old", 2, 2, true, now.Add(-35*time.Minute)),
			}
		default:
			clusters = []map[string]interface{}{
				createMockCluster("cluster-old", 2, 2, true, now.Add(-35*time.Minute)),
				createMockCluster("cluster-not-ready", 1, 1, false, now.Add(-15*time.Second)),
			}
		}
		response := createMockClusterList(clusters)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)

	sentinelConfig := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          TEST_TOPIC,
		PollInterval:   100 * time.Millisecond,
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		ResourceSelector: []config.LabelSelector{
			{Label: "region", Value: "us-east"},
			{Label: "env", Value: "production"},
		},
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	// Create Sentinel with our logger and real RabbitMQ broker
	s, err := sentinel.NewSentinel(sentinelConfig, hyperfleetClient, decisionEngine, helper.RabbitMQ.Publisher(), log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Run Sentinel
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start(ctx)
	}()

	select {
	case <-readyChan:
		t.Log("Sentinel completed required polling cycles")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for sentinel polling")
	}
	cancel()

	// Wait for Sentinel to stop
	select {
	case err := <-errChan:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Sentinel failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sentinel did not stop within timeout")
	}

	// Analyze logs
	logOutput := logBuffer.String()
	t.Logf("Captured log output:\n%s", logOutput)

	logLines := strings.Split(strings.TrimSpace(logOutput), "\n")

	var foundSentinelEventLog bool
	var foundBrokerOperationLog bool

	for _, line := range logLines {
		if line == "" {
			continue
		}

		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Logf("Skipping non-JSON line: %s", line)
			continue
		}

		msg, hasMsg := entry["message"].(string)
		component, hasComponent := entry["component"].(string)

		// Look for Sentinel's own event publishing logs
		if hasMsg && hasComponent && component == SENTINEL_COMPONENT &&
			strings.Contains(msg, "Published event") {
			foundSentinelEventLog = true

			// Verify Sentinel context fields are present
			if entry["decision_reason"] == nil {
				t.Errorf("Sentinel event log missing decision_reason: %v", entry)
			}
			if entry["topic"] == nil {
				t.Errorf("Sentinel event log missing topic: %v", entry)
			}
			if entry["subset"] == nil {
				t.Errorf("Sentinel event log missing subset: %v", entry)
			}
			// TODO: Remove the commented lines as part of https://issues.redhat.com/browse/HYPERFLEET-542. We expect trace_id and span_id to be propagated once they're available
			//if entry["trace_id"] == nil {
			//	t.Errorf("Sentinel event log missing trace_id: %v", entry)
			//}
			//if entry["span_id"] == nil {
			//	t.Errorf("Sentinel event log missing span_id: %v", entry)
			//}

			t.Logf("Found Sentinel event log with context: decision_reason=%v topic=%v subset=%v",
				entry["decision_reason"], entry["topic"], entry["subset"])
		}

		// Look for broker operation logs (these should inherit Sentinel context)
		if hasMsg && hasComponent && component == SENTINEL_COMPONENT &&
			(strings.Contains(msg, "broker") || strings.Contains(msg, "publish") ||
				strings.Contains(msg, "Creating publisher") || strings.Contains(msg, "publisher initialized")) {
			foundBrokerOperationLog = true

			// Broker operations should have Sentinel context
			if entry["component"] != SENTINEL_COMPONENT {
				t.Errorf("Broker operation log missing component=%s: %v", SENTINEL_COMPONENT, entry)
			}

			if entry["version"] != TEST_VERSION {
				t.Errorf("Broker operation log missing version=%s: %v", TEST_VERSION, entry)
			}

			if entry["hostname"] != TEST_HOST {
				t.Errorf("Broker operation log missing hostname=%s: %v", TEST_HOST, entry)
			}

			// Check for context inheritance (these fields should be present if context flowed through)
			if entry["decision_reason"] != nil || entry["topic"] != nil || entry["subset"] != nil {
				t.Logf("Broker operation inherits Sentinel context: decision_reason=%v topic=%v subset=%v",
					entry["decision_reason"], entry["topic"], entry["subset"])
			}

			t.Logf("Found broker operation log with component=sentinel: %s", msg)
		}
	}

	if !foundSentinelEventLog {
		t.Error("No Sentinel event publishing logs found - events may not have been published")
	}

	if !foundBrokerOperationLog {
		t.Error("No broker operation logs found - broker may not be logging")
	}

	// Success criteria: Both Sentinel and broker logs should use component=sentinel
	if foundSentinelEventLog && foundBrokerOperationLog {
		t.Log("SUCCESS: Logger context inheritance verified - both Sentinel and broker operations log with component=sentinel")
	}
}
