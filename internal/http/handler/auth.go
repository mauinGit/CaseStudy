package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/service"
)

const (
	oauthStateCookie    = "gdgoc_oauth_state"
	oauthVerifierCookie = "gdgoc_oauth_verifier"
	oauthCookiePath     = "/api/v1/auth/google"
	oauthCookieTTL      = 10 * time.Minute
)

type AuthHandler struct {
	auth          *service.AuthService
	secureCookies bool
}

func NewAuthHandler(auth *service.AuthService, secureCookies bool) *AuthHandler {
	return &AuthHandler{auth: auth, secureCookies: secureCookies}
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email,max=254" example:"lead@gdgoc-unsri.test"`
	Password string `json:"password" binding:"required,max=72" example:"GdgocUnsri2026!"`
}

type loginResponse struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	ExpiresIn   int64        `json:"expires_in"`
	ExpiresAt   time.Time    `json:"expires_at"`
	User        UserResponse `json:"user"`
}

func toLoginResponse(r *service.LoginResult) loginResponse {
	return loginResponse{
		AccessToken: r.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int64(time.Until(r.ExpiresAt).Seconds()),
		ExpiresAt:   r.ExpiresAt,
		User:        toUserResponse(r.User),
	}
}

// Login authenticates with email and password.
//
//	@Summary	Login dengan email & password
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		loginRequest	true	"Kredensial"
//	@Success	200		{object}	dataResponse{data=loginResponse}
//	@Failure	400,401,429	{object}	apperror.Body
//	@Router		/api/v1/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusOK, toLoginResponse(res))
}

// GoogleLogin redirects the browser to Google's consent screen. A random
// state (CSRF protection) and PKCE verifier are stored in short-lived
// HttpOnly cookies.
//
//	@Summary	Mulai login Google OAuth (buka di browser)
//	@Tags		auth
//	@Success	302
//	@Failure	503	{object}	apperror.Body
//	@Router		/api/v1/auth/google/login [get]
func (h *AuthHandler) GoogleLogin(c *gin.Context) {
	if !h.auth.GoogleEnabled() {
		apperror.Respond(c, domain.ErrGoogleDisabled)
		return
	}
	state, err := randomToken()
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	verifier := oauth2.GenerateVerifier()
	url, err := h.auth.GoogleAuthURL(state, verifier)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	h.setOAuthCookie(c, oauthStateCookie, state, oauthCookieTTL)
	h.setOAuthCookie(c, oauthVerifierCookie, verifier, oauthCookieTTL)
	c.Redirect(http.StatusFound, url)
}

// GoogleCallback completes the Google login and returns an access token.
//
//	@Summary	Callback Google OAuth
//	@Tags		auth
//	@Produce	json
//	@Param		code	query		string	true	"Authorization code"
//	@Param		state	query		string	true	"State"
//	@Success	200		{object}	dataResponse{data=loginResponse}
//	@Failure	400,401,403,503	{object}	apperror.Body
//	@Router		/api/v1/auth/google/callback [get]
func (h *AuthHandler) GoogleCallback(c *gin.Context) {
	if !h.auth.GoogleEnabled() {
		apperror.Respond(c, domain.ErrGoogleDisabled)
		return
	}
	stateCookie, _ := c.Cookie(oauthStateCookie)
	verifier, _ := c.Cookie(oauthVerifierCookie)
	// One-time use: always clear the cookies.
	h.setOAuthCookie(c, oauthStateCookie, "", -1)
	h.setOAuthCookie(c, oauthVerifierCookie, "", -1)

	if c.Query("error") != "" {
		apperror.Respond(c, domain.ErrOAuthFailed)
		return
	}
	state, code := c.Query("state"), c.Query("code")
	if stateCookie == "" || verifier == "" || state == "" ||
		subtle.ConstantTimeCompare([]byte(state), []byte(stateCookie)) != 1 {
		apperror.Respond(c, domain.ErrInvalidOAuthState)
		return
	}
	if code == "" {
		apperror.Respond(c, domain.ErrOAuthFailed)
		return
	}

	res, err := h.auth.GoogleLogin(c.Request.Context(), code, verifier)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusOK, toLoginResponse(res))
}

func (h *AuthHandler) setOAuthCookie(c *gin.Context, name, value string, ttl time.Duration) {
	maxAge := int(ttl.Seconds())
	if ttl < 0 {
		maxAge = -1
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     oauthCookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.secureCookies,
		// Lax: the cookie is still sent on the top-level redirect back from Google.
		SameSite: http.SameSiteLaxMode,
	})
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
