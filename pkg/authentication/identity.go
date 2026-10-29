package authentication

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// Identity is who made a request, taken from its verified token.
type Identity struct {
	Subject string
	TokenID string
}

func NewIdentity(claims *jwt.RegisteredClaims) *Identity {
	return &Identity{Subject: claims.Subject, TokenID: claims.ID}
}

type identityKey struct{}

func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom returns the identity stored by WithIdentity, or nil when the
// request was not authenticated.
func IdentityFrom(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey{}).(*Identity)
	return id
}
