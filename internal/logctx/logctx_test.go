package logctx

import (
	"context"
	"testing"

	hfl "github.com/openshift-hyperfleet/hyperfleet-logger"
)

func TestContextFields(t *testing.T) {
	fields := ContextFields()
	if len(fields) != 3 {
		t.Fatalf("expected 3 context fields, got %d", len(fields))
	}
	if fields[0].Name != "subset" {
		t.Errorf("expected first field name 'subset', got %q", fields[0].Name)
	}
	if fields[1].Name != "topic" {
		t.Errorf("expected second field name 'topic', got %q", fields[1].Name)
	}
	if fields[2].Name != "decision_reason" {
		t.Errorf("expected third field name 'decision_reason', got %q", fields[2].Name)
	}
}

func TestContextFieldRoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = hfl.Set(ctx, SubsetKey, "clusters")
	ctx = hfl.Set(ctx, TopicKey, "test-topic")
	ctx = hfl.Set(ctx, DecisionReasonKey, "matched")

	subset, ok := hfl.Get(ctx, SubsetKey)
	if !ok || subset != "clusters" {
		t.Errorf("expected subset 'clusters', got %q (ok=%v)", subset, ok)
	}

	topic, ok := hfl.Get(ctx, TopicKey)
	if !ok || topic != "test-topic" {
		t.Errorf("expected topic 'test-topic', got %q (ok=%v)", topic, ok)
	}

	reason, ok := hfl.Get(ctx, DecisionReasonKey)
	if !ok || reason != "matched" {
		t.Errorf("expected decision_reason 'matched', got %q (ok=%v)", reason, ok)
	}
}
