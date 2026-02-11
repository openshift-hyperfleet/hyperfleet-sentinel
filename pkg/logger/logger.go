package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// maxStackFrames limits stack trace depth per HyperFleet logging spec (10-15 frames)
	maxStackFrames = 15
	// skipFrames skips internal logger frames to start at caller
	skipFrames = 4
)

// LogLevel represents the logging severity level
type LogLevel int

const (
	// LevelDebug is the most verbose logging level
	LevelDebug LogLevel = iota
	// LevelInfo is the default logging level
	LevelInfo
	// LevelWarn logs warnings and above
	LevelWarn
	// LevelError logs only errors
	LevelError
)

// LogFormat represents the log output format
type LogFormat int

const (
	// FormatText is human-readable text format (default for development)
	FormatText LogFormat = iota
	// FormatJSON is structured JSON format (recommended for production)
	FormatJSON
)

// LogConfig holds the logging configuration
type LogConfig struct {
	Level     LogLevel
	Format    LogFormat
	Output    io.Writer
	Component string
	Version   string
	Hostname  string
}

// HyperFleetLogger interface for structured logging
type HyperFleetLogger interface {
	Debug(ctx context.Context, message string)
	Debugf(ctx context.Context, format string, args ...interface{})
	Info(ctx context.Context, message string)
	Infof(ctx context.Context, format string, args ...interface{})
	Warn(ctx context.Context, message string)
	Warnf(ctx context.Context, format string, args ...interface{})
	Error(ctx context.Context, message string)
	Errorf(ctx context.Context, format string, args ...interface{})
	Fatal(ctx context.Context, message string)
	Fatalf(ctx context.Context, format string, args ...interface{})
	// V returns a logger that only logs if the verbosity level is >= level
	// For compatibility with glog-style logging
	V(level int32) HyperFleetLogger
	// Extra adds additional key-value pairs to the log entry
	Extra(key string, value interface{}) HyperFleetLogger
}

var _ HyperFleetLogger = &logger{}

type extra map[string]interface{}

type logger struct {
	config    *LogConfig
	extra     extra
	verbosity int32
	mu        sync.Mutex
}

var (
	globalConfig *LogConfig
	configMu     sync.RWMutex
)

// DefaultConfig returns a LogConfig with default values
func DefaultConfig() *LogConfig {
	hostname, _ := os.Hostname()
	return &LogConfig{
		Level:     LevelInfo,
		Format:    FormatText,
		Output:    os.Stdout,
		Component: "sentinel",
		Version:   "dev",
		Hostname:  hostname,
	}
}

// SetGlobalConfig sets the global logging configuration
func SetGlobalConfig(cfg *LogConfig) {
	configMu.Lock()
	defer configMu.Unlock()
	globalConfig = cfg
}

// GetGlobalConfig returns the global logging configuration
func GetGlobalConfig() *LogConfig {
	configMu.RLock()
	defer configMu.RUnlock()
	if globalConfig == nil {
		return DefaultConfig()
	}
	return globalConfig
}

// ParseLogLevel converts a string log level to LogLevel
func ParseLogLevel(level string) (LogLevel, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level: %s (valid: debug, info, warn, error)", level)
	}
}

// ParseLogFormat converts a string log format to LogFormat
func ParseLogFormat(format string) (LogFormat, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	default:
		return FormatText, fmt.Errorf("unknown log format: %s (valid: text, json)", format)
	}
}

// ParseLogOutput converts a string output to io.Writer
func ParseLogOutput(output string) (io.Writer, error) {
	switch strings.ToLower(strings.TrimSpace(output)) {
	case "stdout", "":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	default:
		return nil, fmt.Errorf("unknown log output: %s (valid: stdout, stderr)", output)
	}
}

// LogLevelString returns the string representation of LogLevel
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// String returns the string representation of LogFormat
func (f LogFormat) String() string {
	switch f {
	case FormatJSON:
		return "json"
	default:
		return "text"
	}
}

// NewHyperFleetLogger creates a new logger instance using global config
func NewHyperFleetLogger() HyperFleetLogger {
	return NewHyperFleetLoggerWithConfig(GetGlobalConfig())
}

