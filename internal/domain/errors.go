// Package domain holds core entities and business errors. It has no
// dependency on HTTP or the database.
package domain

// Error is a business error with a stable, machine-readable code.
// The apperror package maps each code to an HTTP status.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Is makes errors.Is match on Code, so WithMessage variants still match
// their sentinel (errors.Is(err, ErrNotFound)).
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// WithMessage returns a copy of the error with a more specific message.
func (e *Error) WithMessage(msg string) *Error {
	return &Error{Code: e.Code, Message: msg}
}

// Generic errors. Feature-specific errors are declared next to the feature.
var (
	ErrNotFound     = &Error{Code: "NOT_FOUND", Message: "Data tidak ditemukan"}
	ErrUnauthorized = &Error{Code: "UNAUTHORIZED", Message: "Autentikasi diperlukan"}
	ErrForbidden    = &Error{Code: "FORBIDDEN", Message: "Anda tidak memiliki akses untuk aksi ini"}
	ErrConflict     = &Error{Code: "CONFLICT", Message: "Data bentrok dengan data yang sudah ada"}
)
