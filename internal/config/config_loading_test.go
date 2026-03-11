package config

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
)

// baseConfig is a minimal valid YAML config used as the base for override tests.
// It sets explicit values for all 18 viperKeyMappings entries so that each
// subtest can verify that the override (env var or CLI flag) wins over the file.
const baseConfig = `
sentinel:
  name: test-sentinel
debug_config: false
log:
  level: info
  format: text
  output: stdout
resource_type: clusters
poll_interval: 5s
message_data:
  id: "resource.id"
clients:
  hyperfleet_api:
    base_url: https://api.example.com
    version: v1
    timeout: 10s
  broker:
    topic: base-topic
`

// makeFlags creates a pflag.FlagSet pre-populated with all config override flags
// (mirroring addConfigOverrideFlags in cmd/sentinel/main.go) and marks the
// given name→value pairs as Changed by calling Set on each.
func makeFlags(t *testing.T, pairs map[string]string) *pflag.FlagSet {
	t.Helper()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)

	// General
	fs.Bool("debug-config", false, "")
	// Sentinel
	fs.String("name", "", "")
	// Log
	fs.String("log-level", "", "")
	fs.String("log-format", "", "")
	fs.String("log-output", "", "")
	// HyperFleet API
	fs.String("hyperfleet-api-base-url", "", "")
	fs.String("hyperfleet-api-version", "", "")
	fs.String("hyperfleet-api-timeout", "", "")
	// Broker
	fs.String("broker-topic", "", "")
	// Sentinel-specific
	fs.String("resource-type", "", "")
	fs.String("poll-interval", "", "")

	for name, value := range pairs {
		if err := fs.Set(name, value); err != nil {
			t.Fatalf("failed to set flag %q=%q: %v", name, value, err)
		}
	}
	return fs
}

// ============================================================================
// TestLoadConfig_EnvVarOverrides
// ============================================================================

func TestLoadConfig_EnvVarOverrides(t *testing.T) {
	tests := []struct {
		check    func(t *testing.T, cfg *SentinelConfig)
		name     string
		envVar   string
		envValue string
	}{
		{
			name:     "log::level",
			envVar:   "HYPERFLEET_LOG_LEVEL",
			envValue: "debug",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Log.Level != "debug" {
					t.Errorf("expected Log.Level=%q, got %q", "debug", cfg.Log.Level)
				}
			},
		},
		{
			name:     "log::format",
			envVar:   "HYPERFLEET_LOG_FORMAT",
			envValue: "json",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Log.Format != "json" {
					t.Errorf("expected Log.Format=%q, got %q", "json", cfg.Log.Format)
				}
			},
		},
		{
			name:     "log::output",
			envVar:   "HYPERFLEET_LOG_OUTPUT",
			envValue: "stderr",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Log.Output != "stderr" {
					t.Errorf("expected Log.Output=%q, got %q", "stderr", cfg.Log.Output)
				}
			},
		},
		{
			name:     "sentinel::name",
			envVar:   "HYPERFLEET_SENTINEL_NAME",
			envValue: "env-sentinel",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Sentinel.Name != "env-sentinel" {
					t.Errorf("expected Sentinel.Name=%q, got %q", "env-sentinel", cfg.Sentinel.Name)
				}
			},
		},
		{
			name:     "debug_config",
			envVar:   "HYPERFLEET_DEBUG_CONFIG",
			envValue: "true",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if !cfg.DebugConfig {
					t.Errorf("expected DebugConfig=true, got false")
				}
			},
		},
		{
			name:     "clients::hyperfleet_api::base_url",
			envVar:   "HYPERFLEET_API_BASE_URL",
			envValue: "https://env.example.com",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Clients.HyperFleetAPI.BaseURL != "https://env.example.com" {
					t.Errorf("expected BaseURL=%q, got %q", "https://env.example.com", cfg.Clients.HyperFleetAPI.BaseURL)
				}
			},
		},
		{
			name:     "clients::hyperfleet_api::version",
			envVar:   "HYPERFLEET_API_VERSION",
			envValue: "v2",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Clients.HyperFleetAPI.Version != "v2" {
					t.Errorf("expected Version=%q, got %q", "v2", cfg.Clients.HyperFleetAPI.Version)
				}
			},
		},
		{
			name:     "clients::hyperfleet_api::timeout",
			envVar:   "HYPERFLEET_API_TIMEOUT",
			envValue: "20s",
			check: func(t *testing.T, cfg *SentinelConfig) {
				want := 20 * time.Second
				if cfg.Clients.HyperFleetAPI.Timeout != want {
					t.Errorf("expected Timeout=%v, got %v", want, cfg.Clients.HyperFleetAPI.Timeout)
				}
			},
		},
		{
			name:     "clients::broker::topic",
			envVar:   "HYPERFLEET_BROKER_TOPIC",
			envValue: "env-topic",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Clients.Broker.Topic != "env-topic" {
					t.Errorf("expected Topic=%q, got %q", "env-topic", cfg.Clients.Broker.Topic)
				}
			},
		},
		{
			name:     "resource_type",
			envVar:   "HYPERFLEET_RESOURCE_TYPE",
			envValue: "nodepools",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.ResourceType != "nodepools" {
					t.Errorf("expected ResourceType=%q, got %q", "nodepools", cfg.ResourceType)
				}
			},
		},
		{
			name:     "poll_interval",
			envVar:   "HYPERFLEET_POLL_INTERVAL",
			envValue: "15s",
			check: func(t *testing.T, cfg *SentinelConfig) {
				want := 15 * time.Second
				if cfg.PollInterval != want {
					t.Errorf("expected PollInterval=%v, got %v", want, cfg.PollInterval)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTempConfigFile(t, baseConfig)
			t.Setenv(tt.envVar, tt.envValue)

			cfg, err := LoadConfig(configPath, nil)
			if err != nil {
				t.Fatalf("LoadConfig failed: %v", err)
			}

			tt.check(t, cfg)
		})
	}
}

