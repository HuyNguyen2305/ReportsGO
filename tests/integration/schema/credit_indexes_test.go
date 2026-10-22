//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

// The Credits report searches the method detail with ILIKE '%term%', which only a trigram index can serve.
func TestCreditMethodDetailHasATrigramIndex(t *testing.T) {
	pool := testdb.MigratedPool(t)

	var def string
	err := pool.QueryRow(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE tablename = 'credits' AND indexname = 'credits_method_detail_trgm_idx'`).Scan(&def)

	require.NoError(t, err, "the index must exist")
	require.Contains(t, def, "gin")
	require.Contains(t, def, "gin_trgm_ops")
	require.Contains(t, def, "method_detail")
}
