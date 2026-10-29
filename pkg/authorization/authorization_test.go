package authorization

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"github.com/kubemq-io/kubemq-community/pkg/entities"
	pb "github.com/kubemq-io/protobuf/go"
	"github.com/stretchr/testify/require"
)

func TestMatchChannel(t *testing.T) {
	tests := []struct {
		pattern, channel string
		want             bool
	}{
		{"orders", "orders", true},
		{"orders", "orders.created", false},
		{"orders.*", "orders.created", true},
		{"orders.*", "orders.eu.created", true},
		{"orders.*", "orders.", true},
		{"orders.*", "orders", false},
		{"orders.*", "ordersx", false},
		{"*", "", true},
		{"*", "anything.at.all", true},
		{"*.created", "orders.created", true},
		{"*.created", "orders.updated", false},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
		{"orders.*", "orders.>", true},
		{"orders.*", ">", false},
		{"", "", true},
		{"", "a", false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, MatchChannel(tt.pattern, tt.channel), "%q vs %q", tt.pattern, tt.channel)
	}
}

const rules = `
- subject: orders-service
  channels: ["orders.*"]
  types: [queues, events]
  actions: [send, receive]
- subject: auditor
  channels: ["*"]
  actions: [receive]
- subject: ops
  channels: ["*"]
  actions: ["*"]
- subject: "*"
  channels: ["public.*"]
  actions: [send]
`

func TestAuthorizer(t *testing.T) {
	a, err := New(&config.AuthorizationConfig{Enable: true, PolicyData: rules})
	require.NoError(t, err)

	tests := []struct {
		subject string
		check   Check
		want    bool
	}{
		{"orders-service", Check{Send, "queues", "orders.new"}, true},
		{"orders-service", Check{Receive, "events", "orders.new"}, true},
		{"orders-service", Check{Send, "commands", "orders.new"}, false},
		{"orders-service", Check{Send, "queues", "billing.new"}, false},
		{"orders-service", Check{Manage, "queues", "orders.new"}, false},
		{"orders-service", Check{Action: Admin}, false},
		{"auditor", Check{Receive, "events_store", "anything"}, true},
		{"auditor", Check{Send, "events_store", "anything"}, false},
		{"ops", Check{Manage, "queues", "x"}, true},
		{"ops", Check{Action: Admin}, true},
		{"stranger", Check{Send, "events", "public.chat"}, true},
		{"stranger", Check{Receive, "events", "public.chat"}, false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, a.Allowed(tt.subject, tt.check), "%s: %s", tt.subject, tt.check)
	}

	id := &authentication.Identity{Subject: "orders-service"}
	require.NoError(t, a.Authorize(id, Check{Send, "queues", "orders.a"}, Check{Send, "queues", "orders.b"}))
	err = a.Authorize(id, Check{Send, "queues", "orders.a"}, Check{Send, "queues", "billing.b"})
	require.True(t, errors.Is(err, ErrPermissionDenied))
	require.Contains(t, err.Error(), "billing.b")
	require.Error(t, a.Authorize(nil, Check{Send, "queues", "orders.a"}))
	require.NoError(t, a.Authorize(nil))
}

func TestNew(t *testing.T) {
	a, err := New(&config.AuthorizationConfig{Enable: false, PolicyData: rules})
	require.NoError(t, err)
	require.Nil(t, a)

	_, err = New(&config.AuthorizationConfig{Enable: true, PolicyData: "subject: x"})
	require.Error(t, err)

	path := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`[{"subject":"a","channels":["x"],"actions":["send"]}]`), 0o600))
	a, err = New(&config.AuthorizationConfig{Enable: true, FilePath: path, AutoReload: 1})
	require.NoError(t, err)
	require.True(t, a.Allowed("a", Check{Send, "events", "x"}))
	require.False(t, a.Allowed("a", Check{Send, "events", "y"}))

	// A changed file takes effect after the reload interval.
	require.NoError(t, os.WriteFile(path, []byte(`[{"subject":"a","channels":["x","y"],"actions":["send"]}]`), 0o600))
	require.Eventually(t, func() bool { return a.Allowed("a", Check{Send, "events", "y"}) }, 3*time.Second, 100*time.Millisecond)
}

