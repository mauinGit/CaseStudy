package http_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mauinGit/gdgoc-core-team-api/internal/testutil"
)

func TestSwaggerDocs(t *testing.T) {
	api := testutil.NewAPI(t)

	rec := api.Do(http.MethodGet, "/docs", "", nil)
	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "/docs/index.html", rec.Header().Get("Location"))

	rec = api.Do(http.MethodGet, "/docs/index.html", "", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "swagger-ui")
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "script-src 'self'")

	rec = api.Do(http.MethodGet, "/docs/doc.json", "", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/members", "/api/v1/events/{id}/register", "/api/v1/events/{id}/check-in", "/api/v1/audit-logs"} {
		assert.Contains(t, rec.Body.String(), path)
	}

	// The relaxed policy applies to the docs only.
	rec = api.Do(http.MethodGet, "/healthz", "", nil)
	assert.Equal(t, "default-src 'none'; frame-ancestors 'none'", rec.Header().Get("Content-Security-Policy"))
}
