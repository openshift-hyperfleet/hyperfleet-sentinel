package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  LogLevel
		expectErr bool
	}{
		{"debug lowercase", "debug", LevelDebug, false},
		{"debug uppercase", "DEBUG", LevelDebug, false},
		{"debug mixed case", "Debug", LevelDebug, false},
		{"info lowercase", "info", LevelInfo, false},
		{"info uppercase", "INFO", LevelInfo, false},
		{"warn lowercase", "warn", LevelWarn, false},
		{"warn uppercase", "WARN", LevelWarn, false},
		{"warning lowercase", "warning", LevelWarn, false},
		{"warning uppercase", "WARNING", LevelWarn, false},
		{"error lowercase", "error", LevelError, false},
		{"error uppercase", "ERROR", LevelError, false},
		{"with whitespace", "  info  ", LevelInfo, false},
		{"invalid level", "invalid", LevelInfo, true},
		{"empty string", "", LevelInfo, true},
		{"numeric", "1", LevelInfo, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, err := ParseLogLevel(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for input %q: %v", tt.input, err)
				}
				if level != tt.expected {
					t.Errorf("expected %v, got %v", tt.expected, level)
				}
			}
		})
	}
}

func TestParseLogFormat(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  LogFormat
		expectErr bool
	}{
		{"text lowercase", "text", FormatText, false},
		{"text uppercase", "TEXT", FormatText, false},
		{"text mixed case", "Text", FormatText, false},
		{"json lowercase", "json", FormatJSON, false},
		{"json uppercase", "JSON", FormatJSON, false},
		{"json mixed case", "Json", FormatJSON, false},
		{"with whitespace", "  json  ", FormatJSON, false},
		{"invalid format", "xml", FormatText, true},
		{"empty string", "", FormatText, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, err := ParseLogFormat(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for input %q: %v", tt.input, err)
				}
				if format != tt.expected {
					t.Errorf("expected %v, got %v", tt.expected, format)
				}
			}
		})
	}
}

func TestParseLogOutput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{"stdout lowercase", "stdout", false},
		{"stdout uppercase", "STDOUT", false},
		{"stderr lowercase", "stderr", false},
		{"stderr uppercase", "STDERR", false},
		{"empty string defaults to stdout", "", false},
		{"with whitespace", "  stdout  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := ParseLogOutput(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for input %q: %v", tt.input, err)
				}
				if output == nil {
					t.Errorf("expected non-nil output for input %q", tt.input)
				}
			}
		})
	}
}

func TestParseLogOutput_File(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-log-*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	output, err := ParseLogOutput(tmpPath)
	if err != nil {
		t.Errorf("unexpected error for file path: %v", err)
	}
	if output == nil {
		t.Error("expected non-nil output for file path")
	}

	// Clean up - close the file if it's a file
	if f, ok := output.(*os.File); ok {
		f.Close()
	}
}

func TestParseLogOutput_InvalidFile(t *testing.T) {
	// Try to write to a directory that doesn't exist
	_, err := ParseLogOutput("/nonexistent/directory/file.log")
	if err == nil {
		t.Error("expected error for invalid file path, got nil")
	}
}