func TestChecksFor(t *testing.T) {
	tests := []struct {
		name string
		msg  any
		want []Check
	}{
		{"event", &pb.Event{Channel: "e"}, []Check{{Send, "events", "e"}}},
		{"event store", &pb.Event{Channel: "e", Store: true}, []Check{{Send, "events_store", "e"}}},
		{"subscribe events", &pb.Subscribe{SubscribeTypeData: pb.Subscribe_Events, Channel: "e.>"}, []Check{{Receive, "events", "e.>"}}},
		{"subscribe queries", &pb.Subscribe{SubscribeTypeData: pb.Subscribe_Queries, Channel: "q"}, []Check{{Receive, "queries", "q"}}},
		{"command", &pb.Request{RequestTypeData: pb.Request_Command, Channel: "c"}, []Check{{Send, "commands", "c"}}},
		{"create channel", &pb.Request{RequestTypeData: pb.Request_Query, Channel: entities.ClusterInternalRequestsChannel, Metadata: "create-channel", Tags: map[string]string{"channel_type": "queues", "channel": "q"}}, []Check{{Manage, "queues", "q"}}},
		{"list channels", &pb.Request{RequestTypeData: pb.Request_Query, Channel: entities.ClusterInternalRequestsChannel, Metadata: "list-channels"}, nil},
		{"response", &pb.Response{}, nil},
		{"queue message with dead letter", &pb.QueueMessage{Channel: "q", Policy: &pb.QueueMessagePolicy{MaxReceiveQueue: "dlq"}}, []Check{{Send, "queues", "q"}, {Send, "queues", "dlq"}}},
		{"batch", &pb.QueueMessagesBatchRequest{Messages: []*pb.QueueMessage{{Channel: "a"}, {Channel: "b"}, {Channel: "a"}}}, []Check{{Send, "queues", "a"}, {Send, "queues", "b"}}},
		{"upstream", &pb.QueuesUpstreamRequest{Messages: []*pb.QueueMessage{{Channel: "a"}}}, []Check{{Send, "queues", "a"}}},
		{"receive", &pb.ReceiveQueueMessagesRequest{Channel: "q"}, []Check{{Receive, "queues", "q"}}},
		{"ack all", &pb.AckAllQueueMessagesRequest{Channel: "q"}, []Check{{Receive, "queues", "q"}}},
		{"stream receive", &pb.StreamQueueMessagesRequest{StreamRequestTypeData: pb.StreamRequestType_ReceiveMessage, Channel: "q"}, []Check{{Receive, "queues", "q"}}},
		{"stream ack", &pb.StreamQueueMessagesRequest{StreamRequestTypeData: pb.StreamRequestType_AckMessage}, nil},
		{"stream resend", &pb.StreamQueueMessagesRequest{StreamRequestTypeData: pb.StreamRequestType_ResendMessage, Channel: "other"}, []Check{{Send, "queues", "other"}}},
		{"stream send modified", &pb.StreamQueueMessagesRequest{StreamRequestTypeData: pb.StreamRequestType_SendModifiedMessage, ModifiedMessage: &pb.QueueMessage{Channel: "other"}}, []Check{{Send, "queues", "other"}}},
		{"downstream get", &pb.QueuesDownstreamRequest{RequestTypeData: pb.QueuesDownstreamRequestType_Get, Channel: "q"}, []Check{{Receive, "queues", "q"}}},
		{"downstream requeue", &pb.QueuesDownstreamRequest{RequestTypeData: pb.QueuesDownstreamRequestType_ReQueueAll, ReQueueChannel: "r"}, []Check{{Send, "queues", "r"}}},
		{"downstream ack", &pb.QueuesDownstreamRequest{RequestTypeData: pb.QueuesDownstreamRequestType_AckAll}, nil},
		{"queues info", &pb.QueuesInfoRequest{QueueName: "q"}, []Check{{Manage, "queues", "q"}}},
		{"ping", &pb.Empty{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ChecksFor(tt.msg))
		})
	}
}
