package logctx

import (
	"context"
	"testing"

	hfl "github.com/openshift-hyperfleet/hyperfleet-logger"
)

func TestContextFields(t *testing.T) {
	fields := ContextFields()
	if len(fields) != 2 {
		t.Fatalf("expected 2 context fields, got %d", len(fields))
	}
	if fields[0].Name != "topic" {
		t.Errorf("expected first field name 'topic', got %q", fields[0].Name)
	}
	if fields[1].Name != "decision_reason" {
		t.Errorf("expected second field name 'decision_reason', got %q", fields[1].Name)
	}
}

func TestContextFieldRoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = hfl.Set(ctx, TopicKey, "test-topic")
	ctx = hfl.Set(ctx, DecisionReasonKey, "matched")

	topic, ok := hfl.Get(ctx, TopicKey)
	if !ok || topic != "test-topic" {
		t.Errorf("expected topic 'test-topic', got %q (ok=%v)", topic, ok)
	}

	reason, ok := hfl.Get(ctx, DecisionReasonKey)
	if !ok || reason != "matched" {
		t.Errorf("expected decision_reason 'matched', got %q (ok=%v)", reason, ok)
	}
}
