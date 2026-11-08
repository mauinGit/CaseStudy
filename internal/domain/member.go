package domain

import "github.com/google/uuid"

// Member management errors.
var (
	ErrEmailTaken          = &Error{Code: "EMAIL_TAKEN", Message: "Email sudah digunakan anggota lain"}
	ErrDivisionNotFound    = &Error{Code: "DIVISION_NOT_FOUND", Message: "Divisi tidak ditemukan"}
	ErrDivisionRequired    = &Error{Code: "DIVISION_REQUIRED", Message: "Divisi wajib diisi untuk role selain Lead"}
	ErrPasswordTooLong     = &Error{Code: "PASSWORD_TOO_LONG", Message: "Password maksimal 72 byte"}
	ErrCannotChangeOwnRole = &Error{Code: "CANNOT_CHANGE_OWN_ROLE", Message: "Anda tidak dapat mengubah role Anda sendiri"}
	ErrCannotDeleteSelf    = &Error{Code: "CANNOT_DELETE_SELF", Message: "Anda tidak dapat menghapus akun Anda sendiri"}
)

// MemberSort is a whitelisted sort column for member listing.
type MemberSort string

const (
	MemberSortName      MemberSort = "name"
	MemberSortEmail     MemberSort = "email"
	MemberSortCreatedAt MemberSort = "created_at"
)

// MemberFilter describes a member list query. Scope fields (OnlyUserID,
// DivisionID) are set by the service from the actor's role, never directly
// from user input.
type MemberFilter struct {
	Role       *Role
	DivisionID *uuid.UUID
	OnlyUserID *uuid.UUID
	Search     string
	Sort       MemberSort
	Desc       bool
	Page       int
	Limit      int
}

// Offset returns the SQL offset for the requested page.
func (f MemberFilter) Offset() int { return (f.Page - 1) * f.Limit }

// Page is a page of results with its total count.
type Page[T any] struct {
	Items []T
	Total int
	Page  int
	Limit int
}

// TotalPages returns the number of pages for the total count.
func (p Page[T]) TotalPages() int {
	if p.Limit <= 0 {
		return 0
	}
	return (p.Total + p.Limit - 1) / p.Limit
}
