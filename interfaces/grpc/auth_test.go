package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	sdk "github.com/kubemq-io/kubemq-go"
	pb "github.com/kubemq-io/protobuf/go"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testJWTSecret = "grpc-test-secret-with-enough-length"

func authAppConfig() *config.Config {
	appConfig := getAppConfig()
	appConfig.Authentication = &config.AuthenticationConfig{
		Enable:    true,
		JwtConfig: &config.JWTAuthenticationConfig{Key: testJWTSecret, SignatureType: "HS256"},
	}
	return appConfig
}

func TestGRPCServer_Authentication(t *testing.T) {
	defer leaktest.Check(t)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	appConfig, gs, svc := setup(ctx, t, authAppConfig())
	defer tearDown(gs, svc)

	token, _, err := authentication.Sign(appConfig.Authentication.JwtConfig, nil, "test", time.Minute)
	require.NoError(t, err)
	badToken, _, err := authentication.Sign(&config.JWTAuthenticationConfig{Key: "a-different-secret", SignatureType: "HS256"}, nil, "test", time.Minute)
	require.NoError(t, err)

	client := getNewClient(ctx, t, appConfig)
	defer func() { _ = client.Close() }()
	event := &pb.Event{EventID: nuid.Next(), ClientID: "auth-test", Channel: "auth.events", Body: []byte("hi")}
	withToken := func(token string) context.Context {
		return metadata.AppendToOutgoingContext(ctx, "authorization", token)
	}

	t.Run("ping needs no token", func(t *testing.T) {
		_, err := client.Ping(ctx, &pb.Empty{})
		require.NoError(t, err)
	})
	t.Run("unary without token", func(t *testing.T) {
		_, err := client.SendEvent(ctx, event)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})
	t.Run("unary with wrong token", func(t *testing.T) {
		_, err := client.SendEvent(withToken(badToken), event)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})
	t.Run("unary with token", func(t *testing.T) {
		res, err := client.SendEvent(withToken(token), event)
		require.NoError(t, err)
		require.True(t, res.Sent)
	})
	t.Run("stream without token", func(t *testing.T) {
		stream, err := client.SubscribeToEvents(ctx, &pb.Subscribe{SubscribeTypeData: pb.Subscribe_Events, ClientID: "auth-test", Channel: "auth.events"})
		require.NoError(t, err)
		_, err = stream.Recv()
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})
	t.Run("go sdk with token", func(t *testing.T) {
		c, err := sdk.NewClient(ctx, sdk.WithAddress("localhost", appConfig.Grpc.Port), sdk.WithClientId(nuid.Next()), sdk.WithTransportType(sdk.TransportTypeGRPC), sdk.WithAuthToken(token))
		require.NoError(t, err)
		defer func() { _ = c.Close() }()
		res, err := c.SetQueueMessage(c.NewQueueMessage().SetChannel("auth.queue").SetBody([]byte("job"))).Send(ctx)
		require.NoError(t, err)
		require.False(t, res.IsError, res.Error)
	})
}

func TestGRPCServer_Authorization(t *testing.T) {
	defer leaktest.Check(t)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	appConfig := authAppConfig()
	appConfig.Authorization = &config.AuthorizationConfig{Enable: true, PolicyData: `
- subject: orders
  channels: ["orders.*"]
  types: [events, queues]
  actions: [send, receive]
`}
	appConfig, gs, svc := setup(ctx, t, appConfig)
	defer tearDown(gs, svc)

	token, _, err := authentication.Sign(appConfig.Authentication.JwtConfig, nil, "orders", time.Minute)
	require.NoError(t, err)
	client := getNewClient(ctx, t, appConfig)
	defer func() { _ = client.Close() }()
	authCtx := metadata.AppendToOutgoingContext(ctx, "authorization", token)

	t.Run("unary", func(t *testing.T) {
		res, err := client.SendEvent(authCtx, &pb.Event{EventID: nuid.Next(), ClientID: "c", Channel: "orders.new", Body: []byte("hi")})
		require.NoError(t, err)
		require.True(t, res.Sent)
		_, err = client.SendEvent(authCtx, &pb.Event{EventID: nuid.Next(), ClientID: "c", Channel: "billing.new", Body: []byte("hi")})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
		_, err = client.SendEvent(authCtx, &pb.Event{EventID: nuid.Next(), ClientID: "c", Channel: "orders.new", Store: true, Body: []byte("hi")})
		require.Equal(t, codes.PermissionDenied, status.Code(err), "events_store is not in the rule's types")
	})
	t.Run("subscribe", func(t *testing.T) {
		stream, err := client.SubscribeToEvents(authCtx, &pb.Subscribe{SubscribeTypeData: pb.Subscribe_Events, ClientID: "c", Channel: "billing.new"})
		require.NoError(t, err)
		_, err = stream.Recv()
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	})
	t.Run("queue upstream answers a denied request and stays open", func(t *testing.T) {
		streamCtx, streamCancel := context.WithCancel(authCtx)
		defer streamCancel()
		stream, err := client.QueuesUpstream(streamCtx)
		require.NoError(t, err)
		send := func(channel string) *pb.QueuesUpstreamResponse {
			req := &pb.QueuesUpstreamRequest{RequestID: nuid.Next(), Messages: []*pb.QueueMessage{{MessageID: nuid.Next(), ClientID: "c", Channel: channel, Body: []byte("job")}}}
			require.NoError(t, stream.Send(req))
			res, err := stream.Recv()
			require.NoError(t, err)
			require.Equal(t, req.RequestID, res.RefRequestID)
			return res
		}
		res := send("billing.jobs")
		require.True(t, res.IsError)
		require.Contains(t, res.Error, "orders may not send on queues channel \"billing.jobs\"")
		res = send("orders.jobs")
		require.False(t, res.IsError, res.Error)
	})
	t.Run("events stream answers a denied event and stays open", func(t *testing.T) {
		streamCtx, streamCancel := context.WithCancel(authCtx)
		defer streamCancel()
		stream, err := client.SendEventsStream(streamCtx)
		require.NoError(t, err)
		require.NoError(t, stream.Send(&pb.Event{EventID: "denied", ClientID: "c", Channel: "billing.new", Body: []byte("hi")}))
		res, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, "denied", res.EventID)
		require.False(t, res.Sent)
		require.Contains(t, res.Error, "permission denied")
		// The store event fails for a different reason, events_store not
		// being allowed, which proves the stream still processes messages.
		require.NoError(t, stream.Send(&pb.Event{EventID: "store", ClientID: "c", Channel: "orders.new", Store: true, Body: []byte("hi")}))
		res, err = stream.Recv()
		require.NoError(t, err)
		require.Equal(t, "store", res.EventID)
		require.False(t, res.Sent)
	})
}
