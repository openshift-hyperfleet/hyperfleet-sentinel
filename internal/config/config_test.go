package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testResourceType = "clusters"
	testAPIEndpoint  = "http://api.example.com"
)

// Helper function to create a temporary config file for testing
func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	return configPath
}

// newTestMessageDecision returns a valid message decision config for testing
func newTestMessageDecision() *MessageDecisionConfig {
	return &MessageDecisionConfig{
		Params: map[string]string{
			"is_ready": `condition("Ready").status == "True"`,
		},
		Result: "!is_ready",
	}
}

// ============================================================================
// Loading & Parsing Tests
// ============================================================================

func TestLoadConfig_ValidComplete(t *testing.T) {
	configPath := filepath.Join("testdata", "valid-complete.yaml")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify resource type
	if cfg.ResourceType != testResourceType {
		t.Errorf("Expected resource_type '%s', got '%s'", testResourceType, cfg.ResourceType)
	}

	// Verify durations
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("Expected poll_interval 5s, got %v", cfg.PollInterval)
	}

	// Verify resource selector
	if len(cfg.ResourceSelector) != 2 {
		t.Errorf("Expected 2 resource selectors, got %d", len(cfg.ResourceSelector))
	}

	// Verify HyperFleet API config
	if cfg.HyperFleetAPI.Endpoint != "https://api.hyperfleet.example.com" {
		t.Errorf("Expected endpoint 'https://api.hyperfleet.example.com', got '%s'", cfg.HyperFleetAPI.Endpoint)
	}
	if cfg.HyperFleetAPI.Timeout != 5*time.Second {
		t.Errorf("Expected timeout 5s, got %v", cfg.HyperFleetAPI.Timeout)
	}

	// Verify message_decision
	if cfg.MessageDecision == nil {
		t.Fatal("Expected message_decision to be set")
	}
	if cfg.MessageDecision.Result == "" {
		t.Error("Expected message_decision.result to be set")
	}
	if len(cfg.MessageDecision.Params) != 6 {
		t.Errorf("Expected 6 message_decision params, got %d", len(cfg.MessageDecision.Params))
	}

	// Verify message data
	if len(cfg.MessageData) != 3 {
		t.Errorf("Expected 3 message_data fields, got %d", len(cfg.MessageData))
	}
	resourceID, ok := cfg.MessageData["id"].(string)
	if !ok {
		t.Fatalf("Expected message_data.id to be a string, got %T", cfg.MessageData["id"])
	}
	if resourceID != "resource.id" {
		t.Errorf("Expected message_data.id 'resource.id', got '%v'", resourceID)
	}
}

func TestLoadConfig_Minimal(t *testing.T) {
	configPath := filepath.Join("testdata", "minimal.yaml")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify defaults were applied
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("Expected default poll_interval 5s, got %v", cfg.PollInterval)
	}

	// Verify default message_decision was applied
	if cfg.MessageDecision == nil {
		t.Fatal("Expected default message_decision to be applied")
	}
	if cfg.MessageDecision.Result == "" {
		t.Error("Expected default message_decision.result to be set")
	}
	if len(cfg.MessageDecision.Params) != 6 {
		t.Errorf("Expected 6 default message_decision params, got %d", len(cfg.MessageDecision.Params))
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Expected error for nonexistent file, got nil")
	}
}

func TestLoadConfig_EmptyPath(t *testing.T) {
	_, err := LoadConfig("")
	if err == nil {
		t.Fatal("Expected error for empty config path, got nil")
	}
	if err.Error() != "config file is required" {
		t.Errorf("Expected 'config file is required' error, got: %v", err)
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	yaml := `
resource_type: clusters
invalid yaml here: [
  - broken
`
	configPath := createTempConfigFile(t, yaml)

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("Expected error for invalid YAML, got nil")
	}
}

// ============================================================================
// Default Values Tests
// ============================================================================

func TestNewSentinelConfig_Defaults(t *testing.T) {
	cfg := NewSentinelConfig()

	// ResourceType has no default - must be set in config file
	if cfg.ResourceType != "" {
		t.Errorf("Expected no default resource_type (empty string), got '%s'", cfg.ResourceType)
	}
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("Expected default poll_interval 5s, got %v", cfg.PollInterval)
	}
	if cfg.HyperFleetAPI.Timeout != 5*time.Second {
		t.Errorf("Expected default timeout 5s, got %v", cfg.HyperFleetAPI.Timeout)
	}
	// Endpoint has no default - must be set in config file
	if cfg.HyperFleetAPI.Endpoint != "" {
		t.Errorf("Expected no default endpoint (empty string), got '%s'", cfg.HyperFleetAPI.Endpoint)
	}
	if len(cfg.ResourceSelector) != 0 {
		t.Errorf("Expected empty resource_selector, got %d items", len(cfg.ResourceSelector))
	}
	if cfg.MessageData != nil {
		t.Errorf("Expected nil message_data, got %v", cfg.MessageData)
	}
	// MessageDecision has no default in NewSentinelConfig - LoadConfig applies it
	if cfg.MessageDecision != nil {
		t.Errorf("Expected nil message_decision in NewSentinelConfig, got %v", cfg.MessageDecision)
	}
}

