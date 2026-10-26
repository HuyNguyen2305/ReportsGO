//go:build integration

package testdb

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SnapshotPool wraps a pool so that a test can commit a change from another connection at the exact moment
// the summary query is about to run inside a page's snapshot transaction. It satisfies the repositories'
// connection interface and repository.TxOptionsBeginner, so Page uses it like a real pool.
type SnapshotPool struct {
	*pgxpool.Pool
	// BeforeQueryRow runs before the first QueryRow of every transaction begun through BeginTx, which is how a
	// repository reads its summary.
	BeforeQueryRow func()
}

// BeginTx begins a real transaction and wraps it so that BeforeQueryRow fires before its summary read.
func (p SnapshotPool) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := p.Pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return hookedTx{Tx: tx, before: p.BeforeQueryRow}, nil
}

type hookedTx struct {
	pgx.Tx
	before func()
}

func (h hookedTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	h.before()
	return h.Tx.QueryRow(ctx, sql, args...)
}
