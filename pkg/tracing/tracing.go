// Package tracing exports OpenTelemetry traces and carries trace context
// through the broker in message tags.
//
// A message the broker accepts gets a send span, a child of the trace context
// already in its tags (put there by an instrumented producer) or else of the
// request's own span. The send span's context replaces the traceparent tag, so
// it travels with the message, through storage for queues and events store.
// When the broker hands the message to a consumer it records a receive span as
// a child of that context and points the tag at it, so a consumer that reads
// traceparent from the tags continues the same trace.
package tracing

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/kubemq-io/kubemq-community"

// enabled skips the tag handling entirely while tracing is off.
var enabled atomic.Bool

var propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

func tracer() trace.Tracer {
	return otel.Tracer(instrumentationName)
}

// Enabled reports whether spans are being recorded and exported.
func Enabled() bool {
	return enabled.Load()
}

// Init sets up the exporter and the global tracer provider. The returned
// function flushes and stops the exporter; it is a no-op when tracing is off.
func Init(ctx context.Context, cfg *config.TracingConfig, host, version string) (func(context.Context) error, error) {
	if cfg == nil || !cfg.Enable {
		return func(context.Context) error { return nil }, nil
	}
	var exporter *otlptrace.Exporter
	var err error
	switch cfg.Protocol {
	case "http":
		var opts []otlptracehttp.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
	default:
		var opts []otlptracegrpc.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exporter, err = otlptracegrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("error creating trace exporter: %w", err)
	}
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "kubemq"
	}
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", version),
			attribute.String("service.instance.id", host),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("error creating trace resource: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)
	logger := logging.GetLogFactory().NewLogger("tracing")
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warnw("error exporting traces", "error", err.Error())
	}))
	restore := Use(tp)
	logger.Infof("exporting traces over OTLP/%s", cfg.Protocol)
	return func(ctx context.Context) error {
		restore()
		return tp.Shutdown(ctx)
	}, nil
}

// Use makes tp the global tracer provider and turns tracing on, until the
// returned function is called. Tests use it with an in-memory exporter.
func Use(tp trace.TracerProvider) func() {
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagator)
	enabled.Store(true)
	return func() {
		enabled.Store(false)
		otel.SetTracerProvider(previous)
	}
}

// Message describes a message for the attributes of its spans.
type Message struct {
	// Pattern is events, events_store, commands, queries or queues.
	Pattern  string
	Channel  string
	ID       string
	ClientID string
	Group    string
	BodySize int
}

func (m Message) attributes(operation string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("messaging.system", "kubemq"),
		attribute.String("messaging.operation.name", operation),
		attribute.String("messaging.operation.type", operation),
		attribute.String("messaging.destination.name", m.Channel),
		attribute.String("messaging.kubemq.pattern", m.Pattern),
		attribute.Int("messaging.message.body.size", m.BodySize),
	}
	if m.ID != "" {
		attrs = append(attrs, attribute.String("messaging.message.id", m.ID))
	}
	if m.ClientID != "" {
		attrs = append(attrs, attribute.String("messaging.client.id", m.ClientID))
	}
	if m.Group != "" {
		attrs = append(attrs, attribute.String("messaging.consumer.group.name", m.Group))
	}
	return attrs
}

// Send starts the span for the broker accepting a message: a child of the
// trace context in the message's tags if it has one, else of ctx. It writes
// the span's context into the tags, which are returned (allocated when nil),
// and the caller ends the span with the outcome.
func Send(ctx context.Context, tags map[string]string, m Message) (map[string]string, func(error)) {
	if !Enabled() {
		return tags, func(error) {}
	}
	ctx = propagator.Extract(ctx, propagation.MapCarrier(tags))
	ctx, span := tracer().Start(ctx, "send "+m.Channel,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(m.attributes("send")...),
	)
	tags = inject(ctx, tags)
	return tags, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

// Deliver records the broker handing a message to a consumer, as a child of
// the trace context in its tags, and points the tags at the new span. A
// message without trace context starts a new trace.
func Deliver(tags map[string]string, m Message) map[string]string {
	if !Enabled() {
		return tags
	}
	ctx := propagator.Extract(context.Background(), propagation.MapCarrier(tags))
	ctx, span := tracer().Start(ctx, "receive "+m.Channel,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(m.attributes("receive")...),
	)
	span.End()
	return inject(ctx, tags)
}

func inject(ctx context.Context, tags map[string]string) map[string]string {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return tags
	}
	if tags == nil {
		tags = map[string]string{}
	}
	propagator.Inject(ctx, propagation.MapCarrier(tags))
	return tags
}
