package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// rollbackTimeout bounds a rollback. It runs on a context detached from the request, so without a
// deadline a dead connection would block the caller for as long as the operating system keeps it open.
const rollbackTimeout = 5 * time.Second

// TxBeginner starts transactions. *pgxpool.Pool satisfies it.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// WithTx runs fn inside a transaction. It commits when fn returns nil, and rolls back when fn
// returns an error or panics (the panic is re-raised after the rollback).
func WithTx(ctx context.Context, db TxBeginner, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = rollback(ctx, tx)
			panic(p)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		if rbErr := rollback(ctx, tx); rbErr != nil {
			return errors.Join(err, fmt.Errorf("rollback transaction: %w", rbErr))
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// rollback rolls tx back on a fresh, bounded context: ctx may be the reason fn failed or panicked, so it
// cannot be trusted to still be usable.
func rollback(ctx context.Context, tx pgx.Tx) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	return tx.Rollback(ctx)
}
