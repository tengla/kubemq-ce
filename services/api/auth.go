package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
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

func (s *Server) requireSession(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.auth == nil {
			return next(c)
		}
		token, fromCookie := s.requestToken(c)
		if fromCookie && !sameOrigin(c.Request()) {
			return NewResponse(c).SetErrorWithText("cross-origin request rejected").SetHttpCode(http.StatusForbidden).Send()
		}
		if err := s.auth.Verify(token); err != nil {
			return NewResponse(c).SetErrorWithText(err.Error()).SetHttpCode(http.StatusUnauthorized).Send()
		}
		return next(c)
	}
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
