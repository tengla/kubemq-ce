package tracing

import (
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
)

// pingMethod gets no spans: health checks would drown out real traffic.
const pingMethod = "/kubemq.kubemq/Ping"

// GRPCServerOption adds a server span to every RPC except Ping, continuing
// the trace context a client sends in its metadata.
func GRPCServerOption() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool {
		return info.FullMethodName != pingMethod
	})))
}

// EchoMiddleware adds a server span to every REST request, continuing the
// trace context in its traceparent header. Websocket connections and /ping
// get none, since a span lasting a whole subscription says little; messages
// sent over a websocket still get their own send spans.
func EchoMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			r := c.Request()
			if websocket.IsWebSocketUpgrade(r) || r.URL.Path == "/ping" || r.URL.Path == "/" {
				return next(c)
			}
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer().Start(ctx, r.Method+" "+c.Path(),
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.request.method", r.Method),
					attribute.String("http.route", c.Path()),
					attribute.String("url.path", r.URL.Path),
					attribute.String("client.address", c.RealIP()),
				),
			)
			defer span.End()
			c.SetRequest(r.WithContext(ctx))
			err := next(c)
			if err != nil {
				span.RecordError(err)
			}
			status := c.Response().Status
			span.SetAttributes(attribute.Int("http.response.status_code", status))
			if status >= http.StatusInternalServerError || err != nil {
				span.SetStatus(codes.Error, http.StatusText(status))
			}
			return err
		}
	}
}
