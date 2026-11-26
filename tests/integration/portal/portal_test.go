//go:build integration

// Package portal_test checks the views in db/portal/reports_live.sql: ClientPortalGO's schema is created next to
// ReportsGO's, and the existing report queries run over the views.
package portal_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/seed"
	"reportsgo/tests/integration/testdb"
)

func poolFor(t *testing.T, dbURL, searchPath string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err)
	if searchPath != "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = searchPath
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func execFile(t *testing.T, pool *pgxpool.Pool, path string) {
	t.Helper()
	sql, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), string(sql))
	require.NoError(t, err, path)
}

// world is one database holding ReportsGO's seeded tables in public, the portal's tables with a few rows in
// schema portal, and the views in reports_live.
type world struct{ reports *pgxpool.Pool }

func newWorld(t *testing.T) world {
	t.Helper()
	ctx := context.Background()
	root := testdb.RepoRoot(t)
	portalMigrations := filepath.Join(root, "..", "ClientPortalGO", "migrations")
	if _, err := os.Stat(portalMigrations); err != nil {
		t.Skip("ClientPortalGO is not next to ReportsGO")
	}

	dbURL := testdb.NewDatabase(t)
	require.NoError(t, testdb.NewMigrator(t, dbURL).Up())
	public := poolFor(t, dbURL, "")
	script, err := seed.Load(filepath.Join(root, "db", "seed", "seed.sql"))
	require.NoError(t, err)
	require.NoError(t, seed.Run(ctx, public, script))

	_, err = public.Exec(ctx, `CREATE SCHEMA portal`)
	require.NoError(t, err)
	portal := poolFor(t, dbURL, "portal")
	files, err := filepath.Glob(filepath.Join(portalMigrations, "*.up.sql"))
	require.NoError(t, err)
	sort.Strings(files)
	for _, f := range files {
		execFile(t, portal, f)
	}
	// Jane's October 12 estimate became invoice 85001; a second estimate belongs to a cancelled booking.
	// The 1000-cent payment is made at 22:30 on October 12 in Chicago, which is already October 13 in UTC.
	_, err = portal.Exec(ctx, `
INSERT INTO customers (name, email, phone) VALUES ('Jane Doe', 'jane@example.com', '555-0100');
INSERT INTO locations (customer_id, street, city, state, postal_code) VALUES (1, '1 Main St', 'Austin', 'TX', '78701');
INSERT INTO services (name, duration_minutes, price_cents) VALUES ('Weekly Service', 30, 4500);
INSERT INTO billing_documents (parent_type, number, customer_id, location_id, title, status, document_date, total_cents, converted_from_id)
VALUES ('estimate', 5001, 1, 1, 'Weekly Service', 'accepted', '2026-10-12', 4500, NULL),
       ('estimate', 5002, 1, 1, 'Weekly Service', 'declined', '2026-10-19', 4500, NULL);
INSERT INTO billing_documents (parent_type, number, customer_id, location_id, title, status, document_date, total_cents, amount_paid_cents, converted_from_id)
VALUES ('invoice', 85001, 1, 1, 'Weekly Service', 'partially_paid', '2026-10-12', 4500, 1500, 1);
INSERT INTO bookings (customer_id, location_id, service_id, estimate_id, starts_at, ends_at, cancelled_at)
VALUES (1, 1, 1, 1, '2026-10-12 14:00Z', '2026-10-12 14:30Z', NULL),
       (1, 1, 1, 2, '2026-10-19 14:00Z', '2026-10-19 14:30Z', now());
INSERT INTO work_orders (booking_id, customer_id, service_id, status) VALUES (1, 1, 1, 'completed'), (2, 1, 1, 'scheduled');
INSERT INTO payments (billing_document_id, amount_cents, paid_at, method, card_brand, card_last4)
VALUES (3, 1000, '2026-10-13 03:30:00Z', 'card', 'visa', '4242'), (3, 500, '2026-10-14 15:00:00Z', 'cash', NULL, NULL);`)
	require.NoError(t, err)

	execFile(t, public, filepath.Join(root, "db", "portal", "reports_live.sql"))
	return world{reports: poolFor(t, dbURL, "reports_live,public")}
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestReportsReadPortalData(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	october := func() (time.Time, time.Time) { return day(2026, 10, 1), day(2026, 10, 31) }

	t.Run("Total Sales has the portal invoice and nothing from the seed", func(t *testing.T) {
		from, to := october()
		page, err := repository.NewTotalSalesRepository(w.reports).Page(ctx, model.TotalSaleQuery{
			Filter: model.TotalSaleFilter{DateFrom: from, DateTo: to},
			Sort:   model.TotalSaleSort{Key: model.TotalSaleSortInvoiceNumber}, Limit: 100}, false)
		require.NoError(t, err)
		require.Len(t, page.Rows, 1)
		r := page.Rows[0]
		assert.Equal(t, "85001", r.InvoiceNumber)
		assert.Equal(t, "Jane Doe", r.CustomerName)
		assert.Equal(t, model.Money(4500), r.Amount)
		assert.Equal(t, model.Money(4500), r.WithoutTax, "the portal has no tax")
		assert.Equal(t, model.TotalSalePartial, r.Status)
	})

	t.Run("Payments Collected has the method and card, and dates payments in the business timezone", func(t *testing.T) {
		from, to := october()
		page, err := repository.NewPaymentsCollectedRepository(w.reports).Page(ctx, model.PaymentCollectedQuery{
			Filter: model.PaymentCollectedFilter{DateFrom: from, DateTo: to, PaymentFor: model.PaymentForInvoices},
			Sort:   model.PaymentCollectedSort{Key: model.PaymentCollectedSortDate}, Limit: 100}, false)
		require.NoError(t, err)
		require.Len(t, page.Rows, 2)
		card, cash := page.Rows[0], page.Rows[1]
		assert.Equal(t, day(2026, 10, 12), card.Date, "22:30 on the 12th in Chicago is already the 13th in UTC")
		assert.Equal(t, model.Money(1000), card.Amount)
		assert.Equal(t, model.Money(1000), card.WithoutTax)
		assert.Equal(t, model.MethodCard, card.Method)
		require.NotNil(t, card.CardBrand)
		require.NotNil(t, card.CardLast4)
		assert.Equal(t, "visa", *card.CardBrand)
		assert.Equal(t, "4242", *card.CardLast4)
		assert.Equal(t, "85001", *card.InvoiceNumber)
		require.NotNil(t, card.ServiceState)
		assert.Equal(t, "TX", *card.ServiceState, "through the invoice's job and its location")
		assert.Equal(t, model.MethodCash, cash.Method)
		assert.Nil(t, cash.CardBrand)
		assert.Equal(t, day(2026, 10, 14), cash.Date)
	})

	t.Run("Invoices and Estimates", func(t *testing.T) {
		inv, err := repository.NewInvoiceRepository(w.reports).Page(ctx, model.InvoiceQuery{
			Sort: model.InvoiceSort{Key: model.SortInvoiceNumber}, Limit: 100}, false)
		require.NoError(t, err)
		assert.Len(t, inv.Rows, 1)
		est, err := repository.NewEstimateRepository(w.reports).Page(ctx, model.EstimateQuery{
			Sort: model.EstimateSort{Key: model.EstimateSortNumber}, Limit: 100}, false)
		require.NoError(t, err)
		assert.Len(t, est.Rows, 2)
	})

	t.Run("New Jobs lists each work order, a cancelled booking as cancelled", func(t *testing.T) {
		jobs, err := repository.NewJobRepository(w.reports).Page(ctx, model.JobQuery{
			Sort: model.JobSort{Key: model.JobSortCustomer}, Limit: 100}, false)
		require.NoError(t, err)
		var got []string
		for _, r := range jobs.Rows {
			got = append(got, strings.Join([]string{r.ServiceType, string(r.Status)}, ":"))
		}
		assert.ElementsMatch(t, []string{"Weekly Service:completed", "Weekly Service:cancelled"}, got)
	})

	t.Run("Accounts Aging counts the unpaid balance", func(t *testing.T) {
		page, err := repository.NewAccountsAgingRepository(w.reports).Page(ctx, model.AgingQuery{
			Filter: model.AgingFilter{AsOf: day(2026, 10, 31)},
			Sort:   model.AgingSort{Key: model.AgingSortCustomer}, Limit: 100}, false)
		require.NoError(t, err)
		require.Len(t, page.Rows, 1)
	})
}