func TestLogLevelString(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LevelDebug, "debug"},
		{LevelInfo, "info"},
		{LevelWarn, "warn"},
		{LevelError, "error"},
		{LogLevel(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	tests := []struct {
		name          string
		configLevel   LogLevel
		logLevel      string
		shouldContain bool
	}{
		{"debug message at debug level", LevelDebug, "debug", true},
		{"info message at debug level", LevelDebug, "info", true},
		{"warn message at debug level", LevelDebug, "warn", true},
		{"error message at debug level", LevelDebug, "error", true},

		{"debug message at info level", LevelInfo, "debug", false},
		{"info message at info level", LevelInfo, "info", true},
		{"warn message at info level", LevelInfo, "warn", true},
		{"error message at info level", LevelInfo, "error", true},

		{"debug message at warn level", LevelWarn, "debug", false},
		{"info message at warn level", LevelWarn, "info", false},
		{"warn message at warn level", LevelWarn, "warn", true},
		{"error message at warn level", LevelWarn, "error", true},

		{"debug message at error level", LevelError, "debug", false},
		{"info message at error level", LevelError, "info", false},
		{"warn message at error level", LevelError, "warn", false},
		{"error message at error level", LevelError, "error", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &LogConfig{
				Level:     tt.configLevel,
				Format:    FormatText,
				Output:    &buf,
				Component: "test",
				Version:   "1.0.0",
				Hostname:  "testhost",
			}
			log := NewHyperFleetLoggerWithConfig(cfg)
			ctx := context.Background()
			testMessage := "test message for " + tt.logLevel

			switch tt.logLevel {
			case "debug":
				log.Debug(ctx, testMessage)
			case "info":
				log.Info(ctx, testMessage)
			case "warn":
				log.Warning(ctx, testMessage)
			case "error":
				log.Error(ctx, testMessage)
			}

			output := buf.String()
			contains := strings.Contains(output, testMessage)
			if contains != tt.shouldContain {
				if tt.shouldContain {
					t.Errorf("expected output to contain %q, got: %q", testMessage, output)
				} else {
					t.Errorf("expected output NOT to contain %q, got: %q", testMessage, output)
				}
			}
		})
	}
}

func TestLoggerTextFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := &LogConfig{
		Level:     LevelInfo,
		Format:    FormatText,
		Output:    &buf,
		Component: "sentinel",
		Version:   "v1.2.3",
		Hostname:  "testhost",
	}
	log := NewHyperFleetLoggerWithConfig(cfg)
	ctx := context.Background()

	log.Info(ctx, "Test message")

	output := buf.String()

	// Check required fields are present
	if !strings.Contains(output, "INFO") {
		t.Error("expected output to contain 'INFO'")
	}
	if !strings.Contains(output, "[sentinel]") {
		t.Error("expected output to contain '[sentinel]'")
	}
	if !strings.Contains(output, "[v1.2.3]") {
		t.Error("expected output to contain '[v1.2.3]'")
	}
	if !strings.Contains(output, "[testhost]") {
		t.Error("expected output to contain '[testhost]'")
	}
	if !strings.Contains(output, "Test message") {
		t.Error("expected output to contain 'Test message'")
	}
	// Check timestamp format (RFC3339)
	if !strings.Contains(output, "T") || !strings.Contains(output, "Z") {
		t.Error("expected output to contain RFC3339 timestamp")
	}
}

func TestLoggerJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := &LogConfig{
		Level:     LevelInfo,
		Format:    FormatJSON,
		Output:    &buf,
		Component: "sentinel",
		Version:   "v1.2.3",
		Hostname:  "testhost",
	}
	log := NewHyperFleetLoggerWithConfig(cfg)
	ctx := context.Background()

	log.Info(ctx, "Test message")

	output := buf.String()

	// Parse JSON
	var entry logEntry
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify required fields
	if entry.Level != "info" {
		t.Errorf("expected level 'info', got %q", entry.Level)
	}
	if entry.Component != "sentinel" {
		t.Errorf("expected component 'sentinel', got %q", entry.Component)
	}
	if entry.Version != "v1.2.3" {
		t.Errorf("expected version 'v1.2.3', got %q", entry.Version)
	}
	if entry.Hostname != "testhost" {
		t.Errorf("expected hostname 'testhost', got %q", entry.Hostname)
	}
	if entry.Message != "Test message" {
		t.Errorf("expected message 'Test message', got %q", entry.Message)
	}
	if entry.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestLoggerExtraFields(t *testing.T) {
	var buf bytes.Buffer
	cfg := &LogConfig{
		Level:     LevelInfo,
		Format:    FormatJSON,
		Output:    &buf,
		Component: "test",
		Version:   "1.0.0",
		Hostname:  "testhost",
	}
	log := NewHyperFleetLoggerWithConfig(cfg)
	ctx := context.Background()

	log.Extra("resource_id", "cluster-123").Extra("phase", "Ready").Info(ctx, "Resource processed")

	output := buf.String()

	// Parse JSON
	var entry logEntry
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if entry.Extra == nil {
		t.Fatal("expected extra fields to be present")
	}
	if entry.Extra["resource_id"] != "cluster-123" {
		t.Errorf("expected resource_id 'cluster-123', got %v", entry.Extra["resource_id"])
	}
	if entry.Extra["phase"] != "Ready" {
		t.Errorf("expected phase 'Ready', got %v", entry.Extra["phase"])
	}
}

