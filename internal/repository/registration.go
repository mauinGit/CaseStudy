package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

type RegistrationRepo struct {
	db DBTX
}

const registrationCols = `r.id, r.event_id, r.user_id, r.status::text, r.created_at, r.updated_at`

func scanRegistration(row pgx.Row, extra ...any) (*domain.Registration, error) {
	var reg domain.Registration
	var status string
	dest := append([]any{&reg.ID, &reg.EventID, &reg.UserID, &status, &reg.CreatedAt, &reg.UpdatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRegistrationNotFound
		}
		return nil, err
	}
	reg.Status = domain.RegistrationStatus(status)
	return &reg, nil
}

// Get returns the registration of a user for an event (any status).
func (r *RegistrationRepo) Get(ctx context.Context, eventID, userID uuid.UUID) (*domain.Registration, error) {
	reg, err := scanRegistration(r.db.QueryRow(ctx,
		`SELECT `+registrationCols+` FROM event_registrations r WHERE r.event_id = $1 AND r.user_id = $2`,
		eventID, userID))
	return reg, wrap("get registration", err)
}

// Activate creates the registration, or re-activates a cancelled one. When an
// active registration already exists nothing is written and
// ErrAlreadyRegistered is returned, so the caller's transaction (including
// the slot taken just before) is rolled back.
func (r *RegistrationRepo) Activate(ctx context.Context, eventID, userID uuid.UUID) (*domain.Registration, error) {
	reg, err := scanRegistration(r.db.QueryRow(ctx, `
INSERT INTO event_registrations AS r (event_id, user_id, status)
VALUES ($1, $2, 'registered')
ON CONFLICT ON CONSTRAINT event_registrations_event_user_key
DO UPDATE SET status = 'registered' WHERE r.status = 'cancelled'
RETURNING `+registrationCols, eventID, userID))
	switch {
	case errors.Is(err, domain.ErrRegistrationNotFound):
		// The conflicting row is already active: double registration.
		return nil, domain.ErrAlreadyRegistered
	case isUniqueViolation(err):
		return nil, domain.ErrAlreadyRegistered
	case err != nil:
		return nil, fmt.Errorf("activate registration: %w", err)
	}
	return reg, nil
}

// Cancel marks an active registration as cancelled. Returns
// ErrRegistrationNotFound when there is no active registration.
func (r *RegistrationRepo) Cancel(ctx context.Context, eventID, userID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `
UPDATE event_registrations SET status = 'cancelled'
WHERE event_id = $1 AND user_id = $2 AND status = 'registered'`, eventID, userID)
	if err != nil {
		return fmt.Errorf("cancel registration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRegistrationNotFound
	}
	return nil
}

// HasAttendance reports whether the user has checked in to the event.
func (r *RegistrationRepo) HasAttendance(ctx context.Context, eventID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM attendances WHERE event_id = $1 AND user_id = $2)`, eventID, userID).Scan(&ok)
	return ok, wrap("has attendance", err)
}

// CancelUpcomingForUser cancels a user's active registrations for events that
// have not ended (and that they have not checked in to) and gives the slots
// back. Used when a member is soft-deleted. Event rows are locked first, in id
// order, matching the lock order of registration (event row, then
// registration row) so the two cannot deadlock.
func (r *RegistrationRepo) CancelUpcomingForUser(ctx context.Context, userID uuid.UUID) (int, error) {
	rows, err := r.db.Query(ctx, `
SELECT e.id FROM events e
JOIN event_registrations r ON r.event_id = e.id
WHERE r.user_id = $1 AND r.status = 'registered' AND e.end_at > now()
  AND NOT EXISTS (SELECT 1 FROM attendances a WHERE a.event_id = r.event_id AND a.user_id = r.user_id)
ORDER BY e.id
FOR UPDATE OF e`, userID)
	if err != nil {
		return 0, fmt.Errorf("lock user events: %w", err)
	}
	eventIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, fmt.Errorf("lock user events: %w", err)
	}
	if len(eventIDs) == 0 {
		return 0, nil
	}

	tag, err := r.db.Exec(ctx, `
WITH cancelled AS (
    UPDATE event_registrations SET status = 'cancelled'
    WHERE user_id = $1 AND event_id = ANY($2) AND status = 'registered'
    RETURNING event_id
)
UPDATE events e SET registered_count = e.registered_count - 1
FROM cancelled c WHERE e.id = c.event_id`, userID, eventIDs)
	if err != nil {
		return 0, fmt.Errorf("cancel user registrations: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ListParticipants returns a page of registrations for an event joined with
// the participants' profiles.
func (r *RegistrationRepo) ListParticipants(ctx context.Context, f domain.ParticipantFilter) (domain.Page[*domain.Participant], error) {
	where := []string{"r.event_id = $1"}
	args := []any{f.EventID}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Status != nil {
		where = append(where, "r.status = "+arg(string(*f.Status))+"::registration_status")
	}
	if f.DivisionID != nil {
		where = append(where, "u.division_id = "+arg(*f.DivisionID))
	}
	if f.OnlyUserID != nil {
		where = append(where, "r.user_id = "+arg(*f.OnlyUserID))
	}
	from := ` FROM event_registrations r JOIN users u ON u.id = r.user_id LEFT JOIN divisions d ON d.id = u.division_id`
	cond := " WHERE " + strings.Join(where, " AND ")

	page := domain.Page[*domain.Participant]{Items: []*domain.Participant{}, Page: f.Page, Limit: f.Limit}
	if err := r.db.QueryRow(ctx, `SELECT count(*)`+from+cond, args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count participants: %w", err)
	}
	if page.Total == 0 || f.Offset() >= page.Total {
		return page, nil
	}

	query := `SELECT ` + registrationCols + `, u.name, u.email, u.division_id, d.name` + from + cond +
		fmt.Sprintf(" ORDER BY r.created_at ASC, r.id ASC LIMIT %s OFFSET %s", arg(f.Limit), arg(f.Offset()))
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p domain.Participant
		reg, err := scanRegistration(rows, &p.UserName, &p.UserEmail, &p.DivisionID, &p.DivisionName)
		if err != nil {
			return page, fmt.Errorf("list participants: %w", err)
		}
		p.Registration = *reg
		page.Items = append(page.Items, &p)
	}
	return page, wrap("list participants", rows.Err())
}

// GetForUpdate returns the registration of a user for an event and locks it
// until the transaction ends. Must be called inside InTx.
func (r *RegistrationRepo) GetForUpdate(ctx context.Context, eventID, userID uuid.UUID) (*domain.Registration, error) {
	reg, err := scanRegistration(r.db.QueryRow(ctx,
		`SELECT `+registrationCols+` FROM event_registrations r WHERE r.event_id = $1 AND r.user_id = $2 FOR UPDATE`,
		eventID, userID))
	return reg, wrap("lock registration", err)
}

// GetForShare returns the registration of a user for an event with a shared
// row lock: concurrent check-ins proceed together, while a concurrent cancel
// (which takes FOR UPDATE) waits. Must be called inside InTx.
func (r *RegistrationRepo) GetForShare(ctx context.Context, eventID, userID uuid.UUID) (*domain.Registration, error) {
	reg, err := scanRegistration(r.db.QueryRow(ctx,
		`SELECT `+registrationCols+` FROM event_registrations r WHERE r.event_id = $1 AND r.user_id = $2 FOR SHARE`,
		eventID, userID))
	return reg, wrap("share-lock registration", err)
}