// ============================================================================
// Validation Tests - Required Fields
// ============================================================================

func TestValidate_MissingResourceType(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = ""
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for missing resource_type, got nil")
	}
	if err.Error() != "resource_type is required" {
		t.Errorf("Expected 'resource_type is required' error, got: %v", err)
	}
}

func TestValidate_MissingEndpoint(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for missing endpoint, got nil")
	}
	if err.Error() != "hyperfleet_api.endpoint is required" {
		t.Errorf("Expected 'hyperfleet_api.endpoint is required' error, got: %v", err)
	}
}

// ============================================================================
// Validation Tests - Invalid Values
// ============================================================================

func TestValidate_InvalidResourceType(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = "invalid-type"
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for invalid resource_type, got nil")
	}
}

func TestValidate_InvalidResourceTypes(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		shouldFail   bool
	}{
		{"valid clusters", testResourceType, false},
		{"valid nodepools", "nodepools", false},
		{"invalid manifests", "manifests", true},
		{"invalid workloads", "workloads", true},
		{"invalid pods", "pods", true},
		{"invalid deployments", "deployments", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewSentinelConfig()
			cfg.ResourceType = tt.resourceType
			cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
			cfg.MessageData = map[string]interface{}{"id": "resource.id"}
			cfg.MessageDecision = newTestMessageDecision()

			err := cfg.Validate()
			if tt.shouldFail && err == nil {
				t.Errorf("Expected error for resource_type '%s', got nil", tt.resourceType)
			}
			if !tt.shouldFail && err != nil {
				t.Errorf("Expected no error for resource_type '%s', got: %v", tt.resourceType, err)
			}
		})
	}
}

func TestValidate_NegativeDurations(t *testing.T) {
	tests := []struct {
		modifier func(*SentinelConfig)
		name     string
	}{
		{
			name: "negative poll_interval",
			modifier: func(c *SentinelConfig) {
				c.PollInterval = -5 * time.Second
			},
		},
		{
			name: "zero poll_interval",
			modifier: func(c *SentinelConfig) {
				c.PollInterval = 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewSentinelConfig()
			cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
			cfg.MessageDecision = newTestMessageDecision()
			tt.modifier(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Expected error for invalid duration, got nil")
			}
		})
	}
}

// ============================================================================
// Label Selector Tests
// ============================================================================

func TestLabelSelectorList_ToMap(t *testing.T) {
	selectors := LabelSelectorList{
		{Label: "region", Value: "us-east"},
		{Label: "environment", Value: "production"},
	}

	m := selectors.ToMap()
	if m == nil {
		t.Fatal("Expected non-nil map, got nil")
	}
	if len(m) != 2 {
		t.Errorf("Expected map with 2 entries, got %d", len(m))
	}
	if m["region"] != "us-east" {
		t.Errorf("Expected region 'us-east', got '%s'", m["region"])
	}
	if m["environment"] != "production" {
		t.Errorf("Expected environment 'production', got '%s'", m["environment"])
	}
}

func TestLabelSelectorList_ToMap_Empty(t *testing.T) {
	selectors := LabelSelectorList{}

	m := selectors.ToMap()
	if m != nil {
		t.Errorf("Expected nil map for empty selector list, got: %v", m)
	}
}

func TestLabelSelectorList_ToMap_EmptyLabel(t *testing.T) {
	selectors := LabelSelectorList{
		{Label: "region", Value: "us-east"},
		{Label: "", Value: "ignored"},
		{Label: "environment", Value: "production"},
	}

	m := selectors.ToMap()
	if len(m) != 2 {
		t.Errorf("Expected map with 2 entries (empty label ignored), got %d", len(m))
	}
}

// ============================================================================
// Message Data Validation Tests
// ============================================================================

func TestValidate_ValidMessageDataFlat(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageDecision = newTestMessageDecision()
	cfg.MessageData = map[string]interface{}{
		"id":     "resource.id",
		"kind":   "resource.kind",
		"region": "resource.labels.region",
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Expected no error for valid flat config, got: %v", err)
	}
}

func TestValidate_ValidMessageDataNested(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageDecision = newTestMessageDecision()
	cfg.MessageData = map[string]interface{}{
		"origin": `"sentinel"`,
		"ref": map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Expected no error for valid nested config, got: %v", err)
	}
}

