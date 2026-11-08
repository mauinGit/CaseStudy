// Package apperror maps domain errors to HTTP responses with one consistent
// JSON shape. Internal details (stack traces, SQL errors) are never exposed.
package apperror

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

// Body is the JSON envelope for every error response:
//
//	{ "error": { "code": "QUOTA_FULL", "message": "Kuota event sudah penuh" } }
type Body struct {
	Error Detail `json:"error"`
}

type Detail struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

// FieldError describes one invalid input field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError is returned when request input fails validation.
type ValidationError struct {
	Message string
	Fields  []FieldError
}

func (e *ValidationError) Error() string { return "validation failed: " + e.Message }

const (
	CodeValidation = "VALIDATION_ERROR"
	CodeInternal   = "INTERNAL_ERROR"
	CodeRouteNF    = "ROUTE_NOT_FOUND"
	CodeMethodNA   = "METHOD_NOT_ALLOWED"

	msgInternal = "Terjadi kesalahan pada server"
)

// statusByCode is the single place that maps domain error codes to HTTP
// status codes. Unknown codes fall back to 500.
var statusByCode = map[string]int{
	domain.ErrNotFound.Code:     http.StatusNotFound,
	domain.ErrUnauthorized.Code: http.StatusUnauthorized,
	domain.ErrForbidden.Code:    http.StatusForbidden,
	domain.ErrConflict.Code:     http.StatusConflict,

	// auth
	domain.ErrInvalidCredentials.Code:    http.StatusUnauthorized,
	domain.ErrInvalidToken.Code:          http.StatusUnauthorized,
	domain.ErrTooManyRequests.Code:       http.StatusTooManyRequests,
	domain.ErrGoogleDisabled.Code:        http.StatusServiceUnavailable,
	domain.ErrInvalidOAuthState.Code:     http.StatusBadRequest,
	domain.ErrOAuthFailed.Code:           http.StatusUnauthorized,
	domain.ErrEmailNotVerified.Code:      http.StatusForbidden,
	domain.ErrEmailDomainNotAllowed.Code: http.StatusForbidden,
	domain.ErrAccountNotRegistered.Code:  http.StatusForbidden,

	// members
	domain.ErrEmailTaken.Code:          http.StatusConflict,
	domain.ErrDivisionNotFound.Code:    http.StatusUnprocessableEntity,
	domain.ErrDivisionRequired.Code:    http.StatusBadRequest,
	domain.ErrPasswordTooLong.Code:     http.StatusBadRequest,
	domain.ErrCannotChangeOwnRole.Code: http.StatusForbidden,
	domain.ErrCannotDeleteSelf.Code:    http.StatusForbidden,
}

// StatusFor returns the HTTP status for a domain error code (500 if unknown).
func StatusFor(code string) int {
	if s, ok := statusByCode[code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// Respond writes err as a JSON error response and aborts the request.
func Respond(c *gin.Context, err error) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		Abort(c, http.StatusBadRequest, Detail{Code: CodeValidation, Message: ve.Message, Fields: ve.Fields})
		return
	}

	var de *domain.Error
	if errors.As(err, &de) {
		status := StatusFor(de.Code)
		if status == http.StatusInternalServerError {
			logInternal(c, err)
		}
		Abort(c, status, Detail{Code: de.Code, Message: de.Message})
		return
	}

	logInternal(c, err)
	Abort(c, http.StatusInternalServerError, Detail{Code: CodeInternal, Message: msgInternal})
}

// Abort writes an explicit error detail with the given status.
func Abort(c *gin.Context, status int, d Detail) {
	c.AbortWithStatusJSON(status, Body{Error: d})
}

// Internal writes a generic 500 response without leaking details.
func Internal(c *gin.Context) {
	Abort(c, http.StatusInternalServerError, Detail{Code: CodeInternal, Message: msgInternal})
}

func logInternal(c *gin.Context, err error) {
	slog.ErrorContext(c.Request.Context(), "internal error",
		"error", err,
		"request_id", c.GetString("request_id"),
		"method", c.Request.Method,
		"path", c.FullPath(),
	)
}
