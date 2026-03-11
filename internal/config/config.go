package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	defaultMessagingSystem = "gcp_pubsub"
	defaultConfigFile      = "/etc/hyperfleet/config.yaml"
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

// MessageDecisionConfig represents configurable CEL-based decision logic.
// Params are named CEL expressions evaluated in dependency order.
// Result is a CEL expression that evaluates to a boolean.
type MessageDecisionConfig struct {
	Params map[string]string `mapstructure:"params"`
	Result string            `mapstructure:"result"`
}

// SentinelConfig represents the Sentinel configuration
type SentinelConfig struct {
	Log              LogConfig              `yaml:"log,omitempty" mapstructure:"log"`
	Sentinel         SentinelInfo           `yaml:"sentinel" mapstructure:"sentinel"`
	ResourceType     string                 `yaml:"resource_type" mapstructure:"resource_type"`
	MessagingSystem  string                 `yaml:"messaging_system,omitempty" mapstructure:"messaging_system"`
	Clients          ClientsConfig          `yaml:"clients" mapstructure:"clients"`
	MessageData      map[string]interface{} `yaml:"message_data,omitempty" mapstructure:"message_data"`
	MessageDecision  *MessageDecisionConfig `yaml:"message_decision,omitempty" mapstructure:"message_decision"`
	ResourceSelector LabelSelectorList      `yaml:"resource_selector,omitempty" mapstructure:"resource_selector"`
	PollInterval     time.Duration          `yaml:"poll_interval" mapstructure:"poll_interval"`
	DebugConfig      bool                   `yaml:"debug_config,omitempty" mapstructure:"debug_config"`
	TracingEnabled   bool                   `yaml:"tracing_enabled,omitempty" mapstructure:"tracing_enabled"`
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
	HyperFleetAPI *HyperFleetAPIConfig `yaml:"hyperfleet_api" mapstructure:"hyperfleet_api"`
	Broker        *BrokerConfig        `yaml:"broker,omitempty" mapstructure:"broker"`
}

