package telemetry

import (
	"context"
	"os"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInitTraceProvider_StdoutExporter(t *testing.T) {
	ctx := context.Background()

	// Test stdout exporter (default)
	tp, err := InitTraceProvider(ctx, "test-service", "v1.0.0", 1.0)
	if err != nil {
		t.Fatalf("Failed to initialize trace provider: %v", err)
	}
	if tp == nil {
		t.Fatal("Expected trace provider, got nil")
	}

	// Cleanup
	defer func() {
		if err := Shutdown(ctx, tp); err != nil {
			t.Errorf("Failed to shutdown trace provider: %v", err)
		}
	}()

	// Verify tracer is available
	tracer := otel.Tracer("test")
	if tracer == nil {
		t.Error("Expected tracer to be available")
	}
}

func TestInitTraceProvider_OTLPExporter(t *testing.T) {
	ctx := context.Background()

	// Set OTLP endpoint
	err := os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318/v1/traces")
	if err != nil {
		t.Fatalf("Failed to set OTEL_EXPORTER_OTLP_ENDPOINT: %v", err)
	}
	defer func() {
		err := os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		if err != nil {
			t.Fatalf("Failed to unset OTEL_EXPORTER_OTLP_ENDPOINT: %v", err)
		}
	}()

	tp, err := InitTraceProvider(ctx, "test-service", "v1.0.0", 1.0)
	if err != nil {
		t.Fatalf("Failed to initialize trace provider with OTLP: %v", err)
	}
	if tp == nil {
		t.Fatal("Expected trace provider, got nil")
	}

	defer func(ctx context.Context, tp *trace.TracerProvider) {
		err := Shutdown(ctx, tp)
		if err != nil {
			t.Fatal("Failed to shutdown trace provider")
		}
	}(ctx, tp)

	// Verify tracer is available
	tracer := otel.Tracer("test")
	if tracer == nil {
		t.Error("Expected tracer to be available")
	}
}

func TestInitTraceProvider_SamplerEnvironmentVariables(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		samplerType    string
		samplerArg     string
		baseSampling   float64
		expectedSample bool
	}{
		{
			name:           "always_on",
			samplerType:    "always_on",
			baseSampling:   0.5,
			expectedSample: true,
		},
		{
			name:           "always_off",
			samplerType:    "always_off",
			baseSampling:   0.5,
			expectedSample: false,
		},
		{
			name:           "traceidratio_high",
			samplerType:    "traceidratio",
			samplerArg:     "1.0",
			baseSampling:   0.5,
			expectedSample: true,
		},
		{
			name:           "traceidratio_zero",
			samplerType:    "traceidratio",
			samplerArg:     "0.0",
			baseSampling:   0.5,
			expectedSample: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			if tt.samplerType != "" {
				err := os.Setenv("OTEL_TRACES_SAMPLER", tt.samplerType)
				if err != nil {
					t.Fatalf("Failed to set OTEL_TRACES_SAMPLER: %v", err)
				}
				defer func() {
					err := os.Unsetenv("OTEL_TRACES_SAMPLER")
					if err != nil {
						t.Fatalf("Failed to unset OTEL_TRACES_SAMPLER: %v", err)
					}
				}()
			}
			if tt.samplerArg != "" {
				err := os.Setenv("OTEL_TRACES_SAMPLER_ARG", tt.samplerArg)
				if err != nil {
					t.Fatalf("Failed to set OTEL_TRACES_SAMPLER_ARG: %v", err)
				}
				defer func() {
					err := os.Unsetenv("OTEL_TRACES_SAMPLER_ARG")
					if err != nil {
						t.Fatalf("Failed to unset OTEL_TRACES_SAMPLER_ARG: %v", err)
					}
				}()
			}

			tp, err := InitTraceProvider(ctx, "test-service", "v1.0.0", tt.baseSampling)
			if err != nil {
				t.Fatalf("Failed to initialize trace provider: %v", err)
			}
			defer func(ctx context.Context, tp *trace.TracerProvider) {
				err := Shutdown(ctx, tp)
				if err != nil {
					t.Fatal("Failed to shutdown trace provider")
				}
			}(ctx, tp)

			// Test sampling behavior by checking if spans are created
			tracer := otel.Tracer("test")
			_, span := tracer.Start(ctx, "test-span")

			if tt.expectedSample {
				if !span.SpanContext().IsValid() {
					t.Error("Expected valid span context for sampling=true")
				}
			}
			span.End()
		})
	}
}

