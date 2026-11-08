package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mauinGit/gdgoc-core-team-api/internal/service"
	"github.com/mauinGit/gdgoc-core-team-api/internal/testutil"
	"github.com/mauinGit/gdgoc-core-team-api/seed"
)

const (
	emailLead      = "lead@" + seed.EmailDomain
	emailHeadBE    = "head.backend@" + seed.EmailDomain
	emailHeadFE    = "head.frontend@" + seed.EmailDomain
	emailMemberBE1 = "member.backend1@" + seed.EmailDomain
	emailMemberBE2 = "member.backend2@" + seed.EmailDomain
	emailMemberFE  = "member.frontend@" + seed.EmailDomain
)

type userBody struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Division *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"division"`
	Phone        *string `json:"phone"`
	Bio          *string `json:"bio"`
	GoogleLinked bool    `json:"google_linked"`
}

func login(api *testutil.API, ip, email, password string) *httptest.ResponseRecorder {
	req := api.NewRequest(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": email, "password": password})
	req.RemoteAddr = ip + ":5555"
	return api.Serve(req)
}

func TestLoginSuccess(t *testing.T) {
	api := testutil.NewAPI(t)

	// Email lookup is case-insensitive.
	rec := login(api, "203.0.113.1", "LEAD@GDGoC-UNSRI.test", testutil.SeedPassword)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var res struct {
		Data struct {
			AccessToken string   `json:"access_token"`
			TokenType   string   `json:"token_type"`
			ExpiresIn   int64    `json:"expires_in"`
			User        userBody `json:"user"`
		} `json:"data"`
	}
	testutil.DecodeJSON(t, rec, &res)
	assert.NotEmpty(t, res.Data.AccessToken)
	assert.Equal(t, "Bearer", res.Data.TokenType)
	assert.Positive(t, res.Data.ExpiresIn)
	assert.Equal(t, "lead", res.Data.User.Role)
	assert.NotContains(t, rec.Body.String(), "password_hash")
	assert.NotContains(t, rec.Body.String(), "$2a$")
}

func TestLoginInvalidCredentialsAreIndistinguishable(t *testing.T) {
	api := testutil.NewAPI(t)

	wrongPw := login(api, "203.0.113.2", emailLead, "wrong-password")
	unknown := login(api, "203.0.113.3", "nobody@gdgoc-unsri.test", "whatever123")

	for _, rec := range []*httptest.ResponseRecorder{wrongPw, unknown} {
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t, "INVALID_CREDENTIALS", testutil.ErrorCode(t, rec))
	}
	assert.Equal(t, wrongPw.Body.String(), unknown.Body.String())
}

func TestLoginValidation(t *testing.T) {
	api := testutil.NewAPI(t)

	rec := login(api, "203.0.113.4", "not-an-email", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var body struct {
		Error struct {
			Code   string `json:"code"`
			Fields []struct {
				Field string `json:"field"`
			} `json:"fields"`
		} `json:"error"`
	}
	testutil.DecodeJSON(t, rec, &body)
	assert.Equal(t, "VALIDATION_ERROR", body.Error.Code)
	var fields []string
	for _, f := range body.Error.Fields {
		fields = append(fields, f.Field)
	}
	assert.ElementsMatch(t, []string{"email", "password"}, fields)

	malformed := api.Serve(api.NewRequest(http.MethodPost, "/api/v1/auth/login", "", "{not json"))
	assert.Equal(t, http.StatusBadRequest, malformed.Code)
	assert.Equal(t, "VALIDATION_ERROR", testutil.ErrorCode(t, malformed))
}

func TestLoginRateLimitPerIPAndEmail(t *testing.T) {
	api := testutil.NewAPI(t)
	const ip = "198.51.100.7"

	for i := 0; i < 5; i++ {
		rec := login(api, ip, emailMemberBE1, "wrong-password")
		require.Equal(t, http.StatusUnauthorized, rec.Code, "attempt %d", i+1)
	}

	blocked := login(api, ip, emailMemberBE1, testutil.SeedPassword)
	assert.Equal(t, http.StatusTooManyRequests, blocked.Code, "6th attempt must be throttled even with the right password")
	assert.Equal(t, "TOO_MANY_REQUESTS", testutil.ErrorCode(t, blocked))
	assert.NotEmpty(t, blocked.Header().Get("Retry-After"))

	// Same email from another IP, or another email from the same IP, is not blocked.
	assert.Equal(t, http.StatusOK, login(api, "198.51.100.8", emailMemberBE1, testutil.SeedPassword).Code)
	assert.Equal(t, http.StatusOK, login(api, ip, emailMemberBE2, testutil.SeedPassword).Code)
}

func TestLoginRateLimitPerIP(t *testing.T) {
	api := testutil.NewAPI(t)
	const ip = "198.51.100.9"

	// Password spraying: many different emails from one IP.
	for i := 0; i < api.Config.LoginIPRateLimit; i++ {
		rec := login(api, ip, "spray"+string(rune('a'+i%26))+string(rune('a'+i/26))+"@gdgoc-unsri.test", "x")
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
	assert.Equal(t, http.StatusTooManyRequests, login(api, ip, emailLead, testutil.SeedPassword).Code)
}

func TestSoftDeletedUserCannotLoginAndTokenIsRevoked(t *testing.T) {
	api := testutil.NewAPI(t)
	token := api.Login(emailMemberFE)

	require.Equal(t, http.StatusOK, api.Do(http.MethodGet, "/api/v1/me", token, nil).Code)

	_, err := api.Pool.Exec(context.Background(), `UPDATE users SET deleted_at = now() WHERE email = $1`, emailMemberFE)
	require.NoError(t, err)

	rec := login(api, "203.0.113.20", emailMemberFE, testutil.SeedPassword)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "INVALID_CREDENTIALS", testutil.ErrorCode(t, rec))

	me := api.Do(http.MethodGet, "/api/v1/me", token, nil)
	assert.Equal(t, http.StatusUnauthorized, me.Code, "existing token of a deleted user must stop working")
	assert.Equal(t, "INVALID_TOKEN", testutil.ErrorCode(t, me))
}

func TestMe(t *testing.T) {
	api := testutil.NewAPI(t)
	token := api.Login(emailHeadBE)

	rec := api.Do(http.MethodGet, "/api/v1/me", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var res struct {
		Data userBody `json:"data"`
	}
	testutil.DecodeJSON(t, rec, &res)
	assert.Equal(t, emailHeadBE, res.Data.Email)
	assert.Equal(t, "division_head", res.Data.Role)
	require.NotNil(t, res.Data.Division)
	assert.Equal(t, seed.DivBackend, res.Data.Division.ID)
	assert.Equal(t, "Back-End", res.Data.Division.Name)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}

func TestMeRequiresValidToken(t *testing.T) {
	api := testutil.NewAPI(t)

	cases := map[string]struct {
		header string
		code   string
	}{
		"missing":       {"", "UNAUTHORIZED"},
		"wrong scheme":  {"Basic abc", "UNAUTHORIZED"},
		"garbage token": {"Bearer not-a-token", "INVALID_TOKEN"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := api.NewRequest(http.MethodGet, "/api/v1/me", "", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := api.Serve(req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Equal(t, tc.code, testutil.ErrorCode(t, rec))
		})
	}
}

// ---- Google OAuth (fake provider) ----

type fakeGoogle struct {
	profile *service.GoogleProfile
}

func (f *fakeGoogle) AuthCodeURL(state, _ string) string {
	return "https://accounts.google.test/o/oauth2/auth?state=" + url.QueryEscape(state)
}

func (f *fakeGoogle) Exchange(context.Context, string, string) (*service.GoogleProfile, error) {
	return f.profile, nil
}

// googleFlow runs /google/login then /google/callback with the issued cookies.
func googleFlow(t *testing.T, api *testutil.API, tamperState bool) *httptest.ResponseRecorder {
	t.Helper()
	start := api.Do(http.MethodGet, "/api/v1/auth/google/login", "", nil)
	require.Equal(t, http.StatusFound, start.Code, start.Body.String())

	loc, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	state := loc.Query().Get("state")
	require.NotEmpty(t, state)
	if tamperState {
		state += "x"
	}

	req := api.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=test-code&state="+url.QueryEscape(state), "", nil)
	for _, c := range start.Result().Cookies() {
		assert.True(t, c.HttpOnly, "oauth cookie %s must be HttpOnly", c.Name)
		req.AddCookie(c)
	}
	return api.Serve(req)
}

func TestGoogleLogin(t *testing.T) {
	verified := func(email, sub string) *service.GoogleProfile {
		return &service.GoogleProfile{Sub: sub, Email: email, EmailVerified: true, Name: "X"}
	}

	t.Run("registered member logs in and google account gets linked", func(t *testing.T) {
		api := testutil.NewAPI(t, testutil.WithGoogle(&fakeGoogle{profile: verified(emailMemberBE2, "google-sub-1")}))

		rec := googleFlow(t, api, false)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var res struct {
			Data struct {
				AccessToken string   `json:"access_token"`
				User        userBody `json:"user"`
			} `json:"data"`
		}
		testutil.DecodeJSON(t, rec, &res)
		assert.Equal(t, emailMemberBE2, res.Data.User.Email)
		assert.True(t, res.Data.User.GoogleLinked)

		var sub string
		require.NoError(t, api.Pool.QueryRow(context.Background(),
			`SELECT google_sub FROM users WHERE email = $1`, emailMemberBE2).Scan(&sub))
		assert.Equal(t, "google-sub-1", sub)

		me := api.Do(http.MethodGet, "/api/v1/me", res.Data.AccessToken, nil)
		assert.Equal(t, http.StatusOK, me.Code)
	})

	rejections := map[string]struct {
		profile *service.GoogleProfile
		status  int
		code    string
	}{
		"domain not allowed": {verified("someone@gmail.com", "s2"), http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED"},
		"hd mismatch": {&service.GoogleProfile{Sub: "s3", Email: emailLead, EmailVerified: true, HostedDomain: "evil.test"},
			http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED"},
		"email not verified": {&service.GoogleProfile{Sub: "s4", Email: emailLead, EmailVerified: false},
			http.StatusForbidden, "EMAIL_NOT_VERIFIED"},
		"not registered": {verified("stranger@gdgoc-unsri.test", "s5"), http.StatusForbidden, "ACCOUNT_NOT_REGISTERED"},
	}
	for name, tc := range rejections {
		t.Run(name, func(t *testing.T) {
			api := testutil.NewAPI(t, testutil.WithGoogle(&fakeGoogle{profile: tc.profile}))
			rec := googleFlow(t, api, false)
			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
			assert.Equal(t, tc.code, testutil.ErrorCode(t, rec))
		})
	}

	t.Run("state mismatch is rejected (CSRF)", func(t *testing.T) {
		api := testutil.NewAPI(t, testutil.WithGoogle(&fakeGoogle{profile: verified(emailLead, "s6")}))
		rec := googleFlow(t, api, true)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "INVALID_OAUTH_STATE", testutil.ErrorCode(t, rec))
	})

	t.Run("disabled when not configured", func(t *testing.T) {
		api := testutil.NewAPI(t)
		rec := api.Do(http.MethodGet, "/api/v1/auth/google/login", "", nil)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "GOOGLE_OAUTH_DISABLED", testutil.ErrorCode(t, rec))
	})
}
