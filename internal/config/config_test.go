package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Helper function to create a temporary config file for testing
func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(content), 0600)
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	return configPath
}

// Helper function to set environment variables for testing
func setEnvVars(t *testing.T, vars map[string]string) {
	t.Helper()
	for key, value := range vars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set env var %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		for key := range vars {
			_ = os.Unsetenv(key) // Ignore error in cleanup
		}
	})
}

// ============================================================================
// Loading & Parsing Tests
// ============================================================================

func TestLoadConfig_ValidComplete(t *testing.T) {
	configPath := filepath.Join("testdata", "valid-complete.yaml")

	setEnvVars(t, map[string]string{
		"BROKER_TYPE":       "pubsub",
		"BROKER_PROJECT_ID": "test-project",
	})

	cfg, err := LoadConfig(configPath)
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
	if cfg.HyperFleetAPI.Endpoint != "https://api.hyperfleet.example.com" {
		t.Errorf("Expected endpoint 'https://api.hyperfleet.example.com', got '%s'", cfg.HyperFleetAPI.Endpoint)
	}
	if cfg.HyperFleetAPI.Timeout != 5*time.Second {
		t.Errorf("Expected timeout 5s, got %v", cfg.HyperFleetAPI.Timeout)
	}

	// Verify message data
	if len(cfg.MessageData) != 3 {
		t.Errorf("Expected 3 message_data fields, got %d", len(cfg.MessageData))
	}
	if cfg.MessageData["resource_id"] != ".id" {
		t.Errorf("Expected message_data.resource_id '.id', got '%s'", cfg.MessageData["resource_id"])
	}

	// Verify broker
	if cfg.Broker == nil {
		t.Fatal("Expected broker config, got nil")
	}
	if cfg.Broker.Type() != "pubsub" {
		t.Errorf("Expected broker type 'pubsub', got '%s'", cfg.Broker.Type())
	}
}

func TestLoadConfig_Minimal(t *testing.T) {
	configPath := filepath.Join("testdata", "minimal.yaml")

	setEnvVars(t, map[string]string{
		"BROKER_TYPE":       "pubsub",
		"BROKER_PROJECT_ID": "test-project",
	})

	cfg, err := LoadConfig(configPath)
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
	if cfg.MaxAgeNotReady != 10*time.Second {
		t.Errorf("Expected default max_age_not_ready 10s, got %v", cfg.MaxAgeNotReady)
	}
	if cfg.MaxAgeReady != 30*time.Minute {
		t.Errorf("Expected default max_age_ready 30m, got %v", cfg.MaxAgeReady)
	}
	if cfg.HyperFleetAPI.Timeout != 5*time.Second {
		t.Errorf("Expected default timeout 30s, got %v", cfg.HyperFleetAPI.Timeout)
	}
	// Endpoint has no default - must be set in config file
	if cfg.HyperFleetAPI.Endpoint != "" {
		t.Errorf("Expected no default endpoint (empty string), got '%s'", cfg.HyperFleetAPI.Endpoint)
	}
	if len(cfg.ResourceSelector) != 0 {
		t.Errorf("Expected empty resource_selector, got %d items", len(cfg.ResourceSelector))
	}
	if len(cfg.MessageData) != 0 {
		t.Errorf("Expected empty message_data, got %d items", len(cfg.MessageData))
	}
}

// ============================================================================
// Validation Tests - Required Fields
// ============================================================================

func TestValidate_MissingResourceType(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.ResourceType = ""
	cfg.HyperFleetAPI.Endpoint = "http://api.example.com"

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
	cfg.ResourceType = "clusters" // Set valid resource_type to test endpoint validation
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
	cfg.HyperFleetAPI.Endpoint = "http://api.example.com"

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
			cfg.HyperFleetAPI.Endpoint = "http://api.example.com"

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
			cfg.HyperFleetAPI.Endpoint = "http://api.example.com"
			tt.modifier(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Expected error for invalid duration, got nil")
			}
		})
	}
}

// ============================================================================
// Broker Configuration Tests
// ============================================================================

func TestLoadBrokerConfig_RabbitMQ(t *testing.T) {
	setEnvVars(t, map[string]string{
		"BROKER_TYPE":          "rabbitmq",
		"BROKER_HOST":          "rabbitmq.example.com",
		"BROKER_PORT":          "5672",
		"BROKER_VHOST":         "/prod",
		"BROKER_EXCHANGE":      "hyperfleet-events",
		"BROKER_EXCHANGE_TYPE": "fanout",
	})

	broker, err := LoadBrokerConfig()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if broker.Type() != "rabbitmq" {
		t.Errorf("Expected broker type 'rabbitmq', got '%s'", broker.Type())
	}

	rmqCfg, ok := broker.(*RabbitMQBrokerConfig)
	if !ok {
		t.Fatal("Expected RabbitMQBrokerConfig type")
	}

	if rmqCfg.Host != "rabbitmq.example.com" {
		t.Errorf("Expected host 'rabbitmq.example.com', got '%s'", rmqCfg.Host)
	}
	if rmqCfg.Port != "5672" {
		t.Errorf("Expected port '5672', got '%s'", rmqCfg.Port)
	}
	if rmqCfg.Exchange != "hyperfleet-events" {
		t.Errorf("Expected exchange 'hyperfleet-events', got '%s'", rmqCfg.Exchange)
	}
}

