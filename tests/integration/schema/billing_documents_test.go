//go:build integration

package schema_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

func requireViolation(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, code, pgErr.Code)
	require.Equal(t, constraint, pgErr.ConstraintName)
}

func TestBillingDocumentNumbersAreUniquePerType(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	// the seed holds invoice 84936 and estimate 123187
	_, err := pool.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date)
		VALUES ('estimate', '84936', $1, '2026-10-01'), ('invoice', '123187', $1, '2026-10-01')`, s.customerID)
	require.NoError(t, err, "an estimate and an invoice may share a number")

	_, err = pool.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date)
		VALUES ('estimate', '84936', $1, '2026-10-01')`, s.customerID)
	requireViolation(t, err, "23505", "billing_documents_parent_number_key")
}

func TestBillingDocumentStatusesDependOnType(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)

	tests := []struct {
		parent, status string
		wantErr        bool
	}{
		{"invoice", "paid", false},
		{"invoice", "void", false},
		{"invoice", "accepted", true},
		{"invoice", "declined", true},
		{"estimate", "accepted", false},
		{"estimate", "declined", false},
		{"estimate", "paid", true},
		{"estimate", "void", true},
	}
	for i, tt := range tests {
		t.Run(tt.parent+" "+tt.status, func(t *testing.T) {
			_, err := pool.Exec(context.Background(),
				`INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, status)
				 VALUES ($1, $2, $3, '2026-10-01', $4)`, tt.parent, fmt.Sprintf("S%d", i), s.customerID, tt.status)
			if tt.wantErr {
				requireViolation(t, err, "23514", "billing_documents_status_chk")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestEstimatesCannotCarryInvoiceOnlyData(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	var jobID, termsID, recurringID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM jobs LIMIT 1`).Scan(&jobID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM payment_terms LIMIT 1`).Scan(&termsID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM recurring_invoices LIMIT 1`).Scan(&recurringID))

	tests := []struct{ name, column, value string }{
		{"job", "job_id", fmt.Sprint(jobID)},
		{"payment terms", "payment_terms_id", fmt.Sprint(termsID)},
		{"recurring invoice", "recurring_invoice_id", fmt.Sprint(recurringID)},
		{"discount", "discount", "5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, `+
				tt.column+`) VALUES ('estimate', 'E-`+tt.column+`', $1, '2026-10-01', `+tt.value+`)`, s.customerID)
			requireViolation(t, err, "23514", "billing_documents_estimate_chk")
		})
	}
}

func TestOnlyInvoicesCanBePaidScheduledOrTagged(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	t.Run("payment", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date)
			VALUES ($1, $2, 'cash', 10, '2026-10-16')`, s.customerID, s.estimateID)
		requireViolation(t, err, "23503", "payments_invoice_id_fkey")
	})
	t.Run("recurring invoice", func(t *testing.T) {
		_, err := pool.Exec(ctx, `UPDATE recurring_invoices SET next_invoice_id = $1`, s.estimateID)
		requireViolation(t, err, "23503", "recurring_invoices_next_invoice_id_fkey")
	})
	t.Run("tag", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO invoice_tag_links (invoice_id, tag_id) SELECT $1, id FROM invoice_tags LIMIT 1`, s.estimateID)
		requireViolation(t, err, "23503", "invoice_tag_links_invoice_id_fkey")
	})
	t.Run("an invoice still works for all three and tag links cascade on delete", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date)
			VALUES ($1, $2, 'cash', 10, '2026-10-16')`, s.customerID, s.invoiceID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `DELETE FROM payments; UPDATE recurring_invoices SET next_invoice_id = NULL;
			DELETE FROM billing_documents WHERE id = `+fmt.Sprint(s.invoiceID))
		require.NoError(t, err)
		var links int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM invoice_tag_links`).Scan(&links))
		require.Zero(t, links)
	})
}

// The merge must carry existing invoices and estimates across in both directions.
func TestBillingDocumentsMigrationKeepsData(t *testing.T) {
	ctx := context.Background()
	dbURL := testdb.NewDatabase(t)
	m := testdb.NewMigrator(t, dbURL)
	require.NoError(t, m.Migrate(8))

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	for _, stmt := range []string{
		`INSERT INTO customers (account_number, display_name) VALUES ('5001', 'William Saliba')`,
		`INSERT INTO invoices (invoice_number, customer_id, invoice_date, total, status)
		 SELECT '84936', id, '2026-10-26', 65, 'sent' FROM customers`,
		`INSERT INTO estimates (estimate_number, customer_id, estimate_date, total, status, signature_status, archived_at)
		 SELECT '123187', id, '2026-10-20', 24.40, 'accepted', 'signed', '2026-10-21' FROM customers`,
		`INSERT INTO estimates (estimate_number, customer_id, estimate_date, total)
		 SELECT '84936', id, '2026-10-19', 1 FROM customers`,
	} {
		_, err := pool.Exec(ctx, stmt)
		require.NoError(t, err, stmt)
	}

	type doc struct {
		Kind, Number, Status, Signature, Total string
		Archived                               bool
	}
	merged := func() []doc {
		rows, err := pool.Query(ctx, `SELECT parent_type::text, document_number, status::text, signature_status::text,
			total::text, archived_at IS NOT NULL FROM billing_documents ORDER BY parent_type, document_number`)
		require.NoError(t, err)
		defer rows.Close()
		var out []doc
		for rows.Next() {
			var d doc
			require.NoError(t, rows.Scan(&d.Kind, &d.Number, &d.Status, &d.Signature, &d.Total, &d.Archived))
			out = append(out, d)
		}
		require.NoError(t, rows.Err())
		return out
	}

	require.NoError(t, m.Migrate(9))
	require.Equal(t, []doc{
		{"estimate", "123187", "accepted", "signed", "24.40", true},
		{"estimate", "84936", "draft", "draft", "1.00", false},
		{"invoice", "84936", "sent", "draft", "65.00", false},
	}, merged())

	require.NoError(t, m.Migrate(8))
	var invoices, estimates int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM invoices`).Scan(&invoices))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM estimates`).Scan(&estimates))
	require.Equal(t, 1, invoices)
	require.Equal(t, 2, estimates)
	var status, signature string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text, signature_status::text FROM estimates WHERE estimate_number = '123187'`).Scan(&status, &signature))
	require.Equal(t, "accepted", status)
	require.Equal(t, "signed", signature)

	require.NoError(t, m.Migrate(9))
	require.Len(t, merged(), 3, "migrating up again after a rollback keeps everything")
}
