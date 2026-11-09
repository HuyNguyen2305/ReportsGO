//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

// TestUnusedColumnsAreDropped pins migration 000022: no report reads these columns, so they must stay gone.
func TestUnusedColumnsAreDropped(t *testing.T) {
	pool := testdb.MigratedPool(t)

	for table, columns := range map[string][]string{
		"customers":                {"billing_email"},
		"customer_phones":          {"kind"},
		"materials":                {"unit"},
		"job_materials":            {"created_at"},
		"customer_payment_methods": {"brand"},
		"billing_documents":        {"recurring_invoice_id"},
		"items":                    {"unit_price", "active"},
		"invoice_items":            {"description", "quantity", "unit_price", "taxable"},
	} {
		for _, column := range columns {
			t.Run(table+"."+column, func(t *testing.T) {
				var n int
				require.NoError(t, pool.QueryRow(context.Background(),
					`SELECT count(*) FROM information_schema.columns
					 WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`, table, column).Scan(&n))
				require.Zero(t, n)
			})
		}
	}
}
