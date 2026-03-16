package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// ============================================================================
// Loading & Parsing Tests
// ============================================================================

func TestLoadConfig_ValidComplete(t *testing.T) {
	configPath := filepath.Join("testdata", "valid-complete.yaml")

	cfg, err := LoadConfig(configPath, nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify resource type
	if cfg.ResourceType != "clusters" {
		t.Errorf("Expected resource_type 'clusters', got '%s'", cfg.ResourceType)
	}

	// Verify durations
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("Expected poll_interval 5s, got %v", cfg.PollInterval)
	}
	if cfg.MaxAgeNotReady != 10*time.Second {
		t.Errorf("Expected max_age_not_ready 10s, got %v", cfg.MaxAgeNotReady)
	}
	if cfg.MaxAgeReady != 30*time.Minute {
		t.Errorf("Expected max_age_ready 30m, got %v", cfg.MaxAgeReady)
	}

	// Verify resource selector
	if len(cfg.ResourceSelector) != 2 {
		t.Errorf("Expected 2 resource selectors, got %d", len(cfg.ResourceSelector))
	}

	// Verify HyperFleet API config
	if cfg.Clients.HyperFleetAPI.BaseURL != "https://api.hyperfleet.example.com" {
		t.Errorf("Expected base_url 'https://api.hyperfleet.example.com', got '%s'", cfg.Clients.HyperFleetAPI.BaseURL)
	}
	if cfg.Clients.HyperFleetAPI.Timeout != 10*time.Second {
		t.Errorf("Expected timeout 10s, got %v", cfg.Clients.HyperFleetAPI.Timeout)
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

	cfg, err := LoadConfig(configPath, nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify defaults were applied
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("Expected default poll_interval 5s, got %v", cfg.PollInterval)
	}
	if cfg.MaxAgeNotReady != 10*time.Second {
		t.Errorf("Expected default max_age_not_ready 10s, got %v", cfg.MaxAgeNotReady)
	}
	if cfg.MaxAgeReady != 30*time.Minute {
		t.Errorf("Expected default max_age_ready 30m, got %v", cfg.MaxAgeReady)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml", nil)
	if err == nil {
		t.Fatal("Expected error for nonexistent file, got nil")
	}
}

func TestLoadConfig_EmptyPath_FallsBackToDefault(t *testing.T) {
	t.Setenv("HYPERFLEET_CONFIG", "")
	_, err := LoadConfig("", nil)
	if err == nil {
		t.Fatal("Expected error for missing default config file, got nil")
	}
	if !strings.Contains(err.Error(), "/etc/hyperfleet/config.yaml") {
		t.Errorf("Expected error to mention default path /etc/hyperfleet/config.yaml, got: %v", err)
	}
}

func TestLoadConfig_HyperfleetConfigEnvVar(t *testing.T) {
	yaml := `
sentinel:
  name: env-var-sentinel
resource_type: clusters
message_data:
  id: "resource.id"
  kind: "resource.kind"
poll_interval: 5s
max_age_not_ready: 10s
max_age_ready: 30m
clients:
  hyperfleet_api:
    base_url: https://example.com
    timeout: 10s
`
	configPath := createTempConfigFile(t, yaml)
	t.Setenv("HYPERFLEET_CONFIG", configPath)

	cfg, err := LoadConfig("", nil)
	if err != nil {
		t.Fatalf("Expected config to load from HYPERFLEET_CONFIG env var, got error: %v", err)
	}
	if cfg.Sentinel.Name != "env-var-sentinel" {
		t.Errorf("Expected sentinel name 'env-var-sentinel', got: %s", cfg.Sentinel.Name)
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	yaml := `
resource_type: clusters
invalid yaml here: [
  - broken
`
	configPath := createTempConfigFile(t, yaml)

	_, err := LoadConfig(configPath, nil)
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
	if cfg.MaxAgeNotReady != 10*time.Second {
		t.Errorf("Expected default max_age_not_ready 10s, got %v", cfg.MaxAgeNotReady)
	}
	if cfg.MaxAgeReady != 30*time.Minute {
		t.Errorf("Expected default max_age_ready 30m, got %v", cfg.MaxAgeReady)
	}
	if cfg.Clients.HyperFleetAPI.Timeout != 10*time.Second {
		t.Errorf("Expected default timeout 10s, got %v", cfg.Clients.HyperFleetAPI.Timeout)
	}
	// BaseURL has no default - must be set in config file
	if cfg.Clients.HyperFleetAPI.BaseURL != "" {
		t.Errorf("Expected no default base_url (empty string), got '%s'", cfg.Clients.HyperFleetAPI.BaseURL)
	}
	if len(cfg.ResourceSelector) != 0 {
		t.Errorf("Expected empty resource_selector, got %d items", len(cfg.ResourceSelector))
	}
	if cfg.MessageData != nil {
		t.Errorf("Expected nil message_data, got %v", cfg.MessageData)
	}
}

// ============================================================================
// Validation Tests - Required Fields
// ============================================================================

func TestValidate_MissingSentinelName(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.Sentinel.Name = ""
	cfg.ResourceType = "clusters"
	cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for missing sentinel.name, got nil")
	}
	if err.Error() != "sentinel.name is required" {
		t.Errorf("Expected 'sentinel.name is required' error, got: %v", err)
	}
}

func TestValidate_MissingResourceType(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = ""
	cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for missing resource_type, got nil")
	}
	if err.Error() != "resource_type is required" {
		t.Errorf("Expected 'resource_type is required' error, got: %v", err)
	}
}

func TestValidate_MissingBaseURL(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = "clusters" // Set valid resource_type to test base_url validation
	cfg.Clients.HyperFleetAPI.BaseURL = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for missing base_url, got nil")
	}
	if err.Error() != "clients.hyperfleet_api.base_url is required" {
		t.Errorf("Expected 'clients.hyperfleet_api.base_url is required' error, got: %v", err)
	}
}

