package rest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/fortytw2/leaktest"
	"github.com/kubemq-io/kubemq-community/pkg/tracing"
	pb "github.com/kubemq-io/protobuf/go"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestRestServer_Tracing(t *testing.T) {
	defer leaktest.Check(t)()
	recorder := tracetest.NewSpanRecorder()
	restore := tracing.Use(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	defer restore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port, svc, s := setup(ctx, t)
	defer tearDown(svc, s)

	const clientTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	post := func(path, contentType, body string) int {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://localhost:%d%s", port, path), bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("traceparent", "00-"+clientTrace+"-00f067aa0ba902b7-01")
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		return res.StatusCode
	}
	require.Equal(t, http.StatusOK, post("/queue/send", "application/json", `{"Channel":"rest.trace","ClientID":"rest-client","Body":"aGk="}`))
	require.Equal(t, http.StatusAccepted, post("/cloudevents/queues/rest.trace", ceStructuredType, `{"specversion":"1.0","id":"ce-1","source":"/shop","type":"t"}`))

	var server, send []sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		switch span.Name() {
		case "POST /queue/send", "POST /cloudevents/:pattern/*":
			server = append(server, span)
		case "send rest.trace":
			send = append(send, span)
		}
	}
	require.Len(t, server, 2)
	require.Len(t, send, 2)
	for i := range server {
		require.Equal(t, trace.SpanKindServer, server[i].SpanKind())
		require.Equal(t, "00f067aa0ba902b7", server[i].Parent().SpanID().String())
		require.Equal(t, server[i].SpanContext().SpanID(), send[i].Parent().SpanID())
	}

	res, err := svc.Array.ReceiveQueueMessages(ctx, &pb.ReceiveQueueMessagesRequest{ClientID: "worker", Channel: "rest.trace", MaxNumberOfMessages: 2, WaitTimeSeconds: 2})
	require.NoError(t, err)
	require.Len(t, res.Messages, 2)
	for _, msg := range res.Messages {
		require.Equal(t, clientTrace, msg.Tags["traceparent"][3:35])
	}
}
