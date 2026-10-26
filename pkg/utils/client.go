package utils

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-resty/resty/v2"
	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/uuid"
	"github.com/kubemq-io/kubemq-go"
	"time"
)

func GetKubeMQClient(ctx context.Context, cfg *config.Config) (*kubemq.Client, error) {
	clientID := cfg.Client.ClientID
	if clientID == "" {
		clientID = uuid.New()
	}
	return kubemq.NewClient(ctx,
		kubemq.WithAddress(cfg.Client.GrpcHost, cfg.Client.GrpcPort),
		kubemq.WithClientId(clientID),
		kubemq.WithTransportType(kubemq.TransportTypeGRPC),
		kubemq.WithAutoReconnect(true),
		kubemq.WithReconnectInterval(time.Second),
		kubemq.WithMaxReconnects(0),
		kubemq.WithAuthToken(cfg.Client.AuthToken))
}

// APIRequest starts a request to the API server (client.apiAddress), with
// client.authToken as a bearer token when it is set.
func APIRequest(cfg *config.Config) *resty.Request {
	r := resty.New().R()
	if cfg.Client.AuthToken != "" {
		r.SetAuthToken(cfg.Client.AuthToken)
	}
	return r
}

// APIHeader is the header for websocket connections to the API server.
func APIHeader(cfg *config.Config) http.Header {
	header := http.Header{}
	if cfg.Client.AuthToken != "" {
		header.Set("Authorization", "Bearer "+cfg.Client.AuthToken)
	}
	return header
}

// APIResponseError explains a failed API server response.
func APIResponseError(r *resty.Response) error {
	if r.StatusCode() == http.StatusUnauthorized {
		return fmt.Errorf("the server requires a token: set client.authToken in config.yaml, created with `kubemq token`")
	}
	return fmt.Errorf("not available in current Kubemq version, consider upgrade Kubemq version")
}
