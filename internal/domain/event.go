package domain

import (
	"time"

	"github.com/google/uuid"
)

// Event is a GDGoC activity members can register for. DivisionID nil means a
// general event open to every division.
type Event struct {
	ID              uuid.UUID
	Title           string
	Description     string
	DivisionID      *uuid.UUID
	DivisionName    *string
	Location        string
	StartAt         time.Time
	EndAt           time.Time
	Quota           int
	RegisteredCount int
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

// EventStatus is derived from the event's schedule.
type EventStatus string

const (
	EventUpcoming EventStatus = "upcoming"
	EventOngoing  EventStatus = "ongoing"
	EventFinished EventStatus = "finished"
)

// StatusAt returns the event status at time now. Start and end are inclusive,
// matching the check-in window start_at <= now <= end_at.
func (e *Event) StatusAt(now time.Time) EventStatus {
	switch {
	case now.Before(e.StartAt):
		return EventUpcoming
	case now.After(e.EndAt):
		return EventFinished
	default:
		return EventOngoing
	}
}

// Remaining returns the number of free slots.
func (e *Event) Remaining() int { return max(e.Quota-e.RegisteredCount, 0) }

// EventSort is a whitelisted sort column for event listing.
type EventSort string

const (
	EventSortStartAt   EventSort = "start_at"
	EventSortCreatedAt EventSort = "created_at"
	EventSortTitle     EventSort = "title"
)

// EventFilter describes an event list query.
type EventFilter struct {
	DivisionID *uuid.UUID
	Status     *EventStatus
	Search     string
	Sort       EventSort
	Desc       bool
	Page       int
	Limit      int
}

func (f EventFilter) Offset() int { return (f.Page - 1) * f.Limit }

// RegistrationStatus is the state of an event registration.
type RegistrationStatus string

const (
	RegistrationRegistered RegistrationStatus = "registered"
	RegistrationCancelled  RegistrationStatus = "cancelled"
)

// Registration is a member's registration for an event.
type Registration struct {
	ID        uuid.UUID
	EventID   uuid.UUID
	UserID    uuid.UUID
	Status    RegistrationStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Participant is a registration joined with the participant's profile.
type Participant struct {
	Registration
	UserName     string
	UserEmail    string
	DivisionID   *uuid.UUID
	DivisionName *string
}

// ParticipantFilter describes a participant list query. Scope fields are set
// by the service from the actor's role.
type ParticipantFilter struct {
	EventID    uuid.UUID
	Status     *RegistrationStatus
	DivisionID *uuid.UUID // only participants from this division
	OnlyUserID *uuid.UUID // only this participant
	Page       int
	Limit      int
}

func (f ParticipantFilter) Offset() int { return (f.Page - 1) * f.Limit }

// Event errors.
var (
	ErrEventNotFound         = ErrNotFound.WithMessage("Event tidak ditemukan")
	ErrRegistrationNotFound  = ErrNotFound.WithMessage("Anda belum terdaftar di event ini")
	ErrQuotaFull             = &Error{Code: "QUOTA_FULL", Message: "Kuota event sudah penuh"}
	ErrAlreadyRegistered     = &Error{Code: "ALREADY_REGISTERED", Message: "Anda sudah terdaftar di event ini"}
	ErrEventEnded            = &Error{Code: "EVENT_ENDED", Message: "Event sudah selesai"}
	ErrAlreadyCheckedIn      = &Error{Code: "ALREADY_CHECKED_IN", Message: "Pendaftaran tidak dapat dibatalkan karena Anda sudah check-in"}
	ErrQuotaBelowRegistered  = &Error{Code: "QUOTA_BELOW_REGISTERED", Message: "Kuota tidak boleh lebih kecil dari jumlah peserta terdaftar"}
	ErrInvalidTimeRange      = &Error{Code: "INVALID_TIME_RANGE", Message: "Waktu selesai harus setelah waktu mulai"}
	ErrGeneralEventLeadOnly  = &Error{Code: "FORBIDDEN", Message: "Hanya Lead yang dapat mengelola event umum"}
	ErrEventOtherDivision    = &Error{Code: "FORBIDDEN", Message: "Anda hanya dapat mengelola event divisi Anda sendiri"}
	ErrMemberCannotManageEvt = &Error{Code: "FORBIDDEN", Message: "Hanya Lead dan Division Head yang dapat mengelola event"}
)
