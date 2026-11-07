package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleProfile is the verified identity returned by Google.
type GoogleProfile struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	HostedDomain  string `json:"hd"`
}

// GoogleProvider abstracts the OAuth 2.0 / OpenID Connect flow so the login
// rules can be tested without calling Google.
type GoogleProvider interface {
	AuthCodeURL(state, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (*GoogleProfile, error)
}

const googleUserinfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

type googleOAuth struct {
	cfg          *oauth2.Config
	hostedDomain string
	httpClient   *http.Client
}

// NewGoogleProvider builds the real Google OAuth provider (authorization code
// flow with PKCE).
func NewGoogleProvider(clientID, clientSecret, redirectURL, allowedDomain string) GoogleProvider {
	return &googleOAuth{
		cfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     google.Endpoint,
			Scopes:       []string{"openid", "email", "profile"},
		},
		hostedDomain: allowedDomain,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *googleOAuth) AuthCodeURL(state, verifier string) string {
	opts := []oauth2.AuthCodeOption{
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	}
	// "hd" only pre-filters the Google account picker; the domain is
	// enforced server-side after the code exchange.
	if g.hostedDomain != "" {
		opts = append(opts, oauth2.SetAuthURLParam("hd", g.hostedDomain))
	}
	return g.cfg.AuthCodeURL(state, opts...)
}

// Exchange trades the authorization code for tokens directly with Google's
// token endpoint over TLS, then reads the OpenID userinfo.
func (g *googleOAuth) Exchange(ctx context.Context, code, verifier string) (*GoogleProfile, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, g.httpClient)
	tok, err := g.cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserinfoURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.cfg.Client(ctx, tok).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch userinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch userinfo: status %d", resp.StatusCode)
	}

	var p GoogleProfile
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	if p.Sub == "" || p.Email == "" {
		return nil, fmt.Errorf("userinfo missing sub or email")
	}
	return &p, nil
}
