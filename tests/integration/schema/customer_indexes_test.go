//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

// The New Customers report searches the email and the phone with ILIKE '%term%', which only a trigram index can
// serve.
func TestCustomerSearchColumnsHaveTrigramIndexes(t *testing.T) {
	pool := testdb.MigratedPool(t)

	tests := []struct{ table, index, column string }{
		{"customers", "customers_email_trgm_idx", "email"},
		{"customer_phones", "customer_phones_number_trgm_idx", "number"},
	}
	for _, tt := range tests {
		t.Run(tt.index, func(t *testing.T) {
			var def string
			err := pool.QueryRow(context.Background(),
				`SELECT indexdef FROM pg_indexes WHERE tablename = $1 AND indexname = $2`, tt.table, tt.index).Scan(&def)

			require.NoError(t, err, "the index must exist")
			require.Contains(t, def, "gin")
			require.Contains(t, def, "gin_trgm_ops")
			require.Contains(t, def, tt.column)
		})
	}
}
