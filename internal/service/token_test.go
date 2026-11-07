package service

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

const testSecret = "unit-test-secret-unit-test-secret-0123456789"

func testUser() *domain.User {
	return &domain.User{ID: uuid.New(), Role: domain.RoleMember}
}

func TestTokenIssueAndParse(t *testing.T) {
	m := NewTokenManager(testSecret, "issuer", time.Hour)
	u := testUser()

	tok, exp, err := m.Issue(u)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), exp, 5*time.Second)

	id, err := m.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, u.ID, id)
}

func TestTokenRejected(t *testing.T) {
	m := NewTokenManager(testSecret, "issuer", time.Hour)
	u := testUser()
	valid, _, err := m.Issue(u)
	require.NoError(t, err)

	expired := NewTokenManager(testSecret, "issuer", time.Hour)
	expired.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	expiredTok, _, _ := expired.Issue(u)

	otherSecret, _, _ := NewTokenManager("another-secret-another-secret-0123456789", "issuer", time.Hour).Issue(u)
	otherIssuer, _, _ := NewTokenManager(testSecret, "evil", time.Hour).Issue(u)

	// "alg": "none" token must never be accepted.
	noneTok, err := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: u.ID.String(), Issuer: "issuer",
			IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	cases := map[string]string{
		"garbage":      "not-a-jwt",
		"tampered":     valid[:len(valid)-2] + "xx",
		"expired":      expiredTok,
		"other secret": otherSecret,
		"other issuer": otherIssuer,
		"alg none":     noneTok,
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := m.Parse(tok)
			assert.ErrorIs(t, err, domain.ErrInvalidToken)
		})
	}
}
