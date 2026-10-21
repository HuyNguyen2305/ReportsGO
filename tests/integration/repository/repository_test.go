//go:build integration

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

func newPool(t *testing.T, timeout time.Duration) *pgxpool.Pool {
	t.Helper()

	pool, err := repository.NewPool(context.Background(), repository.PoolConfig{
		URL:              testdb.NewDatabase(t),
		MaxConns:         4,
		MinConns:         1,
		StatementTimeout: timeout,
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestNewPoolAppliesStatementTimeout(t *testing.T) {
	pool := newPool(t, 250*time.Millisecond)
	ctx := context.Background()

	var got string
	require.NoError(t, pool.QueryRow(ctx, "SHOW statement_timeout").Scan(&got))
	require.Equal(t, "250ms", got)

	_, err := pool.Exec(ctx, "SELECT pg_sleep(5)")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "57014", pgErr.Code) // query_canceled
}

func TestNewPoolRespectsPoolSize(t *testing.T) {
	pool := newPool(t, time.Second)
	require.Equal(t, int32(4), pool.Config().MaxConns)
	require.Equal(t, int32(1), pool.Config().MinConns)
}

func TestNewPoolFailsWhenDatabaseUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Port 1 is not a Postgres server.
	_, err := repository.NewPool(ctx, repository.PoolConfig{
		URL: "postgres://u:p@127.0.0.1:1/db?sslmode=disable", MaxConns: 1, StatementTimeout: time.Second,
	})
	require.Error(t, err)
}

func TestWithTx(t *testing.T) {
	pool := newPool(t, 5*time.Second)
	ctx := context.Background()
	_, err := pool.Exec(ctx, "CREATE TABLE items (name text PRIMARY KEY)")
	require.NoError(t, err)

	insert := func(name string) func(context.Context, pgx.Tx) error {
		return func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO items (name) VALUES ($1)", name)
			return err
		}
	}
	count := func() int {
		var n int
		require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM items").Scan(&n))
		return n
	}

	t.Run("commit makes rows visible", func(t *testing.T) {
		require.NoError(t, repository.WithTx(ctx, pool, insert("committed")))
		require.Equal(t, 1, count())
	})

	t.Run("error rolls back", func(t *testing.T) {
		errWork := errors.New("work failed")
		err := repository.WithTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			if err := insert("rolled-back")(ctx, tx); err != nil {
				return err
			}
			return errWork
		})
		require.ErrorIs(t, err, errWork)
		require.Equal(t, 1, count())
	})

	t.Run("panic rolls back and re-panics", func(t *testing.T) {
		require.PanicsWithValue(t, "boom", func() {
			_ = repository.WithTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
				_ = insert("panicked")(ctx, tx)
				panic("boom")
			})
		})
		require.Equal(t, 1, count())
	})

	t.Run("constraint violation inside tx rolls back all statements", func(t *testing.T) {
		err := repository.WithTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			if err := insert("first")(ctx, tx); err != nil {
				return err
			}
			return insert("committed")(ctx, tx) // duplicate key
		})
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "23505", pgErr.Code)
		require.Equal(t, 1, count())
	})
}