// NewHyperFleetLoggerWithConfig creates a new logger instance with specific config
func NewHyperFleetLoggerWithConfig(cfg *LogConfig) HyperFleetLogger {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &logger{
		config:    cfg,
		extra:     make(extra),
		verbosity: 0,
	}
}

// logEntry represents a structured log entry
type logEntry struct {
	// Required fields per HyperFleet logging specification
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Component string `json:"component"`
	Version   string `json:"version"`
	Hostname  string `json:"hostname"`

	// Error fields (for error level logs)
	Error      string   `json:"error,omitempty"`
	StackTrace []string `json:"stack_trace,omitempty"`

	// Correlation fields (when available)
	TraceID string `json:"trace_id,omitempty"`
	SpanID  string `json:"span_id,omitempty"`
	OpID    string `json:"op_id,omitempty"`
	TxID    int64  `json:"tx_id,omitempty"`

	// Sentinel-specific fields
	DecisionReason string `json:"decision_reason,omitempty"`
	Topic          string `json:"topic,omitempty"`
	Subset         string `json:"subset,omitempty"`

	// Additional fields
	Extra map[string]interface{} `json:"extra,omitempty"`
}

func (l *logger) shouldLog(level LogLevel) bool {
	return level >= l.config.Level
}

func (l *logger) buildEntry(ctx context.Context, level LogLevel, message string) *logEntry {
	entry := &logEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level.String(),
		Message:   message,
		Component: l.config.Component,
		Version:   l.config.Version,
		Hostname:  l.config.Hostname,
	}

	// Add context values
	if ctx != nil {
		// Correlation fields
		if opid, ok := ctx.Value(OpIDKey).(string); ok {
			entry.OpID = opid
		}
		if txid, ok := ctx.Value(TxIDKey).(int64); ok {
			entry.TxID = txid
		}
		if traceID, ok := ctx.Value(TraceIDCtxKey).(string); ok {
			entry.TraceID = traceID
		}
		if spanID, ok := ctx.Value(SpanIDCtxKey).(string); ok {
			entry.SpanID = spanID
		}

		// Sentinel-specific fields
		if reason, ok := ctx.Value(DecisionReasonCtxKey).(string); ok {
			entry.DecisionReason = reason
		}
		if topic, ok := ctx.Value(TopicCtxKey).(string); ok {
			entry.Topic = topic
		}
		if subset, ok := ctx.Value(SubsetCtxKey).(string); ok {
			entry.Subset = subset
		}
	}

	// Add extra fields
	if len(l.extra) > 0 {
		entry.Extra = make(map[string]interface{})
		for k, v := range l.extra {
			entry.Extra[k] = v
		}
	}

	return entry
}

