package domain

import "github.com/google/uuid"

// Actor is the authenticated user performing a request. Role and division are
// loaded from the database on every request (not trusted from the JWT), so a
// role change or soft delete takes effect immediately.
type Actor struct {
	UserID     uuid.UUID
	Role       Role
	DivisionID *uuid.UUID
	IP         string
}

func (a Actor) IsLead() bool         { return a.Role == RoleLead }
func (a Actor) IsDivisionHead() bool { return a.Role == RoleDivisionHead }

// InDivision reports whether the actor belongs to the given division.
func (a Actor) InDivision(divisionID *uuid.UUID) bool {
	return a.DivisionID != nil && divisionID != nil && *a.DivisionID == *divisionID
}

// Authentication errors.
var (
	ErrInvalidCredentials    = &Error{Code: "INVALID_CREDENTIALS", Message: "Email atau password salah"}
	ErrInvalidToken          = &Error{Code: "INVALID_TOKEN", Message: "Token tidak valid atau sudah kedaluwarsa"}
	ErrTooManyRequests       = &Error{Code: "TOO_MANY_REQUESTS", Message: "Terlalu banyak percobaan, silakan coba lagi nanti"}
	ErrGoogleDisabled        = &Error{Code: "GOOGLE_OAUTH_DISABLED", Message: "Login Google belum dikonfigurasi"}
	ErrInvalidOAuthState     = &Error{Code: "INVALID_OAUTH_STATE", Message: "Sesi login Google tidak valid atau kedaluwarsa, ulangi login"}
	ErrOAuthFailed           = &Error{Code: "OAUTH_FAILED", Message: "Gagal memverifikasi akun Google"}
	ErrEmailNotVerified      = &Error{Code: "EMAIL_NOT_VERIFIED", Message: "Email Google belum terverifikasi"}
	ErrEmailDomainNotAllowed = &Error{Code: "EMAIL_DOMAIN_NOT_ALLOWED", Message: "Domain email tidak diizinkan untuk login"}
	ErrAccountNotRegistered  = &Error{Code: "ACCOUNT_NOT_REGISTERED", Message: "Akun belum terdaftar sebagai anggota GDGoC UNSRI"}
)
