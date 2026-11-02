//go:build integration

// Package consistency_test checks that the reports agree with each other on the seeded database: the same money seen
// through two reports adds up to the same figure, and independent SQL says so too.
package consistency_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

var (
	jan1  = day(2026, 1, 1)
	dec31 = day(2026, 12, 31)
	today = day(2026, 12, 1) // after every payment and credit application of the seed
)

func scalar(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int64 {
	t.Helper()
	var v int64
	require.NoError(t, pool.QueryRow(context.Background(), sql, args...).Scan(&v))
	return v
}

// breakdown returns the true total of the year 2026 of the Revenue or Sales Forecast grid grouped by by.
func breakdown(t *testing.T, pool *pgxpool.Pool, ty model.SalesBreakdownType, by model.SalesBreakdownBy, measure model.SalesBreakdownMeasure) model.Money {
	t.Helper()
	f := model.SalesBreakdownFilter{Type: ty, By: by, Year: 2026, YearFrom: jan1, YearTo: dec31}
	if ty == model.SalesBreakdownRevenue {
		f.Measure = measure
	}
	for m := range f.MonthStarts {
		f.MonthStarts[m] = time.Date(2026, time.Month(m+1), 1, 0, 0, 0, 0, time.UTC)
	}
	page, err := repository.NewSalesBreakdownRepository(pool).Page(context.Background(), model.SalesBreakdownQuery{
		Filter: f, Sort: model.SalesBreakdownSort{Key: model.SalesBreakdownSortName}, Limit: 1000,
	}, true)
	require.NoError(t, err)
	return page.Summary.Total
}

func TestRevenueIsTheCashCollectedPlusTheCreditUsed(t *testing.T) {
	pool := testdb.SeededPool(t)

	page, err := repository.NewPaymentsCollectedRepository(pool).Page(context.Background(), model.PaymentCollectedQuery{
		Filter: model.PaymentCollectedFilter{DateFrom: jan1, DateTo: dec31, PaymentFor: model.PaymentForInvoices},
		Sort:   model.PaymentCollectedSort{Key: model.PaymentCollectedSortDate}, Limit: 1,
	}, true)
	require.NoError(t, err)
	creditUsed := scalar(t, pool, `SELECT (COALESCE(SUM(ca.amount), 0) * 100)::bigint FROM credit_applications ca
		WHERE ca.applied_on BETWEEN '2026-01-01' AND '2026-12-31'
		  AND EXISTS (SELECT 1 FROM payments cp WHERE cp.credit_id = ca.credit_id AND cp.status = 'paid' AND cp.deleted_at IS NULL)`)
	require.NotZero(t, creditUsed, "the seed uses credit")

	require.Equal(t, page.Summary.Amount+model.Money(creditUsed),
		breakdown(t, pool, model.SalesBreakdownRevenue, model.SalesBreakdownByClient, model.SalesBreakdownCollected),
		"Revenue = Payments Collected + credit applied")
}

func TestEveryRevenueGroupingTotalsTheSame(t *testing.T) {
	pool := testdb.SeededPool(t)
	want := breakdown(t, pool, model.SalesBreakdownRevenue, model.SalesBreakdownByClient, model.SalesBreakdownCollected)

	for _, by := range model.SalesBreakdownBys {
		// by tag too: a payment under two tags is two rows, but the summary counts it once
		require.Equal(t, want, breakdown(t, pool, model.SalesBreakdownRevenue, by, model.SalesBreakdownCollected), "by %s", by)
	}
}

func TestRevenueWithoutTaxMatchesTheTaxOfTheSalesTaxSummary(t *testing.T) {
	pool := testdb.SeededPool(t)

	withTax := breakdown(t, pool, model.SalesBreakdownRevenue, model.SalesBreakdownByClient, model.SalesBreakdownCollected)
	withoutTax := breakdown(t, pool, model.SalesBreakdownRevenue, model.SalesBreakdownByClient, model.SalesBreakdownCollectedWithoutTax)
	rows, err := repository.NewSalesTaxRepository(pool).Summary(context.Background(),
		model.SalesTaxFilter{DateFrom: jan1, DateTo: dec31, Basis: model.SalesTaxCollected},
		model.SalesTaxSort{Key: model.SalesTaxSortName})
	require.NoError(t, err)
	var tax model.Money
	for _, r := range rows {
		tax += r.TaxCollected
	}
	facts := scalar(t, pool, `SELECT ((SELECT count(*) FROM payments WHERE invoice_id IS NOT NULL AND status = 'paid' AND deleted_at IS NULL)
		+ (SELECT count(*) FROM credit_applications))`)

	require.NotZero(t, tax)
	// Revenue takes the tax of a whole payment at once and the Sales Tax Summary tax line by tax line, so they can be a
	// cent apart per payment or application and tax line; the seed's invoices have a few tax lines
	require.InDelta(t, int64(withTax-withoutTax), int64(tax), float64(facts*3), "Revenue less Revenue without tax is the tax collected")
}

func TestAccountsAgingIsWhatTheUnpaidTotalSalesInvoicesStillOwe(t *testing.T) {
	pool := testdb.SeededPool(t)
	ctx := context.Background()

	sales, err := repository.NewTotalSalesRepository(pool).Page(ctx, model.TotalSaleQuery{
		Filter: model.TotalSaleFilter{
			DateFrom: day(2000, 1, 1), DateTo: day(2100, 1, 1),
			Statuses: []model.TotalSaleStatus{model.TotalSaleSent, model.TotalSalePartial},
		},
		Sort: model.TotalSaleSort{Key: model.TotalSaleSortInvoiceNumber}, Limit: 1000,
	}, false)
	require.NoError(t, err)
	ids := make([]int64, len(sales.Rows))
	for i, r := range sales.Rows {
		ids[i] = r.ID
	}
	require.NotEmpty(t, ids)
	owed := scalar(t, pool, `SELECT (SUM(i.total
			- COALESCE((SELECT SUM(p.amount) FROM payments p
				WHERE p.invoice_id = i.id AND p.status = 'paid' AND p.deleted_at IS NULL), 0)
			- COALESCE((SELECT SUM(ca.amount) FROM credit_applications ca WHERE ca.invoice_id = i.id
				AND EXISTS (SELECT 1 FROM payments cp WHERE cp.credit_id = ca.credit_id AND cp.status = 'paid' AND cp.deleted_at IS NULL)), 0)) * 100)::bigint
		FROM billing_documents i WHERE i.id = ANY($1)`, ids)

	require.NotZero(t, owed, "the seed has unpaid sales")

	aging, err := repository.NewAccountsAgingRepository(pool).Page(ctx, model.AgingQuery{
		Filter: model.AgingFilter{AsOf: today, Today: today},
		Sort:   model.AgingSort{Key: model.AgingSortCustomer}, Limit: 1000,
	}, true)
	require.NoError(t, err)

	require.Equal(t, model.Money(owed), aging.Summary.Total, "what the sent and part-paid sales still owe is the aging total")
}

func TestServiceLookupAndTheForecastAgreeOnTheValueOfTheScheduledJobs(t *testing.T) {
	pool := testdb.SeededPool(t)
	ctx := context.Background()
	lookup := repository.NewServiceLookupRepository(pool)

	var jobs model.Money
	for _, tab := range []model.ServiceLookupTab{model.ServiceLookupActive, model.ServiceLookupWorkPool} {
		f := model.ServiceLookupFilter{
			Tab: tab, ScheduledFrom: jan1, ScheduledBefore: day(2027, 1, 1),
			PoolScheduledFrom: jan1, PoolScheduledBefore: day(2027, 1, 1),
		}
		page, err := lookup.Page(ctx, model.ServiceLookupQuery{
			Filter: f, Sort: model.ServiceLookupSort{Key: model.ServiceLookupSortScheduledAt}, Limit: 1,
		}, true)
		require.NoError(t, err)
		jobs += page.Summary.InvoiceValue // a cancelled job adds nothing
	}
	joblessInvoices := scalar(t, pool, `SELECT (COALESCE(SUM(total), 0) * 100)::bigint FROM billing_documents
		WHERE parent_type = 'invoice' AND job_id IS NULL AND status IN ('sent', 'paid') AND deleted_at IS NULL AND archived_at IS NULL
		  AND document_date BETWEEN '2026-01-01' AND '2026-12-31'`)

	forecast := breakdown(t, pool, model.SalesBreakdownForecast, model.SalesBreakdownByClient, "")

	require.Equal(t, forecast-model.Money(joblessInvoices), jobs, "the forecast of the jobs is the Service Lookup value of the live jobs")
}

func TestInvoicesOutstandingIsWhatAccountsAgingSays(t *testing.T) {
	pool := testdb.SeededPool(t)
	ctx := context.Background()

	invoices, err := repository.NewInvoiceRepository(pool).Summary(ctx, model.InvoiceFilter{
		DateFrom: day(2000, 1, 1), DateTo: day(2100, 1, 1), Lifecycle: model.LifecycleActive,
		Statuses: []model.InvoiceStatus{model.InvoiceSent},
	})
	require.NoError(t, err)
	aging, err := repository.NewAccountsAgingRepository(pool).Page(ctx, model.AgingQuery{
		Filter: model.AgingFilter{AsOf: today, Today: today},
		Sort:   model.AgingSort{Key: model.AgingSortCustomer}, Limit: 1,
	}, true)
	require.NoError(t, err)

	require.NotZero(t, invoices.Outstanding)
	require.Equal(t, aging.Summary.Total, invoices.Outstanding, "the sent invoices still owe what Accounts Aging reports")
}
