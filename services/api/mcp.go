package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/api"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"github.com/kubemq-io/kubemq-community/pkg/guard"
	"github.com/kubemq-io/kubemq-community/services/metrics"
	pb "github.com/kubemq-io/protobuf/go"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nats-io/nuid"
)

// The MCP (Model Context Protocol) server lets AI agents list channels, read
// statistics, send messages and peek at queues. It is served at /mcp on the
// API port with the stateless streamable HTTP transport.
//
// With authentication on, agents send a token from `kubemq token` as a
// Bearer header. With authorization on, each tool call needs the permission
// the matching client request would: send for send_message, receive for
// peek_queue, and manage for the channels list_channels and get_stats show.

const mcpInstructions = `This server is a KubeMQ message broker. Channels have one of five types:
events (fire-and-forget pub/sub), events_store (persistent pub/sub), commands and queries
(request/reply; a subscriber must answer within the timeout) and queues (persistent,
pulled by consumers). Use list_channels to find channels, get_stats for broker health,
peek_queue to look at waiting queue messages without consuming them, and send_message to publish.`

const (
	mcpClientID          = "kubemq-mcp"
	mcpMaxPeekMessages   = 100
	mcpMaxRequestTimeout = 60
)

type mcpTools struct {
	svc   *service
	guard *guard.Guard
}

func (s *Server) mcpHandler() http.Handler {
	t := &mcpTools{svc: s.apiService, guard: s.guard}
	server := mcp.NewServer(&mcp.Implementation{Name: "kubemq", Version: appConfig.GetVersion()}, &mcp.ServerOptions{Instructions: mcpInstructions})
	notDestructive := false
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_channels",
		Description: "List channels with their message counts, volume in bytes and last activity (unix ms). For queues, outgoing.waiting is the number of messages waiting.",
		Annotations: &mcp.ToolAnnotations{Title: "List channels", ReadOnlyHint: true},
	}, t.listChannels)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stats",
		Description: "Broker health and totals per channel type: host, version, uptime, CPU and memory use, active clients, and channel and message counts.",
		Annotations: &mcp.ToolAnnotations{Title: "Get broker stats", ReadOnlyHint: true},
	}, t.getStats)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_message",
		Description: "Send a message to a channel. For commands and queries this waits for a subscriber's reply and returns it.",
		Annotations: &mcp.ToolAnnotations{Title: "Send a message", DestructiveHint: &notDestructive},
	}, t.sendMessage)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "peek_queue",
		Description: "Look at messages waiting in a queue without consuming them.",
		Annotations: &mcp.ToolAnnotations{Title: "Peek at a queue", ReadOnlyHint: true},
	}, t.peekQueue)

	var h http.Handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	if s.auth != nil {
		h = auth.RequireBearerToken(s.verifyMCPToken, &auth.RequireBearerTokenOptions{
			// Tokens from `kubemq token` may have no expiry.
			AllowMissingExpiration: true,
		})(h)
	}
	// The token travels in a header, but reject cross-origin browser
	// requests anyway, as the MCP spec advises.
	return http.NewCrossOriginProtection().Handler(h)
}

// verifyMCPToken passes the caller's identity to the tools in TokenInfo,
// which the SDK hands to each tool call.
func (s *Server) verifyMCPToken(ctx context.Context, token string, r *http.Request) (*auth.TokenInfo, error) {
	ctx, err := s.guard.Authenticate(ctx, token, guard.Request{Transport: "mcp", Operation: r.URL.Path, Remote: r.RemoteAddr})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", auth.ErrInvalidToken, err.Error())
	}
	id := authentication.IdentityFrom(ctx)
	return &auth.TokenInfo{UserID: id.Subject, Extra: map[string]any{"identity": id}}, nil
}

// call returns ctx with the caller's identity, the audit description of a
// tool call and the client ID its messages are sent with.
func (t *mcpTools) call(ctx context.Context, req *mcp.CallToolRequest, tool string) (context.Context, guard.Request, string, error) {
	if !t.svc.ready.Load() {
		return ctx, guard.Request{}, "", fmt.Errorf("the broker is starting, try again shortly")
	}
	clientID := mcpClientID
	if info := req.Extra.TokenInfo; info != nil {
		if id, ok := info.Extra["identity"].(*authentication.Identity); ok {
			ctx = authentication.WithIdentity(ctx, id)
			if id.Subject != "" {
				clientID = id.Subject
			}
		}
	}
	return ctx, guard.Request{Transport: "mcp", Operation: "tools/" + tool}, clientID, nil
}

func (t *mcpTools) authorize(ctx context.Context, req guard.Request, msg any) error {
	if t.guard == nil {
		return nil
	}
	return t.guard.AuthorizeMessage(ctx, req, msg, nil)
}

type listChannelsInput struct {
	Type   string `json:"type,omitempty" jsonschema:"one of events, events_store, commands, queries or queues; empty lists every type"`
	Search string `json:"search,omitempty" jsonschema:"only list channels whose name contains this text"`
}

type listChannelsOutput struct {
	Channels []*api.ChannelInfo `json:"channels"`
}

