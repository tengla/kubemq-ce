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

	token, err := authentication.Sign(appConfig.Authentication.JwtConfig, nil, "test", time.Minute)
	require.NoError(t, err)
	badToken, err := authentication.Sign(&config.JWTAuthenticationConfig{Key: "a-different-secret", SignatureType: "HS256"}, nil, "test", time.Minute)
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
