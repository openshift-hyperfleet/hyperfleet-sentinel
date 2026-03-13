package config

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// EnvPrefix is the prefix for all environment variables that override sentinel config
const EnvPrefix = "HYPERFLEET"

// LabelSelector represents a label key-value pair for resource filtering
type LabelSelector struct {
	Label string `yaml:"label" mapstructure:"label"`
	Value string `yaml:"value" mapstructure:"value"`
}

// LabelSelectorList is a list of label selectors
type LabelSelectorList []LabelSelector

// SentinelConfig represents the Sentinel configuration
type SentinelConfig struct {
	Sentinel         SentinelInfo           `yaml:"sentinel" mapstructure:"sentinel"`
	DebugConfig      bool                   `yaml:"debug_config,omitempty" mapstructure:"debug_config"`
	Log              LogConfig              `yaml:"log,omitempty" mapstructure:"log"`
	Clients          ClientsConfig          `yaml:"clients" mapstructure:"clients"`
	ResourceType     string                 `yaml:"resource_type" mapstructure:"resource_type"`
	PollInterval     time.Duration          `yaml:"poll_interval" mapstructure:"poll_interval"`
	MaxAgeNotReady   time.Duration          `yaml:"max_age_not_ready" mapstructure:"max_age_not_ready"`
	MaxAgeReady      time.Duration          `yaml:"max_age_ready" mapstructure:"max_age_ready"`
	ResourceSelector LabelSelectorList      `yaml:"resource_selector,omitempty" mapstructure:"resource_selector"`
	MessageData      map[string]interface{} `yaml:"message_data,omitempty" mapstructure:"message_data"`
}

// SentinelInfo contains basic sentinel information
type SentinelInfo struct {
	Name string `yaml:"name" mapstructure:"name"`
}

// LogConfig contains logging configuration.
// Priority (lowest to highest): config file < HYPERFLEET_LOG_* env vars < --log-* CLI flags
type LogConfig struct {
	Level  string `yaml:"level,omitempty" mapstructure:"level"`
	Format string `yaml:"format,omitempty" mapstructure:"format"`
	Output string `yaml:"output,omitempty" mapstructure:"output"`
}

// ClientsConfig contains all client configurations
type ClientsConfig struct {
	HyperfleetAPI *HyperFleetAPIConfig `yaml:"hyperfleet_api" mapstructure:"hyperfleet_api"`
	Broker        *BrokerConfig        `yaml:"broker,omitempty" mapstructure:"broker"`
}

// HyperFleetAPIConfig defines the HyperFleet API client configuration
type HyperFleetAPIConfig struct {
	BaseURL        string            `yaml:"base_url" mapstructure:"base_url"`
	Version        string            `yaml:"version,omitempty" mapstructure:"version"`
	Timeout        time.Duration     `yaml:"timeout" mapstructure:"timeout"`
	RetryAttempts  int               `yaml:"retry_attempts,omitempty" mapstructure:"retry_attempts"`
	RetryBackoff   string            `yaml:"retry_backoff,omitempty" mapstructure:"retry_backoff"`
	BaseDelay      time.Duration     `yaml:"base_delay,omitempty" mapstructure:"base_delay"`
	MaxDelay       time.Duration     `yaml:"max_delay,omitempty" mapstructure:"max_delay"`
	DefaultHeaders map[string]string `yaml:"default_headers,omitempty" mapstructure:"default_headers"`
}

// BrokerConfig contains broker configuration
type BrokerConfig struct {
	Topic string `yaml:"topic,omitempty" mapstructure:"topic"`
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
		Sentinel: SentinelInfo{
			Name: "hyperfleet-sentinel",
		},
		DebugConfig: false,
		Log: LogConfig{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		},
		Clients: ClientsConfig{
			HyperfleetAPI: &HyperFleetAPIConfig{
				Version:       "v1",
				Timeout:       10 * time.Second,
				RetryAttempts: 3,
				RetryBackoff:  "exponential",
				BaseDelay:     1 * time.Second,
				MaxDelay:      30 * time.Second,
			},
			Broker: &BrokerConfig{},
		},
		// ResourceType is required and must be set in config file
		PollInterval:     5 * time.Second,
		MaxAgeNotReady:   10 * time.Second,
		MaxAgeReady:      30 * time.Minute,
		ResourceSelector: []LabelSelector{}, // Empty means watch all resources
	}
}

// viperKeyMappings defines mappings from config paths to env variable suffixes
// The full env var name is EnvPrefix + "_" + suffix
// Note: Uses "::" as key delimiter to avoid conflicts with dots in YAML keys
// Complex types (maps, slices) are intentionally excluded — they cannot be expressed as scalar env vars.
var viperKeyMappings = map[string]string{
	"debug_config":                                "DEBUG_CONFIG",
	"sentinel::name":                              "SENTINEL_NAME",
	"log::level":                                  "LOG_LEVEL",
	"log::format":                                 "LOG_FORMAT",
	"log::output":                                 "LOG_OUTPUT",
	"clients::hyperfleet_api::base_url":           "API_BASE_URL",
	"clients::hyperfleet_api::version":            "API_VERSION",
	"clients::hyperfleet_api::timeout":            "API_TIMEOUT",
	"clients::hyperfleet_api::retry_attempts":     "API_RETRY_ATTEMPTS",
	"clients::hyperfleet_api::retry_backoff":      "API_RETRY_BACKOFF",
	"clients::hyperfleet_api::base_delay":         "API_BASE_DELAY",
	"clients::hyperfleet_api::max_delay":          "API_MAX_DELAY",
	"clients::broker::topic":                      "BROKER_TOPIC",
	"resource_type":                               "RESOURCE_TYPE",
	"poll_interval":                               "POLL_INTERVAL",
	"max_age_not_ready":                           "MAX_AGE_NOT_READY",
	"max_age_ready":                               "MAX_AGE_READY",
}

