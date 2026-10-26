package rest

import (
	"net/http"

	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"github.com/labstack/echo/v4"
)

// Authentication rejects requests without a valid token in the Authorization
// header. "/" and "/ping" stay open for connectivity checks, as Ping does on
// gRPC.
func Authentication(auth *authentication.Authenticator) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch c.Request().URL.Path {
			case "/", "/ping":
				return next(c)
			}
			if err := auth.Verify(c.Request().Header.Get(echo.HeaderAuthorization)); err != nil {
				return NewResponse(c).SetHttpCode(http.StatusUnauthorized).SetErrorWithText(err.Error()).Send()
			}
			return next(c)
		}
	}
}
