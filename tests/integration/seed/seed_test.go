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
	require.Equal(t, 37, first["jobs"], "29 original + 8 for the New Jobs report")
	require.Equal(t, 3, first["lead_sources"], "Facebook + Google + Referral")
	require.Equal(t, 19, first["customers"], "11 original + 8 for the New Customers report")
	require.Equal(t, 9, first["customer_phones"], "4 original + 5 for the New Customers report")
	require.Equal(t, 7, first["recurring_invoices"], "1 original + 6 for the Recurring Invoices report")
	require.Equal(t, 45, first["billing_documents"], "24 invoices + 21 estimates")
	require.Equal(t, 19, first["documents"], "15 original + sent + 2 signed + 1 deleted")
	require.Equal(t, 17, first["payments"], "12 original + 5 in other states")
	require.Equal(t, 8, first["credits"], "2 original + 6 for the Credits report")
	require.Equal(t, 10, first["tax_rates"], "the ten taxes of the Sales Tax Summary screenshot")
	require.Equal(t, 13, first["invoice_tax_lines"])
	require.Equal(t, 7, first["items"], "5 service types + 2 extra items")
	require.Equal(t, 28, first["invoice_items"], "24 invoices + 4 extra lines")
	require.Equal(t, 8, first["customer_payment_methods"])
	require.Equal(t, 2, first["credit_applications"])
	require.Equal(t, 4, first["job_tags"])
}

func TestSeededTaxAndCreditDataIsConsistent(t *testing.T) {
	ctx := context.Background()
	pool := testdb.MigratedPool(t)
	require.NoError(t, seed.Run(ctx, pool, loadScript(t)))

	t.Run("an invoice's tax never exceeds its total", func(t *testing.T) {
		var bad int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM (
			   SELECT i.id FROM billing_documents i JOIN invoice_tax_lines l ON l.invoice_id = i.id
			   GROUP BY i.id, i.total HAVING sum(l.tax_amount) > i.total) x`).Scan(&bad))
		require.Zero(t, bad)
	})

	t.Run("tax amount follows the rate", func(t *testing.T) {
		var bad int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM invoice_tax_lines l JOIN tax_rates r ON r.id = l.tax_rate_id
			  WHERE l.tax_amount <> round(l.taxable_amount * r.rate / 100, 2)`).Scan(&bad))
		require.Zero(t, bad)
	})

	t.Run("applied credit never exceeds the credit", func(t *testing.T) {
		var bad int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM (
			   SELECT c.id FROM credits c JOIN credit_applications a ON a.credit_id = c.id
			   GROUP BY c.id, c.amount HAVING sum(a.amount) > c.amount) x`).Scan(&bad))
		require.Zero(t, bad)
	})

	t.Run("available credit of the first seeded customer", func(t *testing.T) {
		var available []string
		rows, err := pool.Query(ctx,
			`SELECT (c.amount - coalesce(sum(a.amount), 0))::text FROM credits c
			   LEFT JOIN credit_applications a ON a.credit_id = c.id
			   JOIN customers cu ON cu.id = c.customer_id AND cu.account_number = '63706'
			  GROUP BY c.id, c.amount ORDER BY c.amount`)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var s string
			require.NoError(t, rows.Scan(&s))
			available = append(available, s)
		}
		require.NoError(t, rows.Err())
		require.Equal(t, []string{"70.00", "180.00"}, available)
	})

	t.Run("customers have every status the reports filter on", func(t *testing.T) {
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(DISTINCT status) FROM customers`).Scan(&n))
		require.Equal(t, 4, n)
	})

	t.Run("each job tab has jobs", func(t *testing.T) {
		var active, pool_, deleted int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FILTER (WHERE deleted_at IS NULL AND NOT in_work_pool),
			        count(*) FILTER (WHERE in_work_pool),
			        count(*) FILTER (WHERE deleted_at IS NOT NULL) FROM jobs`).Scan(&active, &pool_, &deleted))
		require.Positive(t, active)
		require.Positive(t, pool_)
		require.Positive(t, deleted)
	})

	t.Run("only cash, check and other payments have a staff member", func(t *testing.T) {
		var bad int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM payments
			  WHERE (method_type = 'card' AND received_by IS NOT NULL)
			     OR (invoice_id IS NOT NULL AND method_type <> 'card' AND received_by IS NULL)`).Scan(&bad))
		require.Zero(t, bad)
	})
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
			`SELECT i.document_number FROM recurring_invoices r JOIN billing_documents i ON i.id = r.next_invoice_id ORDER BY r.id LIMIT 1`).Scan(&number))
		require.Equal(t, "84890", number)
	})
}