// cliFlags defines mappings from CLI flag names to config paths
// Note: Uses "::" as key delimiter to avoid conflicts with dots in YAML keys
var cliFlags = map[string]string{
	"debug-config":                   "debug_config",
	"sentinel-name":                  "sentinel::name",
	"hyperfleet-api-base-url":        "clients::hyperfleet_api::base_url",
	"hyperfleet-api-version":         "clients::hyperfleet_api::version",
	"hyperfleet-api-timeout":         "clients::hyperfleet_api::timeout",
	"hyperfleet-api-retry-attempts":  "clients::hyperfleet_api::retry_attempts",
	"hyperfleet-api-retry-backoff":   "clients::hyperfleet_api::retry_backoff",
	"hyperfleet-api-base-delay":      "clients::hyperfleet_api::base_delay",
	"hyperfleet-api-max-delay":       "clients::hyperfleet_api::max_delay",
	"broker-topic":                   "clients::broker::topic",
	"resource-type":                  "resource_type",
	"poll-interval":                  "poll_interval",
	"max-age-not-ready":              "max_age_not_ready",
	"max-age-ready":                  "max_age_ready",
	"log-level":                      "log::level",
	"log-format":                     "log::format",
	"log-output":                     "log::output",
}

// LoadConfig loads configuration from YAML file with environment variable and CLI flag overrides
// Precedence: CLI flags > Environment variables > YAML file > Defaults
func LoadConfig(configFile string, flags *pflag.FlagSet) (*SentinelConfig, error) {
	cfg := NewSentinelConfig()

	if configFile == "" {
		if env := os.Getenv("HYPERFLEET_CONFIG"); env != "" {
			configFile = env
		} else {
			configFile = "/etc/sentinel/config.yaml"
		}
	}

	log := logger.NewHyperFleetLogger()
	ctx := context.Background()
	log.Infof(ctx, "Loading configuration from %s", configFile)

	// Use "::" as key delimiter to avoid conflicts with dots in YAML keys
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetConfigFile(configFile)

	// Read the YAML file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Bind environment variables
	v.SetEnvPrefix(EnvPrefix)
	v.AutomaticEnv()
	// Replace "::" (our key delimiter) and "-" with "_" for env var lookups
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_", "-", "_"))

	// Bind specific environment variables with HYPERFLEET_ prefix
	for configPath, envSuffix := range viperKeyMappings {
		envVar := EnvPrefix + "_" + envSuffix
		if val := os.Getenv(envVar); val != "" {
			v.Set(configPath, val)
		}
	}

	// Bind CLI flags if provided
	if flags != nil {
		for flagName, configPath := range cliFlags {
			if flag := flags.Lookup(flagName); flag != nil && flag.Changed {
				v.Set(configPath, flag.Value.String())
			}
		}
	}

	// Unmarshal into SentinelConfig struct — ErrorUnused ensures unknown fields are rejected
	if err := v.UnmarshalExact(cfg); err != nil {
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

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	log.Infof(ctx, "Configuration loaded successfully: name=%s resource_type=%s",
		cfg.Sentinel.Name, cfg.ResourceType)

	return cfg, nil
}

// Validate validates the configuration
func (c *SentinelConfig) Validate() error {
	if c.Sentinel.Name == "" {
		return fmt.Errorf("sentinel.name is required")
	}

	if c.ResourceType == "" {
		return fmt.Errorf("resource_type is required")
	}

	validResourceTypes := []string{"clusters", "nodepools"}
	if !contains(validResourceTypes, c.ResourceType) {
		return fmt.Errorf("invalid resource_type: %s (must be one of: %s)",
			c.ResourceType, strings.Join(validResourceTypes, ", "))
	}

	if c.Clients.HyperfleetAPI == nil {
		return fmt.Errorf("clients.hyperfleet_api is required")
	}

	if c.Clients.HyperfleetAPI.BaseURL == "" {
		return fmt.Errorf("clients.hyperfleet_api.base_url is required")
	}

	if c.Clients.HyperfleetAPI.RetryBackoff != "" {
		validBackoffs := []string{"exponential", "linear", "constant"}
		if !contains(validBackoffs, c.Clients.HyperfleetAPI.RetryBackoff) {
			return fmt.Errorf("invalid clients.hyperfleet_api.retry_backoff: %s (must be one of: %s)",
				c.Clients.HyperfleetAPI.RetryBackoff, strings.Join(validBackoffs, ", "))
		}
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

// RedactedCopy returns a deep copy of the config. Use this copy when logging
// the merged configuration at startup so that any future sensitive fields are
// never accidentally shared by reference.
func (c *SentinelConfig) RedactedCopy() *SentinelConfig {
	cp := *c

	if cp.Clients.HyperfleetAPI != nil {
		api := *cp.Clients.HyperfleetAPI
		if api.DefaultHeaders != nil {
			headers := make(map[string]string, len(api.DefaultHeaders))
			for k, v := range api.DefaultHeaders {
				headers[k] = v
			}
			api.DefaultHeaders = headers
		}
		cp.Clients.HyperfleetAPI = &api
	}

	if cp.Clients.Broker != nil {
		b := *cp.Clients.Broker
		cp.Clients.Broker = &b
	}

	if c.ResourceSelector != nil {
		rs := make(LabelSelectorList, len(c.ResourceSelector))
		copy(rs, c.ResourceSelector)
		cp.ResourceSelector = rs
	}

	if c.MessageData != nil {
		md := make(map[string]interface{}, len(c.MessageData))
		for k, v := range c.MessageData {
			md[k] = v
		}
		cp.MessageData = md
	}

	return &cp
}
