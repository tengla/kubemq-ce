package middleware

import (
	"context"
	"errors"

	"github.com/kubemq-io/kubemq-community/pkg/authorization"
	"github.com/kubemq-io/kubemq-community/pkg/guard"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Ping stays open so clients and health checks can test connectivity
// before they have a token.
const pingMethod = "/kubemq.kubemq/Ping"

func guardRequest(ctx context.Context, method string) guard.Request {
	req := guard.Request{Transport: "grpc", Operation: method}
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		req.Remote = p.Addr.String()
	}
	return req
}

func authenticate(ctx context.Context, g *guard.Guard, req guard.Request) (context.Context, error) {
	var token string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("authorization"); len(values) > 0 {
			token = values[0]
		}
	}
	ctx, err := g.Authenticate(ctx, token, req)
	if err != nil {
		return ctx, status.Error(codes.Unauthenticated, err.Error())
	}
	return ctx, nil
}

func authorize(ctx context.Context, g *guard.Guard, req guard.Request, msg any, seen map[authorization.Check]bool) error {
	err := g.AuthorizeMessage(ctx, req, msg, seen)
	if errors.Is(err, authorization.ErrPermissionDenied) {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	return err
}

// UnaryAuthServerInterceptor authenticates each call and checks what its
// request does.
func UnaryAuthServerInterceptor(g *guard.Guard) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if info.FullMethod == pingMethod {
			return handler(ctx, req)
		}
		gr := guardRequest(ctx, info.FullMethod)
		ctx, err := authenticate(ctx, g, gr)
		if err != nil {
			return nil, err
		}
		if err := authorize(ctx, g, gr, req, nil); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamAuthServerInterceptor authenticates each stream once and checks
// every message the client sends on it.
func StreamAuthServerInterceptor(g *guard.Guard) grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		gr := guardRequest(stream.Context(), info.FullMethod)
		ctx, err := authenticate(stream.Context(), g, gr)
		if err != nil {
			return err
		}
		return handler(srv, &guardedStream{ServerStream: stream, ctx: ctx, guard: g, req: gr, seen: map[authorization.Check]bool{}})
	}
}

type guardedStream struct {
	grpc.ServerStream
	ctx   context.Context
	guard *guard.Guard
	req   guard.Request
	// seen is only used from RecvMsg, which gRPC never calls concurrently on
	// one stream.
	seen map[authorization.Check]bool
}

func (s *guardedStream) Context() context.Context {
	return s.ctx
}

func (s *guardedStream) RecvMsg(m interface{}) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	return authorize(s.ctx, s.guard, s.req, m, s.seen)
}
