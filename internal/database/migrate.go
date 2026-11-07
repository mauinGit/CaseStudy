package database

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/mauinGit/gdgoc-core-team-api/migrations"
)

// Migrate applies all pending "up" migrations embedded in the binary.
// It is safe to call on every startup: golang-migrate takes an advisory lock
// and does nothing when the schema is already up to date.
func Migrate(databaseURL string) (version uint, err error) {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return 0, err
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if err == nil {
			err = errors.Join(srcErr, dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, fmt.Errorf("migrate up: %w", err)
	}
	v, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	if dirty {
		return v, fmt.Errorf("database is in dirty migration state at version %d", v)
	}
	return v, nil
}

func newMigrator(databaseURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, toPgx5URL(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("init migrator: %w", err)
	}
	return m, nil
}

// toPgx5URL converts postgres:// or postgresql:// URLs to the pgx5:// scheme
// expected by golang-migrate's pgx/v5 driver.
func toPgx5URL(u string) string {
	for _, prefix := range []string{"postgresql://", "postgres://"} {
		if strings.HasPrefix(u, prefix) {
			return "pgx5://" + strings.TrimPrefix(u, prefix)
		}
	}
	return u
}