func TestValidate_NilMessageData(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageDecision = newTestMessageDecision()
	// MessageData is nil by default — message_data is required so this must fail

	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for nil message_data, got nil")
	}
}

func TestValidate_NilLeafInMessageData(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageDecision = newTestMessageDecision()
	cfg.MessageData = map[string]interface{}{
		"id":   nil,
		"kind": "resource.kind",
	}

	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for nil leaf in message_data, got nil")
	}
}

func TestValidate_EmptyStringLeafInMessageData(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageDecision = newTestMessageDecision()
	cfg.MessageData = map[string]interface{}{
		"id":   "",
		"kind": "resource.kind",
	}

	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for empty-string leaf in message_data, got nil")
	}
}

func TestValidate_NilLeafInNestedMessageData(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageDecision = newTestMessageDecision()
	cfg.MessageData = map[string]interface{}{
		"ref": map[string]interface{}{
			"id":   nil,
			"kind": "resource.kind",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for nil leaf in nested message_data, got nil")
	}
}

// ============================================================================
// Message Decision Validation Tests
// ============================================================================

func TestValidate_MissingMessageDecision(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageData = map[string]interface{}{"id": "resource.id"}
	cfg.MessageDecision = nil

	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for nil message_decision, got nil")
	}
}

func TestValidate_EmptyResultExpression(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageData = map[string]interface{}{"id": "resource.id"}
	cfg.MessageDecision = &MessageDecisionConfig{
		Params: map[string]string{},
		Result: "",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for empty result expression, got nil")
	}
}

func TestValidate_EmptyParamExpression(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageData = map[string]interface{}{"id": "resource.id"}
	cfg.MessageDecision = &MessageDecisionConfig{
		Params: map[string]string{
			"is_ready": "",
		},
		Result: "is_ready",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for empty param expression, got nil")
	}
}

func TestValidate_CircularDependency(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = testResourceType
	cfg.HyperFleetAPI.Endpoint = testAPIEndpoint
	cfg.MessageData = map[string]interface{}{"id": "resource.id"}
	cfg.MessageDecision = &MessageDecisionConfig{
		Params: map[string]string{
			"a": "b",
			"b": "a",
		},
		Result: "a",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for circular dependency, got nil")
	}
	if !strings.Contains(err.Error(), "circular dependency") {
		t.Errorf("Expected circular dependency error, got: %v", err)
	}
}

// ============================================================================
// Topological Sort Tests
// ============================================================================

func TestTopologicalSort_NoDependencies(t *testing.T) {
	md := &MessageDecisionConfig{
		Params: map[string]string{
			"a": `condition("Ready").status`,
			"b": `condition("Available").status`,
		},
		Result: "a == b",
	}

	order, err := md.TopologicalSort()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("Expected 2 params, got %d", len(order))
	}
}

func TestTopologicalSort_LinearDependency(t *testing.T) {
	md := &MessageDecisionConfig{
		Params: map[string]string{
			"is_ready":       `condition("Ready").status == "True"`,
			"should_publish": `!is_ready`,
		},
		Result: "should_publish",
	}

	order, err := md.TopologicalSort()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("Expected 2 params, got %d", len(order))
	}

	// is_ready must come before should_publish
	isReadyIdx := -1
	shouldPublishIdx := -1
	for i, name := range order {
		if name == "is_ready" {
			isReadyIdx = i
		}
		if name == "should_publish" {
			shouldPublishIdx = i
		}
	}
	if isReadyIdx >= shouldPublishIdx {
		t.Errorf("Expected is_ready (%d) before should_publish (%d)", isReadyIdx, shouldPublishIdx)
	}
}

func TestTopologicalSort_CircularDependency(t *testing.T) {
	md := &MessageDecisionConfig{
		Params: map[string]string{
			"a": "b && true",
			"b": "c || false",
			"c": "a",
		},
		Result: "a",
	}

	_, err := md.TopologicalSort()
	if err == nil {
		t.Error("Expected error for circular dependency, got nil")
	}
}

func TestTopologicalSort_EmptyParams(t *testing.T) {
	md := &MessageDecisionConfig{
		Params: map[string]string{},
		Result: "true",
	}

	order, err := md.TopologicalSort()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(order) != 0 {
		t.Errorf("Expected 0 params, got %d", len(order))
	}
}

// ============================================================================
// containsIdentifier Tests
// ============================================================================

func TestContainsIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		expr       string
		identifier string
		want       bool
	}{
		{"exact match", "is_ready", "is_ready", true},
		{"in expression", "!is_ready && x", "is_ready", true},
		{"prefix match should fail", "is_ready_2", "is_ready", false},
		{"suffix match should fail", "not_is_ready", "is_ready", false},
		{"substring in middle", "foo_is_ready_bar", "is_ready", false},
		{"not present", "something_else", "is_ready", false},
		{"at start with operator", "is_ready || other", "is_ready", true},
		{"at end with operator", "other && is_ready", "is_ready", true},
		{"in parentheses", "(is_ready)", "is_ready", true},
		{"with negation", "!is_ready", "is_ready", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsIdentifier(tt.expr, tt.identifier)
			if got != tt.want {
				t.Errorf("containsIdentifier(%q, %q) = %v, want %v", tt.expr, tt.identifier, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Integration-like Test with Full Config
// ============================================================================

func TestLoadConfig_BlankMessageDataLeafReturnsError(t *testing.T) {
	_, err := LoadConfig(filepath.Join("testdata", "message-data-blank-id.yaml"))
	if err == nil {
		t.Fatal("expected error for blank message_data leaf, got nil")
	}
	if !strings.Contains(err.Error(), "message_data.id") {
		t.Errorf("expected error to mention message_data.id, got: %v", err)
	}
}

func TestLoadConfig_FullWorkflow(t *testing.T) {
	configPath := filepath.Join("testdata", "full-workflow.yaml")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify all parts are loaded correctly
	if cfg.ResourceType != "nodepools" {
		t.Errorf("Expected resource_type 'nodepools', got '%s'", cfg.ResourceType)
	}
	if cfg.PollInterval != 3*time.Second {
		t.Errorf("Expected poll_interval 3s, got %v", cfg.PollInterval)
	}
	if len(cfg.ResourceSelector) != 2 {
		t.Errorf("Expected 2 resource selectors, got %d", len(cfg.ResourceSelector))
	}
	md := cfg.MessageData
	if len(md) != 4 {
		t.Errorf("Expected 4 message_data fields, got %d", len(md))
	}

	// Verify message_decision is loaded from file
	if cfg.MessageDecision == nil {
		t.Fatal("Expected message_decision to be set")
	}
	if len(cfg.MessageDecision.Params) != 6 {
		t.Errorf("Expected 6 message_decision params, got %d", len(cfg.MessageDecision.Params))
	}
}

// ============================================================================
// Topic Tests
// ============================================================================

func TestLoadConfig_TopicFromEnvVar(t *testing.T) {
	t.Setenv("BROKER_TOPIC", "test-namespace-clusters")

	configPath := filepath.Join("testdata", "minimal.yaml")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Topic != "test-namespace-clusters" {
		t.Errorf("Expected topic 'test-namespace-clusters', got '%s'", cfg.Topic)
	}
}

func TestLoadConfig_TopicEnvVarOverridesConfig(t *testing.T) {
	t.Setenv("BROKER_TOPIC", "env-topic")

	yaml := `
resource_type: clusters
hyperfleet_api:
  endpoint: http://localhost:8000
topic: config-topic
message_data:
  id: resource.id
`
	configPath := createTempConfigFile(t, yaml)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Topic != "env-topic" {
		t.Errorf("Expected topic 'env-topic' (from env), got '%s'", cfg.Topic)
	}
}

func TestLoadConfig_TopicFromConfigFile(t *testing.T) {
	origValue, wasSet := os.LookupEnv("BROKER_TOPIC")
	if wasSet {
		defer func() { _ = os.Setenv("BROKER_TOPIC", origValue) }()
	}
	_ = os.Unsetenv("BROKER_TOPIC")

	yaml := `
resource_type: clusters
hyperfleet_api:
  endpoint: http://localhost:8000
topic: my-namespace-clusters
message_data:
  id: resource.id
`
	configPath := createTempConfigFile(t, yaml)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Topic != "my-namespace-clusters" {
		t.Errorf("Expected topic 'my-namespace-clusters', got '%s'", cfg.Topic)
	}
}

func TestLoadConfig_TopicEmpty(t *testing.T) {
	origValue, wasSet := os.LookupEnv("BROKER_TOPIC")
	if wasSet {
		defer func() { _ = os.Setenv("BROKER_TOPIC", origValue) }()
	}
	_ = os.Unsetenv("BROKER_TOPIC")

	configPath := filepath.Join("testdata", "minimal.yaml")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Topic != "" {
		t.Errorf("Expected empty topic, got '%s'", cfg.Topic)
	}
}

func TestLoadConfig_TopicEnvVarEmptyClearsConfig(t *testing.T) {
	t.Setenv("BROKER_TOPIC", "")

	yaml := `
resource_type: clusters
hyperfleet_api:
  endpoint: http://localhost:8000
topic: config-topic
message_data:
  id: resource.id
`
	configPath := createTempConfigFile(t, yaml)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Topic != "" {
		t.Errorf("Expected empty topic (cleared by env var), got '%s'", cfg.Topic)
	}
}
