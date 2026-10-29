package tracing

import (
	"context"
	"errors"
	"testing"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func record(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	restore := Use(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(restore)
	return recorder
}

func TestDisabled(t *testing.T) {
	require.False(t, Enabled())
	tags, end := Send(context.Background(), nil, Message{Channel: "a"})
	end(nil)
	require.Nil(t, tags)
	require.Nil(t, Deliver(nil, Message{Channel: "a"}))

	shutdown, err := Init(context.Background(), &config.TracingConfig{Enable: false}, "h", "v")
	require.NoError(t, err)
	require.NoError(t, shutdown(context.Background()))
	require.False(t, Enabled())
}

func TestSendThenDeliver(t *testing.T) {
	recorder := record(t)
	m := Message{Pattern: "queues", Channel: "orders", ID: "m1", ClientID: "shop", BodySize: 5}

	tags, end := Send(context.Background(), map[string]string{"k": "v"}, m)
	end(nil)
	require.Equal(t, "v", tags["k"])
	require.NotEmpty(t, tags["traceparent"])
	sentTraceparent := tags["traceparent"]

	tags = Deliver(tags, Message{Pattern: "queues", Channel: "orders", ID: "m1", ClientID: "worker"})
	require.NotEqual(t, sentTraceparent, tags["traceparent"], "the tag points at the receive span")

	spans := recorder.Ended()
	require.Len(t, spans, 2)
	send, receive := spans[0], spans[1]
	require.Equal(t, "send orders", send.Name())
	require.Equal(t, trace.SpanKindProducer, send.SpanKind())
	require.Equal(t, "receive orders", receive.Name())
	require.Equal(t, trace.SpanKindConsumer, receive.SpanKind())
	require.Equal(t, send.SpanContext().TraceID(), receive.SpanContext().TraceID())
	require.Equal(t, send.SpanContext().SpanID(), receive.Parent().SpanID())
	attrs := map[string]string{}
	for _, a := range send.Attributes() {
		attrs[string(a.Key)] = a.Value.String()
	}
	require.Equal(t, "kubemq", attrs["messaging.system"])
	require.Equal(t, "orders", attrs["messaging.destination.name"])
	require.Equal(t, "queues", attrs["messaging.kubemq.pattern"])
	require.Equal(t, "m1", attrs["messaging.message.id"])
	require.Equal(t, "shop", attrs["messaging.client.id"])
	require.Equal(t, "5", attrs["messaging.message.body.size"])
}

func TestSendContinuesTheProducersTrace(t *testing.T) {
	recorder := record(t)
	const producer = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tags, end := Send(context.Background(), map[string]string{"traceparent": producer}, Message{Channel: "a"})
	end(errors.New("broker down"))

	span := recorder.Ended()[0]
	require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", span.SpanContext().TraceID().String())
	require.Equal(t, "00f067aa0ba902b7", span.Parent().SpanID().String())
	require.Equal(t, codes.Error, span.Status().Code)
	require.NotEqual(t, producer, tags["traceparent"])
}

func TestSendUsesTheRequestSpan(t *testing.T) {
	recorder := record(t)
	ctx, parent := otelTracer().Start(context.Background(), "rpc")
	_, end := Send(ctx, nil, Message{Channel: "a"})
	end(nil)
	parent.End()
	send := recorder.Ended()[0]
	require.Equal(t, parent.SpanContext().SpanID(), send.Parent().SpanID())
}

func otelTracer() trace.Tracer { return tracer() }

func TestDeliverWithoutContextStartsATrace(t *testing.T) {
	recorder := record(t)
	tags := Deliver(nil, Message{Channel: "a"})
	require.NotEmpty(t, tags["traceparent"])
	require.False(t, recorder.Ended()[0].Parent().IsValid())
}
