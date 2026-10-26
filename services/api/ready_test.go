package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestRequireReady(t *testing.T) {
	s := &service{}
	e := echo.New()
	e.Group("/api", s.requireReady).GET("/snapshot", func(c echo.Context) error {
		return c.String(http.StatusOK, "snapshot")
	})
	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/snapshot", nil))
		return rec
	}

	rec := get()
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "broker is starting")

	s.ready.Store(true)
	rec = get()
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "snapshot", rec.Body.String())
}
