package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/repository"
	pgxmocks "reportsgo/tests/mocks/pgx"
	repositorymocks "reportsgo/tests/mocks/repository"
)

var (
	errBegin    = errors.New("begin failed")
	errCommit   = errors.New("commit failed")
	errRollback = errors.New("rollback failed")
	errWork     = errors.New("work failed")
)

func TestWithTx(t *testing.T) {
	tests := []struct {
		name    string
		begin   error
		fn      func(context.Context, pgx.Tx) error
		setup   func(tx *pgxmocks.MockTx)
		wantErr []error // each must be matched by errors.Is
	}{
		{
			name:  "commits when fn succeeds",
			fn:    func(context.Context, pgx.Tx) error { return nil },
			setup: func(tx *pgxmocks.MockTx) { tx.EXPECT().Commit(mock.Anything).Return(nil).Once() },
		},
		{
			name:    "rolls back and returns fn error",
			fn:      func(context.Context, pgx.Tx) error { return errWork },
			setup:   func(tx *pgxmocks.MockTx) { tx.EXPECT().Rollback(mock.Anything).Return(nil).Once() },
			wantErr: []error{errWork},
		},
		{
			name: "joins fn error and rollback error",
			fn:   func(context.Context, pgx.Tx) error { return errWork },
			setup: func(tx *pgxmocks.MockTx) {
				tx.EXPECT().Rollback(mock.Anything).Return(errRollback).Once()
			},
			wantErr: []error{errWork, errRollback},
		},
		{
			name:    "returns commit error",
			fn:      func(context.Context, pgx.Tx) error { return nil },
			setup:   func(tx *pgxmocks.MockTx) { tx.EXPECT().Commit(mock.Anything).Return(errCommit).Once() },
			wantErr: []error{errCommit},
		},
		{
			name:    "returns begin error without touching a transaction",
			begin:   errBegin,
			fn:      func(context.Context, pgx.Tx) error { t.Fatal("fn must not run"); return nil },
			wantErr: []error{errBegin},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := pgxmocks.NewMockTx(t)
			db := repositorymocks.NewMockTxBeginner(t)
			if tt.begin != nil {
				db.EXPECT().Begin(mock.Anything).Return(nil, tt.begin).Once()
			} else {
				db.EXPECT().Begin(mock.Anything).Return(tx, nil).Once()
				tt.setup(tx)
			}

			err := repository.WithTx(context.Background(), db, tt.fn)

			if len(tt.wantErr) == 0 {
				require.NoError(t, err)
				return
			}
			for _, want := range tt.wantErr {
				require.ErrorIs(t, err, want)
			}
		})
	}
}

func TestWithTxPassesTransactionToFn(t *testing.T) {
	tx := pgxmocks.NewMockTx(t)
	db := repositorymocks.NewMockTxBeginner(t)
	db.EXPECT().Begin(mock.Anything).Return(tx, nil).Once()
	tx.EXPECT().Commit(mock.Anything).Return(nil).Once()

	var got pgx.Tx
	require.NoError(t, repository.WithTx(context.Background(), db, func(_ context.Context, t pgx.Tx) error {
		got = t
		return nil
	}))
	require.Same(t, tx, got)
}

func TestWithTxRollsBackAndRepanics(t *testing.T) {
	tx := pgxmocks.NewMockTx(t)
	db := repositorymocks.NewMockTxBeginner(t)
	db.EXPECT().Begin(mock.Anything).Return(tx, nil).Once()
	tx.EXPECT().Rollback(mock.Anything).Run(func(rbCtx context.Context) {
		requireBoundedRollback(t, rbCtx)
	}).Return(nil).Once()

	require.PanicsWithValue(t, "boom", func() {
		_ = repository.WithTx(context.Background(), db, func(context.Context, pgx.Tx) error {
			panic("boom")
		})
	})
}

func TestWithTxRollbackIgnoresCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tx := pgxmocks.NewMockTx(t)
	db := repositorymocks.NewMockTxBeginner(t)
	db.EXPECT().Begin(mock.Anything).Return(tx, nil).Once()
	tx.EXPECT().Rollback(mock.Anything).Run(func(rbCtx context.Context) {
		require.NoError(t, rbCtx.Err(), "rollback must not use the cancelled request context")
		requireBoundedRollback(t, rbCtx)
	}).Return(nil).Once()

	err := repository.WithTx(ctx, db, func(context.Context, pgx.Tx) error {
		cancel()
		return errWork
	})
	require.ErrorIs(t, err, errWork)
}

// requireBoundedRollback checks that a rollback context carries a deadline, so a dead connection cannot
// block the caller forever.
func requireBoundedRollback(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	require.True(t, ok, "rollback context must have a deadline")
	require.WithinDuration(t, time.Now().Add(5*time.Second), deadline, 2*time.Second)
}