// ============================================================================
// Validation Tests - Invalid Values
// ============================================================================

func TestValidate_InvalidResourceType(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = "invalid-type"
	cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"

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
		{"valid clusters", "clusters", false},
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
			cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"
			cfg.MessageData = map[string]interface{}{"id": "resource.id"}

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
		name     string
		modifier func(*SentinelConfig)
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
		{
			name: "negative max_age_not_ready",
			modifier: func(c *SentinelConfig) {
				c.MaxAgeNotReady = -10 * time.Second
			},
		},
		{
			name: "zero max_age_ready",
			modifier: func(c *SentinelConfig) {
				c.MaxAgeReady = 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewSentinelConfig()
			cfg.ResourceType = "clusters"
			cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"
			cfg.MessageData = map[string]interface{}{"id": "resource.id"}
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
	cfg.ResourceType = "clusters"
	cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"
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
	cfg.ResourceType = "clusters"
	cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"
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
	cfg.ResourceType = "clusters"
	cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"
	// MessageData is nil by default — message_data is required so this must fail

	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for nil message_data, got nil")
	}
}

func TestValidate_NilLeafInMessageData(t *testing.T) {
	// Mirrors YAML: `id:` — viper may drop the key, but if it doesn't the nil leaf must be rejected.
	cfg := NewSentinelConfig()
	cfg.ResourceType = "clusters"
	cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"
	cfg.MessageData = map[string]interface{}{
		"id":   nil,
		"kind": "resource.kind",
	}

	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for nil leaf in message_data, got nil")
	}
}

func TestValidate_EmptyStringLeafInMessageData(t *testing.T) {
	// Mirrors YAML: `id: ""` — an explicitly-set empty CEL expression.
	cfg := NewSentinelConfig()
	cfg.ResourceType = "clusters"
	cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"
	cfg.MessageData = map[string]interface{}{
		"id":   "",
		"kind": "resource.kind",
	}

	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for empty-string leaf in message_data, got nil")
	}
}

