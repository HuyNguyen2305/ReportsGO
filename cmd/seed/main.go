// Command seed reloads the development sample data into DATABASE_URL.
// It TRUNCATES every data table first; never point it at production.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/jackc/pgx/v5/pgxpool"

	"reportsgo/internal/seed"
)

const defaultScript = "db/seed/seed.sql"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(); err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}
	path := defaultScript
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	script, err := seed.Load(path)
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	fmt.Printf("Seeding database %q from %s\n", pool.Config().ConnConfig.Database, path)
	if err := seed.Run(ctx, pool, script); err != nil {
		return err
	}

	counts, err := seed.Counts(ctx, pool)
	if err != nil {
		return err
	}
	for _, table := range seed.Tables {
		fmt.Printf("  %-20s %d\n", table, counts[table])
	}
	return nil
}
