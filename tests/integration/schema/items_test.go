//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

func insertItem(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`INSERT INTO items (name) VALUES ($1) RETURNING id`, name).Scan(&id))
	return id
}

func TestItemNamesAreUnique(t *testing.T) {
	pool := testdb.MigratedPool(t)
	insertItem(t, pool, "Bi-Monthly Service")

	_, err := pool.Exec(context.Background(), `INSERT INTO items (name) VALUES ('Bi-Monthly Service')`)
	requireViolation(t, err, "23505", "items_name_key")
}

func TestLineItemsBelongToInvoicesOnly(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()
	item := insertItem(t, pool, "Bi-Monthly Service")

	_, err := pool.Exec(ctx, `INSERT INTO invoice_items (invoice_id, item_id, line_total)
		VALUES ($1, $2, 65)`, s.invoiceID, item)
	require.NoError(t, err, "an invoice may have line items")

	_, err = pool.Exec(ctx, `INSERT INTO invoice_items (invoice_id, item_id, line_total) VALUES ($1, $2, 24.40)`, s.estimateID, item)
	requireViolation(t, err, "23503", "invoice_items_invoice_id_fkey")
}

func TestUsedItemsCannotBeDeleted(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()
	item := insertItem(t, pool, "Bi-Monthly Service")

	_, err := pool.Exec(ctx, `INSERT INTO invoice_items (invoice_id, item_id) VALUES ($1, $2)`, s.invoiceID, item)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM items WHERE id = $1`, item)
	requireViolation(t, err, "23503", "invoice_items_item_id_fkey")
}

func TestLineItemsAreRemovedWithTheirInvoice(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()
	item := insertItem(t, pool, "Bi-Monthly Service")

	// the seeded invoice is referenced by a recurring invoice and by tag links, so use a fresh one
	var invoiceID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO billing_documents (document_number, customer_id, document_date)
		VALUES ('99999', $1, '2026-10-01') RETURNING id`, s.customerID).Scan(&invoiceID))
	_, err := pool.Exec(ctx, `INSERT INTO invoice_items (invoice_id, item_id) VALUES ($1, $2)`, invoiceID, item)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM billing_documents WHERE id = $1`, invoiceID)
	require.NoError(t, err)

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM invoice_items WHERE invoice_id = $1`, invoiceID).Scan(&n))
	require.Zero(t, n)
}
