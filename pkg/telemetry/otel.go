package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	hfl "github.com/openshift-hyperfleet/hyperfleet-logger"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	samplerAlwaysOn             = "always_on"
	samplerAlwaysOff            = "always_off"
	samplerTraceIDRatio         = "traceidratio"
	envOtelTracesSampler        = "OTEL_TRACES_SAMPLER"
	envOtelTracesSamplerArg     = "OTEL_TRACES_SAMPLER_ARG"
	envOtelExporterOtlpEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envOtelExporterOtlpProtocol = "OTEL_EXPORTER_OTLP_PROTOCOL"
	parentBasedTraceIDRatio     = "parentbased_traceidratio"
	parentBasedAlwaysOn         = "parentbased_always_on"
	parentBasedAlwaysOff        = "parentbased_always_off"
	defaultSamplingRate         = 1.0
)

// InitTraceProvider initializes OpenTelemetry trace provider
func InitTraceProvider(ctx context.Context, serviceName, serviceVersion string) (*trace.TracerProvider, error) {

	var exporter trace.SpanExporter
	var err error

	if otlpEndpoint := os.Getenv(envOtelExporterOtlpEndpoint); otlpEndpoint != "" {
		protocol := os.Getenv(envOtelExporterOtlpProtocol)
		switch strings.ToLower(protocol) {
		case "http", "http/protobuf":
			exporter, err = otlptracehttp.New(ctx)
		case "grpc", "": // Default to gRPC per standard
			exporter, err = otlptracegrpc.New(ctx)
		// Uses gRPC exporter (port 4317) following OpenTelemetry standards
		// This is compatible with standard OTEL Collector configurations
		default:
			slog.WarnContext(ctx, "Unrecognized OTEL_EXPORTER_OTLP_PROTOCOL, using default grpc", "protocol", protocol)
			exporter, err = otlptracegrpc.New(ctx)
		}
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create OTLP exporter", "protocol", protocol, "error", err)
			return nil, fmt.Errorf("failed to create OTLP exporter (protocol=%s): %w", protocol, err)
		}
	} else {
		// Create stdout exporter
		exporter, err = stdouttrace.New()
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create OpenTelemetry stdout exporter", "error", err)
			return nil, fmt.Errorf("failed to create OpenTelemetry stdout exporter: %w", err)
		}
	}

	// Create resource (service information)
	res, err := resource.New(ctx,
		resource.WithFromEnv(), // parse OTEL_RESOURCE_ATTRIBUTES
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
		),
	)
	if err != nil {
		if shutdownErr := exporter.Shutdown(ctx); shutdownErr != nil {
			slog.WarnContext(ctx, "Failed to shutdown exporter", "error", shutdownErr)
		}
		slog.ErrorContext(ctx, "Failed to create OpenTelemetry resource",
			"error", err, "service_name", serviceName, "service_version", serviceVersion)
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	var sampler trace.Sampler
	samplerType := strings.ToLower(os.Getenv(envOtelTracesSampler))

	switch samplerType {
	case samplerAlwaysOn:
		sampler = trace.AlwaysSample()
	case samplerAlwaysOff:
		sampler = trace.NeverSample()
	case samplerTraceIDRatio:
		sampler = trace.TraceIDRatioBased(parseSamplingRate(ctx))
	case parentBasedTraceIDRatio, "":
		// Default per tracing standard
		sampler = trace.ParentBased(trace.TraceIDRatioBased(parseSamplingRate(ctx)))
	case parentBasedAlwaysOn:
		sampler = trace.ParentBased(trace.AlwaysSample())
	case parentBasedAlwaysOff:
		sampler = trace.ParentBased(trace.NeverSample())
	default: // Intentional fallback to default sampler
		slog.WarnContext(ctx, "Unrecognized sampler, using default", "sampler", samplerType)
		sampler = trace.ParentBased(trace.TraceIDRatioBased(parseSamplingRate(ctx)))
	}

	// Create trace provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
		trace.WithSampler(sampler),
	)

	// Set global trace provider
	otel.SetTracerProvider(tp)
	// Select the propagator based on envivronment variable OTEL_PROPAGATORS
	// If OTEL_PROPAGATORS is not provided, uses default "tracecontext,baggage"
	textMapProp := autoprop.NewTextMapPropagator()
	otel.SetTextMapPropagator(textMapProp)

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
func StartSpan(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, oteltrace.Span) {
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
		ctx = hfl.Set(ctx, hfl.TraceIDKey, traceID)
		ctx = hfl.Set(ctx, hfl.SpanIDKey, spanID)
	}

	return ctx, span
}

// SetTraceContext adds W3C traceParent extension to CloudEvent for distributed tracing
func SetTraceContext(event *cloudevents.Event, span oteltrace.Span) {
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

// Helper to parse sampling rate from env
func parseSamplingRate(ctx context.Context) float64 {
	rate := defaultSamplingRate
	if arg := os.Getenv(envOtelTracesSamplerArg); arg != "" {
		if parsedRate, err := strconv.ParseFloat(arg, 64); err == nil && parsedRate >= 0.0 && parsedRate <= 1.0 {
			rate = parsedRate
		} else { // Intentional fallback to default rate
			slog.WarnContext(ctx, "Invalid sampler arg value, using default",
				"env_var", envOtelTracesSamplerArg, "value", arg, "default_rate", rate)
		}
	}
	return rate
}
