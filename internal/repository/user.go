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

type UserRepo struct {
	db DBTX
}

// userSelect returns users joined with their division name.
const userSelect = `
SELECT u.id, u.name, u.email, u.password_hash, u.google_sub, u.role::text,
       u.division_id, d.name, u.phone, u.bio, u.created_at, u.updated_at, u.deleted_at
FROM users u
LEFT JOIN divisions d ON d.id = u.division_id`

func scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User
	var role string
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.GoogleSub, &role,
		&u.DivisionID, &u.DivisionName, &u.Phone, &u.Bio, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound.WithMessage("Anggota tidak ditemukan")
		}
		return nil, err
	}
	u.Role = domain.Role(role)
	return &u, nil
}

// GetActiveByID returns a non-deleted user.
func (r *UserRepo) GetActiveByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	u, err := scanUser(r.db.QueryRow(ctx, userSelect+` WHERE u.id = $1 AND u.deleted_at IS NULL`, id))
	if err != nil {
		return nil, wrap("get user by id", err)
	}
	return u, nil
}

// GetActiveByEmail returns a non-deleted user by (lowercased) email.
func (r *UserRepo) GetActiveByEmail(ctx context.Context, email string) (*domain.User, error) {
	u, err := scanUser(r.db.QueryRow(ctx, userSelect+` WHERE u.email = lower($1) AND u.deleted_at IS NULL`, email))
	if err != nil {
		return nil, wrap("get user by email", err)
	}
	return u, nil
}

// GetActiveByGoogleSub returns a non-deleted user linked to a Google account.
func (r *UserRepo) GetActiveByGoogleSub(ctx context.Context, sub string) (*domain.User, error) {
	u, err := scanUser(r.db.QueryRow(ctx, userSelect+` WHERE u.google_sub = $1 AND u.deleted_at IS NULL`, sub))
	if err != nil {
		return nil, wrap("get user by google sub", err)
	}
	return u, nil
}