func TestLoadBrokerConfig_SQS(t *testing.T) {
	setEnvVars(t, map[string]string{
		"BROKER_TYPE":      "awsSqs",
		"BROKER_REGION":    "us-east-1",
		"BROKER_QUEUE_URL": "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
	})

	broker, err := LoadBrokerConfig()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if broker.Type() != "awsSqs" {
		t.Errorf("Expected broker type 'awsSqs', got '%s'", broker.Type())
	}

	sqsCfg, ok := broker.(*SQSBrokerConfig)
	if !ok {
		t.Fatal("Expected SQSBrokerConfig type")
	}

	if sqsCfg.Region != "us-east-1" {
		t.Errorf("Expected region 'us-east-1', got '%s'", sqsCfg.Region)
	}
	if sqsCfg.QueueURL != "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue" {
		t.Errorf("Expected queue_url, got '%s'", sqsCfg.QueueURL)
	}
}

func TestLoadBrokerConfig_MissingType(t *testing.T) {
	// Ensure no BROKER_TYPE is set
	_ = os.Unsetenv("BROKER_TYPE") // Ignore error - we want it unset

	_, err := LoadBrokerConfig()
	if err == nil {
		t.Fatal("Expected error for missing BROKER_TYPE, got nil")
	}
	if err.Error() != "BROKER_TYPE environment variable is required" {
		t.Errorf("Expected 'BROKER_TYPE environment variable is required' error, got: %v", err)
	}
}

func TestLoadBrokerConfig_InvalidType(t *testing.T) {
	setEnvVars(t, map[string]string{
		"BROKER_TYPE": "invalid-broker",
	})

	_, err := LoadBrokerConfig()
	if err == nil {
		t.Fatal("Expected error for invalid BROKER_TYPE, got nil")
	}
}

// ============================================================================
// Broker Validation Tests
// ============================================================================

func TestPubSubBrokerConfig_Validate_MissingProjectID(t *testing.T) {
	cfg := &PubSubBrokerConfig{
		ProjectID: "",
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for missing project_id, got nil")
	}
	if err.Error() != "BROKER_PROJECT_ID is required for pubsub broker" {
		t.Errorf("Expected 'BROKER_PROJECT_ID is required' error, got: %v", err)
	}
}

func TestRabbitMQBrokerConfig_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *RabbitMQBrokerConfig
		errMsg string
	}{
		{
			name:   "missing host",
			cfg:    &RabbitMQBrokerConfig{Port: "5672", Exchange: "test"},
			errMsg: "BROKER_HOST is required for rabbitmq broker",
		},
		{
			name:   "missing port",
			cfg:    &RabbitMQBrokerConfig{Host: "localhost", Exchange: "test"},
			errMsg: "BROKER_PORT is required for rabbitmq broker",
		},
		{
			name:   "missing exchange",
			cfg:    &RabbitMQBrokerConfig{Host: "localhost", Port: "5672"},
			errMsg: "BROKER_EXCHANGE is required for rabbitmq broker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatal("Expected validation error, got nil")
			}
			if err.Error() != tt.errMsg {
				t.Errorf("Expected error '%s', got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestSQSBrokerConfig_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *SQSBrokerConfig
		errMsg string
	}{
		{
			name:   "missing region",
			cfg:    &SQSBrokerConfig{QueueURL: "https://sqs.us-east-1.amazonaws.com/123/queue"},
			errMsg: "BROKER_REGION is required for awsSqs broker",
		},
		{
			name:   "missing queue_url",
			cfg:    &SQSBrokerConfig{Region: "us-east-1"},
			errMsg: "BROKER_QUEUE_URL is required for awsSqs broker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatal("Expected validation error, got nil")
			}
			if err.Error() != tt.errMsg {
				t.Errorf("Expected error '%s', got: %v", tt.errMsg, err)
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
// Template Validation Tests
// ============================================================================

func TestValidateTemplates_Valid(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.MessageData = map[string]string{
		"resource_id":   ".id",
		"resource_type": ".kind",
		"region":        ".metadata.labels.region",
	}

	err := cfg.ValidateTemplates()
	if err != nil {
		t.Errorf("Expected no error for valid templates, got: %v", err)
	}
}

func TestValidateTemplates_ValidWithBraces(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.MessageData = map[string]string{
		"resource_id": "{{.id}}",
		"complex":     "{{if .metadata.name}}{{.metadata.name}}{{else}}unknown{{end}}",
	}

	err := cfg.ValidateTemplates()
	if err != nil {
		t.Errorf("Expected no error for valid templates with braces, got: %v", err)
	}
}

func TestValidateTemplates_Invalid(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.MessageData = map[string]string{
		"valid":   ".id",
		"invalid": "{{.id",
	}

	err := cfg.ValidateTemplates()
	if err == nil {
		t.Fatal("Expected error for invalid template syntax, got nil")
	}
}

func TestValidateTemplates_Empty(t *testing.T) {
	cfg := NewSentinelConfig()
	cfg.MessageData = map[string]string{}

	// Empty message_data should not error, just log a warning
	err := cfg.ValidateTemplates()
	if err != nil {
		t.Errorf("Expected no error for empty message_data, got: %v", err)
	}
}

// ============================================================================
// Integration-like Test with Full Config
// ============================================================================

func TestLoadConfig_FullWorkflow(t *testing.T) {
	configPath := filepath.Join("testdata", "full-workflow.yaml")

	setEnvVars(t, map[string]string{
		"BROKER_TYPE":     "rabbitmq",
		"BROKER_HOST":     "rabbitmq.local",
		"BROKER_PORT":     "5672",
		"BROKER_EXCHANGE": "events",
	})

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
	if len(cfg.MessageData) != 4 {
		t.Errorf("Expected 4 message_data fields, got %d", len(cfg.MessageData))
	}
	if cfg.Broker.Type() != "rabbitmq" {
		t.Errorf("Expected broker type 'rabbitmq', got '%s'", cfg.Broker.Type())
	}
}
