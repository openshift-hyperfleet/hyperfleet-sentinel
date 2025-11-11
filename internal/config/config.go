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
	BackoffNotReady  time.Duration        `mapstructure:"backoff_not_ready"`
	BackoffReady     time.Duration        `mapstructure:"backoff_ready"`
	ResourceSelector LabelSelectorList    `mapstructure:"resource_selector"`
	HyperFleetAPI    *HyperFleetAPIConfig `mapstructure:"hyperfleet_api"`
	MessageData      map[string]string    `mapstructure:"message_data"`
	Broker           BrokerConfig         `mapstructure:"-"` // Loaded from env vars
}

// HyperFleetAPIConfig defines the HyperFleet API client configuration
type HyperFleetAPIConfig struct {
	Endpoint string        `mapstructure:"endpoint"`
	Timeout  time.Duration `mapstructure:"timeout"`
	Token    string        `mapstructure:"-"` // Loaded from HYPERFLEET_API_TOKEN env var
}

// BrokerConfig is the interface for broker configurations
type BrokerConfig interface {
	Type() string
	Validate() error
}

// PubSubBrokerConfig defines Google Cloud Pub/Sub broker configuration
type PubSubBrokerConfig struct {
	ProjectID string
}

func (c *PubSubBrokerConfig) Type() string {
	return "pubsub"
}

func (c *PubSubBrokerConfig) Validate() error {
	if c.ProjectID == "" {
		return fmt.Errorf("BROKER_PROJECT_ID is required for pubsub broker")
	}
	return nil
}

// SQSBrokerConfig defines AWS SQS broker configuration
type SQSBrokerConfig struct {
	Region   string
	QueueURL string
}

func (c *SQSBrokerConfig) Type() string {
	return "awsSqs"
}

func (c *SQSBrokerConfig) Validate() error {
	if c.Region == "" {
		return fmt.Errorf("BROKER_REGION is required for awsSqs broker")
	}
	if c.QueueURL == "" {
		return fmt.Errorf("BROKER_QUEUE_URL is required for awsSqs broker")
	}
	return nil
}

// RabbitMQBrokerConfig defines RabbitMQ broker configuration
type RabbitMQBrokerConfig struct {
	Host         string
	Port         string
	VHost        string
	Exchange     string
	ExchangeType string
}

func (c *RabbitMQBrokerConfig) Type() string {
	return "rabbitmq"
}

func (c *RabbitMQBrokerConfig) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("BROKER_HOST is required for rabbitmq broker")
	}
	if c.Port == "" {
		return fmt.Errorf("BROKER_PORT is required for rabbitmq broker")
	}
	if c.Exchange == "" {
		return fmt.Errorf("BROKER_EXCHANGE is required for rabbitmq broker")
	}
	return nil
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
		ResourceType:     "clusters",
		PollInterval:     5 * time.Second,
		BackoffNotReady:  10 * time.Second,
		BackoffReady:     30 * time.Minute,
		ResourceSelector: []LabelSelector{}, // Empty means watch all resources
		HyperFleetAPI: &HyperFleetAPIConfig{
			Endpoint: "",
			Timeout:  10 * time.Second,
			Token:    "",
		},
		MessageData: make(map[string]string),
		Broker:      nil, // Loaded from environment variables
	}
}

// LoadConfig loads configuration from YAML file and environment variables
// Precedence: Environment variables > YAML file > Defaults
// Secrets (tokens, broker credentials) are loaded exclusively from environment variables
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

	// Override with environment variables
	applyEnvVarOverrides(cfg)

	// Load broker configuration from environment variables
	broker, err := LoadBrokerConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load broker config: %w", err)
	}
	cfg.Broker = broker

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Validate message data templates
	if err := cfg.ValidateTemplates(); err != nil {
		return nil, fmt.Errorf("invalid message_data templates: %w", err)
	}

	glog.Infof("Configuration loaded successfully: resource_type=%s broker_type=%s",
		cfg.ResourceType, cfg.Broker.Type())

	return cfg, nil
}

// applyEnvVarOverrides applies environment variable overrides
func applyEnvVarOverrides(cfg *SentinelConfig) {
	// Override HyperFleet API token from environment variable
	if token := os.Getenv("HYPERFLEET_API_TOKEN"); token != "" {
		cfg.HyperFleetAPI.Token = token
	}
}

// LoadBrokerConfig loads broker configuration from environment variables
// Returns the appropriate BrokerConfig implementation based on BROKER_TYPE
func LoadBrokerConfig() (BrokerConfig, error) {
	brokerType := os.Getenv("BROKER_TYPE")
	if brokerType == "" {
		return nil, fmt.Errorf("BROKER_TYPE environment variable is required")
	}

	var broker BrokerConfig

	switch brokerType {
	case "pubsub":
		cfg := &PubSubBrokerConfig{
			ProjectID: os.Getenv("BROKER_PROJECT_ID"),
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		broker = cfg

	case "awsSqs":
		cfg := &SQSBrokerConfig{
			Region:   os.Getenv("BROKER_REGION"),
			QueueURL: os.Getenv("BROKER_QUEUE_URL"),
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		broker = cfg

	case "rabbitmq":
		cfg := &RabbitMQBrokerConfig{
			Host:         os.Getenv("BROKER_HOST"),
			Port:         os.Getenv("BROKER_PORT"),
			VHost:        os.Getenv("BROKER_VHOST"),
			Exchange:     os.Getenv("BROKER_EXCHANGE"),
			ExchangeType: os.Getenv("BROKER_EXCHANGE_TYPE"),
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		broker = cfg

	default:
		return nil, fmt.Errorf("unsupported BROKER_TYPE: %s (must be pubsub, awsSqs, or rabbitmq)", brokerType)
	}

	return broker, nil
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

	validResourceTypes := []string{"clusters", "nodepools", "manifests", "workloads"}
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

	if c.BackoffNotReady <= 0 {
		return fmt.Errorf("backoff_not_ready must be positive")
	}

	if c.BackoffReady <= 0 {
		return fmt.Errorf("backoff_ready must be positive")
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
