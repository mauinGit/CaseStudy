package domain

import (
	"time"

	"github.com/google/uuid"
)

// Role is a user's role inside GDGoC UNSRI.
type Role string

const (
	RoleLead         Role = "lead"
	RoleDivisionHead Role = "division_head"
	RoleMember       Role = "member"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleLead, RoleDivisionHead, RoleMember:
		return true
	}
	return false
}

// User is a Core Team member account.
type User struct {
	ID           uuid.UUID
	Name         string
	Email        string
	PasswordHash *string
	GoogleSub    *string
	Role         Role
	DivisionID   *uuid.UUID
	Phone        *string
	Bio          *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

// Division is an organisational unit (e.g. Back-End, Mobile).
type Division struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}
