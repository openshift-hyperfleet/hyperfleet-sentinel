package logger

import (
	"context"

	"github.com/segmentio/ksuid"
)

// Context key types for type-safe context values
type OperationIDKey string
type TransactionIDKey string
type TraceIDKey string
type SpanIDKey string
type DecisionReasonKey string
type TopicKey string
type SubsetKey string

// Context keys for logging fields
const (
	OpIDKey              OperationIDKey    = "op_id"
	TxIDKey              TransactionIDKey  = "tx_id"
	TraceIDCtxKey        TraceIDKey        = "trace_id"
	SpanIDCtxKey         SpanIDKey         = "span_id"
	DecisionReasonCtxKey DecisionReasonKey = "decision_reason"
	TopicCtxKey          TopicKey          = "topic"
	SubsetCtxKey         SubsetKey         = "subset"
)

func WithOpID(ctx context.Context) context.Context {
	if ctx.Value(OpIDKey) != nil {
		return ctx
	}
	opID := ksuid.New().String()
	return context.WithValue(ctx, OpIDKey, opID)
}

// GetOperationID get operationID of the context
func GetOperationID(ctx context.Context) string {
	if opID, ok := ctx.Value(OpIDKey).(string); ok {
		return opID
	}
	return ""
}

// WithTraceID adds a trace ID to the context
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceIDCtxKey, traceID)
}

// WithSpanID adds a span ID to the context
func WithSpanID(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, SpanIDCtxKey, spanID)
}

// WithDecisionReason adds a decision reason to the context
func WithDecisionReason(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, DecisionReasonCtxKey, reason)
}

// WithTopic adds a topic to the context
func WithTopic(ctx context.Context, topic string) context.Context {
	return context.WithValue(ctx, TopicCtxKey, topic)
}

// WithSubset adds a subset (resource type) to the context
func WithSubset(ctx context.Context, subset string) context.Context {
	return context.WithValue(ctx, SubsetCtxKey, subset)
}

// WithSentinelFields adds all sentinel-specific fields to the context
func WithSentinelFields(ctx context.Context, decisionReason, topic, subset string) context.Context {
	ctx = WithDecisionReason(ctx, decisionReason)
	ctx = WithTopic(ctx, topic)
	ctx = WithSubset(ctx, subset)
	return ctx
}
