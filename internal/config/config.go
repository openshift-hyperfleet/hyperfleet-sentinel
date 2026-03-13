package config

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
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
	ResourceType     string                 `mapstructure:"resource_type"`
	PollInterval     time.Duration          `mapstructure:"poll_interval"`
	MaxAgeNotReady   time.Duration          `mapstructure:"max_age_not_ready"`
	MaxAgeReady      time.Duration          `mapstructure:"max_age_ready"`
	ResourceSelector LabelSelectorList      `mapstructure:"resource_selector"`
	HyperFleetAPI    *HyperFleetAPIConfig   `mapstructure:"hyperfleet_api"`
	MessageData      map[string]interface{} `mapstructure:"message_data"`
	Topic            string                 `mapstructure:"topic"`
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

	log := logger.NewHyperFleetLogger()
	ctx := context.Background()
	log.Infof(ctx, "Loading configuration from %s", configFile)

	v := viper.New()
	v.SetConfigFile(configFile)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate message_data leaves against the raw viper value, because
	// mapstructure silently drops nil-valued keys during Unmarshal — meaning
	// a blank `id:` in the YAML disappears before Validate() ever sees it.
	if rawMD, ok := v.Get("message_data").(map[string]interface{}); ok {
		if err := validateMessageDataLeaves(rawMD, "message_data"); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
	}

	// Override topic from environment variable if explicitly provided
	// Environment variable takes precedence over config file (including empty value to clear)
	if topic, ok := os.LookupEnv("BROKER_TOPIC"); ok {
		cfg.Topic = topic
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	log.Infof(ctx, "Configuration loaded successfully: resource_type=%s", cfg.ResourceType)

	return cfg, nil
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

	if c.MessageData == nil {
		return fmt.Errorf("message_data is required")
	}

	if err := validateMessageDataLeaves(c.MessageData, "message_data"); err != nil {
		return err
	}

	return nil
}

// validateMessageDataLeaves recursively checks that every leaf value in a
// message_data map is a non-empty string (CEL expression). nil values and
// empty strings are rejected early so that the error is reported at config
// load time rather than silently producing a broken payload.
func validateMessageDataLeaves(data map[string]interface{}, path string) error {
	for k, v := range data {
		fullKey := path + "." + k
		switch val := v.(type) {
		case nil:
			return fmt.Errorf("%s: nil value is not allowed; every leaf must be a non-empty CEL expression string (was the field left blank in the config?)", fullKey)
		case string:
			if val == "" {
				return fmt.Errorf("%s: empty CEL expression is not allowed; every leaf must be a non-empty CEL expression string", fullKey)
			}
		case map[string]interface{}:
			if err := validateMessageDataLeaves(val, fullKey); err != nil {
				return err
			}
		}
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
