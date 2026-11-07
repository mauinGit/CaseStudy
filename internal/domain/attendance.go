package domain

import (
	"time"

	"github.com/google/uuid"
)

// Attendance is a participant's check-in to an event.
type Attendance struct {
	ID          uuid.UUID
	EventID     uuid.UUID
	UserID      uuid.UUID
	CheckedInAt time.Time
}

// AttendanceRecord is an attendance joined with the participant's profile.
type AttendanceRecord struct {
	Attendance
	UserName     string
	UserEmail    string
	DivisionID   *uuid.UUID
	DivisionName *string
}

// AttendanceFilter describes an attendance list query. Scope fields are set
// by the service from the actor's role.
type AttendanceFilter struct {
	EventID    uuid.UUID
	DivisionID *uuid.UUID
	OnlyUserID *uuid.UUID
	Page       int
	Limit      int
}

func (f AttendanceFilter) Offset() int { return (f.Page - 1) * f.Limit }

// Attendance errors.
var (
	ErrCheckInNotOpen = &Error{Code: "CHECK_IN_NOT_OPEN", Message: "Check-in belum dibuka, event belum dimulai"}
	ErrNotRegistered  = &Error{Code: "NOT_REGISTERED", Message: "Hanya peserta terdaftar yang dapat check-in"}
)
