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

type EventRepo struct {
	db DBTX
}

const eventSelect = `
SELECT e.id, e.title, e.description, e.division_id, d.name, e.location, e.start_at, e.end_at,
       e.quota, e.registered_count, e.created_by, e.created_at, e.updated_at, e.deleted_at
FROM events e
LEFT JOIN divisions d ON d.id = e.division_id`

func scanEvent(row pgx.Row) (*domain.Event, error) {
	var e domain.Event
	err := row.Scan(&e.ID, &e.Title, &e.Description, &e.DivisionID, &e.DivisionName, &e.Location,
		&e.StartAt, &e.EndAt, &e.Quota, &e.RegisteredCount, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &e.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrEventNotFound
		}
		return nil, err
	}
	return &e, nil
}

// GetActive returns a non-deleted event.
func (r *EventRepo) GetActive(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
	e, err := scanEvent(r.db.QueryRow(ctx, eventSelect+` WHERE e.id = $1 AND e.deleted_at IS NULL`, id))
	return e, wrap("get event", err)
}

// GetActiveForUpdate returns a non-deleted event and locks its row until the
// transaction ends. Must be called inside InTx.
func (r *EventRepo) GetActiveForUpdate(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
	e, err := scanEvent(r.db.QueryRow(ctx,
		eventSelect+` WHERE e.id = $1 AND e.deleted_at IS NULL FOR UPDATE OF e`, id))
	return e, wrap("lock event", err)
}

var eventSortColumns = map[domain.EventSort]string{
	domain.EventSortStartAt:   "e.start_at",
	domain.EventSortCreatedAt: "e.created_at",
	domain.EventSortTitle:     "e.title",
}

// List returns a page of non-deleted events.
func (r *EventRepo) List(ctx context.Context, f domain.EventFilter) (domain.Page[*domain.Event], error) {
	where := []string{"e.deleted_at IS NULL"}
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if f.DivisionID != nil {
		where = append(where, "e.division_id = "+arg(*f.DivisionID))
	}
	if f.Status != nil {
		switch *f.Status {
		case domain.EventUpcoming:
			where = append(where, "e.start_at > now()")
		case domain.EventOngoing:
			where = append(where, "e.start_at <= now() AND e.end_at >= now()")
		case domain.EventFinished:
			where = append(where, "e.end_at < now()")
		}
	}
	if f.Search != "" {
		where = append(where, fmt.Sprintf(`e.title ILIKE %s ESCAPE '\'`, arg("%"+escapeLike(f.Search)+"%")))
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	page := domain.Page[*domain.Event]{Items: []*domain.Event{}, Page: f.Page, Limit: f.Limit}
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM events e`+cond, args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count events: %w", err)
	}
	if page.Total == 0 || f.Offset() >= page.Total {
		return page, nil
	}

	col, ok := eventSortColumns[f.Sort]
	if !ok {
		col = eventSortColumns[domain.EventSortStartAt]
	}
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	query := eventSelect + cond + fmt.Sprintf(" ORDER BY %s %s, e.id ASC LIMIT %s OFFSET %s",
		col, dir, arg(f.Limit), arg(f.Offset()))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return page, fmt.Errorf("list events: %w", err)
		}
		page.Items = append(page.Items, e)
	}
	return page, wrap("list events", rows.Err())
}

// Create inserts an event and returns its id.
func (r *EventRepo) Create(ctx context.Context, e *domain.Event) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `
INSERT INTO events (title, description, division_id, location, start_at, end_at, quota, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id`,
		e.Title, e.Description, e.DivisionID, e.Location, e.StartAt, e.EndAt, e.Quota, e.CreatedBy,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, mapEventWriteErr("create event", err)
	}
	return id, nil
}

// Update writes the editable columns. Division, registered_count, creator and
// deleted_at are never written here.
func (r *EventRepo) Update(ctx context.Context, e *domain.Event) error {
	tag, err := r.db.Exec(ctx, `
UPDATE events SET title = $2, description = $3, location = $4, start_at = $5, end_at = $6, quota = $7
WHERE id = $1 AND deleted_at IS NULL`,
		e.ID, e.Title, e.Description, e.Location, e.StartAt, e.EndAt, e.Quota)
	if err != nil {
		return mapEventWriteErr("update event", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrEventNotFound
	}
	return nil
}

// SoftDelete marks an event as deleted.
func (r *EventRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `UPDATE events SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrEventNotFound
	}
	return nil
}

// TakeSlot atomically reserves one slot. The condition and the increment run
// in a single UPDATE: PostgreSQL locks the row, re-evaluates
// "registered_count < quota" against the latest committed value, and only
// then increments. Concurrent requests for the last slot are therefore
// serialized and exactly one of them gets it. Returns false when the event is
// full, finished or deleted.
func (r *EventRepo) TakeSlot(ctx context.Context, id uuid.UUID) (bool, error) {
	var got uuid.UUID
	err := r.db.QueryRow(ctx, `
UPDATE events SET registered_count = registered_count + 1
WHERE id = $1 AND registered_count < quota AND deleted_at IS NULL AND end_at > now()
RETURNING id`, id).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("take slot: %w", err)
	}
	return true, nil
}

// ReleaseSlot gives one slot back.
func (r *EventRepo) ReleaseSlot(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE events SET registered_count = registered_count - 1 WHERE id = $1 AND registered_count > 0`, id)
	return wrap("release slot", err)
}

func mapEventWriteErr(op string, err error) error {
	code, constraint := pgErrCode(err)
	switch {
	case code == pgFKViolation && constraint == "events_division_id_fkey":
		return domain.ErrDivisionNotFound
	case code == pgCheckViolation && constraint == "events_time_range_chk":
		return domain.ErrInvalidTimeRange
	case code == pgCheckViolation && constraint == "events_registered_count_max_chk":
		return domain.ErrQuotaBelowRegistered
	}
	return fmt.Errorf("%s: %w", op, err)
}
