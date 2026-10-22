// Package seed loads development sample data into the Reports database.
package seed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Tables lists every data table the seed populates, in report order.
var Tables = []string{
	"users", "lead_sources", "service_types", "payment_terms", "invoice_tags", "document_types",
	"customers", "customer_phones", "service_locations", "jobs",
	"billing_documents", "invoice_tag_links", "recurring_invoices",
	"documents", "credits", "payments",
}

const runTimeout = 60 * time.Second

// ErrEmptyScript is returned when the seed file has no SQL in it.
var ErrEmptyScript = errors.New("seed script is empty")

// Load reads the seed script at path and rejects an empty one.
func Load(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read seed file %q: %w", path, err)
	}
	script := string(b)
	if strings.TrimSpace(script) == "" {
		return "", fmt.Errorf("seed file %q: %w", path, ErrEmptyScript)
	}
	return script, nil
}

// Run executes script in a single transaction, so a failure leaves the database untouched.
func Run(ctx context.Context, pool *pgxpool.Pool, script string) error {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// No arguments, so pgx uses the simple protocol and accepts multiple statements.
	if _, err := tx.Exec(ctx, script); err != nil {
		return fmt.Errorf("execute seed script: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed transaction: %w", err)
	}
	return nil
}

// Counts returns the row count of every seeded table.
func Counts(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	counts := make(map[string]int, len(Tables))
	for _, table := range Tables {
		var n int
		// Table names come from the fixed Tables list above, never from input.
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		counts[table] = n
	}
	return counts, nil
}
