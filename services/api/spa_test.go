package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestSpaHandler(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":       {Data: []byte("index")},
		"app/index-abc.js": {Data: []byte("script")},
	}
	e := echo.New()
	e.GET("/api/snapshot", func(c echo.Context) error { return c.String(http.StatusOK, "api") })
	e.GET("/*", spaHandler(assets))

	tests := []struct {
		path     string
		wantCode int
		wantBody string
	}{
		{"/", http.StatusOK, "index"},
		{"/app/index-abc.js", http.StatusOK, "script"},
		{"/queues", http.StatusOK, "index"},
		{"/queues/q.jobs", http.StatusOK, "index"},
		{"/app", http.StatusOK, "index"},
		{"/app/index-old.js", http.StatusNotFound, ""},
		{"/api/snapshot", http.StatusOK, "api"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			require.Equal(t, tt.wantCode, rec.Code)
			if tt.wantBody != "" {
				require.Equal(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}