func TestValidate_NilLeafInNestedMessageData(t *testing.T) {
	// Ensures the recursive check reaches nested objects.
	cfg := NewSentinelConfig()
	cfg.ResourceType = "clusters"
	cfg.Clients.HyperFleetAPI.BaseURL = "http://api.example.com"
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
// Integration-like Test with Full Config
// ============================================================================

func TestLoadConfig_BlankMessageDataLeafReturnsError(t *testing.T) {
	// A blank leaf (e.g. `id:`) in message_data is decoded as nil by the YAML
	// parser. mapstructure then silently drops nil-valued keys during Unmarshal,
	// so the key disappears from cfg.MessageData before Validate() runs.
	// LoadConfig must catch this via the raw viper value.
	_, err := LoadConfig(filepath.Join("testdata", "message-data-blank-id.yaml"), nil)
	if err == nil {
		t.Fatal("expected error for blank message_data leaf, got nil")
	}
	if !strings.Contains(err.Error(), "message_data.id") {
		t.Errorf("expected error to mention message_data.id, got: %v", err)
	}
}

func TestLoadConfig_FullWorkflow(t *testing.T) {
	configPath := filepath.Join("testdata", "full-workflow.yaml")

	cfg, err := LoadConfig(configPath, nil)
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
}

// ============================================================================
// RedactedCopy Tests
// ============================================================================

func TestRedactedCopy_NilBrokerHandled(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.Clients.Broker = nil

	redacted := cfg.RedactedCopy()

	if redacted.Clients.Broker != nil {
		t.Errorf("Expected nil Broker to stay nil after redaction")
	}
}

func TestRedactedCopy_DoesNotMutateOriginal(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.Clients.Broker = &BrokerConfig{Topic: "my-topic"}

	_ = cfg.RedactedCopy()

	if cfg.Clients.Broker.Topic != "my-topic" {
		t.Errorf("RedactedCopy must not mutate the original; got '%s'", cfg.Clients.Broker.Topic)
	}
}

// ============================================================================
// Unknown Field Tests
// ============================================================================

func TestLoadConfig_UnknownFieldReturnsError(t *testing.T) {
	_, err := LoadConfig(filepath.Join("testdata", "unknown-field.yaml"), nil)
	if err == nil {
		t.Fatal("Expected error for unknown field 'resouce_type', got nil")
	}
}

func TestLoadConfig_UnknownFieldInline(t *testing.T) {
	yaml := `
sentinel:
  name: test-sentinel
clients:
  hyperfleet_api:
    base_url: http://localhost:8000
resource_type: clusters
message_data:
  id: resource.id
hyperfleet_api:
  endpoint: http://old-format.example.com
`
	configPath := createTempConfigFile(t, yaml)

	_, err := LoadConfig(configPath, nil)
	if err == nil {
		t.Fatal("Expected error for unknown field 'hyperfleet_api', got nil")
	}
}

// ============================================================================
// Topic Tests
// ============================================================================

func TestLoadConfig_TopicFromEnvVar(t *testing.T) {
	t.Setenv("HYPERFLEET_BROKER_TOPIC", "test-namespace-clusters")

	configPath := filepath.Join("testdata", "minimal.yaml")

	cfg, err := LoadConfig(configPath, nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Clients.Broker.Topic != "test-namespace-clusters" {
		t.Errorf("Expected topic 'test-namespace-clusters', got '%s'", cfg.Clients.Broker.Topic)
	}
}

func TestLoadConfig_TopicEnvVarOverridesConfig(t *testing.T) {
	t.Setenv("HYPERFLEET_BROKER_TOPIC", "env-topic")

	yaml := `
sentinel:
  name: test-sentinel
clients:
  hyperfleet_api:
    base_url: http://localhost:8000
  broker:
    topic: config-topic
resource_type: clusters
message_data:
  id: resource.id
`
	configPath := createTempConfigFile(t, yaml)

	cfg, err := LoadConfig(configPath, nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Clients.Broker.Topic != "env-topic" {
		t.Errorf("Expected topic 'env-topic' (from env), got '%s'", cfg.Clients.Broker.Topic)
	}
}

func TestLoadConfig_TopicFromConfigFile(t *testing.T) {
	yaml := `
sentinel:
  name: test-sentinel
clients:
  hyperfleet_api:
    base_url: http://localhost:8000
  broker:
    topic: my-namespace-clusters
resource_type: clusters
message_data:
  id: resource.id
`
	configPath := createTempConfigFile(t, yaml)

	cfg, err := LoadConfig(configPath, nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Clients.Broker.Topic != "my-namespace-clusters" {
		t.Errorf("Expected topic 'my-namespace-clusters', got '%s'", cfg.Clients.Broker.Topic)
	}
}

func TestLoadConfig_TopicEmpty(t *testing.T) {
	configPath := filepath.Join("testdata", "minimal.yaml")

	cfg, err := LoadConfig(configPath, nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Clients.Broker.Topic != "" {
		t.Errorf("Expected empty topic, got '%s'", cfg.Clients.Broker.Topic)
	}
}
