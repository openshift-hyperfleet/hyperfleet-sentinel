package config

import (
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/viper"
)

// LabelSelector represents a label key-value pair for resource filtering
type LabelSelector struct {
	Label string `mapstructure:"label"`
	Value string `mapstructure:"value"`
}

// LabelSelectorList is a list of label selectors
type LabelSelectorList []LabelSelector

// SentinelConfig represents the Sentinel configuration
type SentinelConfig struct {
	ResourceType     string               `mapstructure:"resource_type"`
	PollInterval     time.Duration        `mapstructure:"poll_interval"`
	MaxAgeNotReady   time.Duration        `mapstructure:"max_age_not_ready"`
	MaxAgeReady      time.Duration        `mapstructure:"max_age_ready"`
	ResourceSelector LabelSelectorList    `mapstructure:"resource_selector"`
	HyperFleetAPI    *HyperFleetAPIConfig `mapstructure:"hyperfleet_api"`
	MessageData      map[string]string    `mapstructure:"message_data"`
	TopicPrefix      string               `mapstructure:"topic_prefix"`
}

// HyperFleetAPIConfig defines the HyperFleet API client configuration
type HyperFleetAPIConfig struct {
	Endpoint string        `mapstructure:"endpoint"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// ToMap converts label selectors to a map for filtering
func (ls LabelSelectorList) ToMap() map[string]string {
	if len(ls) == 0 {
		return nil
	}

	result := make(map[string]string, len(ls))
	for _, selector := range ls {
		if selector.Label != "" {
			result[selector.Label] = selector.Value
		}
	}
	return result
}

// NewSentinelConfig creates a new configuration with defaults
func NewSentinelConfig() *SentinelConfig {
	return &SentinelConfig{
		// ResourceType is required and must be set in config file
		PollInterval:     5 * time.Second,
		MaxAgeNotReady:   10 * time.Second,
		MaxAgeReady:      30 * time.Minute,
		ResourceSelector: []LabelSelector{}, // Empty means watch all resources
		HyperFleetAPI: &HyperFleetAPIConfig{
			// Endpoint is required and must be set in config file
			Timeout: 5 * time.Second,
		},
		MessageData: make(map[string]string),
	}
}

// LoadConfig loads configuration from YAML file and environment variables
// Precedence: Environment variables > YAML file > Defaults
func LoadConfig(configFile string) (*SentinelConfig, error) {
	cfg := NewSentinelConfig()

	// Load from YAML file
	if configFile == "" {
		return nil, fmt.Errorf("config file is required")
	}

	glog.Infof("Loading configuration from %s", configFile)

	v := viper.New()
	v.SetConfigFile(configFile)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Override topic_prefix from environment variable if set
	// Environment variable takes precedence over config file
	if prefix := os.Getenv("BROKER_TOPIC_PREFIX"); prefix != "" {
		cfg.TopicPrefix = prefix
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Validate message data templates
	if err := cfg.ValidateTemplates(); err != nil {
		return nil, fmt.Errorf("invalid message_data templates: %w", err)
	}

	glog.Infof("Configuration loaded successfully: resource_type=%s", cfg.ResourceType)

	return cfg, nil
}

// ValidateTemplates validates Go template syntax in message_data fields
// Templates are validated at startup to fail-fast on invalid configuration
func (c *SentinelConfig) ValidateTemplates() error {
	if len(c.MessageData) == 0 {
		glog.Warning("message_data is empty, CloudEvents will have minimal data payload")
		return nil
	}

	// Validate each template expression
	for key, tmplStr := range c.MessageData {
		// Wrap the template string in {{ }} if not already wrapped
		// This allows both ".id" and "{{.id}}" syntax in YAML
		if !strings.HasPrefix(tmplStr, "{{") {
			tmplStr = "{{" + tmplStr + "}}"
		}

		// Try to parse and validate the template
		_, err := template.New(key).Parse(tmplStr)
		if err != nil {
			return fmt.Errorf("invalid template for message_data.%s (%s): %w", key, c.MessageData[key], err)
		}
	}

	glog.V(2).Infof("Validated %d message_data templates", len(c.MessageData))
	return nil
}

// Validate validates the configuration
func (c *SentinelConfig) Validate() error {
	if c.ResourceType == "" {
		return fmt.Errorf("resource_type is required")
	}

	validResourceTypes := []string{"clusters", "nodepools"}
	if !contains(validResourceTypes, c.ResourceType) {
		return fmt.Errorf("invalid resource_type: %s (must be one of: %s)",
			c.ResourceType, strings.Join(validResourceTypes, ", "))
	}

	if c.HyperFleetAPI.Endpoint == "" {
		return fmt.Errorf("hyperfleet_api.endpoint is required")
	}

	if c.PollInterval <= 0 {
		return fmt.Errorf("poll_interval must be positive")
	}

	if c.MaxAgeNotReady <= 0 {
		return fmt.Errorf("max_age_not_ready must be positive")
	}

	if c.MaxAgeReady <= 0 {
		return fmt.Errorf("max_age_ready must be positive")
	}

	return nil
}

// contains checks if a string slice contains a value
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}
