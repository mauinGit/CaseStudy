package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/repository"
)

// EventService manages events and registrations.
type EventService struct {
	store *repository.Store
	now   func() time.Time
}

func NewEventService(store *repository.Store) *EventService {
	return &EventService{store: store, now: time.Now}
}

// CreateEventInput is a new event.
type CreateEventInput struct {
	Title       string
	Description string
	DivisionID  *uuid.UUID // nil = general event (Lead only)
	Location    string
	StartAt     time.Time
	EndAt       time.Time
	Quota       int
}

// EventPatch holds editable event fields; nil means unchanged.
type EventPatch struct {
	Title       *string
	Description *string
	Location    *string
	StartAt     *time.Time
	EndAt       *time.Time
	Quota       *int
}

// canManageEvent: Lead manages every event; a Division Head manages events of
// their own division only (general events are Lead-only); Members manage none.
func canManageEvent(a domain.Actor, divisionID *uuid.UUID) error {
	switch {
	case a.IsLead():
		return nil
	case a.IsDivisionHead():
		if divisionID == nil {
			return domain.ErrGeneralEventLeadOnly
		}
		if !a.InDivision(divisionID) {
			return domain.ErrEventOtherDivision
		}
		return nil
	default:
		return domain.ErrMemberCannotManageEvt
	}
}

// ListEvents returns events. Every authenticated user can browse events.
func (s *EventService) ListEvents(ctx context.Context, f domain.EventFilter) (domain.Page[*domain.Event], error) {
	return s.store.Events.List(ctx, f)
}

// GetEvent returns one event.
func (s *EventService) GetEvent(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
	return s.store.Events.GetActive(ctx, id)
}

// CreateEvent creates an event. A Division Head's event defaults to (and is
// restricted to) their own division.
func (s *EventService) CreateEvent(ctx context.Context, actor domain.Actor, in CreateEventInput) (*domain.Event, error) {
	if actor.IsDivisionHead() && in.DivisionID == nil {
		in.DivisionID = actor.DivisionID
	}
	if err := canManageEvent(actor, in.DivisionID); err != nil {
		return nil, err
	}
	if !in.EndAt.After(in.StartAt) {
		return nil, domain.ErrInvalidTimeRange
	}
	e := &domain.Event{
		Title:       strings.TrimSpace(in.Title),
		Description: strings.TrimSpace(in.Description),
		DivisionID:  in.DivisionID,
		Location:    strings.TrimSpace(in.Location),
		StartAt:     in.StartAt.UTC(),
		EndAt:       in.EndAt.UTC(),
		Quota:       in.Quota,
		CreatedBy:   actor.UserID,
	}
	id, err := s.store.Events.Create(ctx, e)
	if err != nil {
		return nil, err
	}
	return s.store.Events.GetActive(ctx, id)
}

type quotaSnapshot struct {
	Quota           int `json:"quota"`
	RegisteredCount int `json:"registered_count"`
}

// UpdateEvent edits an event. A quota change is validated against the current
// number of registrations on the locked row and audited in the same
// transaction.
func (s *EventService) UpdateEvent(ctx context.Context, actor domain.Actor, id uuid.UUID, p EventPatch) (*domain.Event, error) {
	if !canManageMembers(actor) {
		return nil, domain.ErrMemberCannotManageEvt
	}
	var out *domain.Event
	err := s.store.InTx(ctx, func(r repository.Repos) error {
		// Locking the row serializes this update with concurrent registrations,
		// so registered_count cannot change between the check and the write.
		e, err := r.Events.GetActiveForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := canManageEvent(actor, e.DivisionID); err != nil {
			return err
		}

		oldQuota := e.Quota
		if p.Title != nil {
			e.Title = strings.TrimSpace(*p.Title)
		}
		if p.Description != nil {
			e.Description = strings.TrimSpace(*p.Description)
		}
		if p.Location != nil {
			e.Location = strings.TrimSpace(*p.Location)
		}
		if p.StartAt != nil {
			e.StartAt = p.StartAt.UTC()
		}
		if p.EndAt != nil {
			e.EndAt = p.EndAt.UTC()
		}
		if p.Quota != nil {
			e.Quota = *p.Quota
		}
		if !e.EndAt.After(e.StartAt) {
			return domain.ErrInvalidTimeRange
		}
		if e.Quota < e.RegisteredCount {
			return domain.ErrQuotaBelowRegistered
		}
		if err := r.Events.Update(ctx, e); err != nil {
			return err
		}

		if e.Quota != oldQuota {
			if err := r.Audit.Insert(ctx, domain.AuditEntry{
				ActorID:  &actor.UserID,
				Action:   domain.AuditEventQuotaChanged,
				Entity:   domain.EntityEvent,
				EntityID: e.ID.String(),
				OldValue: quotaSnapshot{Quota: oldQuota, RegisteredCount: e.RegisteredCount},
				NewValue: quotaSnapshot{Quota: e.Quota, RegisteredCount: e.RegisteredCount},
				IP:       actor.IP,
			}); err != nil {
				return err
			}
		}
		out, err = r.Events.GetActive(ctx, e.ID)
		return err
	})
	return out, err
}