func (t *mcpTools) listChannels(ctx context.Context, req *mcp.CallToolRequest, in listChannelsInput) (*mcp.CallToolResult, listChannelsOutput, error) {
	out := listChannelsOutput{Channels: []*api.ChannelInfo{}}
	ctx, _, _, err := t.call(ctx, req, "list_channels")
	if err != nil {
		return nil, out, err
	}
	types := config.ChannelTypes
	if in.Type != "" {
		if err := validChannelType(in.Type); err != nil {
			return nil, out, err
		}
		types = []string{in.Type}
	}
	ss, err := t.svc.getCurrentSnapshot(ctx)
	if err != nil {
		return nil, out, err
	}
	for _, channelType := range types {
		out.Channels = append(out.Channels, t.svc.channelInfos(ctx, ss, channelType, in.Search)...)
	}
	return nil, out, nil
}

type typeStats struct {
	Channels       int   `json:"channels"`
	ActiveChannels int   `json:"active_channels"`
	MessagesIn     int64 `json:"messages_in"`
	MessagesOut    int64 `json:"messages_out"`
	VolumeIn       int64 `json:"volume_in_bytes"`
	VolumeOut      int64 `json:"volume_out_bytes"`
	Errors         int64 `json:"errors"`
	Waiting        int64 `json:"waiting"`
}

type statsOutput struct {
	Host           string                `json:"host"`
	Version        string                `json:"version"`
	UptimeSeconds  int64                 `json:"uptime_seconds"`
	CPUPercent     float64               `json:"cpu_percent"`
	MemoryBytes    int64                 `json:"memory_bytes"`
	Goroutines     int64                 `json:"goroutines"`
	ActiveClients  int                   `json:"active_clients"`
	ChannelsByType map[string]*typeStats `json:"channels_by_type"`
}

func (t *mcpTools) getStats(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, statsOutput, error) {
	out := statsOutput{ChannelsByType: map[string]*typeStats{}}
	ctx, _, _, err := t.call(ctx, req, "get_stats")
	if err != nil {
		return nil, out, err
	}
	ss, err := t.svc.getCurrentSnapshot(ctx)
	if err != nil {
		return nil, out, err
	}
	if sys := ss.System; sys != nil {
		out.Host = sys.Hostname
		out.Version = sys.Version
		out.UptimeSeconds = int64(sys.Uptime)
		out.CPUPercent = sys.CPUUtilization
		out.MemoryBytes = int64(sys.ProcessMemory)
		out.Goroutines = sys.GoRoutines
		out.ActiveClients = sys.ActiveClients
	}
	// Totals cover the channels the caller may see in list_channels.
	for _, channelType := range config.ChannelTypes {
		ts := &typeStats{}
		for _, c := range t.svc.channelInfos(ctx, ss, channelType, "") {
			ts.Channels++
			if c.IsActive {
				ts.ActiveChannels++
			}
			ts.MessagesIn += c.Incoming.Messages
			ts.MessagesOut += c.Outgoing.Messages
			ts.VolumeIn += c.Incoming.Volume
			ts.VolumeOut += c.Outgoing.Volume
			ts.Errors += c.Incoming.Errors + c.Outgoing.Errors
			ts.Waiting += c.Outgoing.Waiting
		}
		out.ChannelsByType[channelType] = ts
	}
	return nil, out, nil
}

type sendMessageInput struct {
	Type           string            `json:"type" jsonschema:"one of events, events_store, commands, queries or queues"`
	Channel        string            `json:"channel" jsonschema:"channel name"`
	Body           string            `json:"body,omitempty" jsonschema:"message body as text"`
	Metadata       string            `json:"metadata,omitempty" jsonschema:"optional metadata string"`
	Tags           map[string]string `json:"tags,omitempty" jsonschema:"optional key/value tags"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty" jsonschema:"commands and queries: how long to wait for a reply, default 10, at most 60"`
}

type sendMessageOutput struct {
	ID   string `json:"id"`
	Sent bool   `json:"sent"`
	// Executed and Response are set for commands and queries.
	Executed *bool       `json:"executed,omitempty"`
	Response *mcpMessage `json:"response,omitempty"`
	Error    string      `json:"error,omitempty"`
}

