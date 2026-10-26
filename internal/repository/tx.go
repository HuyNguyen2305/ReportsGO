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

// TxOptionsBeginner starts transactions with options. *pgxpool.Pool satisfies it; a pgx.Tx does not.
type TxOptionsBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// WithTx runs fn inside a transaction. It commits when fn returns nil, and rolls back when fn
// returns an error or panics (the panic is re-raised after the rollback).
func WithTx(ctx context.Context, db TxBeginner, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	return runTx(ctx, tx, fn)
}

// WithReadSnapshot runs fn in a read-only REPEATABLE READ transaction, so every query in fn sees the database
// as it was when the first one ran, even while other connections commit. Commit and rollback behave as in WithTx.
func WithReadSnapshot(ctx context.Context, db TxOptionsBeginner, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin read snapshot: %w", err)
	}
	return runTx(ctx, tx, fn)
}

// runTx runs fn on an open transaction and finishes it: commit on success, rollback on error or panic.
func runTx(ctx context.Context, tx pgx.Tx, fn func(ctx context.Context, tx pgx.Tx) error) error {
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
