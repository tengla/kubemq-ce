package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/kubemq-io/kubemq-community/pkg/tracing"
	pb "github.com/kubemq-io/protobuf/go"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

// traceOf returns the trace ID in a traceparent value.
func traceOf(t *testing.T, traceparent string) string {
	t.Helper()
	require.Len(t, traceparent, 55, "traceparent %q", traceparent)
	return traceparent[3:35]
}

func spansNamed(spans []sdktrace.ReadOnlySpan, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

func TestGRPCServer_Tracing(t *testing.T) {
	defer leaktest.Check(t)()
	recorder := tracetest.NewSpanRecorder()
	restore := tracing.Use(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	defer restore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	appConfig, gs, svc := setup(ctx, t)
	defer tearDown(gs, svc)
	client := getNewClient(ctx, t, appConfig)
	defer func() { _ = client.Close() }()

	t.Run("event: client metadata to subscriber", func(t *testing.T) {
		subCtx, subCancel := context.WithCancel(ctx)
		defer subCancel()
		stream, err := client.SubscribeToEvents(subCtx, &pb.Subscribe{SubscribeTypeData: pb.Subscribe_Events, ClientID: "trace-sub", Channel: "trace.events"})
		require.NoError(t, err)
		time.Sleep(500 * time.Millisecond)

		// The client's own span, sent the way instrumented gRPC clients do.
		const clientTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
		sendCtx := metadata.AppendToOutgoingContext(ctx, "traceparent", "00-"+clientTrace+"-00f067aa0ba902b7-01")
		res, err := client.SendEvent(sendCtx, &pb.Event{EventID: nuid.Next(), ClientID: "trace-pub", Channel: "trace.events", Body: []byte("hi")})
		require.NoError(t, err)
		require.True(t, res.Sent)

		received, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, clientTrace, traceOf(t, received.Tags["traceparent"]))

		spans := recorder.Ended()
		rpc := spansNamed(spans, "kubemq.kubemq/SendEvent")
		require.Len(t, rpc, 1)
		require.Equal(t, trace.SpanKindServer, rpc[0].SpanKind())
		require.Equal(t, "00f067aa0ba902b7", rpc[0].Parent().SpanID().String())
		send := spansNamed(spans, "send trace.events")
		require.Len(t, send, 1)
		require.Equal(t, rpc[0].SpanContext().SpanID(), send[0].Parent().SpanID())
		receive := spansNamed(spans, "receive trace.events")
		require.Len(t, receive, 1)
		require.Equal(t, send[0].SpanContext().SpanID(), receive[0].Parent().SpanID())
		require.Equal(t, clientTrace, receive[0].SpanContext().TraceID().String())
		require.Contains(t, received.Tags["traceparent"], receive[0].SpanContext().SpanID().String(), "the consumer continues from the receive span")
	})

	t.Run("queue: traceparent tag survives storage", func(t *testing.T) {
		const producerTrace = "0af7651916cd43dd8448eb211c80319c"
		res, err := client.SendQueueMessage(ctx, &pb.QueueMessage{
			MessageID: nuid.Next(), ClientID: "trace-pub", Channel: "trace.queue", Body: []byte("job"),
			Tags: map[string]string{"traceparent": "00-" + producerTrace + "-b7ad6b7169203331-01", "k": "v"},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, res.Error)

		got, err := client.ReceiveQueueMessages(ctx, &pb.ReceiveQueueMessagesRequest{ClientID: "trace-worker", Channel: "trace.queue", MaxNumberOfMessages: 1, WaitTimeSeconds: 2})
		require.NoError(t, err)
		require.Len(t, got.Messages, 1)
		require.Equal(t, "v", got.Messages[0].Tags["k"])
		require.Equal(t, producerTrace, traceOf(t, got.Messages[0].Tags["traceparent"]))

		send := spansNamed(recorder.Ended(), "send trace.queue")
		require.Len(t, send, 1)
		require.Equal(t, "b7ad6b7169203331", send[0].Parent().SpanID().String(), "the message's tag beats the RPC span")
		require.Len(t, spansNamed(recorder.Ended(), "receive trace.queue"), 1)
	})

	sendTraced := func(t *testing.T, channel string) string {
		t.Helper()
		res, err := client.SendQueueMessage(ctx, &pb.QueueMessage{MessageID: nuid.Next(), ClientID: "trace-pub", Channel: channel, Body: []byte("job")})
		require.NoError(t, err)
		require.False(t, res.IsError, res.Error)
		send := spansNamed(recorder.Ended(), "send "+channel)
		require.Len(t, send, 1)
		return send[0].SpanContext().TraceID().String()
	}

	t.Run("queue downstream get", func(t *testing.T) {
		traceID := sendTraced(t, "trace.downstream")
		streamCtx, streamCancel := context.WithCancel(ctx)
		defer streamCancel()
		stream, err := client.QueuesDownstream(streamCtx)
		require.NoError(t, err)
		require.NoError(t, stream.Send(&pb.QueuesDownstreamRequest{RequestID: nuid.Next(), ClientID: "trace-worker", RequestTypeData: pb.QueuesDownstreamRequestType_Get, Channel: "trace.downstream", MaxItems: 1, WaitTimeout: 2000, AutoAck: true}))
		res, err := stream.Recv()
		require.NoError(t, err)
		require.False(t, res.IsError, res.Error)
		require.Len(t, res.Messages, 1)
		require.Equal(t, traceID, traceOf(t, res.Messages[0].Tags["traceparent"]))
		require.Len(t, spansNamed(recorder.Ended(), "receive trace.downstream"), 1)
	})

	t.Run("stream queue message", func(t *testing.T) {
		traceID := sendTraced(t, "trace.stream")
		streamCtx, streamCancel := context.WithCancel(ctx)
		defer streamCancel()
		stream, err := client.StreamQueueMessage(streamCtx)
		require.NoError(t, err)
		require.NoError(t, stream.Send(&pb.StreamQueueMessagesRequest{RequestID: nuid.Next(), ClientID: "trace-worker", StreamRequestTypeData: pb.StreamRequestType_ReceiveMessage, Channel: "trace.stream", VisibilitySeconds: 5, WaitTimeSeconds: 2}))
		res, err := stream.Recv()
		require.NoError(t, err)
		require.False(t, res.IsError, res.Error)
		require.NotNil(t, res.Message)
		require.Equal(t, traceID, traceOf(t, res.Message.Tags["traceparent"]))
		require.NoError(t, stream.Send(&pb.StreamQueueMessagesRequest{RequestID: nuid.Next(), ClientID: "trace-worker", StreamRequestTypeData: pb.StreamRequestType_AckMessage, RefSequence: res.Message.Attributes.Sequence}))
		_, err = stream.Recv()
		require.NoError(t, err)
		require.Len(t, spansNamed(recorder.Ended(), "receive trace.stream"), 1)
	})

	t.Run("query: request reaches the responder in the same trace", func(t *testing.T) {
		subCtx, subCancel := context.WithCancel(ctx)
		defer subCancel()
		stream, err := client.SubscribeToRequests(subCtx, &pb.Subscribe{SubscribeTypeData: pb.Subscribe_Queries, ClientID: "trace-responder", Channel: "trace.query"})
		require.NoError(t, err)
		time.Sleep(500 * time.Millisecond)
		done := make(chan *pb.Request, 1)
		go func() {
			req, err := stream.Recv()
			if err != nil {
				return
			}
			done <- req
			_, _ = client.SendResponse(ctx, &pb.Response{ClientID: "trace-responder", RequestID: req.RequestID, ReplyChannel: req.ReplyChannel, Executed: true, Body: []byte("ok")})
		}()
		res, err := client.SendRequest(ctx, &pb.Request{RequestID: nuid.Next(), RequestTypeData: pb.Request_Query, ClientID: "trace-asker", Channel: "trace.query", Body: []byte("?"), Timeout: 2000})
		require.NoError(t, err)
		require.True(t, res.Executed)
		req := <-done
		send := spansNamed(recorder.Ended(), "send trace.query")
		require.Len(t, send, 1)
		require.Equal(t, send[0].SpanContext().TraceID().String(), traceOf(t, req.Tags["traceparent"]))
	})

	t.Run("ping is not traced", func(t *testing.T) {
		_, err := client.Ping(ctx, &pb.Empty{})
		require.NoError(t, err)
		require.Empty(t, spansNamed(recorder.Ended(), "kubemq.kubemq/Ping"))
	})
}