// DeleteEvent soft-deletes an event.
func (s *EventService) DeleteEvent(ctx context.Context, actor domain.Actor, id uuid.UUID) error {
	if !canManageMembers(actor) {
		return domain.ErrMemberCannotManageEvt
	}
	return s.store.InTx(ctx, func(r repository.Repos) error {
		e, err := r.Events.GetActiveForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := canManageEvent(actor, e.DivisionID); err != nil {
			return err
		}
		return r.Events.SoftDelete(ctx, id)
	})
}

// Register registers the actor for an event.
//
// Inside one transaction: (1) take a slot with a single conditional UPDATE
// (registered_count < quota), which PostgreSQL serializes on the event row,
// then (2) insert the registration, protected by UNIQUE(event_id, user_id).
// If (2) finds an active registration the whole transaction rolls back, so
// the slot taken in (1) is released and ALREADY_REGISTERED is returned.
func (s *EventService) Register(ctx context.Context, actor domain.Actor, eventID uuid.UUID) (*domain.Registration, error) {
	e, err := s.store.Events.GetActive(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if e.StatusAt(s.now()) == domain.EventFinished {
		return nil, domain.ErrEventEnded
	}
	// Fast path for a clear error (e.g. already registered on a full event).
	// Correctness does not depend on it: see the transaction below.
	reg, err := s.store.Registrations.Get(ctx, eventID, actor.UserID)
	switch {
	case err == nil && reg.Status == domain.RegistrationRegistered:
		return nil, domain.ErrAlreadyRegistered
	case err != nil && !errors.Is(err, domain.ErrRegistrationNotFound):
		return nil, err
	}

	var out *domain.Registration
	err = s.store.InTx(ctx, func(r repository.Repos) error {
		ok, err := r.Events.TakeSlot(ctx, eventID)
		if err != nil {
			return err
		}
		if !ok {
			return s.slotRefusal(ctx, r, eventID)
		}
		out, err = r.Registrations.Activate(ctx, eventID, actor.UserID)
		return err
	})
	return out, err
}

// slotRefusal explains why TakeSlot did not get a slot.
func (s *EventService) slotRefusal(ctx context.Context, r repository.Repos, eventID uuid.UUID) error {
	e, err := r.Events.GetActive(ctx, eventID)
	if err != nil {
		return err
	}
	if e.StatusAt(s.now()) == domain.EventFinished {
		return domain.ErrEventEnded
	}
	return domain.ErrQuotaFull
}

// CancelRegistration cancels the actor's registration and gives the slot
// back. Not allowed after the event ended or after check-in.
func (s *EventService) CancelRegistration(ctx context.Context, actor domain.Actor, eventID uuid.UUID) error {
	return s.store.InTx(ctx, func(r repository.Repos) error {
		// Lock order: event row, then registration row (same as Register).
		e, err := r.Events.GetActiveForUpdate(ctx, eventID)
		if err != nil {
			return err
		}
		if e.StatusAt(s.now()) == domain.EventFinished {
			return domain.ErrEventEnded
		}
		reg, err := r.Registrations.GetForUpdate(ctx, eventID, actor.UserID)
		if err != nil {
			return err
		}
		if reg.Status != domain.RegistrationRegistered {
			return domain.ErrRegistrationNotFound
		}
		attended, err := r.Registrations.HasAttendance(ctx, eventID, actor.UserID)
		if err != nil {
			return err
		}
		if attended {
			return domain.ErrAlreadyCheckedIn
		}
		if err := r.Registrations.Cancel(ctx, eventID, actor.UserID); err != nil {
			return err
		}
		return r.Events.ReleaseSlot(ctx, eventID)
	})
}

// ListParticipants lists an event's registrations, scoped by role: Lead sees
// everyone; a Division Head sees everyone on their division's events and only
// their own division's members elsewhere; a Member sees only themselves.
func (s *EventService) ListParticipants(ctx context.Context, actor domain.Actor, f domain.ParticipantFilter) (domain.Page[*domain.Participant], error) {
	e, err := s.store.Events.GetActive(ctx, f.EventID)
	if err != nil {
		return domain.Page[*domain.Participant]{}, err
	}
	f.DivisionID, f.OnlyUserID = nil, nil
	switch {
	case actor.IsLead():
	case actor.IsDivisionHead() && actor.DivisionID != nil:
		if !actor.InDivision(e.DivisionID) {
			f.DivisionID = actor.DivisionID
		}
	default:
		f.OnlyUserID = &actor.UserID
	}
	return s.store.Registrations.ListParticipants(ctx, f)
}
