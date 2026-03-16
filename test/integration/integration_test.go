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
	"sync/atomic"
	"testing"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/sentinel"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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

	// Create mock HyperFleet API server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// First call: Return clusters with one needing reconciliation
		if callCount == 1 {
			clusters := []map[string]interface{}{
				createMockCluster("cluster-1", 2, 2, true, now.Add(-31*time.Minute)), // Max age exceeded
				createMockCluster("cluster-2", 1, 1, true, now.Add(-5*time.Minute)),  // Within max age
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
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second, "test-sentinel", "test")
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
	if callCount < 2 {
		t.Errorf("Expected at least 2 polling cycles, got %d", callCount)
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
				true,
				now.Add(-31*time.Minute), // Exceeds max_age_ready (30m)
				map[string]string{"shard": "1"},
			),
			// This cluster has shard:2 - should NOT match selector
			createMockClusterWithLabels(
				"cluster-shard-2",
				2,
				2,
				true,
				now.Add(-31*time.Minute), // Also exceeds max_age, but should be filtered
				map[string]string{"shard": "2"},
			),
		}

		// Server-side filtering: check for search parameter
		// TSL syntax: labels.key='value' and labels.key2='value2'
		searchParam := r.URL.Query().Get("search")
		filteredClusters := allClusters

		if searchParam != "" {
			filteredClusters = []map[string]interface{}{}
			for _, cluster := range allClusters {
				labels, ok := cluster["labels"].(map[string]string)
				if !ok {
					continue
				}

				// TSL matching: if search contains "labels.shard='1'", only include clusters with shard=1
				if searchParam == "labels.shard='1'" && labels["shard"] == "1" {
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
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second, "test-sentinel", "test")
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

	// Integration test validates label selector filtering works end-to-end
	// Event verification requires subscriber implementation (future enhancement)
	t.Log("Label selector filtering test with real RabbitMQ broker completed successfully")
}

// TestIntegration_TSLSyntaxMultipleLabels validates TSL syntax with multiple label selectors
func TestIntegration_TSLSyntaxMultipleLabels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get shared RabbitMQ testcontainer from helper
	helper := NewHelper()

	now := time.Now()

	// Track the search parameter received by the mock server
	var receivedSearchParam string

	// Create mock server that validates TSL syntax
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSearchParam = r.URL.Query().Get("search")

		// All available clusters
		allClusters := []map[string]interface{}{
			createMockClusterWithLabels(
				"cluster-region-env-match",
				2,
				2,
				true,
				now.Add(-31*time.Minute),
				map[string]string{"region": "us-east", "env": "production"},
			),
			createMockClusterWithLabels(
				"cluster-region-only",
				2,
				2,
				true,
				now.Add(-31*time.Minute),
				map[string]string{"region": "us-east", "env": "staging"},
			),
		}

		// Server-side filtering using TSL syntax
		filteredClusters := allClusters

		// Expected TSL format: "labels.env='production' and labels.region='us-east'"
		expectedTSL := "labels.env='production' and labels.region='us-east'"
		if receivedSearchParam == expectedTSL {
			filteredClusters = []map[string]interface{}{}
			for _, cluster := range allClusters {
				labels, ok := cluster["labels"].(map[string]string)
				if !ok {
					continue
				}
				// Match clusters with both labels
				if labels["region"] == "us-east" && labels["env"] == "production" {
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
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second, "test-sentinel", "test")
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

	// Validate the search parameter format is correct TSL syntax
	expectedTSL := "labels.env='production' and labels.region='us-east'"
	if receivedSearchParam != expectedTSL {
		t.Errorf("Expected TSL search parameter %q, got %q", expectedTSL, receivedSearchParam)
	}

	// Wait for sentinel to stop
	startErr := <-errChan
	if startErr != nil && startErr != context.Canceled {
		t.Errorf("Expected Start to return nil or context.Canceled, got: %v", startErr)
	}

	t.Logf("TSL syntax validation completed - received correct format: %s", receivedSearchParam)
}

// TestIntegration_BrokerLoggerContext validates that OpenTelemetry trace context and Sentinel-specific context fields are properly propagated through the logging system
// during event publishing workflows.
//
// Context Propagation Flow:
//  1. OpenTelemetry creates trace_id and span_id for spans
//  2. telemetry.StartSpan() enriches context with trace IDs for logging
//  3. Sentinel adds decision context (decision_reason, topic, subset) to context
//  4. Context flows through to broker operations via logger.WithXXX() calls
//  5. All log entries contain both OpenTelemetry and Sentinel context fields
//
// Validated Log Fields:
//
//	OpenTelemetry fields:
//	- trace_id: Distributed trace identifier from active span
//	- span_id: Current span identifier from active span
//
//	Sentinel-specific fields:
//	- decision_reason: Why the event was triggered (e.g., "max age exceeded", "generation changed")
//	- topic: Message broker topic where event is published
//	- subset: Resource type being monitored (e.g., "clusters")
//	- component: Always "sentinel" for consistent log attribution
//
// Test Approach:
//   - Captures structured JSON logs to a buffer for analysis
//   - Uses real RabbitMQ broker to generate authentic broker operation logs
//   - Mock server returns resources that trigger multiple event types
//   - Parses JSON log entries to verify required context fields are present
//
// Success Criteria:
//   - Sentinel's "Published event" logs contain all OpenTelemetry and context fields
//   - Broker operation logs inherit Sentinel's component and context
//   - No context fields are lost during the broker publishing workflow
//
// This test ensures distributed tracing correlation and structured logging work correctly across the Sentinel → Broker boundary for observability.
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

	// Set OTLP sampler
	err := os.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	if err != nil {
		t.Errorf("Failed to set OTEL_TRACES_SAMPLER: %v", err)
	}
	defer func() {
		err := os.Unsetenv("OTEL_TRACES_SAMPLER")
		if err != nil {
			t.Errorf("Failed to unset OTEL_TRACES_SAMPLER: %v", err)
		}
	}()

	// Setup OpenTelemetry for integration test
	tp, err := telemetry.InitTraceProvider(ctx, "sentinel", "test")
	if err != nil {
		t.Fatalf("Failed to initialize OpenTelemetry: %v", err)
	}
	defer telemetry.Shutdown(context.Background(), tp)

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
		OTel: logger.OTelConfig{
			Enabled: true,
		},
	}
	log := logger.NewHyperFleetLoggerWithConfig(cfg)

	// Mock server returns clusters that will trigger event publishing
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount >= 2 {
			select {
			case readyChan <- true:
			default:
			}
		}
		clusters := []map[string]interface{}{
			// This cluster will trigger max_age_ready exceeded event
			createMockCluster("cluster-old", 2, 2, true, now.Add(-35*time.Minute)), // Exceeds 30min
			// This cluster will trigger max_age_not_ready exceeded event
			createMockCluster("cluster-not-ready", 1, 1, false, now.Add(-15*time.Second)), // Exceeds 10sec
		}
		response := createMockClusterList(clusters)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second, "test-sentinel", "test")
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)

	sentinelConfig := &config.SentinelConfig{
		ResourceType: "clusters",
		Clients: config.ClientsConfig{
			HyperFleetAPI: &config.HyperFleetAPIConfig{},
			Broker:        &config.BrokerConfig{Topic: TEST_TOPIC},
		},
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

			if entry["trace_id"] == nil {
				t.Errorf("Sentinel event log missing trace_id: %v", entry)
			}
			if entry["span_id"] == nil {
				t.Errorf("Sentinel event log missing span_id: %v", entry)
			}

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

// TestIntegration_EndToEndSpanHierarchy validates the complete OpenTelemetry span hierarchy created during Sentinel's polling and event publishing workflow.
//
// Expected Span Hierarchy:
//
//	sentinel.poll (root span - one per polling cycle)
//	├── HTTP GET (API call to fetch resources)
//	├── sentinel.evaluate (one per resource evaluated)
//	│   └── {topic} publish (one per event published)
//	├── sentinel.evaluate (next resource)
//	│   └── {topic} publish
//	└── ...
//
// The test validates:
//  1. Required spans are created: sentinel.poll, sentinel.evaluate, {topic} publish
//  2. Parent-child relationships: evaluate HTTP spans are children of poll spans, publish spans are children of evaluate spans
//  3. Multiple spans: One evaluate/publish span per resource that triggers an event
//  4. Trace continuity: All spans within a poll cycle belong to the same trace
//
// Test Setup:
//   - Uses in-memory OpenTelemetry exporter to capture and analyze spans
//   - Mock server returns 3 test resources that will trigger events
//   - Real RabbitMQ broker for realistic message publishing
//
// Note: The test may capture multiple poll cycles. Due to context cancellation timing, only 2 poll cycles are validated
func TestIntegration_EndToEndSpanHierarchy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Setup in-memory trace exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithBatcher(exporter),
	)
	prevTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prevTP)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := tp.Shutdown(cleanupCtx); err != nil {
			t.Logf("Warning: shutdown of tracer provider failed: %v", err)
		}
	}()

	now := time.Now()
	var callCount atomic.Int32
	readyChan := make(chan bool, 1)

	// Mock server that returns clusters requiring reconciliation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		if callCount.Load() > 2 {
			select {
			case readyChan <- true:
			default:
			}
		}

		// Return clusters that will trigger different decision types
		clusters := []map[string]interface{}{
			// Triggers max_age_ready exceeded
			createMockCluster("cluster-ready-old", 2, 2, true, now.Add(-35*time.Minute)),
			// Triggers max_age_not_ready exceeded
			createMockCluster("cluster-not-ready-old", 1, 1, false, now.Add(-15*time.Second)),
			// Triggers generation mismatch
			createMockCluster("cluster-generation-mismatch", 5, 3, true, now.Add(-5*time.Minute)),
		}
		response := createMockClusterList(clusters)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Get shared RabbitMQ from helper
	helper := NewHelper()

	// Setup components
	hyperfleetClient, clientErr := client.NewHyperFleetClient(server.URL, 10*time.Second, "test-sentinel", "test")
	if clientErr != nil {
		t.Fatalf("failed to create HyperFleet client: %v", clientErr)
	}
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:    "clusters",
		Clients:         config.ClientsConfig{Broker: &config.BrokerConfig{Topic: "test-spans-topic"}},
		PollInterval:    100 * time.Millisecond,
		MaxAgeNotReady:  10 * time.Second,
		MaxAgeReady:     30 * time.Minute,
		MessagingSystem: "rabbitmq",
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := sentinel.NewSentinel(cfg, hyperfleetClient, decisionEngine, helper.RabbitMQ.Publisher(), log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Run Sentinel to generate spans
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start(ctx)
	}()

	// Wait for at least 2 polling cycles
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

	// Force flush spans to exporter
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer flushCancel()

	err = tp.ForceFlush(flushCtx)
	if err != nil {
		t.Fatalf("force flush error: %v", err)
	}

	// Analyze captured spans
	spans := exporter.GetSpans()
	t.Logf("Captured %d spans", len(spans))

	// Build span maps for analysis
	spansByName := make(map[string][]tracetest.SpanStub)
	spansByID := make(map[string]tracetest.SpanStub)
	spansByParentID := make(map[string][]tracetest.SpanStub)

	for _, span := range spans {
		spansByName[span.Name] = append(spansByName[span.Name], span)
		spansByID[span.SpanContext.SpanID().String()] = span

		if span.Parent.IsValid() {
			parentID := span.Parent.SpanID().String()
			spansByParentID[parentID] = append(spansByParentID[parentID], span)
		}
	}

	// Print span hierarchy for debugging
	t.Log("Span hierarchy:")
	for _, span := range spans {
		parentInfo := "ROOT"
		if span.Parent.IsValid() {
			parentInfo = "child of " + span.Parent.SpanID().String()
		}
		t.Logf("  %s (%s) - %s", span.Name, span.SpanContext.SpanID().String()[:8], parentInfo)
	}

	// Validate required spans exist
	requiredSpans := []string{
		"sentinel.poll",
		"sentinel.evaluate",
		"test-spans-topic publish",
	}

	for _, requiredSpan := range requiredSpans {
		if _, exists := spansByName[requiredSpan]; !exists {
			t.Errorf("Required span '%s' not found. Available spans: %v", requiredSpan, getSpanNames(spans))
		}
	}

	// Validate span hierarchy structure
	pollSpans := spansByName["sentinel.poll"]
	if len(pollSpans) == 0 {
		t.Fatal("No sentinel.poll spans found")
	}

	pollSpansToValidate := pollSpans
	if len(pollSpansToValidate) > 2 {
		pollSpansToValidate = pollSpansToValidate[:2]
	}

	// For each poll span, validate it has the expected children, evaluate only the first two poll spans
	for _, pollSpan := range pollSpansToValidate {
		pollSpanID := pollSpan.SpanContext.SpanID().String()
		directChildren := spansByParentID[pollSpanID]

		t.Logf("Poll span %s has %d direct children", pollSpanID[:8], len(directChildren))

		// Verify poll span has evaluate children (direct)
		hasEvaluateChild := false
		hasHTTPChild := false
		evaluateChildCount := 0

		for _, child := range directChildren {
			switch {
			case child.Name == "sentinel.evaluate":
				hasEvaluateChild = true
				evaluateChildCount++
			case strings.Contains(child.Name, "HTTP"):
				hasHTTPChild = true
			}
		}

		if !hasEvaluateChild {
			t.Errorf("Poll span %s missing sentinel.evaluate child", pollSpanID[:8])
			continue
		}

		// Verify each evaluate span has publish grandchildren
		publishGrandchildCount := 0
		for _, child := range directChildren {
			if child.Name == "sentinel.evaluate" {
				childID := child.SpanContext.SpanID().String()
				grandchildren := spansByParentID[childID]
				for _, grandchild := range grandchildren {
					if strings.HasSuffix(grandchild.Name, " publish") {
						publishGrandchildCount++
					}
				}
			}
		}

		t.Logf("Poll span %s: evaluate children=%d, publish grandchildren=%d, http=%t",
			pollSpanID[:8], evaluateChildCount, publishGrandchildCount, hasHTTPChild)

		// Only validate publish grandchildren if this poll span actually processed events
		if evaluateChildCount > 0 && publishGrandchildCount == 0 {
			t.Errorf("Poll span %s has %d evaluate children but no publish grandchildren",
				pollSpanID[:8], evaluateChildCount)
		}
	}

	// Validate we have multiple evaluation spans (one per resource)
	evaluateSpans := spansByName["sentinel.evaluate"]
	if len(evaluateSpans) < 3 {
		t.Errorf("Expected at least 3 sentinel.evaluate spans (one per test resource), got %d", len(evaluateSpans))
	}

	// Validate we have multiple publish spans (one per event)
	publishSpans := spansByName["test-spans-topic publish"]
	if len(publishSpans) < 3 {
		t.Errorf("Expected at least 3 publish spans (one per test event), got %d", len(publishSpans))
	}

	validateSpanAttribute(t, publishSpans, "test-spans-topic publish", "messaging.system", cfg.MessagingSystem)
	validateSpanAttribute(t, publishSpans, "test-spans-topic publish", "messaging.operation.type", "publish")
	validateSpanAttribute(t, publishSpans, "test-spans-topic publish", "messaging.destination.name", cfg.Clients.Broker.Topic)

	for _, publishSpan := range publishSpans {
		if !publishSpan.Parent.IsValid() {
			t.Errorf("Publish span %s should have a parent", publishSpan.SpanContext.SpanID().String()[:8])
			continue
		}

		parentSpan, exists := spansByID[publishSpan.Parent.SpanID().String()]
		if !exists {
			t.Errorf("Publish span parent not found")
			continue
		}

		if parentSpan.Name != "sentinel.evaluate" {
			t.Errorf("Publish span parent should be sentinel.evaluate, got %s", parentSpan.Name)
		}
	}

	// Verify trace continuity - spans should form coherent traces
	traceIDs := make(map[string]bool)
	for _, span := range spans {
		traceIDs[span.SpanContext.TraceID().String()] = true
	}

	if len(traceIDs) > len(pollSpans) {
		t.Errorf("Expected spans to belong to %d traces (one per poll), but found %d trace IDs", len(pollSpans), len(traceIDs))
	}
}

// Helper function for span name extraction
func getSpanNames(spans []tracetest.SpanStub) []string {
	names := make([]string, len(spans))
	for i, span := range spans {
		names[i] = span.Name
	}
	return names
}

func validateSpanAttribute(t *testing.T, spans []tracetest.SpanStub, spanName, attrKey, expectedValue string) {
	for _, span := range spans {
		if span.Name == spanName {
			for _, attr := range span.Attributes {
				if string(attr.Key) == attrKey {
					if attr.Value.AsString() != expectedValue {
						t.Errorf("Span '%s': expected %s=%s, got %s", spanName, attrKey, expectedValue, attr.Value.AsString())
					}
					return
				}
			}
			t.Errorf("Span '%s': attribute '%s' not found", spanName, attrKey)
			return
		}
	}
	t.Errorf("Span '%s' not found", spanName)
}
