package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gorilla/websocket"
	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/audit"
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
	token, _, err := authentication.Sign(appConfig.Authentication.JwtConfig, nil, "test", time.Minute)
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

func TestRestServer_AuthorizationAndAudit(t *testing.T) {
	defer leaktest.Check(t)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	auditFile := filepath.Join(t.TempDir(), "audit.log")
	appConfig := getAppConfig()
	appConfig.Authentication = &config.AuthenticationConfig{
		Enable:    true,
		JwtConfig: &config.JWTAuthenticationConfig{Key: "rest-test-secret-with-enough-length", SignatureType: "HS256"},
	}
	appConfig.Authorization = &config.AuthorizationConfig{Enable: true, PolicyData: `[{"subject":"orders","channels":["orders.*"],"actions":["send","receive"]}]`}
	appConfig.Audit = &config.AuditConfig{Enable: true, FilePath: auditFile}
	port, svc, s := setup(ctx, t, appConfig)
	defer tearDown(svc, s)
	base := fmt.Sprintf("http://localhost:%d", port)
	token, tokenID, err := authentication.Sign(appConfig.Authentication.JwtConfig, nil, "orders", time.Minute)
	require.NoError(t, err)

	post := func(path, body string) int {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		return res.StatusCode
	}
	require.Equal(t, http.StatusOK, post("/queue/send", `{"Channel":"orders.jobs","ClientID":"rest-client","Body":"aGk="}`))
	require.Equal(t, http.StatusForbidden, post("/queue/send", `{"Channel":"billing.jobs","ClientID":"rest-client","Body":"aGk="}`))
	require.Equal(t, http.StatusForbidden, post("/queue/send_batch", `{"Messages":[{"Channel":"orders.jobs","ClientID":"c","Body":"aGk="},{"Channel":"billing.jobs","ClientID":"c","Body":"aGk="}]}`))

	wsURI := fmt.Sprintf("ws://localhost:%d/subscribe/events?client_id=c&channel=billing.events&subscribe_type=events", port)
	_, res, err := websocket.DefaultDialer.Dial(wsURI, http.Header{"Authorization": []string{token}})
	require.Error(t, err)
	require.NotNil(t, res)
	require.Equal(t, http.StatusForbidden, res.StatusCode)

	data, err := os.ReadFile(auditFile)
	require.NoError(t, err)
	var entries []audit.Entry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry audit.Entry
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		entries = append(entries, entry)
	}
	require.Len(t, entries, 5)
	first := entries[0]
	require.Equal(t, "rest", first.Transport)
	require.Equal(t, "/queue/send", first.Operation)
	require.Equal(t, "orders", first.Subject)
	require.Equal(t, tokenID, first.TokenID)
	require.Equal(t, "rest-client", first.ClientID)
	require.Equal(t, "send", first.Action)
	require.Equal(t, "queues", first.Type)
	require.Equal(t, "orders.jobs", first.Channel)
	require.Equal(t, audit.Allowed, first.Outcome)
	require.Equal(t, audit.Denied, entries[1].Outcome)
	require.Equal(t, "billing.jobs", entries[1].Channel)
	// The batch logs each channel it sends to.
	require.Equal(t, []string{audit.Allowed, audit.Denied}, []string{entries[2].Outcome, entries[3].Outcome})
	require.Equal(t, "/subscribe/events", entries[4].Operation)
	require.Equal(t, "receive", entries[4].Action)
	require.Equal(t, audit.Denied, entries[4].Outcome)
}
