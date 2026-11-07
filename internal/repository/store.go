// Package repository contains all SQL. Every query is parameterized; dynamic
// parts (sort column/direction) come only from server-side whitelists.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is satisfied by both *pgxpool.Pool and pgx.Tx, so every repository
// works the same inside or outside a transaction.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repos groups repositories bound to the same DBTX.
type Repos struct {
	Users         *UserRepo
	Divisions     *DivisionRepo
	Audit         *AuditRepo
	Events        *EventRepo
	Registrations *RegistrationRepo
	Attendances   *AttendanceRepo
}

func newRepos(db DBTX) Repos {
	return Repos{
		Users:         &UserRepo{db: db},
		Divisions:     &DivisionRepo{db: db},
		Audit:         &AuditRepo{db: db},
		Events:        &EventRepo{db: db},
		Registrations: &RegistrationRepo{db: db},
		Attendances:   &AttendanceRepo{db: db},
	}
}

// Store gives services access to repositories and transactions.
type Store struct {
	Repos
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{Repos: newRepos(pool), pool: pool}
}

// InTx runs fn inside one database transaction. It commits when fn returns
// nil and rolls back on error or panic.
func (s *Store) InTx(ctx context.Context, fn func(r Repos) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(newRepos(tx))
	})
}
