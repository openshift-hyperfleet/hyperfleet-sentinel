package logger

import (
	"context"

	brokerlogger "github.com/openshift-hyperfleet/hyperfleet-broker/pkg/logger"
)

// BrokerLoggerAdapter adapts HyperFleetLogger to the broker's logger.Logger interface.
// The broker expects Warn/Warnf while HyperFleetLogger provides Warning/Warningf.
type BrokerLoggerAdapter struct {
	log HyperFleetLogger
}

// NewBrokerLoggerAdapter creates a new adapter that wraps HyperFleetLogger
// to satisfy the broker's logger.Logger interface.
func NewBrokerLoggerAdapter(log HyperFleetLogger) brokerlogger.Logger {
	return &BrokerLoggerAdapter{log: log}
}

func (a *BrokerLoggerAdapter) Debug(ctx context.Context, message string) {
	a.log.Debug(ctx, message)
}

func (a *BrokerLoggerAdapter) Debugf(ctx context.Context, format string, args ...any) {
	a.log.Debugf(ctx, format, args...)
}

func (a *BrokerLoggerAdapter) Info(ctx context.Context, message string) {
	a.log.Info(ctx, message)
}

func (a *BrokerLoggerAdapter) Infof(ctx context.Context, format string, args ...any) {
	a.log.Infof(ctx, format, args...)
}

func (a *BrokerLoggerAdapter) Warn(ctx context.Context, message string) {
	a.log.Warn(ctx, message)
}

func (a *BrokerLoggerAdapter) Warnf(ctx context.Context, format string, args ...any) {
	a.log.Warnf(ctx, format, args...)
}

func (a *BrokerLoggerAdapter) Error(ctx context.Context, message string) {
	a.log.Error(ctx, message)
}

func (a *BrokerLoggerAdapter) Errorf(ctx context.Context, format string, args ...any) {
	a.log.Errorf(ctx, format, args...)
}
