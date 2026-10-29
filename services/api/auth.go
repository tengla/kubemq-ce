package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"github.com/kubemq-io/kubemq-community/pkg/authorization"
	"github.com/kubemq-io/kubemq-community/pkg/guard"
	"github.com/labstack/echo/v4"
)

// The dashboard signs in with the same JWTs that clients use. After login the
// token lives in an HttpOnly cookie, so page scripts never hold it and
// websocket handshakes carry it automatically.
//
// Browsers share cookies between ports, so the cookie name includes the API
// port: dashboards of two brokers on one host keep separate sessions.
func sessionCookieName(apiPort int) string {
	return fmt.Sprintf("kubemq_session_%d", apiPort)
}

type sessionInfo struct {
	Enabled       bool       `json:"enabled"`
	Authenticated bool       `json:"authenticated"`
	Subject       string     `json:"subject,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
}

func newSessionInfo(claims *jwt.RegisteredClaims) sessionInfo {
	info := sessionInfo{Enabled: true, Authenticated: true, Subject: claims.Subject}
	if claims.ExpiresAt != nil {
		info.ExpiresAt = &claims.ExpiresAt.Time
	}
	return info
}

// requestToken prefers the Authorization header, which scripts and the CLI
// use, over the session cookie.
func (s *Server) requestToken(c echo.Context) (token string, fromCookie bool) {
	if header := c.Request().Header.Get(echo.HeaderAuthorization); header != "" {
		return header, false
	}
	if cookie, err := c.Cookie(s.cookieName); err == nil {
		return cookie.Value, true
	}
	return "", false
}

// sameOrigin rejects cross-origin browser requests riding on the session
// cookie. Browsers send Origin on POSTs, websocket handshakes and cross-origin
// fetches, so a request without one is not a cross-origin browser request.
// X-Forwarded-Host covers reverse proxies that rewrite Host.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	forwarded, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
	forwarded = strings.TrimSpace(forwarded)
	return forwarded != "" && strings.EqualFold(u.Host, forwarded)
}

func guardRequest(c echo.Context) guard.Request {
	return guard.Request{Transport: "api", Operation: c.Request().URL.Path, Remote: c.RealIP()}
}

var adminCheck = authorization.Check{Action: authorization.Admin}

// requireAdmin checks that the caller may use the dashboard, which can do
// anything, so with authorization on it takes a rule with the admin action.
// Only refusals are audited: the dashboard polls every few seconds.
func (s *Server) requireAdmin(ctx context.Context, req guard.Request) error {
	if s.guard.Allowed(ctx, adminCheck) {
		return nil
	}
	subject := ""
	if id := authentication.IdentityFrom(ctx); id != nil {
		subject = id.Subject
	}
	err := fmt.Errorf("%w%s may not use the dashboard: no authorization rule gives it the admin action", authorization.ErrPermissionDenied, subject)
	s.guard.Log(ctx, req, &adminCheck, err)
	return err
}

func (s *Server) requireSession(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.auth == nil {
			return next(c)
		}
		token, fromCookie := s.requestToken(c)
		if fromCookie && !sameOrigin(c.Request()) {
			return NewResponse(c).SetErrorWithText("cross-origin request rejected").SetHttpCode(http.StatusForbidden).Send()
		}
		req := guardRequest(c)
		ctx, err := s.guard.Authenticate(c.Request().Context(), token, req)
		if err != nil {
			return NewResponse(c).SetErrorWithText(err.Error()).SetHttpCode(http.StatusUnauthorized).Send()
		}
		if err := s.requireAdmin(ctx, req); err != nil {
			return NewResponse(c).SetErrorWithText(err.Error()).SetHttpCode(http.StatusForbidden).Send()
		}
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

// audited records a dashboard operation, such as opening a subscription, in
// the audit log.
func (s *Server) audited(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		s.auditAction(c, c.QueryParam("channel"))
		return next(c)
	}
}

func (s *Server) auditAction(c echo.Context, channel string) {
	if s.guard == nil {
		return
	}
	check := authorization.Check{Action: authorization.Admin, Channel: channel}
	s.guard.Log(c.Request().Context(), guardRequest(c), &check, nil)
}

func (s *Server) handleSession(c echo.Context) error {
	if s.auth == nil {
		return NewResponse(c).SetResponseBody(sessionInfo{Authenticated: true}).Send()
	}
	token, _ := s.requestToken(c)
	claims, err := s.auth.Claims(token)
	if err != nil {
		return NewResponse(c).SetResponseBody(sessionInfo{Enabled: true}).Send()
	}
	// A token that may no longer use the dashboard gets the login page,
	// which explains why when it is tried again.
	if !s.guard.Allowed(authentication.WithIdentity(c.Request().Context(), authentication.NewIdentity(claims)), adminCheck) {
		return NewResponse(c).SetResponseBody(sessionInfo{Enabled: true}).Send()
	}
	return NewResponse(c).SetResponseBody(newSessionInfo(claims)).Send()
}

func (s *Server) handleLogin(c echo.Context) error {
	if s.auth == nil {
		return NewResponse(c).SetResponseBody(sessionInfo{Authenticated: true}).Send()
	}
	if !sameOrigin(c.Request()) {
		return NewResponse(c).SetErrorWithText("cross-origin request rejected").SetHttpCode(http.StatusForbidden).Send()
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := c.Bind(&req); err != nil {
		return NewResponse(c).SetErrorWithText("invalid login request").SetHttpCode(http.StatusBadRequest).Send()
	}
	gr := guardRequest(c)
	ctx, err := s.guard.Authenticate(c.Request().Context(), req.Token, gr)
	if err != nil {
		return NewResponse(c).SetErrorWithText(err.Error()).SetHttpCode(http.StatusUnauthorized).Send()
	}
	if err := s.requireAdmin(ctx, gr); err != nil {
		return NewResponse(c).SetErrorWithText(err.Error()).SetHttpCode(http.StatusForbidden).Send()
	}
	s.guard.Log(ctx, gr, &adminCheck, nil)
	claims, err := s.auth.Claims(req.Token)
	if err != nil {
		return NewResponse(c).SetErrorWithText(err.Error()).SetHttpCode(http.StatusUnauthorized).Send()
	}
	cookie := s.cookie(c, authentication.BareToken(req.Token))
	// A token without an expiry gets a browser-session cookie.
	if claims.ExpiresAt != nil {
		cookie.Expires = claims.ExpiresAt.Time
	}
	c.SetCookie(cookie)
	return NewResponse(c).SetResponseBody(newSessionInfo(claims)).Send()
}

func (s *Server) handleLogout(c echo.Context) error {
	cookie := s.cookie(c, "")
	cookie.MaxAge = -1
	c.SetCookie(cookie)
	return NewResponse(c).SetResponseBody(sessionInfo{Enabled: s.auth != nil, Authenticated: s.auth == nil}).Send()
}

func (s *Server) cookie(c echo.Context, value string) *http.Cookie {
	return &http.Cookie{
		Name:     s.cookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   c.Scheme() == "https",
	}
}