func TestLoggerWithField(t *testing.T) {
	var buf bytes.Buffer
	cfg := &LogConfig{
		Level:     LevelInfo,
		Format:    FormatJSON,
		Output:    &buf,
		Component: "test",
		Version:   "1.0.0",
		Hostname:  "testhost",
	}
	log := NewHyperFleetLoggerWithConfig(cfg)
	ctx := context.Background()

	log.WithField("key", "value").Info(ctx, "Test message")

	output := buf.String()

	var entry logEntry
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if entry.Extra == nil || entry.Extra["key"] != "value" {
		t.Errorf("expected extra field 'key'='value', got %v", entry.Extra)
	}
}

func TestLoggerVerbosity(t *testing.T) {
	t.Run("V(0) logs at info level", func(t *testing.T) {
		var buf bytes.Buffer
		cfg := &LogConfig{
			Level:     LevelInfo,
			Format:    FormatText,
			Output:    &buf,
			Component: "test",
			Version:   "1.0.0",
			Hostname:  "testhost",
		}
		log := NewHyperFleetLoggerWithConfig(cfg)
		ctx := context.Background()

		log.V(0).Info(ctx, "V0 message")

		if !strings.Contains(buf.String(), "V0 message") {
			t.Error("expected V(0) message to be logged at info level")
		}
	})

	t.Run("V(1) logs at debug level only", func(t *testing.T) {
		var buf bytes.Buffer
		cfg := &LogConfig{
			Level:     LevelInfo,
			Format:    FormatText,
			Output:    &buf,
			Component: "test",
			Version:   "1.0.0",
			Hostname:  "testhost",
		}
		log := NewHyperFleetLoggerWithConfig(cfg)
		ctx := context.Background()

		log.V(1).Info(ctx, "V1 message")

		if strings.Contains(buf.String(), "V1 message") {
			t.Error("expected V(1) message NOT to be logged at info level")
		}
	})

	t.Run("V(1) logs when debug enabled", func(t *testing.T) {
		var buf bytes.Buffer
		cfg := &LogConfig{
			Level:     LevelDebug,
			Format:    FormatText,
			Output:    &buf,
			Component: "test",
			Version:   "1.0.0",
			Hostname:  "testhost",
		}
		log := NewHyperFleetLoggerWithConfig(cfg)
		ctx := context.Background()

		log.V(1).Info(ctx, "V1 message")

		if !strings.Contains(buf.String(), "V1 message") {
			t.Error("expected V(1) message to be logged at debug level")
		}
	})

	t.Run("V(2) logs when debug enabled", func(t *testing.T) {
		var buf bytes.Buffer
		cfg := &LogConfig{
			Level:     LevelDebug,
			Format:    FormatText,
			Output:    &buf,
			Component: "test",
			Version:   "1.0.0",
			Hostname:  "testhost",
		}
		log := NewHyperFleetLoggerWithConfig(cfg)
		ctx := context.Background()

		log.V(2).Info(ctx, "V2 message")

		if !strings.Contains(buf.String(), "V2 message") {
			t.Error("expected V(2) message to be logged at debug level")
		}
	})
}

