// Package repository holds Postgres access: the shared connection pool and transaction helper.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig configures the shared connection pool.
type PoolConfig struct {
	URL              string
	MaxConns         int32
	MinConns         int32
	StatementTimeout time.Duration
}

// Validate checks the settings that can be verified without a database.
func (c PoolConfig) Validate() error {
	var errs []error
	if c.URL == "" {
		errs = append(errs, errors.New("URL is required"))
	}
	if c.MaxConns < 1 {
		errs = append(errs, errors.New("MaxConns must be at least 1"))
	}
	if c.MinConns < 0 || c.MinConns > c.MaxConns {
		errs = append(errs, errors.New("MinConns must be between 0 and MaxConns"))
	}
	if c.StatementTimeout <= 0 {
		errs = append(errs, errors.New("StatementTimeout must be positive"))
	}
	return errors.Join(errs...)
}

// NewPool opens the process-wide pgx pool and verifies connectivity.
// StatementTimeout is applied as the server-side statement_timeout on every connection,
// so no query can run unbounded even if a caller forgets to set a context deadline.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid pool config: %w", err)
	}

	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// pgx redacts the password in parse errors.
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = cfg.MinConns
	if pc.ConnConfig.RuntimeParams == nil {
		pc.ConnConfig.RuntimeParams = map[string]string{}
	}
	pc.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%d", cfg.StatementTimeout.Milliseconds())

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
