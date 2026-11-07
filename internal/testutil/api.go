package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mauinGit/gdgoc-core-team-api/internal/config"
	apihttp "github.com/mauinGit/gdgoc-core-team-api/internal/http"
	"github.com/mauinGit/gdgoc-core-team-api/internal/service"
)

// Config returns a valid configuration for tests.
func Config() *config.Config {
	return &config.Config{
		AppEnv:             "test",
		JWTSecret:          "test-secret-test-secret-test-secret-0123456789",
		JWTIssuer:          "gdgoc-test",
		JWTTTL:             time.Hour,
		BcryptCost:         10,
		LoginRateLimit:     5,
		LoginIPRateLimit:   30,
		LoginRateWindow:    time.Minute,
		AllowedEmailDomain: "gdgoc-unsri.test",
	}
}

// API is a fully wired router backed by an isolated, seeded database.
type API struct {
	T      testing.TB
	Pool   *pgxpool.Pool
	Router http.Handler
	Config *config.Config
}

type Option func(*apiOptions)

type apiOptions struct {
	cfg    func(*config.Config)
	google service.GoogleProvider
}

// WithConfig tweaks the test configuration.
func WithConfig(fn func(*config.Config)) Option { return func(o *apiOptions) { o.cfg = fn } }

// WithGoogle injects a fake Google OAuth provider.
func WithGoogle(p service.GoogleProvider) Option { return func(o *apiOptions) { o.google = p } }

func NewAPI(t testing.TB, opts ...Option) *API {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if os.Getenv("TEST_LOG") == "" {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	var o apiOptions
	for _, opt := range opts {
		opt(&o)
	}
	cfg := Config()
	if o.cfg != nil {
		o.cfg(cfg)
	}

	pool := NewDB(t)
	Seed(t, pool)

	router, err := apihttp.NewRouter(apihttp.Deps{Config: cfg, DB: pool, Google: o.google})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	return &API{T: t, Pool: pool, Router: router, Config: cfg}
}

// NewRequest builds a request. body may be nil, a string (sent raw) or any
// value (JSON-encoded).
func (a *API) NewRequest(method, path, token string, body any) *http.Request {
	a.T.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			a.T.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// Serve executes a request against the router.
func (a *API) Serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	return rec
}

// Do builds and executes a request.
func (a *API) Do(method, path, token string, body any) *httptest.ResponseRecorder {
	a.T.Helper()
	return a.Serve(a.NewRequest(method, path, token, body))
}

var loginCounter atomic.Int64

// Login signs in a seeded account and returns its access token.
func (a *API) Login(email string) string {
	a.T.Helper()
	req := a.NewRequest(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": email, "password": SeedPassword,
	})
	// Unique source IP per login so helper logins never hit the rate limit.
	n := loginCounter.Add(1)
	req.RemoteAddr = fmt.Sprintf("10.99.%d.%d:1234", (n/250)%250, n%250+1)
	rec := a.Serve(req)
	if rec.Code != http.StatusOK {
		a.T.Fatalf("login %s: status %d body %s", email, rec.Code, rec.Body.String())
	}
	var res struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	DecodeJSON(a.T, rec, &res)
	return res.Data.AccessToken
}

// DecodeJSON decodes the recorder body into dst.
func DecodeJSON(t testing.TB, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
}

// ErrorCode extracts error.code from an error response.
func ErrorCode(t testing.TB, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	DecodeJSON(t, rec, &body)
	return body.Error.Code
}