func (l *logger) formatText(entry *logEntry) string {
	var sb strings.Builder

	// Format: {timestamp} {LEVEL} [{component}] [{version}] [{hostname}] {message} {key=value}...
	sb.WriteString(entry.Timestamp)
	sb.WriteString(" ")
	sb.WriteString(strings.ToUpper(entry.Level))
	sb.WriteString(" [")
	sb.WriteString(entry.Component)
	sb.WriteString("] [")
	sb.WriteString(entry.Version)
	sb.WriteString("] [")
	sb.WriteString(entry.Hostname)
	sb.WriteString("] ")
	sb.WriteString(entry.Message)

	// Add correlation fields
	if entry.TraceID != "" {
		sb.WriteString(" trace_id=")
		sb.WriteString(entry.TraceID)
	}
	if entry.SpanID != "" {
		sb.WriteString(" span_id=")
		sb.WriteString(entry.SpanID)
	}
	if entry.OpID != "" {
		sb.WriteString(" op_id=")
		sb.WriteString(entry.OpID)
	}
	if entry.TxID != 0 {
		sb.WriteString(fmt.Sprintf(" tx_id=%d", entry.TxID))
	}

	// Add Sentinel-specific fields
	if entry.DecisionReason != "" {
		sb.WriteString(" decision_reason=")
		sb.WriteString(entry.DecisionReason)
	}
	if entry.Topic != "" {
		sb.WriteString(" topic=")
		sb.WriteString(entry.Topic)
	}
	if entry.Subset != "" {
		sb.WriteString(" subset=")
		sb.WriteString(entry.Subset)
	}

	// Add extra fields
	for k, v := range entry.Extra {
		sb.WriteString(fmt.Sprintf(" %s=%v", k, v))
	}

	// Add error field only if it differs from message (avoid duplication)
	if entry.Error != "" && entry.Error != entry.Message {
		sb.WriteString(" error=\"")
		sb.WriteString(entry.Error)
		sb.WriteString("\"")
	}

	sb.WriteString("\n")

	// Add stack trace indented below main log line (per HyperFleet logging spec)
	if len(entry.StackTrace) > 0 {
		for _, frame := range entry.StackTrace {
			sb.WriteString("    ")
			sb.WriteString(frame)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func (l *logger) formatJSON(entry *logEntry) string {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to marshal log entry: %v"}`, err) + "\n"
	}
	return string(data) + "\n"
}

// getStackTrace captures a stack trace, limited to maxStackFrames
// per HyperFleet logging specification (10-15 frames)
func getStackTrace(skip int) []string {
	var frames []string
	pcs := make([]uintptr, maxStackFrames)
	n := runtime.Callers(skip, pcs)
	if n == 0 {
		return nil
	}

	pcs = pcs[:n]
	callersFrames := runtime.CallersFrames(pcs)

	for {
		frame, more := callersFrames.Next()
		// Skip runtime and testing frames
		if strings.Contains(frame.Function, "runtime.") ||
			strings.Contains(frame.Function, "testing.") {
			if !more {
				break
			}
			continue
		}

		// Format: "function() file:line"
		shortFunc := frame.Function
		if idx := strings.LastIndex(shortFunc, "/"); idx >= 0 {
			shortFunc = shortFunc[idx+1:]
		}
		shortFile := frame.File
		if idx := strings.LastIndex(shortFile, "/"); idx >= 0 {
			shortFile = shortFile[idx+1:]
		}
		frames = append(frames, fmt.Sprintf("%s() %s:%d", shortFunc, shortFile, frame.Line))

		if !more || len(frames) >= maxStackFrames {
			break
		}
	}

	return frames
}

func (l *logger) log(ctx context.Context, level LogLevel, message string) {
	l.logWithError(ctx, level, message, "")
}

func (l *logger) logWithError(ctx context.Context, level LogLevel, message string, errorMsg string) {
	if !l.shouldLog(level) {
		return
	}

	entry := l.buildEntry(ctx, level, message)

	// For error level, add error field and stack trace per HyperFleet logging spec
	if level == LevelError {
		if errorMsg != "" {
			entry.Error = errorMsg
		} else {
			entry.Error = message
		}
		entry.StackTrace = getStackTrace(skipFrames)
	}

	var output string
	switch l.config.Format {
	case FormatJSON:
		output = l.formatJSON(entry)
	default:
		output = l.formatText(entry)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.config.Output.Write([]byte(output))
}

func (l *logger) Debug(ctx context.Context, message string) {
	l.log(ctx, LevelDebug, message)
}

func (l *logger) Debugf(ctx context.Context, format string, args ...interface{}) {
	l.log(ctx, LevelDebug, fmt.Sprintf(format, args...))
}

func (l *logger) Info(ctx context.Context, message string) {
	l.log(ctx, LevelInfo, message)
}

func (l *logger) Infof(ctx context.Context, format string, args ...interface{}) {
	l.log(ctx, LevelInfo, fmt.Sprintf(format, args...))
}

func (l *logger) Warn(ctx context.Context, message string) {
	l.log(ctx, LevelWarn, message)
}

func (l *logger) Warnf(ctx context.Context, format string, args ...interface{}) {
	l.log(ctx, LevelWarn, fmt.Sprintf(format, args...))
}

func (l *logger) Error(ctx context.Context, message string) {
	l.log(ctx, LevelError, message)
}

func (l *logger) Errorf(ctx context.Context, format string, args ...interface{}) {
	l.log(ctx, LevelError, fmt.Sprintf(format, args...))
}

func (l *logger) Fatal(ctx context.Context, message string) {
	l.log(ctx, LevelError, "FATAL: "+message)
	os.Exit(1)
}

func (l *logger) Fatalf(ctx context.Context, format string, args ...interface{}) {
	l.log(ctx, LevelError, "FATAL: "+fmt.Sprintf(format, args...))
	os.Exit(1)
}

// V returns a logger that only logs if the verbosity level is >= level
// For compatibility with glog-style logging:
// - V(0) = always log (info level)
// - V(1) = log if debug enabled
// - V(2+) = log if debug enabled (detailed debug)
func (l *logger) V(level int32) HyperFleetLogger {
	// Early return to avoid unnecessary allocation
	if level > 0 && l.config.Level > LevelDebug {
		return &noopLogger{}
	}

	newLogger := &logger{
		config:    l.config,
		extra:     make(extra),
		verbosity: level,
	}
	for k, v := range l.extra {
		newLogger.extra[k] = v
	}

	return newLogger
}

func (l *logger) Extra(key string, value interface{}) HyperFleetLogger {
	newLogger := &logger{
		config:    l.config,
		extra:     make(extra),
		verbosity: l.verbosity,
	}
	for k, v := range l.extra {
		newLogger.extra[k] = v
	}
	newLogger.extra[key] = value
	return newLogger
}

// noopLogger is a logger that does nothing (used for verbosity filtering)
type noopLogger struct{}

func (n *noopLogger) Debug(ctx context.Context, message string)                      {}
func (n *noopLogger) Debugf(ctx context.Context, format string, args ...interface{}) {}
func (n *noopLogger) Info(ctx context.Context, message string)                       {}
func (n *noopLogger) Infof(ctx context.Context, format string, args ...interface{})  {}
func (n *noopLogger) Warn(ctx context.Context, message string)                       {}
func (n *noopLogger) Warnf(ctx context.Context, format string, args ...interface{})  {}
func (n *noopLogger) Error(ctx context.Context, message string)                      {}
func (n *noopLogger) Errorf(ctx context.Context, format string, args ...interface{}) {}
func (n *noopLogger) Fatal(ctx context.Context, message string) {
	fmt.Fprintf(os.Stderr, "FATAL: %s\n", message)
	os.Exit(1)
}
func (n *noopLogger) Fatalf(ctx context.Context, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "FATAL: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}
func (n *noopLogger) V(level int32) HyperFleetLogger                       { return n }
func (n *noopLogger) Extra(key string, value interface{}) HyperFleetLogger { return n }

type MockLoggerWithContext struct {
	CapturedLogs     *[]string
	CapturedContexts *[]context.Context
}

func (m *MockLoggerWithContext) capture(ctx context.Context, msg string) {
	if m.CapturedLogs == nil {
		m.CapturedLogs = &[]string{}
	}
	if m.CapturedContexts == nil {
		m.CapturedContexts = &[]context.Context{}
	}
	*m.CapturedLogs = append(*m.CapturedLogs, msg)
	*m.CapturedContexts = append(*m.CapturedContexts, ctx)
}

func (m *MockLoggerWithContext) Debug(ctx context.Context, message string) {
	m.capture(ctx, message)
}

func (m *MockLoggerWithContext) Debugf(ctx context.Context, format string, args ...interface{}) {
	m.capture(ctx, fmt.Sprintf(format, args...))
}

func (m *MockLoggerWithContext) Info(ctx context.Context, message string) {
	m.capture(ctx, message)
}

func (m *MockLoggerWithContext) Infof(ctx context.Context, format string, args ...interface{}) {
	m.capture(ctx, fmt.Sprintf(format, args...))
}

func (m *MockLoggerWithContext) Warn(ctx context.Context, message string) {
	m.capture(ctx, message)
}

func (m *MockLoggerWithContext) Warnf(ctx context.Context, format string, args ...interface{}) {
	m.capture(ctx, fmt.Sprintf(format, args...))
}

func (m *MockLoggerWithContext) Error(ctx context.Context, message string) {
	m.capture(ctx, message)
}

func (m *MockLoggerWithContext) Errorf(ctx context.Context, format string, args ...interface{}) {
	m.capture(ctx, fmt.Sprintf(format, args...))
}

func (m *MockLoggerWithContext) Fatal(ctx context.Context, message string) {
	m.capture(ctx, message)
	fmt.Fprintf(os.Stderr, "FATAL: %s\n", message)
	os.Exit(1)
}

func (m *MockLoggerWithContext) Fatalf(ctx context.Context, format string, args ...interface{}) {
	m.capture(ctx, fmt.Sprintf(format, args...))
	fmt.Fprintf(os.Stderr, "FATAL: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}
