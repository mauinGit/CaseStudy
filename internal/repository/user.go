package repository

import (
	"context"
	"errors"
	"fmt"

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