// LinkGoogleSub links a Google account to a user that has none yet.
func (r *UserRepo) LinkGoogleSub(ctx context.Context, id uuid.UUID, sub string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE users SET google_sub = $2 WHERE id = $1 AND google_sub IS NULL AND deleted_at IS NULL`, id, sub)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict.WithMessage("Akun Google sudah terhubung ke anggota lain")
		}
		return fmt.Errorf("link google sub: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound.WithMessage("Anggota tidak ditemukan")
	}
	return nil
}

// wrap adds context to infrastructure errors but leaves domain errors intact.
func wrap(op string, err error) error {
	var de *domain.Error
	if errors.As(err, &de) {
		return err
	}
	return fmt.Errorf("%s: %w", op, err)
}

// LockActive loads the given non-deleted users and locks their rows until the
// transaction ends. Rows are locked in id order so concurrent callers locking
// the same pair of users cannot deadlock. Must be called inside InTx.
func (r *UserRepo) LockActive(ctx context.Context, ids ...uuid.UUID) (map[uuid.UUID]*domain.User, error) {
	rows, err := r.db.Query(ctx,
		userSelect+` WHERE u.id = ANY($1) AND u.deleted_at IS NULL ORDER BY u.id FOR UPDATE OF u`, ids)
	if err != nil {
		return nil, fmt.Errorf("lock users: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]*domain.User, len(ids))
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("lock users: %w", err)
		}
		out[u.ID] = u
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lock users: %w", err)
	}
	return out, nil
}

// memberSortColumns whitelists sortable columns. User input is only ever used
// as a key into this map, never concatenated into SQL.
var memberSortColumns = map[domain.MemberSort]string{
	domain.MemberSortName:      "u.name",
	domain.MemberSortEmail:     "u.email",
	domain.MemberSortCreatedAt: "u.created_at",
}

// List returns a page of non-deleted users matching the filter.
func (r *UserRepo) List(ctx context.Context, f domain.MemberFilter) (domain.Page[*domain.User], error) {
	where := []string{"u.deleted_at IS NULL"}
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if f.OnlyUserID != nil {
		where = append(where, "u.id = "+arg(*f.OnlyUserID))
	}
	if f.DivisionID != nil {
		where = append(where, "u.division_id = "+arg(*f.DivisionID))
	}
	if f.Role != nil {
		where = append(where, "u.role = "+arg(string(*f.Role))+"::user_role")
	}
	if f.Search != "" {
		p := arg("%" + escapeLike(f.Search) + "%")
		where = append(where, fmt.Sprintf(`(u.name ILIKE %s ESCAPE '\' OR u.email ILIKE %s ESCAPE '\')`, p, p))
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	page := domain.Page[*domain.User]{Items: []*domain.User{}, Page: f.Page, Limit: f.Limit}
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM users u`+cond, args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count users: %w", err)
	}
	if page.Total == 0 || f.Offset() >= page.Total {
		return page, nil
	}

	col, ok := memberSortColumns[f.Sort]
	if !ok {
		col = memberSortColumns[domain.MemberSortName]
	}
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	// u.id is a tie-breaker so pagination is stable when sort values repeat.
	query := userSelect + cond + fmt.Sprintf(" ORDER BY %s %s, u.id ASC LIMIT %s OFFSET %s",
		col, dir, arg(f.Limit), arg(f.Offset()))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return page, fmt.Errorf("list users: %w", err)
		}
		page.Items = append(page.Items, u)
	}
	return page, wrapIf("list users", rows.Err())
}

// escapeLike escapes LIKE wildcards so user input is matched literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Create inserts a new user and returns its id.
func (r *UserRepo) Create(ctx context.Context, u *domain.User) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `
INSERT INTO users (name, email, password_hash, role, division_id, phone, bio)
VALUES ($1, $2, $3, $4::user_role, $5, $6, $7)
RETURNING id`,
		u.Name, u.Email, u.PasswordHash, string(u.Role), u.DivisionID, u.Phone, u.Bio,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, mapUserWriteErr("create user", err)
	}
	return id, nil
}

// UpdateProfile writes the profile columns (name, email, phone, bio) only.
// Role, division and deleted_at are deliberately not writable here.
func (r *UserRepo) UpdateProfile(ctx context.Context, u *domain.User) error {
	tag, err := r.db.Exec(ctx, `
UPDATE users SET name = $2, email = $3, phone = $4, bio = $5
WHERE id = $1 AND deleted_at IS NULL`,
		u.ID, u.Name, u.Email, u.Phone, u.Bio)
	if err != nil {
		return mapUserWriteErr("update user profile", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound.WithMessage("Anggota tidak ditemukan")
	}
	return nil
}

// UpdateRole changes a user's role and division.
func (r *UserRepo) UpdateRole(ctx context.Context, id uuid.UUID, role domain.Role, divisionID *uuid.UUID) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE users SET role = $2::user_role, division_id = $3 WHERE id = $1 AND deleted_at IS NULL`,
		id, string(role), divisionID)
	if err != nil {
		return mapUserWriteErr("update user role", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound.WithMessage("Anggota tidak ditemukan")
	}
	return nil
}

// SoftDelete marks a user as deleted. Deleted users cannot log in and their
// existing tokens stop working immediately.
func (r *UserRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `UPDATE users SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound.WithMessage("Anggota tidak ditemukan")
	}
	return nil
}

func mapUserWriteErr(op string, err error) error {
	code, constraint := pgErrCode(err)
	switch {
	case code == pgUniqueViolation && constraint == "users_email_key":
		return domain.ErrEmailTaken
	case code == pgFKViolation && constraint == "users_division_id_fkey":
		return domain.ErrDivisionNotFound
	case code == pgCheckViolation && constraint == "users_division_required_chk":
		return domain.ErrDivisionRequired
	}
	return fmt.Errorf("%s: %w", op, err)
}

func wrapIf(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
