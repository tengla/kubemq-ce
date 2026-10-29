package rest

import (
	"errors"
	"net/http"

	"github.com/kubemq-io/kubemq-community/pkg/authorization"
	"github.com/kubemq-io/kubemq-community/pkg/guard"
	"github.com/labstack/echo/v4"
)

const guardRequestKey = "guard_request"

// Authentication rejects requests without a valid token in the Authorization
// header and puts the caller's identity in the request context. "/" and
// "/ping" stay open for connectivity checks, as Ping does on gRPC.
func Authentication(g *guard.Guard) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch c.Request().URL.Path {
			case "/", "/ping":
				return next(c)
			}
			req := guard.Request{Transport: "rest", Operation: c.Request().URL.Path, Remote: c.RealIP()}
			c.Set(guardRequestKey, req)
			ctx, err := g.Authenticate(c.Request().Context(), c.Request().Header.Get(echo.HeaderAuthorization), req)
			if err != nil {
				return NewResponse(c).SetHttpCode(http.StatusUnauthorized).SetErrorWithText(err.Error()).Send()
			}
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// authorize checks what a client message does. On a websocket stream, pass
// the same seen map for every message, so each channel is audited once.
func (s *Server) authorize(c echo.Context, msg any, seen map[authorization.Check]bool) error {
	if s.guard == nil {
		return nil
	}
	req, _ := c.Get(guardRequestKey).(guard.Request)
	return s.guard.AuthorizeMessage(c.Request().Context(), req, msg, seen)
}

// denied answers 403 for a permission error and 400 for anything else.
func denied(res *Response, err error) error {
	if errors.Is(err, authorization.ErrPermissionDenied) {
		return res.SetError(err).SetHttpCode(http.StatusForbidden).Send()
	}
	return res.SetError(err).SetHttpCode(http.StatusBadRequest).Send()
}
