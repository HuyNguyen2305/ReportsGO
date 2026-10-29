//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

func insertTaxRate(t *testing.T, pool *pgxpool.Pool, name string, rate float64) int64 {
	t.Helper()
	var id int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`INSERT INTO tax_rates (name, rate) VALUES ($1, $2) RETURNING id`, name, rate).Scan(&id))
	return id
}

func TestTaxRateMustBeAPercentage(t *testing.T) {
	pool := testdb.MigratedPool(t)
	ctx := context.Background()

	for _, rate := range []float64{0, 12.5, 100} {
		_, err := pool.Exec(ctx, `INSERT INTO tax_rates (name, rate) VALUES ($1, $2)`, "ok", rate)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `DELETE FROM tax_rates`)
		require.NoError(t, err)
	}
	for _, rate := range []float64{-0.01, 100.01} {
		_, err := pool.Exec(ctx, `INSERT INTO tax_rates (name, rate) VALUES ('bad', $1)`, rate)
		requireViolation(t, err, "23514", "tax_rates_rate_chk")
	}
}

func TestTaxRateNamesAreUnique(t *testing.T) {
	pool := testdb.MigratedPool(t)
	insertTaxRate(t, pool, "VAT", 10)

	_, err := pool.Exec(context.Background(), `INSERT INTO tax_rates (name, rate) VALUES ('VAT', 15)`)
	requireViolation(t, err, "23505", "tax_rates_name_key")
}

func TestTaxLinesBelongToInvoicesOnly(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()
	vat := insertTaxRate(t, pool, "VAT", 10)

	_, err := pool.Exec(ctx, `INSERT INTO invoice_tax_lines (invoice_id, tax_rate_id, taxable_amount, tax_amount, surcharge_amount)
		VALUES ($1, $2, 59.09, 5.91, 0.50)`, s.invoiceID, vat)
	require.NoError(t, err, "an invoice may carry tax lines")

	_, err = pool.Exec(ctx, `INSERT INTO invoice_tax_lines (invoice_id, tax_rate_id, taxable_amount, tax_amount)
		VALUES ($1, $2, 20, 2)`, s.estimateID, vat)
	requireViolation(t, err, "23503", "invoice_tax_lines_invoice_id_fkey")
}

func TestOneTaxLinePerInvoiceAndTax(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()
	vat := insertTaxRate(t, pool, "VAT", 10)

	_, err := pool.Exec(ctx, `INSERT INTO invoice_tax_lines (invoice_id, tax_rate_id) VALUES ($1, $2)`, s.invoiceID, vat)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO invoice_tax_lines (invoice_id, tax_rate_id) VALUES ($1, $2)`, s.invoiceID, vat)
	requireViolation(t, err, "23505", "invoice_tax_lines_pkey")
}

func TestTaxLineAmountsCannotBeNegative(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	vat := insertTaxRate(t, pool, "VAT", 10)

	for _, col := range []string{"taxable_amount", "tax_amount", "surcharge_amount"} {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO invoice_tax_lines (invoice_id, tax_rate_id, `+col+`) VALUES ($1, $2, -1)`, s.invoiceID, vat)
		requireViolation(t, err, "23514", "invoice_tax_lines_amounts_chk")
	}
}

func TestTaxLinesAreRemovedWithTheirInvoice(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()
	vat := insertTaxRate(t, pool, "VAT", 10)

	// the seeded invoice is referenced by a recurring invoice and by tag links, so use a fresh one
	var invoiceID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO billing_documents (document_number, customer_id, document_date)
		VALUES ('99999', $1, '2026-10-01') RETURNING id`, s.customerID).Scan(&invoiceID))
	_, err := pool.Exec(ctx, `INSERT INTO invoice_tax_lines (invoice_id, tax_rate_id) VALUES ($1, $2)`, invoiceID, vat)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM billing_documents WHERE id = $1`, invoiceID)
	require.NoError(t, err)

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM invoice_tax_lines WHERE invoice_id = $1`, invoiceID).Scan(&n))
	require.Zero(t, n)
}

func TestPaymentsRecordWhoReceivedThem(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	var userID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM users LIMIT 1`).Scan(&userID))

	_, err := pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date, received_by)
		VALUES ($1, $2, 'cash', 65, '2026-10-26', $3), ($1, $2, 'card', 5, '2026-10-27', NULL)`, s.customerID, s.invoiceID, userID)
	require.NoError(t, err, "received_by is optional")

	_, err = pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date, received_by)
		VALUES ($1, $2, 'cash', 1, '2026-10-28', 999999)`, s.customerID, s.invoiceID)
	requireViolation(t, err, "23503", "payments_received_by_fkey")
}
