// Package testutil provides helpers for integration tests against a real
// PostgreSQL. Each test gets its own freshly migrated database, so packages
// and tests can run in parallel without sharing state.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/mauinGit/gdgoc-core-team-api/internal/database"
	"github.com/mauinGit/gdgoc-core-team-api/seed"
)

// SeedPassword is the password of every seeded account in tests.
const SeedPassword = "Password123!"

// NewDB creates an isolated, migrated database and drops it after the test.
// It needs TEST_DATABASE_URL pointing at a server where the user may CREATE
// DATABASE (e.g. the docker compose Postgres or the CI service container).
func NewDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL must be set in CI")
		}
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()

	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "gdgoc_test_" + hex.EncodeToString(suffix)
	ident := pgx.Identifier{name}.Sanitize()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create test database: %v", err)
	}
	_ = admin.Close(ctx)

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + name
	dbURL := u.String()

	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.Connect(cctx, base)
		if err != nil {
			t.Logf("cleanup connect: %v", err)
			return
		}
		defer conn.Close(cctx)
		if _, err := conn.Exec(cctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database: %v", err)
		}
	})

	if _, err := database.Migrate(dbURL); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	pool, err := database.NewPool(ctx, dbURL, 25)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Seed loads the standard seed data (cheap bcrypt cost for speed).
func Seed(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	if err := seed.Run(context.Background(), pool, seed.Options{
		Password:   SeedPassword,
		BcryptCost: bcrypt.MinCost,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}
