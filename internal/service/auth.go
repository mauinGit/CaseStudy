package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/repository"
)

// AuthService handles login (password & Google) and token authentication.
type AuthService struct {
	store         *repository.Store
	tokens        *TokenManager
	google        GoogleProvider // nil when Google OAuth is not configured
	allowedDomain string
	// dummyHash is compared against when the user does not exist so that
	// response time does not reveal which emails are registered.
	dummyHash []byte
}

func NewAuthService(store *repository.Store, tokens *TokenManager, google GoogleProvider, allowedDomain string, bcryptCost int) (*AuthService, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	// bcrypt only uses the first 72 bytes; 32 random bytes is plenty.
	dummy, err := bcrypt.GenerateFromPassword(random, bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("build dummy hash: %w", err)
	}
	return &AuthService{
		store:         store,
		tokens:        tokens,
		google:        google,
		allowedDomain: strings.ToLower(allowedDomain),
		dummyHash:     dummy,
	}, nil
}

// LoginResult is returned after a successful login.
type LoginResult struct {
	AccessToken string
	ExpiresAt   time.Time
	User        *domain.User
}

// Login verifies email + password. Unknown email, wrong password, Google-only
// account and soft-deleted account all return the same error.
func (s *AuthService) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	u, err := s.store.Users.GetActiveByEmail(ctx, NormalizeEmail(email))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return nil, domain.ErrInvalidCredentials
	case err != nil:
		return nil, err
	}
	if u.PasswordHash == nil {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return nil, domain.ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(*u.PasswordHash), []byte(password)) != nil {
		return nil, domain.ErrInvalidCredentials
	}
	return s.issue(u)
}

// Authenticate validates an access token and loads the current state of the
// user. Deleted users are rejected even if their token has not expired.
func (s *AuthService) Authenticate(ctx context.Context, rawToken, ip string) (domain.Actor, error) {
	id, err := s.tokens.Parse(rawToken)
	if err != nil {
		return domain.Actor{}, err
	}
	u, err := s.store.Users.GetActiveByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Actor{}, domain.ErrInvalidToken
		}
		return domain.Actor{}, err
	}
	return domain.Actor{UserID: u.ID, Role: u.Role, DivisionID: u.DivisionID, IP: ip}, nil
}

// GoogleEnabled reports whether Google login is available.
func (s *AuthService) GoogleEnabled() bool { return s.google != nil }

// GoogleAuthURL returns the Google consent URL for the given state & PKCE verifier.
func (s *AuthService) GoogleAuthURL(state, verifier string) (string, error) {
	if s.google == nil {
		return "", domain.ErrGoogleDisabled
	}
	return s.google.AuthCodeURL(state, verifier), nil
}

// GoogleLogin completes the OAuth flow. Only verified emails from the allowed
// domain that belong to an existing (pre-registered) member may sign in; the
// first successful login links the Google account (sub) to the member.
func (s *AuthService) GoogleLogin(ctx context.Context, code, verifier string) (*LoginResult, error) {
	if s.google == nil {
		return nil, domain.ErrGoogleDisabled
	}
	p, err := s.google.Exchange(ctx, code, verifier)
	if err != nil {
		slog.WarnContext(ctx, "google oauth exchange failed", "error", err)
		return nil, domain.ErrOAuthFailed
	}
	if !p.EmailVerified {
		return nil, domain.ErrEmailNotVerified
	}
	email := NormalizeEmail(p.Email)
	if !s.domainAllowed(email, p.HostedDomain) {
		return nil, domain.ErrEmailDomainNotAllowed
	}

	u, err := s.store.Users.GetActiveByGoogleSub(ctx, p.Sub)
	if errors.Is(err, domain.ErrNotFound) {
		u, err = s.store.Users.GetActiveByEmail(ctx, email)
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrAccountNotRegistered
		}
		if err != nil {
			return nil, err
		}
		if u.GoogleSub != nil && *u.GoogleSub != p.Sub {
			// The email is already bound to a different Google account.
			return nil, domain.ErrAccountNotRegistered
		}
		if u.GoogleSub == nil {
			if err := s.store.Users.LinkGoogleSub(ctx, u.ID, p.Sub); err != nil {
				return nil, err
			}
			u.GoogleSub = &p.Sub
		}
	} else if err != nil {
		return nil, err
	}
	return s.issue(u)
}

func (s *AuthService) domainAllowed(email, hostedDomain string) bool {
	if s.allowedDomain == "" {
		return false
	}
	if !strings.HasSuffix(email, "@"+s.allowedDomain) {
		return false
	}
	// Google Workspace accounts carry "hd"; when present it must match too.
	return hostedDomain == "" || strings.EqualFold(hostedDomain, s.allowedDomain)
}

func (s *AuthService) issue(u *domain.User) (*LoginResult, error) {
	token, exp, err := s.tokens.Issue(u)
	if err != nil {
		return nil, err
	}
	return &LoginResult{AccessToken: token, ExpiresAt: exp, User: u}, nil
}

// NormalizeEmail trims and lowercases an email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
