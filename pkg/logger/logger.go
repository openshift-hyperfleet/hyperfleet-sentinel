package logger

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang/glog"
)

type HyperFleetLogger interface {
	V(level int32) HyperFleetLogger
	Infof(ctx context.Context, format string, args ...interface{})
	Extra(key string, value interface{}) HyperFleetLogger
	Info(ctx context.Context, message string)
	Warning(ctx context.Context, message string)
	Error(ctx context.Context, message string)
	Fatal(ctx context.Context, message string)
}

var _ HyperFleetLogger = &logger{}

type extra map[string]interface{}

type logger struct {
	level     int32
	accountID string
	// TODO username is unused, should we be logging it? Could be pii
	username string
	extra    extra
}

// NewHyperFleetLogger creates a new logger instance with a default verbosity of 1
func NewHyperFleetLogger() HyperFleetLogger {
	logger := &logger{
		level:     1,
		extra:     make(extra),
		accountID: "", // Sentinel doesn't have account concept
	}
	return logger
}

func (l *logger) buildContextPrefix(ctx context.Context) string {
	prefix := " "

	if ctx != nil {
		if txid, ok := ctx.Value(TxIDKey).(int64); ok {
			prefix = fmt.Sprintf("[tx_id=%d]%s", txid, prefix)
		}
	}

	if l.accountID != "" {
		prefix = fmt.Sprintf("[accountID=%s]%s", l.accountID, prefix)
	}

	if ctx != nil {
		if opid, ok := ctx.Value(OpIDKey).(string); ok {
			prefix = fmt.Sprintf("[opid=%s]%s", opid, prefix)
		}
	}

	return prefix
}

func (l *logger) prepareLogPrefix(ctx context.Context, message string, extra extra) string {
	prefix := l.buildContextPrefix(ctx)

	var args []string
	for k, v := range extra {
		args = append(args, fmt.Sprintf("%s=%v", k, v))
	}

	return fmt.Sprintf("%s %s %s", prefix, message, strings.Join(args, " "))
}

func (l *logger) prepareLogPrefixf(ctx context.Context, format string, args ...interface{}) string {
	orig := fmt.Sprintf(format, args...)
	prefix := l.buildContextPrefix(ctx)

	return fmt.Sprintf("%s%s", prefix, orig)
}

func (l *logger) V(level int32) HyperFleetLogger {
	return &logger{
		accountID: l.accountID,
		username:  l.username,
		level:     level,
		extra:     l.extra,
	}
}

// Infof doesn't trigger Sentry error
func (l *logger) Infof(ctx context.Context, format string, args ...interface{}) {
	prefixed := l.prepareLogPrefixf(ctx, format, args...)
	glog.V(glog.Level(l.level)).Infof("%s", prefixed)
}

func (l *logger) Extra(key string, value interface{}) HyperFleetLogger {
	l.extra[key] = value
	return l
}

func (l *logger) Info(ctx context.Context, message string) {
	l.log(ctx, message, glog.V(glog.Level(l.level)).Infoln)
}

func (l *logger) Warning(ctx context.Context, message string) {
	l.log(ctx, message, glog.Warningln)
}

func (l *logger) Error(ctx context.Context, message string) {
	l.log(ctx, message, glog.Errorln)
}

func (l *logger) Fatal(ctx context.Context, message string) {
	l.log(ctx, message, glog.Fatalln)
}

func (l *logger) log(ctx context.Context, message string, glogFunc func(args ...interface{})) {
	prefixed := l.prepareLogPrefix(ctx, message, l.extra)
	glogFunc(prefixed)
}
