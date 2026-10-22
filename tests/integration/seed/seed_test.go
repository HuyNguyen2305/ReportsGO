//go:build integration

package seed_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/seed"
	"reportsgo/tests/integration/testdb"
)

func loadScript(t *testing.T) string {
	t.Helper()
	script, err := seed.Load(filepath.Join(testdb.RepoRoot(t), "db", "seed", "seed.sql"))
	require.NoError(t, err)
	return script
}

func TestSeedIsIdempotentAndPopulatesEveryTable(t *testing.T) {
	ctx := context.Background()
	pool := testdb.MigratedPool(t)
	script := loadScript(t)

	require.NoError(t, seed.Run(ctx, pool, script))
	first, err := seed.Counts(ctx, pool)
	require.NoError(t, err)

	require.NoError(t, seed.Run(ctx, pool, script))
	second, err := seed.Counts(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, first, second, "re-running the seed must give identical counts")
	for _, table := range seed.Tables {
		require.Positive(t, first[table], "table %s should have rows", table)
	}
	require.Equal(t, 29, first["jobs"])
	require.Equal(t, 1, first["recurring_invoices"])
	require.Equal(t, 45, first["billing_documents"], "24 invoices + 21 estimates")
	require.Equal(t, 19, first["documents"], "15 original + sent + 2 signed + 1 deleted")
	require.Equal(t, 12, first["payments"], "11 original + 1 partial payment")
}

func TestSeedRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	pool := testdb.MigratedPool(t)
	require.NoError(t, seed.Run(ctx, pool, loadScript(t)))
	before, err := seed.Counts(ctx, pool)
	require.NoError(t, err)

	// TRUNCATE succeeds, then the invalid statement fails: nothing may be committed.
	err = seed.Run(ctx, pool, "TRUNCATE payments; SELECT * FROM no_such_table;")
	require.Error(t, err)

	after, err := seed.Counts(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestSeededDataSupportsReportQueries(t *testing.T) {
	ctx := context.Background()
	pool := testdb.MigratedPool(t)
	require.NoError(t, seed.Run(ctx, pool, loadScript(t)))

	t.Run("every payment has exactly one target", func(t *testing.T) {
		var bad int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM payments WHERE num_nonnulls(invoice_id, credit_id) <> 1`).Scan(&bad))
		require.Zero(t, bad)
	})

	t.Run("billing documents split into invoices and estimates", func(t *testing.T) {
		counts := map[string]int{}
		rows, err := pool.Query(ctx, `SELECT parent_type::text, count(*) FROM billing_documents GROUP BY 1`)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var kind string
			var n int
			require.NoError(t, rows.Scan(&kind, &n))
			counts[kind] = n
		}
		require.NoError(t, rows.Err())
		require.Equal(t, map[string]int{"invoice": 24, "estimate": 21}, counts, "19 original + 3 sent + 1 archived + 1 deleted invoices")
	})

	t.Run("invoices report joins customer and tags", func(t *testing.T) {
		var customer string
		var tags []string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT c.display_name, coalesce(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.id IS NOT NULL), '{}')
			   FROM billing_documents i
			   JOIN customers c ON c.id = i.customer_id
			   LEFT JOIN invoice_tag_links l ON l.invoice_id = i.id
			   LEFT JOIN invoice_tags t ON t.id = l.tag_id
			  WHERE i.parent_type = 'invoice' AND i.document_number = '85030'
			  GROUP BY c.display_name`).Scan(&customer, &tags))
		require.Equal(t, "#F Last Name", customer)
		require.Equal(t, []string{"a", "b", "c"}, tags)
	})

	t.Run("october invoice total matches the screenshot filter", func(t *testing.T) {
		var n int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM billing_documents
			  WHERE parent_type = 'invoice' AND document_date BETWEEN '2026-10-01' AND '2026-10-31' AND deleted_at IS NULL AND archived_at IS NULL`).Scan(&n))
		require.Equal(t, 21, n, "18 original + 3 sent; the archived and deleted invoices are excluded")
	})

	t.Run("recurring invoice points at its next invoice", func(t *testing.T) {
		var number string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT i.document_number FROM recurring_invoices r JOIN billing_documents i ON i.id = r.next_invoice_id`).Scan(&number))
		require.Equal(t, "84890", number)
	})
}