// HyperFleetAPIConfig defines the HyperFleet API client configuration
type HyperFleetAPIConfig struct {
	BaseURL string        `yaml:"base_url" mapstructure:"base_url"`
	Version string        `yaml:"version,omitempty" mapstructure:"version"`
	Timeout time.Duration `yaml:"timeout" mapstructure:"timeout"`
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

// DefaultMessageDecision returns the default message_decision configuration
// used when message_decision is not set in the config file.
func DefaultMessageDecision() *MessageDecisionConfig {
	return &MessageDecisionConfig{
		Params: map[string]string{
			"ref_time":                `condition("Ready").last_updated_time`,
			"is_ready":                `condition("Ready").status == "True"`,
			"has_ref_time":            `ref_time != ""`,
			"is_new_resource":         `!is_ready && resource.generation == 1`,
			"generation_mismatch":     `resource.generation > condition("Ready").observed_generation`,
			"ready_and_stale":         `is_ready && has_ref_time && now - timestamp(ref_time) > duration("30m")`,
			"not_ready_and_debounced": `!is_ready && has_ref_time && now - timestamp(ref_time) > duration("10s")`,
		},
		Result: "is_new_resource || generation_mismatch || ready_and_stale || not_ready_and_debounced",
	}
}

// NewSentinelConfig creates a new configuration with defaults
func NewSentinelConfig() *SentinelConfig {
	return &SentinelConfig{
		Sentinel: SentinelInfo{
			Name: "hyperfleet-sentinel",
		},
		DebugConfig:    false,
		TracingEnabled: true,
		Log: LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
		Clients: ClientsConfig{
			HyperFleetAPI: &HyperFleetAPIConfig{
				Version: "v1",
				Timeout: 10 * time.Second,
			},
			Broker: &BrokerConfig{},
		},
		// ResourceType is required and must be set in config file
		PollInterval:     5 * time.Second,
		ResourceSelector: []LabelSelector{}, // Empty means watch all resources
		MessagingSystem:  defaultMessagingSystem,
	}
}

// viperKeyMappings defines mappings from config paths to env variable suffixes
// The full env var name is EnvPrefix + "_" + suffix
// Note: Uses "::" as key delimiter to avoid conflicts with dots in YAML keys
// Complex types (maps, slices) are intentionally excluded — they cannot be expressed as scalar env vars.
var viperKeyMappings = map[string]string{
	"debug_config":                      "DEBUG_CONFIG",
	"sentinel::name":                    "SENTINEL_NAME",
	"log::level":                        "LOG_LEVEL",
	"log::format":                       "LOG_FORMAT",
	"log::output":                       "LOG_OUTPUT",
	"clients::hyperfleet_api::base_url": "API_BASE_URL",
	"clients::hyperfleet_api::version":  "API_VERSION",
	"clients::hyperfleet_api::timeout":  "API_TIMEOUT",
	"clients::broker::topic":            "BROKER_TOPIC",
	"resource_type":                     "RESOURCE_TYPE",
	"poll_interval":                     "POLL_INTERVAL",
	"messaging_system":                  "MESSAGING_SYSTEM",
	"tracing_enabled":                   "TRACING_ENABLED",
}

// cliFlags defines mappings from CLI flag names to config paths
// Note: Uses "::" as key delimiter to avoid conflicts with dots in YAML keys
var cliFlags = map[string]string{
	"debug-config":            "debug_config",
	"name":                    "sentinel::name",
	"hyperfleet-api-base-url": "clients::hyperfleet_api::base_url",
	"hyperfleet-api-version":  "clients::hyperfleet_api::version",
	"hyperfleet-api-timeout":  "clients::hyperfleet_api::timeout",
	"broker-topic":            "clients::broker::topic",
	"resource-type":           "resource_type",
	"poll-interval":           "poll_interval",
	"log-level":               "log::level",
	"log-format":              "log::format",
	"log-output":              "log::output",
	"tracing-enabled":         "tracing_enabled",
}

// LoadConfig loads configuration from YAML file with environment variable and CLI flag overrides
// Precedence: CLI flags > Environment variables > YAML file > Defaults
func LoadConfig(configFile string, flags *pflag.FlagSet) (*SentinelConfig, error) {
	cfg := NewSentinelConfig()

	if configFile == "" {
		if env := os.Getenv("HYPERFLEET_CONFIG"); env != "" {
			configFile = env
		} else {
			configFile = defaultConfigFile
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

	// Bind environment variables into viper's env layer so they sit below CLI
	// flag overrides (v.Set) but above the config file layer.
	for configPath, envSuffix := range viperKeyMappings {
		if err := v.BindEnv(configPath, EnvPrefix+"_"+envSuffix); err != nil {
			return nil, fmt.Errorf("failed to bind env var %s_%s: %w", EnvPrefix, envSuffix, err)
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

	// Apply default message_decision if not configured
	if cfg.MessageDecision == nil {
		cfg.MessageDecision = DefaultMessageDecision()
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	log.Infof(ctx, "Configuration loaded successfully: name=%s resource_type=%s",
		cfg.Sentinel.Name, cfg.ResourceType)

	return cfg, nil
}

// fieldRemediation describes how a user can set a configuration field.
type fieldRemediation struct {
	Flag string // empty if not settable via CLI flag
	Env  string // empty if not settable via env var
	File string // YAML path in the config file
}

// fieldRemediations maps config field identifiers to their remediation hints.
var fieldRemediations = map[string]fieldRemediation{
	"sentinel.name": {
		Flag: "--name",
		Env:  "HYPERFLEET_SENTINEL_NAME",
		File: "sentinel.name",
	},
	"resource_type": {
		Flag: "--resource-type",
		Env:  "HYPERFLEET_RESOURCE_TYPE",
		File: "resource_type",
	},
	"clients.hyperfleet_api": {
		File: "clients.hyperfleet_api",
	},
	"clients.hyperfleet_api.base_url": {
		Flag: "--hyperfleet-api-base-url",
		Env:  "HYPERFLEET_API_BASE_URL",
		File: "clients.hyperfleet_api.base_url",
	},
	"poll_interval": {
		Flag: "--poll-interval",
		Env:  "HYPERFLEET_POLL_INTERVAL",
		File: "poll_interval",
	},
	"message_decision": {
		File: "message_decision",
	},
	"message_data": {
		File: "message_data",
	},
	"tracing_enabled": {
		Flag: "--tracing-enabled",
		Env:  "HYPERFLEET_TRACING_ENABLED",
		File: "tracing_enabled",
	},
}

// validationErr returns a validation error for the given field with remediation guidance.
// The optional actualValue parameter includes the offending value in the error message,
// which is useful for invalid-value failures (e.g. poll_interval: -1s) so operators
// can immediately see what was set without having to re-inspect the config file.
func validationErr(field, reason string, actualValue ...string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Field '%s' failed validation: %s\n", field, reason)
	if len(actualValue) > 0 && actualValue[0] != "" {
		fmt.Fprintf(&b, "  Actual value: %s\n", actualValue[0])
	}
	fmt.Fprintf(&b, "Please provide via:\n")
	if rem, ok := fieldRemediations[field]; ok {
		if rem.Flag != "" {
			fmt.Fprintf(&b, "  • Flag: %s\n", rem.Flag)
		}
		if rem.Env != "" {
			fmt.Fprintf(&b, "  • Env:  %s\n", rem.Env)
		}
		if rem.File != "" {
			fmt.Fprintf(&b, "  • File: %s\n", rem.File)
		}
	}
	return errors.New(b.String())
}

// Validate validates the configuration
func (c *SentinelConfig) Validate() error {
	if c.Sentinel.Name == "" {
		return validationErr("sentinel.name", "required")
	}

	if c.ResourceType == "" {
		return validationErr("resource_type", "required")
	}

	validResourceTypes := []string{"clusters", "nodepools"}
	if !contains(validResourceTypes, c.ResourceType) {
		return validationErr("resource_type", fmt.Sprintf("invalid value %q (must be one of: %s)",
			c.ResourceType, strings.Join(validResourceTypes, ", ")))
	}

	if c.Clients.HyperFleetAPI == nil {
		return validationErr("clients.hyperfleet_api", "required")
	}

	if c.Clients.HyperFleetAPI.BaseURL == "" {
		return validationErr("clients.hyperfleet_api.base_url", "required")
	}

	if c.PollInterval <= 0 {
		return validationErr("poll_interval", "must be positive", c.PollInterval.String())
	}

	if c.MessageDecision == nil {
		return validationErr("message_decision", "required")
	}

	if err := c.MessageDecision.Validate(); err != nil {
		return fmt.Errorf("message_decision: %w", err)
	}

	if c.MessageData == nil {
		return validationErr("message_data", "required")
	}

	if err := validateMessageDataLeaves(c.MessageData, "message_data"); err != nil {
		return err
	}

	return nil
}

// Validate validates the message decision configuration
func (md *MessageDecisionConfig) Validate() error {
	if md.Result == "" {
		return fmt.Errorf("result expression is required")
	}

	for name, expr := range md.Params {
		if expr == "" {
			return fmt.Errorf("param %q has empty expression", name)
		}
	}

	// Check for circular dependencies
	if _, err := md.TopologicalSort(); err != nil {
		return err
	}

	return nil
}

// TopologicalSort resolves param evaluation order based on inter-param dependencies.
// Returns an ordered list of param names or an error if circular dependencies exist.
func (md *MessageDecisionConfig) TopologicalSort() ([]string, error) {
	// Build dependency graph: for each param, find which other params it references
	deps := make(map[string][]string, len(md.Params))
	paramNames := make(map[string]bool, len(md.Params))
	for name := range md.Params {
		paramNames[name] = true
		deps[name] = nil
	}

	for name, expr := range md.Params {
		for otherName := range md.Params {
			if otherName != name && containsIdentifier(expr, otherName) {
				deps[name] = append(deps[name], otherName)
			}
		}
	}

	// Kahn's algorithm for topological sort
	inDegree := make(map[string]int, len(md.Params))
	for name := range md.Params {
		inDegree[name] = 0
	}
	for _, dependencies := range deps {
		for _, dep := range dependencies {
			inDegree[dep]++
		}
	}

	// Wait — inDegree should count how many params depend ON each param,
	// but we need the reverse: how many dependencies each param HAS.
	// Actually for topological sort, we need: for each node, its in-degree
	// is the number of edges pointing TO it. In our graph, an edge from A to B
	// means "A depends on B" (B must be evaluated before A).
	// So B's in-degree is the count of params that depend on B.
	// Nodes with in-degree 0 have no dependents waiting — wrong.
	// Actually, in topological sort, we want to process nodes with no DEPENDENCIES first.
	// In our graph, edge A→B means "A depends on B", so A has out-degree to B.
	// For Kahn's, we reverse: edge B→A means "B must come before A".
	// In-degree of A = number of dependencies A has.

	// Let me redo this correctly.
	// deps[A] = [B, C] means A depends on B and C (B and C must be evaluated first)
	// For Kahn's: in-degree of A = len(deps[A])
	inDegree = make(map[string]int, len(md.Params))
	for name := range md.Params {
		inDegree[name] = len(deps[name])
	}

	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		// For each param that depends on this node, decrease its in-degree
		for other, dependencies := range deps {
			for _, dep := range dependencies {
				if dep == node {
					inDegree[other]--
					if inDegree[other] == 0 {
						queue = append(queue, other)
					}
				}
			}
		}
	}

	if len(sorted) != len(md.Params) {
		return nil, fmt.Errorf("circular dependency detected in params")
	}

	return sorted, nil
}

// containsIdentifier checks if an expression contains a reference to a param name.
// Uses word boundary detection to avoid false positives (e.g., "is_ready" matching "is_ready_2").
func containsIdentifier(expr, identifier string) bool {
	idx := 0
	for {
		pos := strings.Index(expr[idx:], identifier)
		if pos == -1 {
			return false
		}
		absPos := idx + pos
		endPos := absPos + len(identifier)

		// Check word boundaries
		validStart := absPos == 0 || !isIdentChar(expr[absPos-1])
		validEnd := endPos >= len(expr) || !isIdentChar(expr[endPos])

		if validStart && validEnd {
			return true
		}
		idx = absPos + 1
	}
}

// isIdentChar returns true if the byte is a valid identifier character (letter, digit, underscore)
func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
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
			return fmt.Errorf(
				"%s: nil value is not allowed; every leaf must be a non-empty CEL expression string "+
					"(was the field left blank in the config?)",
				fullKey,
			)
		case string:
			if val == "" {
				return fmt.Errorf(
					"%s: empty CEL expression is not allowed; every leaf must be a non-empty CEL expression string",
					fullKey,
				)
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
// Currently there are no sensitive params in the configuration, so this function is doing just a deep copy
func (c *SentinelConfig) RedactedCopy() *SentinelConfig {
	cp := *c

	if cp.Clients.HyperFleetAPI != nil {
		api := *cp.Clients.HyperFleetAPI
		cp.Clients.HyperFleetAPI = &api
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
