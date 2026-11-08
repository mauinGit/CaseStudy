// Package service holds every business rule and authorization decision.
// Handlers only translate HTTP <-> service calls.
package service

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

// TokenManager issues and verifies HS256 access tokens.
type TokenManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

func NewTokenManager(secret, issuer string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), issuer: issuer, ttl: ttl, now: time.Now}
}

// Claims carried by the access token. Role is informational for clients only:
// authorization always re-reads role and division from the database.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// Issue creates a signed access token for the user.
func (m *TokenManager) Issue(u *domain.User) (string, time.Time, error) {
	now := m.now()
	exp := now.Add(m.ttl)
	claims := Claims{
		Role: string(u.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID.String(),
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Parse verifies signature, algorithm, issuer and expiry, then returns the
// user ID from the subject claim.
func (m *TokenManager) Parse(raw string) (uuid.UUID, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), // blocks "alg":"none" & alg confusion
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
		jwt.WithTimeFunc(m.now),
	)
	var claims Claims
	if _, err := parser.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) {
		return m.secret, nil
	}); err != nil {
		return uuid.Nil, domain.ErrInvalidToken
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, domain.ErrInvalidToken
	}
	return id, nil
}

// TTL returns the configured token lifetime.
func (m *TokenManager) TTL() time.Duration { return m.ttl }
