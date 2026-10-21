//go:build integration

// Package testdb provides throwaway Postgres databases for integration tests.
package testdb

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5 driver
	_ "github.com/golang-migrate/migrate/v4/source/file"     // registers the file source
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// NewDatabase creates an empty throwaway database (dropped on cleanup) and returns its URL.
// The test is skipped when TEST_POSTGRES_URL is not set.
func NewDatabase(t *testing.T) string {
	t.Helper()

	adminURL := os.Getenv("TEST_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL)
	require.NoError(t, err)
	defer func() { _ = admin.Close(ctx) }()

	// Packages run in parallel and the clock is coarse on Windows, so add a random suffix.
	suffix := make([]byte, 6)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	name := fmt.Sprintf("reportsgo_test_%d_%x", time.Now().UnixNano(), suffix)
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)

	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		c, cerr := pgx.Connect(cctx, adminURL)
		if cerr != nil {
			return
		}
		defer func() { _ = c.Close(cctx) }()
		_, _ = c.Exec(cctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})

	u, err := url.Parse(adminURL)
	require.NoError(t, err)
	u.Path = "/" + name
	return u.String()
}

// NewMigrator returns a migrator for dbURL using the repository's migrations directory.
func NewMigrator(t *testing.T, dbURL string) *migrate.Migrate {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join(RepoRoot(t), "migrations"))
	require.NoError(t, err)

	u, err := url.Parse(dbURL)
	require.NoError(t, err)
	u.Scheme = "pgx5"

	m, err := migrate.New("file://"+filepath.ToSlash(dir), u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	return m
}

// MigratedPool creates a throwaway database, applies all migrations and returns a pool for it.
func MigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := NewDatabase(t)
	require.NoError(t, NewMigrator(t, dbURL).Up())

	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// RepoRoot returns the repository root, found by walking up from the test's working directory to go.mod.
func RepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found above the working directory")
		dir = parent
	}
}