func TestLoggerContextValues(t *testing.T) {
	var buf bytes.Buffer
	cfg := &LogConfig{
		Level:     LevelInfo,
		Format:    FormatJSON,
		Output:    &buf,
		Component: "test",
		Version:   "1.0.0",
		Hostname:  "testhost",
	}
	log := NewHyperFleetLoggerWithConfig(cfg)

	// Create context with operation ID
	ctx := context.WithValue(context.Background(), OpIDKey, "op-12345")
	ctx = context.WithValue(ctx, TxIDKey, int64(67890))

	log.Info(ctx, "Test with context")

	output := buf.String()

	var entry logEntry
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if entry.OpID != "op-12345" {
		t.Errorf("expected op_id 'op-12345', got %q", entry.OpID)
	}
	if entry.TxID != 67890 {
		t.Errorf("expected tx_id 67890, got %d", entry.TxID)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Level != LevelInfo {
		t.Errorf("expected default level to be Info, got %v", cfg.Level)
	}
	if cfg.Format != FormatText {
		t.Errorf("expected default format to be Text, got %v", cfg.Format)
	}
	if cfg.Output != os.Stdout {
		t.Error("expected default output to be stdout")
	}
	if cfg.Component != "sentinel" {
		t.Errorf("expected default component to be 'sentinel', got %q", cfg.Component)
	}
	if cfg.Hostname == "" {
		t.Error("expected hostname to be set")
	}
}

func TestGlobalConfig(t *testing.T) {
	// Save original global config
	originalConfig := GetGlobalConfig()
	defer SetGlobalConfig(originalConfig)

	// Set custom config
	customCfg := &LogConfig{
		Level:     LevelDebug,
		Format:    FormatJSON,
		Output:    os.Stderr,
		Component: "custom",
		Version:   "custom-version",
		Hostname:  "custom-host",
	}
	SetGlobalConfig(customCfg)

	// Verify it was set
	got := GetGlobalConfig()
	if got.Level != LevelDebug {
		t.Errorf("expected level Debug, got %v", got.Level)
	}
	if got.Component != "custom" {
		t.Errorf("expected component 'custom', got %q", got.Component)
	}
}

func TestNewHyperFleetLogger(t *testing.T) {
	// Reset global config to ensure clean state
	SetGlobalConfig(nil)

	log := NewHyperFleetLogger()
	if log == nil {
		t.Error("expected non-nil logger")
	}
}

func TestNoopLogger(t *testing.T) {
	// noopLogger should not panic and should return itself for chaining
	noop := &noopLogger{}
	ctx := context.Background()

	// These should all be no-ops
	noop.Debug(ctx, "test")
	noop.Debugf(ctx, "test %s", "arg")
	noop.Info(ctx, "test")
	noop.Infof(ctx, "test %s", "arg")
	noop.Warning(ctx, "test")
	noop.Warningf(ctx, "test %s", "arg")
	noop.Error(ctx, "test")
	noop.Errorf(ctx, "test %s", "arg")

	// V should return itself
	if noop.V(1) != noop {
		t.Error("expected V() to return the same noopLogger")
	}

	// Extra should return itself
	if noop.Extra("key", "value") != noop {
		t.Error("expected Extra() to return the same noopLogger")
	}

	// WithField should return itself
	if noop.WithField("key", "value") != noop {
		t.Error("expected WithField() to return the same noopLogger")
	}
}

func TestLoggerFormattedMethods(t *testing.T) {
	var buf bytes.Buffer
	cfg := &LogConfig{
		Level:     LevelDebug,
		Format:    FormatText,
		Output:    &buf,
		Component: "test",
		Version:   "1.0.0",
		Hostname:  "testhost",
	}
	log := NewHyperFleetLoggerWithConfig(cfg)
	ctx := context.Background()

	tests := []struct {
		name     string
		logFunc  func()
		expected string
	}{
		{"Debugf", func() { log.Debugf(ctx, "debug %s %d", "test", 123) }, "debug test 123"},
		{"Infof", func() { log.Infof(ctx, "info %s %d", "test", 456) }, "info test 456"},
		{"Warningf", func() { log.Warningf(ctx, "warn %s %d", "test", 789) }, "warn test 789"},
		{"Errorf", func() { log.Errorf(ctx, "error %s %d", "test", 101) }, "error test 101"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc()
			if !strings.Contains(buf.String(), tt.expected) {
				t.Errorf("expected output to contain %q, got %q", tt.expected, buf.String())
			}
		})
	}
}