func (t *mcpTools) sendMessage(ctx context.Context, req *mcp.CallToolRequest, in sendMessageInput) (*mcp.CallToolResult, sendMessageOutput, error) {
	var out sendMessageOutput
	ctx, gr, clientID, err := t.call(ctx, req, "send_message")
	if err != nil {
		return nil, out, err
	}
	if err := validChannelType(in.Type); err != nil {
		return nil, out, err
	}
	if in.Channel == "" {
		return nil, out, fmt.Errorf("channel not set")
	}
	array := t.svc.array
	out.ID = nuid.Next()
	switch in.Type {
	case "events", "events_store":
		msg := &pb.Event{EventID: out.ID, ClientID: clientID, Channel: in.Channel, Metadata: in.Metadata, Body: []byte(in.Body), Tags: in.Tags, Store: in.Type == "events_store"}
		if err := t.authorize(ctx, gr, msg); err != nil {
			return nil, out, err
		}
		var res *pb.Result
		if msg.Store {
			res, err = array.SendEventsStore(ctx, msg)
		} else {
			res, err = array.SendEvents(ctx, msg)
		}
		if err != nil {
			return nil, out, err
		}
		out.Sent, out.Error = res.Sent, res.Error
	case "queues":
		msg := &pb.QueueMessage{MessageID: out.ID, ClientID: clientID, Channel: in.Channel, Metadata: in.Metadata, Body: []byte(in.Body), Tags: in.Tags}
		if err := t.authorize(ctx, gr, msg); err != nil {
			return nil, out, err
		}
		res, err := array.SendQueueMessage(ctx, msg)
		if err != nil {
			return nil, out, err
		}
		out.Sent, out.Error = !res.IsError, res.Error
	case "commands", "queries":
		timeout := in.TimeoutSeconds
		if timeout <= 0 {
			timeout = 10
		}
		timeout = min(timeout, mcpMaxRequestTimeout)
		msg := &pb.Request{RequestID: out.ID, ClientID: clientID, Channel: in.Channel, Metadata: in.Metadata, Body: []byte(in.Body), Tags: in.Tags, Timeout: int32(timeout * 1000)}
		if in.Type == "commands" {
			msg.RequestTypeData = pb.Request_Command
		} else {
			msg.RequestTypeData = pb.Request_Query
		}
		if err := t.authorize(ctx, gr, msg); err != nil {
			return nil, out, err
		}
		var res *pb.Response
		if in.Type == "commands" {
			res, err = array.SendCommand(ctx, msg)
		} else {
			res, err = array.SendQuery(ctx, msg)
		}
		metrics.ReportRequest(msg, res, err)
		if err != nil {
			return nil, out, err
		}
		out.Sent = true
		out.Executed = &res.Executed
		out.Error = res.Error
		if in.Type == "queries" {
			out.Response = &mcpMessage{Metadata: res.Metadata, Tags: res.Tags}
			out.Response.setBody(res.Body)
		}
	}
	return nil, out, nil
}

type peekQueueInput struct {
	Channel     string `json:"channel" jsonschema:"queue name"`
	MaxMessages int    `json:"max_messages,omitempty" jsonschema:"how many messages to return, default 10, at most 100"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"how long to wait for messages to arrive, default 1"`
}

type peekQueueOutput struct {
	Messages []*mcpMessage `json:"messages"`
}

// mcpMessage is a message as an agent sees it. A body that is not UTF-8 text
// is base64 encoded, which BodyEncoding says.
type mcpMessage struct {
	ID           string            `json:"id,omitempty"`
	Channel      string            `json:"channel,omitempty"`
	ClientID     string            `json:"client_id,omitempty"`
	Metadata     string            `json:"metadata,omitempty"`
	Body         string            `json:"body"`
	BodyEncoding string            `json:"body_encoding,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	SentAt       string            `json:"sent_at,omitempty"`
	Sequence     uint64            `json:"sequence,omitempty"`
	ReceiveCount int32             `json:"receive_count,omitempty"`
}

func (m *mcpMessage) setBody(body []byte) {
	if utf8.Valid(body) {
		m.Body = string(body)
		return
	}
	m.Body = base64.StdEncoding.EncodeToString(body)
	m.BodyEncoding = "base64"
}

func (t *mcpTools) peekQueue(ctx context.Context, req *mcp.CallToolRequest, in peekQueueInput) (*mcp.CallToolResult, peekQueueOutput, error) {
	out := peekQueueOutput{Messages: []*mcpMessage{}}
	ctx, gr, clientID, err := t.call(ctx, req, "peek_queue")
	if err != nil {
		return nil, out, err
	}
	maxMessages := in.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 10
	}
	maxMessages = min(maxMessages, mcpMaxPeekMessages)
	wait := in.WaitSeconds
	if wait <= 0 {
		wait = 1
	}
	request := &pb.ReceiveQueueMessagesRequest{
		RequestID:           nuid.Next(),
		ClientID:            clientID,
		Channel:             in.Channel,
		MaxNumberOfMessages: int32(maxMessages),
		WaitTimeSeconds:     int32(wait),
		IsPeak:              true,
	}
	if err := t.authorize(ctx, gr, request); err != nil {
		return nil, out, err
	}
	res, err := t.svc.array.ReceiveQueueMessages(ctx, request)
	if err != nil {
		return nil, out, err
	}
	if res.IsError {
		return nil, out, fmt.Errorf("%s", res.Error)
	}
	for _, msg := range res.Messages {
		m := &mcpMessage{ID: msg.MessageID, Channel: msg.Channel, ClientID: msg.ClientID, Metadata: msg.Metadata, Tags: msg.Tags}
		m.setBody(msg.Body)
		if attrs := msg.Attributes; attrs != nil {
			m.Sequence = attrs.Sequence
			m.ReceiveCount = attrs.ReceiveCount
			if attrs.Timestamp > 0 {
				m.SentAt = time.Unix(0, attrs.Timestamp).UTC().Format(time.RFC3339Nano)
			}
		}
		out.Messages = append(out.Messages, m)
	}
	return nil, out, nil
}