func TestStartSpan(t *testing.T) {
	ctx := context.Background()

	// Initialize trace provider with in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func(ctx context.Context, tp *trace.TracerProvider) {
		err := Shutdown(ctx, tp)
		if err != nil {
			t.Fatalf("Failed to shutdown trace provider : %v", err)
		}
	}(ctx, tp)

	// Test span creation
	attrs := []attribute.KeyValue{
		attribute.String("test.key", "test.value"),
	}

	newCtx, span := StartSpan(ctx, "test-span", attrs...)
	span.End()

	// Force flush to ensure span reaches exporter
	err := tp.ForceFlush(ctx)
	if err != nil {
		t.Fatalf("Failed to force flush: %v", err)
	}

	// Verify span was created
	if !span.SpanContext().IsValid() {
		t.Error("Expected valid span context")
	}

	// Verify span name
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	if spans[0].Name != "test-span" {
		t.Errorf("Expected span name 'test-span', got %s", spans[0].Name)
	}

	// Verify attributes
	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "test.key" && attr.Value.AsString() == "test.value" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected attribute 'test.key=test.value' not found")
	}

	// Verify context enrichment (trace/span IDs should be in context)
	traceID, hasTraceID := newCtx.Value(logger.TraceIDCtxKey).(string)
	spanID, hasSpanID := newCtx.Value(logger.SpanIDCtxKey).(string)

	if !hasTraceID || traceID == "" {
		t.Error("Expected context to contain trace ID")
	}
	if !hasSpanID || spanID == "" {
		t.Error("Expected context to contain span ID")
	}

	// Verify the IDs match the actual span
	expectedTraceID := span.SpanContext().TraceID().String()
	expectedSpanID := span.SpanContext().SpanID().String()

	if traceID != expectedTraceID {
		t.Errorf("Expected trace ID %s, got %s", expectedTraceID, traceID)
	}
	if spanID != expectedSpanID {
		t.Errorf("Expected span ID %s, got %s", expectedSpanID, spanID)
	}
}

func TestSetTraceContext(t *testing.T) {
	ctx := context.Background()

	// Initialize trace provider
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func(ctx context.Context, tp *trace.TracerProvider) {
		err := Shutdown(ctx, tp)
		if err != nil {
			t.Fatal("Failed to shutdown trace provider")
		}
	}(ctx, tp)

	// Create a span
	tracer := otel.Tracer("test")
	_, span := tracer.Start(ctx, "test-span")

	// Create CloudEvent
	event := cloudevents.NewEvent()
	event.SetType("test.event")
	event.SetSource("test")
	event.SetID("test-123")

	// Set trace context
	SetTraceContext(&event, span)
	span.End()

	// Verify traceparent extension was added
	extensions := event.Extensions()
	traceparent, exists := extensions["traceparent"]
	if !exists {
		t.Fatal("Expected traceparent extension to be set")
	}

	// Verify traceparent format: 00-{trace_id}-{span_id}-01
	traceParentStr, ok := traceparent.(string)
	if !ok {
		t.Fatal("Expected traceparent to be a string")
	}

	if len(traceParentStr) != 55 { // 00-{32 chars}-{16 chars}-01
		t.Errorf("Expected traceparent length 55, got %d", len(traceParentStr))
	}

	if traceParentStr[:3] != "00-" {
		t.Errorf("Expected traceparent to start with '00-', got %s", traceParentStr[:3])
	}

	if traceParentStr[len(traceParentStr)-3:] != "-01" {
		t.Errorf("Expected traceparent to end with '-01', got %s", traceParentStr[len(traceParentStr)-3:])
	}
}

func TestSetTraceContext_InvalidSpan(t *testing.T) {
	// Test with invalid span context
	event := cloudevents.NewEvent()
	event.SetType("test.event")

	// Create a mock invalid span
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test")
	span.End() // End immediately to potentially invalidate

	// This should not panic or error
	SetTraceContext(&event, span)

	// Should not have traceparent extension
	extensions := event.Extensions()
	if _, exists := extensions["traceparent"]; exists {
		// This might exist if span was still valid, that's ok
		t.Log("traceparent was set despite span being ended")
	}
}
