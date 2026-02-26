package telemetry

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	t2 "go.opentelemetry.io/otel/trace"
)

const (
	SAMPLER_ALWAYS_ON               = "always_on"
	SAMPLER_ALWAYS_OFF              = "always_off"
	SAMPLER_TRACE_ID_RATIO          = "traceidratio"
	ENV_OTEL_TRACES_SAMPLER         = "OTEL_TRACES_SAMPLER"
	ENV_OTEL_TRACES_SAMPLER_ARG     = "OTEL_TRACES_SAMPLER_ARG"
	ENV_OTEL_EXPORTER_OTLP_ENDPOINT = "OTEL_EXPORTER_OTLP_ENDPOINT"
)

// InitTraceProvider initializes OpenTelemetry trace provider
func InitTraceProvider(
	ctx context.Context, serviceName, serviceVersion string, samplingRate float64,
) (*trace.TracerProvider, error) {

	var exporter trace.SpanExporter
	var err error

	log := logger.NewHyperFleetLogger()

	if otlpEndpoint := os.Getenv(ENV_OTEL_EXPORTER_OTLP_ENDPOINT); otlpEndpoint != "" {
		exporter, err = otlptracehttp.New(ctx)
		if err != nil {
			log.Errorf(ctx, "Failed to create OTLP HTTP exporter: %v", err)
			return nil, err
		}
	} else {
		// Create stdout exporter
		exporter, err = stdouttrace.New(
			stdouttrace.WithPrettyPrint(), // Formatted output
		)
		if err != nil {
			log.Errorf(ctx, "Failed to create OpenTelemetry stdout exporter: %v", err)
			return nil, err
		}
	}

	// Create resource (service information)
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
		),
	)
	if err != nil {
		log.Extra("service_name", serviceName).Extra("service_version", serviceVersion).Errorf(ctx, "Failed to create OpenTelemetry resource: %v", err)
		return nil, err
	}

	if samplerType := os.Getenv(ENV_OTEL_TRACES_SAMPLER); samplerType != "" {
		switch strings.ToLower(samplerType) {
		case SAMPLER_ALWAYS_ON:
			samplingRate = 1.0
		case SAMPLER_ALWAYS_OFF:
			samplingRate = 0.0
		case SAMPLER_TRACE_ID_RATIO:
			if arg := os.Getenv(ENV_OTEL_TRACES_SAMPLER_ARG); arg != "" {
				rate, err := strconv.ParseFloat(arg, 64)
				if err != nil {
					log.Warnf(ctx, "Invalid %s value=%q, using default samplingRate=%v: %v", ENV_OTEL_TRACES_SAMPLER_ARG, arg, samplingRate, err)
				} else {
					samplingRate = rate
				}
			}
		}
	}

	// Determine sampler based on sampling rate
	var sampler trace.Sampler
	switch {
	case samplingRate >= 1.0:
		sampler = trace.AlwaysSample() // Sample all
	case samplingRate <= 0.0:
		sampler = trace.NeverSample() // Sample none
	default:
		sampler = trace.ParentBased(trace.TraceIDRatioBased(samplingRate))
	}

	// Create trace provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
		trace.WithSampler(sampler),
	)

	// Set global trace provider
	otel.SetTracerProvider(tp)

	return tp, nil
}

// Shutdown gracefully shuts down the trace provider
func Shutdown(ctx context.Context, tp *trace.TracerProvider) error {
	if tp == nil {
		return nil
	}
	return tp.Shutdown(ctx)
}

// StartSpan starts a span and enriches context with trace/span IDs for logging
func StartSpan(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, t2.Span) {
	tracer := otel.Tracer("hyperfleet-sentinel")
	ctx, span := tracer.Start(ctx, spanName)

	// Add attributes if provided
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}

	// Enrich context with trace/span IDs for logging
	if span.SpanContext().IsValid() {
		traceID := span.SpanContext().TraceID().String()
		spanID := span.SpanContext().SpanID().String()
		ctx = logger.WithTraceID(ctx, traceID)
		ctx = logger.WithSpanID(ctx, spanID)
	}

	return ctx, span
}

// SetTraceContext adds W3C traceParent extension to CloudEvent for distributed tracing
func SetTraceContext(event *cloudevents.Event, span t2.Span) {
	if event == nil || span == nil {
		return
	}
	if span.SpanContext().IsValid() {
		traceParent := fmt.Sprintf("00-%s-%s-%02x",
			span.SpanContext().TraceID().String(),
			span.SpanContext().SpanID().String(),
			uint8(span.SpanContext().TraceFlags()))
		event.SetExtension("traceparent", traceParent)
	}
}
