// Package guard puts authentication, authorization and the audit log behind
// one check that the gRPC, REST and dashboard servers share.
package guard

import (
	"context"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/audit"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"github.com/kubemq-io/kubemq-community/pkg/authorization"
)

type Guard struct {
	auth  *authentication.Authenticator
	authz *authorization.Authorizer
	audit *audit.Logger
}

// Request says where a request came from, for the audit log.
type Request struct {
	// Transport is grpc, rest or api.
	Transport string
	// Operation is the gRPC method, REST path or dashboard action.
	Operation string
	Remote    string
}

// New returns nil when authentication, authorization and the audit log are
// all off, so servers can skip the guard entirely.
func New(cfg *config.Config) (*Guard, error) {
	auth, err := authentication.New(cfg.Authentication)
	if err != nil {
		return nil, err
	}
	authz, err := authorization.New(cfg.Authorization)
	if err != nil {
		return nil, err
	}
	auditLog, err := audit.New(cfg.Audit)
	if err != nil {
		return nil, err
	}
	if auth == nil && authz == nil && auditLog == nil {
		return nil, nil
	}
	return &Guard{auth: auth, authz: authz, audit: auditLog}, nil
}

// Authenticator is nil when authentication is off.
func (g *Guard) Authenticator() *authentication.Authenticator {
	return g.auth
}

func (g *Guard) Close() {
	g.audit.Close()
}

// Authenticate verifies the request's token, when authentication is on, and
// returns ctx carrying the caller's identity.
func (g *Guard) Authenticate(ctx context.Context, token string, req Request) (context.Context, error) {
	if g.auth == nil {
		return ctx, nil
	}
	claims, err := g.auth.Claims(token)
	if err != nil {
		g.audit.Log(&audit.Entry{
			Transport: req.Transport,
			Remote:    req.Remote,
			Operation: req.Operation,
			Outcome:   audit.Unauthenticated,
			Error:     err.Error(),
		})
		return ctx, err
	}
	return authentication.WithIdentity(ctx, authentication.NewIdentity(claims)), nil
}

// AuthorizeMessage checks what a client message does (see
// authorization.ChecksFor) and audits it. On a stream, pass the same seen
// map for every message: each allowed check is then audited once per stream
// rather than once per message. Denials are always audited.
func (g *Guard) AuthorizeMessage(ctx context.Context, req Request, msg any, seen map[authorization.Check]bool) error {
	var clientID string
	if m, ok := msg.(interface{ GetClientID() string }); ok {
		clientID = m.GetClientID()
	}
	checks := authorization.ChecksFor(msg)
	if len(checks) == 0 {
		// Nothing to check, such as a response or an ack. Audit it on its
		// own unless it is one more message on a stream.
		if seen == nil {
			g.log(ctx, req, clientID, nil, nil)
		}
		return nil
	}
	return g.authorize(ctx, req, clientID, checks, seen)
}

// Authorize checks and audits explicit checks, such as the dashboard's
// admin check.
func (g *Guard) Authorize(ctx context.Context, req Request, checks ...authorization.Check) error {
	return g.authorize(ctx, req, "", checks, nil)
}

func (g *Guard) authorize(ctx context.Context, req Request, clientID string, checks []authorization.Check, seen map[authorization.Check]bool) error {
	id := authentication.IdentityFrom(ctx)
	var firstErr error
	for _, c := range checks {
		var err error
		if g.authz != nil {
			err = g.authz.Authorize(id, c)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if err == nil && seen != nil {
			if seen[c] {
				continue
			}
			seen[c] = true
		}
		g.log(ctx, req, clientID, &c, err)
	}
	return firstErr
}

// Allowed reports whether the caller in ctx may do c, without auditing it.
// It is true when authorization is off.
func (g *Guard) Allowed(ctx context.Context, c authorization.Check) bool {
	if g == nil || g.authz == nil {
		return true
	}
	return g.authz.Authorize(authentication.IdentityFrom(ctx), c) == nil
}

// Log audits a request that needs no authorization check of its own, such
// as a dashboard sign-in.
func (g *Guard) Log(ctx context.Context, req Request, c *authorization.Check, err error) {
	g.log(ctx, req, "", c, err)
}

func (g *Guard) log(ctx context.Context, req Request, clientID string, c *authorization.Check, err error) {
	if g.audit == nil {
		return
	}
	e := &audit.Entry{
		Transport: req.Transport,
		Remote:    req.Remote,
		ClientID:  clientID,
		Operation: req.Operation,
		Outcome:   audit.Allowed,
	}
	if id := authentication.IdentityFrom(ctx); id != nil {
		e.Subject, e.TokenID = id.Subject, id.TokenID
	}
	if c != nil {
		e.Action, e.Type, e.Channel = c.Action, c.Type, c.Channel
	}
	if err != nil {
		e.Outcome, e.Error = audit.Denied, err.Error()
	}
	g.audit.Log(e)
}
