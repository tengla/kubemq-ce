package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

var testJWT = &config.JWTAuthenticationConfig{Key: "dashboard-test-secret-with-enough-length", SignatureType: "HS256"}

func authServer(t *testing.T, enabled bool) (*Server, *echo.Echo) {
	t.Helper()
	auth, err := authentication.New(&config.AuthenticationConfig{Enable: enabled, JwtConfig: testJWT})
	require.NoError(t, err)
	s := &Server{auth: auth, cookieName: sessionCookieName(8080)}
	e := echo.New()
	e.GET("/api/auth/session", s.handleSession)
	e.POST("/api/auth/login", s.handleLogin)
	e.POST("/api/auth/logout", s.handleLogout)
	e.Group("/api", s.requireSession).GET("/snapshot", func(c echo.Context) error {
		return c.String(http.StatusOK, "snapshot")
	})
	return s, e
}

type requestOption func(*http.Request)

func withHeader(key, value string) requestOption {
	return func(r *http.Request) { r.Header.Set(key, value) }
}

func withCookie(token string) requestOption {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: sessionCookieName(8080), Value: token}) }
}

func serve(e *echo.Echo, method, path, body string, opts ...requestOption) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "dash.local:8080"
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	for _, opt := range opts {
		opt(req)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func sessionOf(t *testing.T, rec *httptest.ResponseRecorder) sessionInfo {
	t.Helper()
	var res struct {
		Data sessionInfo `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	return res.Data
}

func sign(t *testing.T, ttl time.Duration) string {
	t.Helper()
	token, err := authentication.Sign(testJWT, nil, "alice", ttl)
	require.NoError(t, err)
	return token
}

func TestDashboardAuth_Disabled(t *testing.T) {
	_, e := authServer(t, false)
	require.Equal(t, http.StatusOK, serve(e, http.MethodGet, "/api/snapshot", "").Code)
	info := sessionOf(t, serve(e, http.MethodGet, "/api/auth/session", ""))
	require.Equal(t, sessionInfo{Authenticated: true}, info)
}

func TestDashboardAuth_RequireSession(t *testing.T) {
	_, e := authServer(t, true)
	token := sign(t, time.Hour)

	tests := []struct {
		name string
		opts []requestOption
		want int
	}{
		{"no token", nil, http.StatusUnauthorized},
		{"bad cookie", []requestOption{withCookie("not-a-jwt")}, http.StatusUnauthorized},
		{"header", []requestOption{withHeader("Authorization", "Bearer "+token)}, http.StatusOK},
		{"cookie", []requestOption{withCookie(token)}, http.StatusOK},
		{"cookie, same origin", []requestOption{withCookie(token), withHeader("Origin", "http://dash.local:8080")}, http.StatusOK},
		{"cookie, other origin", []requestOption{withCookie(token), withHeader("Origin", "http://evil.local")}, http.StatusForbidden},
		{"cookie, other port", []requestOption{withCookie(token), withHeader("Origin", "http://dash.local:3000")}, http.StatusForbidden},
		{"cookie, proxied host", []requestOption{withCookie(token), withHeader("Origin", "https://mq.example.com"), withHeader("X-Forwarded-Host", "mq.example.com")}, http.StatusOK},
		// A token in the header is not sent automatically by browsers, so origin doesn't matter.
		{"header, other origin", []requestOption{withHeader("Authorization", token), withHeader("Origin", "http://evil.local")}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, serve(e, http.MethodGet, "/api/snapshot", "", tt.opts...).Code)
		})
	}
}

func TestDashboardAuth_LoginLogout(t *testing.T) {
	_, e := authServer(t, true)

	require.Equal(t, sessionInfo{Enabled: true}, sessionOf(t, serve(e, http.MethodGet, "/api/auth/session", "")))

	rec := serve(e, http.MethodPost, "/api/auth/login", `{"token":"not-a-jwt"}`)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, rec.Result().Cookies())

	token := sign(t, time.Hour)
	rec = serve(e, http.MethodPost, "/api/auth/login", `{"token":"`+token+`"}`, withHeader("Origin", "http://evil.local"))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, rec.Result().Cookies())

	rec = serve(e, http.MethodPost, "/api/auth/login", `{"token":"Bearer `+token+`"}`, withHeader("Origin", "http://dash.local:8080"))
	require.Equal(t, http.StatusOK, rec.Code)
	info := sessionOf(t, rec)
	require.True(t, info.Authenticated)
	require.Equal(t, "alice", info.Subject)
	require.NotNil(t, info.ExpiresAt)
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	require.Equal(t, "kubemq_session_8080", cookie.Name)
	require.Equal(t, token, cookie.Value, "the Bearer prefix is not stored")
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.WithinDuration(t, time.Now().Add(time.Hour), cookie.Expires, time.Minute)

	info = sessionOf(t, serve(e, http.MethodGet, "/api/auth/session", "", withCookie(cookie.Value)))
	require.True(t, info.Authenticated)
	require.Equal(t, "alice", info.Subject)

	rec = serve(e, http.MethodPost, "/api/auth/logout", "", withCookie(cookie.Value))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, sessionInfo{Enabled: true}, sessionOf(t, rec))
	cleared := rec.Result().Cookies()
	require.Len(t, cleared, 1)
	require.Equal(t, "kubemq_session_8080", cleared[0].Name)
	require.Empty(t, cleared[0].Value)
	require.Less(t, cleared[0].MaxAge, 0)
}

func TestDashboardAuth_LoginWithoutExpiry(t *testing.T) {
	_, e := authServer(t, true)
	rec := serve(e, http.MethodPost, "/api/auth/login", `{"token":"`+sign(t, 0)+`"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.True(t, cookies[0].Expires.IsZero(), "a token without exp gives a browser-session cookie")
	require.Nil(t, sessionOf(t, rec).ExpiresAt)
}
