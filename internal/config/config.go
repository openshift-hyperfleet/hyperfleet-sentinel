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

const (
	defaultMessagingSystem = "gcp_pubsub"
)

// LabelSelector represents a label key-value pair for resource filtering
type LabelSelector struct {
	Label string `mapstructure:"label"`
	Value string `mapstructure:"value"`
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
	HyperFleetAPI    *HyperFleetAPIConfig   `mapstructure:"hyperfleet_api"`
	MessageData      map[string]interface{} `mapstructure:"message_data"`
	MessageDecision  *MessageDecisionConfig `mapstructure:"message_decision"`
	ResourceType     string                 `mapstructure:"resource_type"`
	Topic            string                 `mapstructure:"topic"`
	MessagingSystem  string                 `mapstructure:"messaging_system"`
	ResourceSelector LabelSelectorList      `mapstructure:"resource_selector"`
	PollInterval     time.Duration          `mapstructure:"poll_interval"`
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

// DefaultMessageDecision returns the default message_decision configuration
// that replicates the previous hardcoded max_age_not_ready/max_age_ready behavior.
func DefaultMessageDecision() *MessageDecisionConfig {
	return &MessageDecisionConfig{
		Params: map[string]string{
			"ref_time":                `condition("Ready").last_updated_time`,
			"is_ready":                `condition("Ready").status == "True"`,
			"has_ref_time":            `ref_time != ""`,
			"is_new_resource":         `!is_ready && resource.generation == 1`,
			"ready_and_stale":         `is_ready && has_ref_time && now - timestamp(ref_time) > duration("30m")`,
			"not_ready_and_debounced": `!is_ready && has_ref_time && now - timestamp(ref_time) > duration("10s")`,
		},
		Result: "is_new_resource || ready_and_stale || not_ready_and_debounced",
	}
}

// NewSentinelConfig creates a new configuration with defaults
func NewSentinelConfig() *SentinelConfig {
	return &SentinelConfig{
		// ResourceType is required and must be set in config file
		PollInterval:     5 * time.Second,
		ResourceSelector: []LabelSelector{}, // Empty means watch all resources
		HyperFleetAPI: &HyperFleetAPIConfig{
			// Endpoint is required and must be set in config file
			Timeout: 5 * time.Second,
		},
		MessagingSystem: defaultMessagingSystem,
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

	// Apply default message_decision if not configured
	if cfg.MessageDecision == nil {
		cfg.MessageDecision = DefaultMessageDecision()
	}

	// Override topic from environment variable if explicitly provided
	// Environment variable takes precedence over config file (including empty value to clear)
	if topic, ok := os.LookupEnv("BROKER_TOPIC"); ok {
		cfg.Topic = topic
	}

	if messagingSystem, ok := os.LookupEnv("MESSAGING_SYSTEM"); ok {
		messagingSystem = strings.TrimSpace(messagingSystem)
		if messagingSystem != "" {
			cfg.MessagingSystem = messagingSystem
		}
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

	if c.MessageDecision == nil {
		return fmt.Errorf("message_decision is required")
	}

	if err := c.MessageDecision.Validate(); err != nil {
		return fmt.Errorf("message_decision: %w", err)
	}

	if c.MessageData == nil {
		return fmt.Errorf("message_data is required")
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
