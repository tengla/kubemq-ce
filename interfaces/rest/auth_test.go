package rest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gorilla/websocket"
	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	sdk "github.com/kubemq-io/kubemq-go"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/require"
)

func TestRestServer_Authentication(t *testing.T) {
	defer leaktest.Check(t)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	appConfig := getAppConfig()
	appConfig.Authentication = &config.AuthenticationConfig{
		Enable:    true,
		JwtConfig: &config.JWTAuthenticationConfig{Key: "rest-test-secret-with-enough-length", SignatureType: "HS256"},
	}
	port, svc, s := setup(ctx, t, appConfig)
	defer tearDown(svc, s)
	base := fmt.Sprintf("http://localhost:%d", port)
	token, err := authentication.Sign(appConfig.Authentication.JwtConfig, nil, "test", time.Minute)
	require.NoError(t, err)

	request := func(method, path, authHeader string) int {
		req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewBufferString(`{"Channel":"auth.events","ClientID":"auth-test","Body":"aGk="}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		return res.StatusCode
	}

	require.Equal(t, http.StatusOK, request(http.MethodGet, "/ping", ""))
	require.Equal(t, http.StatusUnauthorized, request(http.MethodPost, "/send/event", ""))
	require.Equal(t, http.StatusUnauthorized, request(http.MethodPost, "/send/event", "Bearer not-a-jwt"))
	require.Equal(t, http.StatusOK, request(http.MethodPost, "/send/event", "Bearer "+token))
	require.Equal(t, http.StatusOK, request(http.MethodPost, "/send/event", token))

	// Websocket upgrades go through the same check.
	wsURI := fmt.Sprintf("ws://localhost:%d/subscribe/events?client_id=auth-test&channel=auth.events&subscribe_type=events", port)
	_, res, err := websocket.DefaultDialer.Dial(wsURI, nil)
	require.Error(t, err)
	require.NotNil(t, res)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)

	// The Go SDK's REST transport sends "Bearer <token>".
	client, err := sdk.NewClient(ctx, sdk.WithUri(base), sdk.WithClientId(nuid.Next()), sdk.WithTransportType(sdk.TransportTypeRest), sdk.WithAuthToken(token))
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	channel := nuid.Next()
	sent, err := client.QM().SetChannel(channel).SetBody([]byte("job")).Send(ctx)
	require.NoError(t, err)
	require.False(t, sent.IsError, sent.Error)
	received, err := client.RQM().SetChannel(channel).SetMaxNumberOfMessages(1).SetWaitTimeSeconds(2).Send(ctx)
	require.NoError(t, err)
	require.Len(t, received.Messages, 1)
}
