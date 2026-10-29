package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fortytw2/leaktest"
	pb "github.com/kubemq-io/protobuf/go"
	"github.com/stretchr/testify/require"
)

func ceRequest(contentType, body string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/cloudevents/queues", bytes.NewBufferString(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestReadCloudEvents(t *testing.T) {
	t.Run("binary", func(t *testing.T) {
		events, err := readCloudEvents(ceRequest("text/plain", "hello", map[string]string{
			"ce-specversion": "1.0", "ce-id": "1", "ce-source": "/orders", "ce-type": "order.created", "ce-region": "eu%2Dwest",
		}))
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, []byte("hello"), events[0].data)
		require.Equal(t, map[string]string{
			"ce-specversion": "1.0", "ce-id": "1", "ce-source": "/orders", "ce-type": "order.created", "ce-region": "eu-west", "ce-datacontenttype": "text/plain",
		}, events[0].tags())
	})
	t.Run("structured json data", func(t *testing.T) {
		events, err := readCloudEvents(ceRequest("application/cloudevents+json; charset=utf-8",
			`{"specversion":"1.0","id":"1","source":"/orders","type":"order.created","priority":3,"data":{"total":10}}`, nil))
		require.NoError(t, err)
		require.JSONEq(t, `{"total":10}`, string(events[0].data))
		require.Equal(t, "3", events[0].attributes["priority"])
	})
	t.Run("structured text data", func(t *testing.T) {
		events, err := readCloudEvents(ceRequest(ceStructuredType,
			`{"specversion":"1.0","id":"1","source":"s","type":"t","datacontenttype":"text/plain","data":"hi"}`, nil))
		require.NoError(t, err)
		require.Equal(t, []byte("hi"), events[0].data)
	})
	t.Run("structured base64 data", func(t *testing.T) {
		events, err := readCloudEvents(ceRequest(ceStructuredType,
			`{"specversion":"1.0","id":"1","source":"s","type":"t","data_base64":"AAEC"}`, nil))
		require.NoError(t, err)
		require.Equal(t, []byte{0, 1, 2}, events[0].data)
	})
	t.Run("batch", func(t *testing.T) {
		events, err := readCloudEvents(ceRequest(ceBatchType,
			`[{"specversion":"1.0","id":"1","source":"s","type":"t"},{"specversion":"1.0","id":"2","source":"s","type":"t","data":"x"}]`, nil))
		require.NoError(t, err)
		require.Len(t, events, 2)
		require.Nil(t, events[0].data)
		require.Equal(t, []byte(`"x"`), events[1].data, "JSON data stays JSON")
	})

	bad := map[string]*http.Request{
		"plain body":       ceRequest("application/json", `{"a":1}`, nil),
		"missing id":       ceRequest(ceStructuredType, `{"specversion":"1.0","source":"s","type":"t"}`, nil),
		"old specversion":  ceRequest(ceStructuredType, `{"specversion":"0.3","id":"1","source":"s","type":"t"}`, nil),
		"invalid json":     ceRequest(ceStructuredType, `{`, nil),
		"bad base64":       ceRequest(ceStructuredType, `{"specversion":"1.0","id":"1","source":"s","type":"t","data_base64":"!"}`, nil),
		"empty batch":      ceRequest(ceBatchType, `[]`, nil),
		"bad batch member": ceRequest(ceBatchType, `[{"specversion":"1.0","id":"1","source":"s"}]`, nil),
		"binary no type":   ceRequest("", "x", map[string]string{"ce-specversion": "1.0", "ce-id": "1", "ce-source": "s"}),
	}
	for name, r := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := readCloudEvents(r)
			require.Error(t, err)
		})
	}
}

func TestRestServer_CloudEvents(t *testing.T) {
	defer leaktest.Check(t)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port, svc, s := setup(ctx, t)
	defer tearDown(svc, s)
	base := fmt.Sprintf("http://localhost:%d", port)

	post := func(path, contentType, body string, headers map[string]string) (int, string) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", contentType)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = res.Body.Close() }()
		var out bytes.Buffer
		_, _ = out.ReadFrom(res.Body)
		return res.StatusCode, out.String()
	}

	code, body := post("/cloudevents/queues/ce.orders", "application/json", `{"total":10}`, map[string]string{
		"ce-specversion": "1.0", "ce-id": "order-1", "ce-source": "/shop", "ce-type": "order.created",
	})
	require.Equal(t, http.StatusAccepted, code, body)
	// Without a channel in the path the type is the channel.
	code, body = post("/cloudevents/queues", ceBatchType,
		`[{"specversion":"1.0","id":"a","source":"/shop","type":"ce.audit","data":"one"},{"specversion":"1.0","id":"b","source":"/shop","type":"ce.audit","data":"two"}]`, nil)
	require.Equal(t, http.StatusAccepted, code, body)
	code, body = post("/cloudevents/events", ceStructuredType, `{"specversion":"1.0","id":"e","source":"/shop","type":"ce.events"}`, nil)
	require.Equal(t, http.StatusAccepted, code, body)
	code, _ = post("/cloudevents/topics/x", ceStructuredType, `{"specversion":"1.0","id":"e","source":"/shop","type":"t"}`, nil)
	require.Equal(t, http.StatusNotFound, code)
	code, _ = post("/cloudevents/queues/x", "application/json", `{}`, nil)
	require.Equal(t, http.StatusBadRequest, code)

	receive := func(channel string, n int) []*pb.QueueMessage {
		res, err := s.services.Array.ReceiveQueueMessages(ctx, &pb.ReceiveQueueMessagesRequest{ClientID: "ce-test", Channel: channel, MaxNumberOfMessages: int32(n), WaitTimeSeconds: 2})
		require.NoError(t, err)
		require.False(t, res.IsError, res.Error)
		return res.Messages
	}
	msgs := receive("ce.orders", 1)
	require.Len(t, msgs, 1)
	msg := msgs[0]
	require.Equal(t, "order-1", msg.MessageID)
	require.Equal(t, "/shop", msg.ClientID)
	require.Equal(t, "order.created", msg.Metadata)
	require.JSONEq(t, `{"total":10}`, string(msg.Body))
	require.Equal(t, "application/json", msg.Tags["ce-datacontenttype"])
	require.Equal(t, "1.0", msg.Tags["ce-specversion"])

	msgs = receive("ce.audit", 2)
	require.Len(t, msgs, 2)
	var bodies []string
	for _, m := range msgs {
		var s string
		require.NoError(t, json.Unmarshal(m.Body, &s))
		bodies = append(bodies, s)
	}
	require.ElementsMatch(t, []string{"one", "two"}, bodies)
}
