package middleware

import (
	"context"

	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Ping stays open so clients and health checks can test connectivity
// before they have a token.
const pingMethod = "/kubemq.kubemq/Ping"

func authenticate(ctx context.Context, auth *authentication.Authenticator, method string) error {
	if method == pingMethod {
		return nil
	}
	var token string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("authorization"); len(values) > 0 {
			token = values[0]
		}
	}
	if err := auth.Verify(token); err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	return nil
}

func UnaryAuthServerInterceptor(auth *authentication.Authenticator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if err := authenticate(ctx, auth, info.FullMethod); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func StreamAuthServerInterceptor(auth *authentication.Authenticator) grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authenticate(stream.Context(), auth, info.FullMethod); err != nil {
			return err
		}
		return handler(srv, stream)
	}
}
