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

type AttendanceRepo struct {
	db DBTX
}

var errNoAttendance = errors.New("attendance not found")

// Get returns the attendance of a user for an event; ok is false when the
// user has not checked in.
func (r *AttendanceRepo) Get(ctx context.Context, eventID, userID uuid.UUID) (*domain.Attendance, bool, error) {
	var a domain.Attendance
	err := r.db.QueryRow(ctx,
		`SELECT id, event_id, user_id, checked_in_at FROM attendances WHERE event_id = $1 AND user_id = $2`,
		eventID, userID).Scan(&a.ID, &a.EventID, &a.UserID, &a.CheckedInAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get attendance: %w", err)
	}
	return &a, true, nil
}

// Insert records a check-in idempotently with INSERT ... ON CONFLICT DO
// NOTHING. created reports whether this call inserted the row; when a row
// already existed (e.g. a concurrent duplicate request won), the existing row
// is returned with created=false.
func (r *AttendanceRepo) Insert(ctx context.Context, eventID, userID uuid.UUID) (a *domain.Attendance, created bool, err error) {
	var row domain.Attendance
	err = r.db.QueryRow(ctx, `
INSERT INTO attendances (event_id, user_id) VALUES ($1, $2)
ON CONFLICT ON CONSTRAINT attendances_event_user_key DO NOTHING
RETURNING id, event_id, user_id, checked_in_at`, eventID, userID).
		Scan(&row.ID, &row.EventID, &row.UserID, &row.CheckedInAt)
	switch {
	case err == nil:
		return &row, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		existing, ok, err := r.Get(ctx, eventID, userID)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, errNoAttendance
		}
		return existing, false, nil
	default:
		if code, constraint := pgErrCode(err); code == pgFKViolation && constraint == "attendances_registration_fk" {
			return nil, false, domain.ErrNotRegistered
		}
		return nil, false, fmt.Errorf("insert attendance: %w", err)
	}
}

// List returns a page of an event's attendances joined with the participants'
// profiles, earliest check-in first.
func (r *AttendanceRepo) List(ctx context.Context, f domain.AttendanceFilter) (domain.Page[*domain.AttendanceRecord], error) {
	where := []string{"a.event_id = $1"}
	args := []any{f.EventID}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.DivisionID != nil {
		where = append(where, "u.division_id = "+arg(*f.DivisionID))
	}
	if f.OnlyUserID != nil {
		where = append(where, "a.user_id = "+arg(*f.OnlyUserID))
	}
	from := ` FROM attendances a JOIN users u ON u.id = a.user_id LEFT JOIN divisions d ON d.id = u.division_id`
	cond := " WHERE " + strings.Join(where, " AND ")

	page := domain.Page[*domain.AttendanceRecord]{Items: []*domain.AttendanceRecord{}, Page: f.Page, Limit: f.Limit}
	if err := r.db.QueryRow(ctx, `SELECT count(*)`+from+cond, args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count attendances: %w", err)
	}
	if page.Total == 0 || f.Offset() >= page.Total {
		return page, nil
	}

	query := `SELECT a.id, a.event_id, a.user_id, a.checked_in_at, u.name, u.email, u.division_id, d.name` +
		from + cond + fmt.Sprintf(" ORDER BY a.checked_in_at ASC, a.id ASC LIMIT %s OFFSET %s", arg(f.Limit), arg(f.Offset()))
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("list attendances: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rec domain.AttendanceRecord
		if err := rows.Scan(&rec.ID, &rec.EventID, &rec.UserID, &rec.CheckedInAt,
			&rec.UserName, &rec.UserEmail, &rec.DivisionID, &rec.DivisionName); err != nil {
			return page, fmt.Errorf("list attendances: %w", err)
		}
		page.Items = append(page.Items, &rec)
	}
	return page, wrap("list attendances", rows.Err())
}