// ============================================================================
// TestLoadConfig_CLIFlagOverrides
// ============================================================================

func TestLoadConfig_CLIFlagOverrides(t *testing.T) {
	tests := []struct {
		check     func(t *testing.T, cfg *SentinelConfig)
		name      string
		envVar    string
		envValue  string
		flagName  string
		flagValue string
	}{
		{
			name:      "name beats env and file",
			envVar:    "HYPERFLEET_SENTINEL_NAME",
			envValue:  "env-sentinel",
			flagName:  "name",
			flagValue: "flag-sentinel",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Sentinel.Name != "flag-sentinel" {
					t.Errorf("expected Sentinel.Name=%q (flag wins), got %q", "flag-sentinel", cfg.Sentinel.Name)
				}
			},
		},
		{
			name:      "hyperfleet-api-base-url beats file",
			flagName:  "hyperfleet-api-base-url",
			flagValue: "https://flag.example.com",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Clients.HyperFleetAPI.BaseURL != "https://flag.example.com" {
					t.Errorf("expected BaseURL=%q (flag wins), got %q", "https://flag.example.com", cfg.Clients.HyperFleetAPI.BaseURL)
				}
			},
		},
		{
			name:      "poll-interval beats file",
			flagName:  "poll-interval",
			flagValue: "45s",
			check: func(t *testing.T, cfg *SentinelConfig) {
				want := 45 * time.Second
				if cfg.PollInterval != want {
					t.Errorf("expected PollInterval=%v (flag wins), got %v", want, cfg.PollInterval)
				}
			},
		},
		{
			name:      "log-level beats file",
			flagName:  "log-level",
			flagValue: "warn",
			check: func(t *testing.T, cfg *SentinelConfig) {
				if cfg.Log.Level != "warn" {
					t.Errorf("expected Log.Level=%q (flag wins), got %q", "warn", cfg.Log.Level)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTempConfigFile(t, baseConfig)
			if tt.envVar != "" {
				t.Setenv(tt.envVar, tt.envValue)
			}
			flags := makeFlags(t, map[string]string{tt.flagName: tt.flagValue})

			cfg, err := LoadConfig(configPath, flags)
			if err != nil {
				t.Fatalf("LoadConfig failed: %v", err)
			}

			tt.check(t, cfg)
		})
	}
}

// ============================================================================
// TestLoadConfig_LegacyBrokerEnvVars
// ============================================================================

// ============================================================================
// TestLoadConfig_FilePrecedence
// ============================================================================

func TestLoadConfig_FilePrecedence(t *testing.T) {
	configPath := createTempConfigFile(t, baseConfig)

	cfg, err := LoadConfig(configPath, nil)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Sentinel.Name != "test-sentinel" {
		t.Errorf("expected Sentinel.Name=%q, got %q", "test-sentinel", cfg.Sentinel.Name)
	}
	if cfg.ResourceType != "clusters" {
		t.Errorf("expected ResourceType=%q, got %q", "clusters", cfg.ResourceType)
	}
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("expected PollInterval=5s, got %v", cfg.PollInterval)
	}
	if cfg.Clients.HyperFleetAPI.BaseURL != "https://api.example.com" {
		t.Errorf("expected BaseURL=%q, got %q", "https://api.example.com", cfg.Clients.HyperFleetAPI.BaseURL)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("expected Log.Level=%q, got %q", "info", cfg.Log.Level)
	}
	if cfg.Clients.Broker.Topic != "base-topic" {
		t.Errorf("expected Topic=%q, got %q", "base-topic", cfg.Clients.Broker.Topic)
	}
}

// ============================================================================
// TestLoadConfig_PriorityChain
// ============================================================================

func TestLoadConfig_PriorityChain(t *testing.T) {
	t.Run("flag beats env beats file", func(t *testing.T) {
		configPath := createTempConfigFile(t, baseConfig)
		t.Setenv("HYPERFLEET_POLL_INTERVAL", "10s")
		flags := makeFlags(t, map[string]string{"poll-interval": "15s"})

		cfg, err := LoadConfig(configPath, flags)
		if err != nil {
			t.Fatalf("LoadConfig failed: %v", err)
		}

		want := 15 * time.Second
		if cfg.PollInterval != want {
			t.Errorf("expected PollInterval=%v (flag wins), got %v", want, cfg.PollInterval)
		}
	})

	t.Run("env beats file when no flag", func(t *testing.T) {
		configPath := createTempConfigFile(t, baseConfig)
		t.Setenv("HYPERFLEET_POLL_INTERVAL", "10s")

		cfg, err := LoadConfig(configPath, nil)
		if err != nil {
			t.Fatalf("LoadConfig failed: %v", err)
		}

		want := 10 * time.Second
		if cfg.PollInterval != want {
			t.Errorf("expected PollInterval=%v (env wins), got %v", want, cfg.PollInterval)
		}
	})

	t.Run("file value used when no env or flag", func(t *testing.T) {
		configPath := createTempConfigFile(t, baseConfig)

		cfg, err := LoadConfig(configPath, nil)
		if err != nil {
			t.Fatalf("LoadConfig failed: %v", err)
		}

		want := 5 * time.Second
		if cfg.PollInterval != want {
			t.Errorf("expected PollInterval=%v (file value), got %v", want, cfg.PollInterval)
		}
	})
}
