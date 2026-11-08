package repository

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL error codes we react to.
const (
	pgUniqueViolation = "23505"
	pgCheckViolation  = "23514"
	pgFKViolation     = "23503"
)

func pgErrCode(err error) (code, constraint string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code, pgErr.ConstraintName
	}
	return "", ""
}

func isUniqueViolation(err error) bool {
	code, _ := pgErrCode(err)
	return code == pgUniqueViolation
}
