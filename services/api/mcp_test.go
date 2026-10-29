package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/kubemq-io/broker/pkg/pipe"
	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"github.com/kubemq-io/kubemq-community/services"
	pb "github.com/kubemq-io/protobuf/go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/require"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// bearer adds a token to every request of an MCP client.
type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestMCP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A default config, not GetCopyAppConfig, which would write a
	// config.yaml into this directory.
	appConfig := config.GetDefaultConfig()
	appConfig.Host = "mcp-test"
	appConfig.Api.Port = freePort(t)
	appConfig.Broker.MonitoringPort = freePort(t)
	appConfig.Broker.MemoryPipe = pipe.NewPipe(nuid.Next())
	appConfig.Store.StorePath = t.TempDir()
	appConfig.Authentication = &config.AuthenticationConfig{
		Enable:    true,
		JwtConfig: &config.JWTAuthenticationConfig{Key: "mcp-test-secret-with-enough-length", SignatureType: "HS256"},
	}
	appConfig.Authorization = &config.AuthorizationConfig{Enable: true, PolicyData: `
- subject: agent
  channels: ["agent.*"]
  actions: [send, receive, manage]
`}
	svc, err := services.Start(ctx, appConfig)
	require.NoError(t, err)
	defer func() {
		svc.Close()
		<-svc.Stopped
	}()
	// A queue the agent may not see.
	res, err := svc.Array.SendQueueMessage(ctx, &pb.QueueMessage{MessageID: nuid.Next(), ClientID: "other", Channel: "other.q", Body: []byte("secret")})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Error)

	endpoint := fmt.Sprintf("http://localhost:%d/mcp", appConfig.Api.Port)
	connect := func(token string) (*mcp.ClientSession, error) {
		client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
		return client.Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:   endpoint,
			HTTPClient: &http.Client{Transport: bearer{token}, Timeout: 30 * time.Second},
			MaxRetries: -1,
		}, nil)
	}

	_, err = connect("not-a-jwt")
	require.Error(t, err)

	token, _, err := authentication.Sign(appConfig.Authentication.JwtConfig, nil, "agent", time.Hour)
	require.NoError(t, err)
	session, err := connect(token)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	require.ElementsMatch(t, []string{"list_channels", "get_stats", "send_message", "peek_queue"}, names)

	call := func(name string, args map[string]any, out any) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		require.NoError(t, err)
		if out != nil && !result.IsError {
			data, err := json.Marshal(result.StructuredContent)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(data, out))
		}
		return result
	}
	errorText := func(result *mcp.CallToolResult) string {
		require.True(t, result.IsError)
		return result.Content[0].(*mcp.TextContent).Text
	}

	var sent struct {
		ID   string `json:"id"`
		Sent bool   `json:"sent"`
	}
	require.False(t, call("send_message", map[string]any{"type": "queues", "channel": "agent.q", "body": "hello", "tags": map[string]string{"k": "v"}}, &sent).IsError)
	require.True(t, sent.Sent)
	require.NotEmpty(t, sent.ID)

	require.Contains(t, errorText(call("send_message", map[string]any{"type": "queues", "channel": "other.q", "body": "x"}, nil)), "agent may not send")
	require.Contains(t, errorText(call("send_message", map[string]any{"type": "topics", "channel": "agent.q"}, nil)), "unsupported channel type")
	require.Contains(t, errorText(call("peek_queue", map[string]any{"channel": "other.q"}, nil)), "agent may not receive")

	var peeked struct {
		Messages []struct {
			ID       string            `json:"id"`
			Body     string            `json:"body"`
			ClientID string            `json:"client_id"`
			Tags     map[string]string `json:"tags"`
		} `json:"messages"`
	}
	call("peek_queue", map[string]any{"channel": "agent.q"}, &peeked)
	require.Len(t, peeked.Messages, 1)
	require.Equal(t, "hello", peeked.Messages[0].Body)
	require.Equal(t, sent.ID, peeked.Messages[0].ID)
	require.Equal(t, "agent", peeked.Messages[0].ClientID)
	require.Equal(t, "v", peeked.Messages[0].Tags["k"])
	// Peeking leaves the message in the queue.
	call("peek_queue", map[string]any{"channel": "agent.q"}, &peeked)
	require.Len(t, peeked.Messages, 1)

	var listed struct {
		Channels []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"channels"`
	}
	require.Eventually(t, func() bool {
		call("list_channels", map[string]any{"type": "queues"}, &listed)
		return len(listed.Channels) > 0
	}, 5*time.Second, 200*time.Millisecond)
	require.Len(t, listed.Channels, 1, "other.q is not the agent's to see")
	require.Equal(t, "agent.q", listed.Channels[0].Name)

	var stats struct {
		Version        string `json:"version"`
		ChannelsByType map[string]struct {
			Channels int `json:"channels"`
		} `json:"channels_by_type"`
	}
	call("get_stats", nil, &stats)
	require.Equal(t, 1, stats.ChannelsByType["queues"].Channels)
	require.Contains(t, stats.ChannelsByType, "events")

	// A query with nobody answering times out within the requested time.
	result := call("send_message", map[string]any{"type": "queries", "channel": "agent.ask", "body": "?", "timeout_seconds": 1}, nil)
	require.True(t, result.IsError)
}
