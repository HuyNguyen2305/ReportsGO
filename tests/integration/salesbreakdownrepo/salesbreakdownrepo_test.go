//go:build integration

package salesbreakdownrepo_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

// env is a migrated, seeded throwaway database with a sales breakdown repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.SalesBreakdownRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewSalesBreakdownRepository(pool)}
}

// filter is the 2026 grid of ty by by, with the months read in loc.
func filter(ty model.SalesBreakdownType, by model.SalesBreakdownBy, loc *time.Location) model.SalesBreakdownFilter {
	f := model.SalesBreakdownFilter{
		Type: ty, By: by, Year: 2026,
		YearFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		YearTo:   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
	}
	for m := range f.MonthStarts {
		f.MonthStarts[m] = time.Date(2026, time.Month(m+1), 1, 0, 0, 0, 0, loc)
	}
	if ty == model.SalesBreakdownRevenue {
		f.Measure = model.SalesBreakdownCollected
	}
	return f
}

func query(f model.SalesBreakdownFilter, key model.SalesBreakdownSortKey, desc bool, limit int) model.SalesBreakdownQuery {
	return model.SalesBreakdownQuery{Filter: f, Sort: model.SalesBreakdownSort{Key: key, Desc: desc}, Limit: limit}
}

func (e env) page(t *testing.T, f model.SalesBreakdownFilter) model.SalesBreakdownPage {
	t.Helper()
	page, err := e.repo.Page(context.Background(), query(f, model.SalesBreakdownSortName, false, 1000), true)
	require.NoError(t, err)
	return page
}

func scalar[T any](t *testing.T, e env, sql string, args ...any) T {
	t.Helper()
	var v T
	require.NoError(t, e.pool.QueryRow(context.Background(), sql, args...).Scan(&v))
	return v
}

// The facts of 2026 in UTC, computed independently of the report.
const (
	forecastFactsCents = `SELECT (COALESCE((SELECT SUM(invoice_value) FROM jobs
		WHERE deleted_at IS NULL AND status <> 'cancelled'
		  AND scheduled_at >= '2026-01-01 00:00+00' AND scheduled_at < '2027-01-01 00:00+00'), 0) * 100
	  + COALESCE((SELECT SUM(total) FROM billing_documents
		WHERE parent_type = 'invoice' AND job_id IS NULL AND status IN ('sent', 'paid')
		  AND deleted_at IS NULL AND archived_at IS NULL AND document_date BETWEEN '2026-01-01' AND '2026-12-31'), 0) * 100)::bigint`
	revenueFactsCents = `SELECT (((SELECT COALESCE(SUM(amount), 0) FROM payments
		WHERE invoice_id IS NOT NULL AND status = 'paid' AND deleted_at IS NULL
		  AND payment_date BETWEEN '2026-01-01' AND '2026-12-31')
	  + (SELECT COALESCE(SUM(ca.amount), 0) FROM credit_applications ca
		WHERE ca.applied_on BETWEEN '2026-01-01' AND '2026-12-31'
		  AND EXISTS (SELECT 1 FROM payments cp WHERE cp.credit_id = ca.credit_id AND cp.status = 'paid' AND cp.deleted_at IS NULL))) * 100)::bigint`
	// revenueCardFactsCents is the same for the card method: card payments and credits bought by card.
	revenueCardFactsCents = `SELECT (((SELECT COALESCE(SUM(amount), 0) FROM payments
		WHERE invoice_id IS NOT NULL AND status = 'paid' AND deleted_at IS NULL AND method_type = 'card'
		  AND payment_date BETWEEN '2026-01-01' AND '2026-12-31')
	  + (SELECT COALESCE(SUM(ca.amount), 0) FROM credit_applications ca JOIN credits cr ON cr.id = ca.credit_id
		WHERE cr.method_type = 'card' AND ca.applied_on BETWEEN '2026-01-01' AND '2026-12-31'
		  AND EXISTS (SELECT 1 FROM payments cp WHERE cp.credit_id = ca.credit_id AND cp.status = 'paid' AND cp.deleted_at IS NULL))) * 100)::bigint`
)

func rowsPlusUnassigned(p model.SalesBreakdownPage) model.SalesBreakdownAmounts {
	var sum model.SalesBreakdownAmounts
	for _, r := range p.Rows {
		for i := range sum.Months {
			sum.Months[i] += r.Months[i]
		}
		sum.Total += r.Total
	}
	if p.Unassigned != nil {
		for i := range sum.Months {
			sum.Months[i] += p.Unassigned.Months[i]
		}
		sum.Total += p.Unassigned.Total
	}
	return sum
}

func TestPageEveryTypeAndGroupingAgreesWithTheFacts(t *testing.T) {
	e := newEnv(t)
	facts := map[model.SalesBreakdownType]model.Money{
		model.SalesBreakdownForecast: model.Money(scalar[int64](t, e, forecastFactsCents)),
		model.SalesBreakdownRevenue:  model.Money(scalar[int64](t, e, revenueFactsCents)),
	}
	require.Equal(t, model.Money(185740), facts[model.SalesBreakdownForecast])
	require.Equal(t, model.Money(87106), facts[model.SalesBreakdownRevenue])

	for _, ty := range model.SalesBreakdownTypes {
		for _, by := range model.SalesBreakdownBys {
			t.Run(fmt.Sprintf("%s by %s", ty, by), func(t *testing.T) {
				page := e.page(t, filter(ty, by, time.UTC))

				require.Equal(t, int64(len(page.Rows)), page.Summary.Count)
				var months model.Money
				for _, m := range page.Summary.Months {
					months += m
				}
				require.Equal(t, page.Summary.Total, months, "the months add up to the total")

				switch by {
				case model.SalesBreakdownByItem:
					// the biggest line takes the rounding remainder, so the shares add up to the facts exactly
					require.Equal(t, rowsPlusUnassigned(page), page.Summary.SalesBreakdownAmounts)
					require.Equal(t, facts[ty], page.Summary.Total)
				case model.SalesBreakdownByTag:
					require.Equal(t, facts[ty], page.Summary.Total, "each fact counted once")
				default:
					require.Equal(t, facts[ty], page.Summary.Total)
					require.Equal(t, rowsPlusUnassigned(page), page.Summary.SalesBreakdownAmounts,
						"a single-valued grouping: rows and unassigned add up to the total")
				}
			})
		}
	}
}

func TestPageSeedFigures(t *testing.T) {
	e := newEnv(t)

	t.Run("forecast by client", func(t *testing.T) {
		page := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByClient, time.UTC))
		require.Nil(t, page.Unassigned, "every amount has a client")
		var saliba model.SalesBreakdownRow
		for _, r := range page.Rows {
			if r.Name == "William Saliba" {
				saliba = r
			}
		}
		require.NotNil(t, saliba.AccountNumber)
		require.Equal(t, "5001", *saliba.AccountNumber)
		require.Equal(t, model.Money(97500), saliba.Months[9], "October")
		require.Equal(t, model.Money(97500), saliba.Total)
	})

	t.Run("revenue by source has the unassigned line", func(t *testing.T) {
		page := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownBySource, time.UTC))
		require.Len(t, page.Rows, 1)
		require.Equal(t, "Facebook", page.Rows[0].Name)
		require.Equal(t, model.Money(15226), page.Rows[0].Total)
		require.NotNil(t, page.Unassigned)
		require.Equal(t, model.Money(71880), page.Unassigned.Total, "including the 50.00 of credit used on 85032")
		require.Nil(t, page.Rows[0].AccountNumber)
	})

	t.Run("revenue without tax", func(t *testing.T) {
		f := filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC)
		f.Measure = model.SalesBreakdownCollectedWithoutTax
		page := e.page(t, f)

		share := func(amount string) string {
			return `CASE WHEN i.total = 0 THEN ` + amount + `
				ELSE ` + amount + ` - COALESCE((SELECT SUM(round(l.tax_amount * ` + amount + ` / i.total, 2)) FROM invoice_tax_lines l WHERE l.invoice_id = i.id), 0) END`
		}
		want := scalar[int64](t, e, `SELECT (((SELECT COALESCE(SUM(`+share("p.amount")+`), 0)
				FROM payments p JOIN billing_documents i ON i.id = p.invoice_id
				WHERE p.status = 'paid' AND p.deleted_at IS NULL AND p.payment_date BETWEEN '2026-01-01' AND '2026-12-31')
			+ (SELECT COALESCE(SUM(`+share("ca.amount")+`), 0)
				FROM credit_applications ca JOIN billing_documents i ON i.id = ca.invoice_id
				WHERE ca.applied_on BETWEEN '2026-01-01' AND '2026-12-31'
				  AND EXISTS (SELECT 1 FROM payments cp WHERE cp.credit_id = ca.credit_id AND cp.status = 'paid' AND cp.deleted_at IS NULL))) * 100)::bigint`)
		require.Equal(t, model.Money(want), page.Summary.Total)
		require.Less(t, int64(page.Summary.Total), int64(87106))
	})

	t.Run("revenue by payment method", func(t *testing.T) {
		f := filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC)
		f.Methods = []model.PaymentMethod{model.MethodCard}
		page := e.page(t, f)
		require.Equal(t, model.Money(scalar[int64](t, e, revenueCardFactsCents)), page.Summary.Total)
	})
}

func TestPageForecastCountsOnlyScheduledLiveJobs(t *testing.T) {
	e := newEnv(t)
	before := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByClient, time.UTC)).Summary.Total

	for _, sql := range []string{
		`INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, invoice_value)
		 SELECT customer_id, location_id, service_type_id, 'cancelled', '2026-10-10 08:00+00', 1000 FROM jobs LIMIT 1`,
		`INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, invoice_value, deleted_at)
		 SELECT customer_id, location_id, service_type_id, 'confirmed', '2026-10-10 08:00+00', 1000, now() FROM jobs LIMIT 1`,
		`INSERT INTO jobs (customer_id, location_id, service_type_id, status, invoice_value)
		 SELECT customer_id, location_id, service_type_id, 'confirmed', 1000 FROM jobs LIMIT 1`,
		`INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, invoice_value)
		 SELECT customer_id, location_id, service_type_id, 'confirmed', '2027-01-01 00:00+00', 1000 FROM jobs LIMIT 1`,
	} {
		_, err := e.pool.Exec(context.Background(), sql)
		require.NoError(t, err)
	}

	after := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByClient, time.UTC)).Summary.Total
	require.Equal(t, before, after, "cancelled, deleted, unscheduled and next year's jobs are not forecast")
}

func TestPageRevenueCountsOnlyPaidInvoicePayments(t *testing.T) {
	e := newEnv(t)
	before := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC)).Summary.Total

	_, err := e.pool.Exec(context.Background(), `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date, status)
		SELECT customer_id, id, 'cash', 500, '2026-10-20', 'pending' FROM billing_documents WHERE document_number = '85031'`)
	require.NoError(t, err)

	after := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC)).Summary.Total
	require.Equal(t, before, after, "a pending payment is not revenue")
	require.Equal(t, model.Money(scalar[int64](t, e, revenueFactsCents)), after, "credit payments are left out")
}

func TestPageForecastMonthsAreReadInTheBusinessTimeZone(t *testing.T) {
	e := newEnv(t)
	_, err := e.pool.Exec(context.Background(), `INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, invoice_value)
		SELECT c.id, l.id, (SELECT min(id) FROM service_types), 'confirmed', '2026-10-31 23:30+00', 7.77
		FROM customers c JOIN service_locations l ON l.customer_id = c.id WHERE c.account_number = '5002'`)
	require.NoError(t, err)

	alaba := func(loc *time.Location) model.SalesBreakdownRow {
		f := filter(model.SalesBreakdownForecast, model.SalesBreakdownByClient, loc)
		f.Search = "Alaba"
		page := e.page(t, f)
		require.Len(t, page.Rows, 1)
		return page.Rows[0]
	}

	require.Equal(t, model.Money(777), alaba(time.UTC).Months[9], "October in UTC")
	require.Equal(t, model.Money(777), alaba(time.FixedZone("UTC+7", 7*3600)).Months[10], "already November in UTC+7")
}

func TestPageTagsCountAJobUnderEachTagButOnceInTheSummary(t *testing.T) {
	e := newEnv(t)
	overcount := func() model.Money {
		page := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC))
		var rowSum model.Money
		for _, r := range page.Rows {
			rowSum += r.Total
		}
		return rowSum - page.Summary.Total
	}
	// the seed's invoices carry several tags each, so the rows already add up to more than the total
	before := overcount()
	jobID := scalar[int64](t, e, `INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, invoice_value)
		SELECT c.id, l.id, (SELECT min(id) FROM service_types), 'confirmed', '2026-10-20 08:00+00', 100
		FROM customers c JOIN service_locations l ON l.customer_id = c.id WHERE c.account_number = '5002' RETURNING id`)
	_, err := e.pool.Exec(context.Background(), `INSERT INTO job_tag_links (job_id, tag_id)
		SELECT $1, id FROM job_tags WHERE name IN ('VIP', 'Commercial')`, jobID)
	require.NoError(t, err)

	page := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC))

	require.Equal(t, before+10000, overcount(), "the two-tag job shows under both tags")
	require.Equal(t, model.Money(185740+10000), page.Summary.Total, "but the summary counts it once")
}

func TestPageGroupAndSearchFiltersDropTheUnassignedLine(t *testing.T) {
	e := newEnv(t)
	facebook := scalar[int64](t, e, `SELECT id FROM lead_sources WHERE name = 'Facebook'`)

	f := filter(model.SalesBreakdownRevenue, model.SalesBreakdownBySource, time.UTC)
	f.GroupIDs = []int64{facebook}
	byGroup := e.page(t, f)

	f = filter(model.SalesBreakdownRevenue, model.SalesBreakdownBySource, time.UTC)
	f.Search = "face"
	bySearch := e.page(t, f)

	for _, page := range []model.SalesBreakdownPage{byGroup, bySearch} {
		require.Len(t, page.Rows, 1)
		require.Nil(t, page.Unassigned)
		require.Equal(t, page.Rows[0].SalesBreakdownAmounts, page.Summary.SalesBreakdownAmounts, "the summary covers the kept groups only")
	}

	t.Run("a client by account number, wildcards literal", func(t *testing.T) {
		f := filter(model.SalesBreakdownForecast, model.SalesBreakdownByClient, time.UTC)
		f.Search = "5001"
		require.Len(t, e.page(t, f).Rows, 1)
		f.Search = "%"
		require.Empty(t, e.page(t, f).Rows)
	})
}

func TestPageItemSharesFollowTheInvoiceLines(t *testing.T) {
	e := newEnv(t)
	// 85000 (65.00) has a Bi-Monthly Service line of 65.00 and two Travel Fees of 20.00: a 10.00 payment on it is
	// split 10 * 65/85 = 7.65 and 10 * 20/85 = 2.35, the two September cells of the seed.
	page := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByItem, time.UTC))

	sep := map[string]model.Money{}
	for _, r := range page.Rows {
		sep[r.Name] = r.Months[8]
	}
	require.Equal(t, model.Money(765), sep["Bi-Monthly Service"])
	require.Equal(t, model.Money(235), sep["Travel Fee"])
}

func TestPageKeysetPagingReproducesTheUnpagedOrderForEverySort(t *testing.T) {
	e := newEnv(t)

	for _, ty := range model.SalesBreakdownTypes {
		for _, by := range []model.SalesBreakdownBy{model.SalesBreakdownByClient, model.SalesBreakdownByItem} {
			for _, key := range model.SalesBreakdownSortKeysFor(by) {
				for _, desc := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s %s %s desc=%v", ty, by, key, desc), func(t *testing.T) {
						f := filter(ty, by, time.UTC)
						all, err := e.repo.Page(context.Background(), query(f, key, desc, 1000), false)
						require.NoError(t, err)
						require.GreaterOrEqual(t, len(all.Rows), 3)

						var got []int64
						var after *model.SalesBreakdownCursor
						for range len(all.Rows) + 2 {
							q := query(f, key, desc, 2)
							q.After = after
							page, err := e.repo.Page(context.Background(), q, false)
							require.NoError(t, err)
							rows := page.Rows
							more := len(rows) > 2
							if more {
								rows = rows[:2]
							}
							for _, r := range rows {
								got = append(got, r.ID)
							}
							if !more {
								break
							}
							last := rows[len(rows)-1]
							after = &model.SalesBreakdownCursor{SortValue: last.SortValue, ID: last.ID}
						}

						want := make([]int64, len(all.Rows))
						for i, r := range all.Rows {
							want[i] = r.ID
						}
						require.Equal(t, want, got)
					})
				}
			}
		}
	}
}

func TestPageRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)

	measured := filter(model.SalesBreakdownForecast, model.SalesBreakdownByClient, time.UTC)
	measured.Measure = model.SalesBreakdownCollected
	noYear := filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC)
	noYear.YearFrom = time.Time{}

	tests := []struct {
		name string
		q    model.SalesBreakdownQuery
	}{
		{"unknown type", query(filter("budget", model.SalesBreakdownByClient, time.UTC), model.SalesBreakdownSortName, false, 10)},
		{"unknown grouping", query(filter(model.SalesBreakdownRevenue, "region", time.UTC), model.SalesBreakdownSortName, false, 10)},
		{"unknown sort key", query(filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC), "name; DROP TABLE jobs", false, 10)},
		{"account number outside clients", query(filter(model.SalesBreakdownRevenue, model.SalesBreakdownByItem, time.UTC), model.SalesBreakdownSortAccountNumber, false, 10)},
		{"a measure on the forecast", query(measured, model.SalesBreakdownSortName, false, 10)},
		{"no year", query(noYear, model.SalesBreakdownSortName, false, 10)},
		{"no limit", query(filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC), model.SalesBreakdownSortName, false, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.repo.Page(context.Background(), tt.q, true)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestGroupOptions(t *testing.T) {
	e := newEnv(t)

	opts, err := e.repo.GroupOptions(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, opts.ServiceTypes)
	require.NotEmpty(t, opts.Items)
	require.Len(t, opts.Staff, 2)
	require.NotEmpty(t, opts.Sources)
	require.Len(t, opts.Tags, 7, "4 job tags and the 3 invoice tags that no job tag shares a name with")
}

func TestPageRevenueCountsCreditUsedNotCreditBought(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	total := func() model.Money {
		return e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC)).Summary.Total
	}
	before := total()

	// A new credit, paid for in September, is not revenue until it is used. One whose payment was refunded never is.
	for _, status := range []string{"paid", "refunded"} {
		var creditID int64
		require.NoError(t, e.pool.QueryRow(ctx, `INSERT INTO credits (customer_id, amount, method_type, credit_date)
			SELECT customer_id, 40, 'check', '2026-09-01' FROM billing_documents WHERE document_number = '85031' RETURNING id`).Scan(&creditID))
		_, err := e.pool.Exec(ctx, `INSERT INTO payments (customer_id, credit_id, method_type, amount, payment_date, status)
			SELECT customer_id, id, 'check', 40, '2026-09-01', $2::payment_status FROM credits WHERE id = $1`, creditID, status)
		require.NoError(t, err)
		_, err = e.pool.Exec(ctx, `INSERT INTO credit_applications (credit_id, invoice_id, amount, applied_on)
			SELECT $1, id, 40, '2026-11-03' FROM billing_documents WHERE parent_type = 'invoice' AND document_number = '85031'`, creditID)
		require.NoError(t, err)

		if status == "paid" {
			require.Equal(t, before+4000, total(), "the 40.00 is revenue when the credit is used")
			page := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC))
			require.Equal(t, model.Money(4000), page.Summary.Months[10], "in November, the month it is applied")
		} else {
			require.Equal(t, before+4000, total(), "a credit without a paid payment is not revenue")
		}
	}

	f := filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC)
	f.Methods = []model.PaymentMethod{model.MethodCheck}
	require.Equal(t, model.Money(scalar[int64](t, e, `SELECT (COALESCE(SUM(amount), 0) * 100)::bigint FROM (
			SELECT amount FROM payments WHERE invoice_id IS NOT NULL AND status = 'paid' AND deleted_at IS NULL AND method_type = 'check'
			UNION ALL
			SELECT ca.amount FROM credit_applications ca JOIN credits cr ON cr.id = ca.credit_id
			WHERE cr.method_type = 'check' AND EXISTS (SELECT 1 FROM payments cp WHERE cp.credit_id = cr.id AND cp.status = 'paid' AND cp.deleted_at IS NULL)) x`)),
		e.page(t, f).Summary.Total, "a credit application follows the method of its credit")
}

func TestPageRevenueByStaffIsWhoeverTookThePayment(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	page := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByStaff, time.UTC))

	rows, err := e.pool.Query(ctx, `SELECT u.full_name, (SUM(p.amount) * 100)::bigint
		FROM payments p JOIN users u ON u.id = p.received_by
		WHERE p.invoice_id IS NOT NULL AND p.status = 'paid' AND p.deleted_at IS NULL
		  AND p.payment_date BETWEEN '2026-01-01' AND '2026-12-31'
		GROUP BY u.full_name`)
	require.NoError(t, err)
	defer rows.Close()
	want := map[string]model.Money{}
	for rows.Next() {
		var name string
		var cents int64
		require.NoError(t, rows.Scan(&name, &cents))
		want[name] = model.Money(cents)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, want, "the seed has cash and check payments with a staff member")

	got := map[string]model.Money{}
	for _, r := range page.Rows {
		got[r.Name] = r.Total
	}
	require.Equal(t, want, got)

	// online card payments have no one who took them, and used credit has none either
	unassigned := model.Money(scalar[int64](t, e, `SELECT (((SELECT COALESCE(SUM(amount), 0) FROM payments
			WHERE invoice_id IS NOT NULL AND received_by IS NULL AND status = 'paid' AND deleted_at IS NULL
			  AND payment_date BETWEEN '2026-01-01' AND '2026-12-31')
		+ (SELECT COALESCE(SUM(ca.amount), 0) FROM credit_applications ca
			WHERE EXISTS (SELECT 1 FROM payments cp WHERE cp.credit_id = ca.credit_id AND cp.status = 'paid' AND cp.deleted_at IS NULL))) * 100)::bigint`))
	require.NotNil(t, page.Unassigned)
	require.Equal(t, unassigned, page.Unassigned.Total)
}

// tagRow finds the row of the tag named name.
func tagRow(t *testing.T, page model.SalesBreakdownPage, name string) model.SalesBreakdownRow {
	t.Helper()
	for _, r := range page.Rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no tag row %q in %v", name, page.Rows)
	return model.SalesBreakdownRow{}
}

func TestPageTagsComeFromTheJobAndTheInvoice(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// addInvoice inserts a sent, job-less invoice of 5002 dated October 2026, with the given invoice tags.
	addInvoice := func(number string, total float64, tags ...string) {
		_, err := e.pool.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total, status)
			SELECT 'invoice', $1, id, '2026-10-10', $2, 'sent' FROM customers WHERE account_number = '5002'`, number, total)
		require.NoError(t, err)
		for _, tag := range tags {
			_, err = e.pool.Exec(ctx, `INSERT INTO invoice_tags (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`, tag)
			require.NoError(t, err)
			_, err = e.pool.Exec(ctx, `INSERT INTO invoice_tag_links (invoice_id, tag_id)
				SELECT i.id, t.id FROM billing_documents i, invoice_tags t
				WHERE i.parent_type = 'invoice' AND i.document_number = $1 AND t.name = $2`, number, tag)
			require.NoError(t, err)
		}
	}

	t.Run("a job-less invoice is under its invoice tags, an invoice-only tag has a negative id", func(t *testing.T) {
		addInvoice("T-1", 40, "rush")

		page := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC))

		rush := tagRow(t, page, "rush")
		require.Less(t, rush.ID, int64(0))
		require.Equal(t, model.Money(4000), rush.Total)
		for _, name := range []string{"a", "b", "c"} {
			require.Less(t, tagRow(t, page, name).ID, int64(0), "invoice-only tag %s", name)
		}
	})

	t.Run("a job's amount is also under the tags of its invoices", func(t *testing.T) {
		page := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC))
		// the seed's 85030, 85028 and 85005 carry a, b and c; their job is worth 1.00
		require.Equal(t, model.Money(100), tagRow(t, page, "a").Total)
	})

	t.Run("an invoice tag and a job tag of the same name are one row", func(t *testing.T) {
		_, err := e.pool.Exec(ctx, `INSERT INTO invoice_tags (name) VALUES ('vip')`)
		require.NoError(t, err)
		_, err = e.pool.Exec(ctx, `INSERT INTO invoice_tag_links (invoice_id, tag_id)
			SELECT i.id, t.id FROM billing_documents i, invoice_tags t
			WHERE i.parent_type = 'invoice' AND i.document_number = '85030' AND t.name = 'vip'`)
		require.NoError(t, err)

		page := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC))

		vip := tagRow(t, page, "VIP")
		require.Greater(t, vip.ID, int64(0), "it is the job tag's id")
		count := 0
		for _, r := range page.Rows {
			if r.Name == "vip" || r.Name == "VIP" {
				count++
			}
		}
		require.Equal(t, 1, count)
	})

	t.Run("a payment is under the tags of its job and of its invoice, once", func(t *testing.T) {
		revenue := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByTag, time.UTC))
		require.Equal(t, model.Money(scalar[int64](t, e, revenueFactsCents)), revenue.Summary.Total, "each payment counted once")
		require.NotZero(t, tagRow(t, revenue, "a").Total, "the paid 85005 carries a and b")
	})

	t.Run("untagged amounts stay unassigned", func(t *testing.T) {
		require.Nil(t, e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC)).Unassigned, "every seed amount is tagged")
		addInvoice("T-2", 25)

		page := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC))

		require.NotNil(t, page.Unassigned)
		require.Equal(t, model.Money(2500), page.Unassigned.Total)
	})

	t.Run("a group filter takes a negative id", func(t *testing.T) {
		all := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC))
		a := tagRow(t, all, "a")
		f := filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC)
		f.GroupIDs = []int64{a.ID}

		page := e.page(t, f)

		require.Len(t, page.Rows, 1)
		require.Equal(t, a.Total, page.Rows[0].Total)
		require.Equal(t, a.Total, page.Summary.Total)
		require.Nil(t, page.Unassigned)
	})
}

// addTaggedInvoice inserts a sent, job-less invoice of customer 5002 in October 2026 with the given total, tags and
// state, and returns its id.
func (e env) addTaggedInvoice(t *testing.T, number string, total float64, state string, tags ...string) int64 {
	t.Helper()
	ctx := context.Background()
	id := scalar[int64](t, e, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total, status,
			archived_at, deleted_at)
		SELECT 'invoice', $1, id, '2026-10-10', $2, CASE WHEN $3 = 'void' THEN 'void' ELSE 'sent' END::billing_status,
		       CASE WHEN $3 = 'archived' THEN now() END, CASE WHEN $3 = 'deleted' THEN now() END
		FROM customers WHERE account_number = '5002' RETURNING id`, number, total, state)
	for _, tag := range tags {
		_, err := e.pool.Exec(ctx, `INSERT INTO invoice_tags (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`, tag)
		require.NoError(t, err)
		_, err = e.pool.Exec(ctx, `INSERT INTO invoice_tag_links (invoice_id, tag_id) SELECT $1, id FROM invoice_tags WHERE name = $2`, id, tag)
		require.NoError(t, err)
	}
	return id
}

func TestPageTagsOnlyComeFromInvoicesThatAreNotVoidOrDeleted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rows := func(ty model.SalesBreakdownType) map[string]model.Money {
		out := map[string]model.Money{}
		for _, r := range e.page(t, filter(ty, model.SalesBreakdownByTag, time.UTC)).Rows {
			out[r.Name] = r.Total
		}
		return out
	}

	// job-less invoices (a forecast fact) in every state, each with a tag of its own
	e.addTaggedInvoice(t, "S-live", 10, "live", "t-live")
	e.addTaggedInvoice(t, "S-archived", 20, "archived", "t-archived")
	e.addTaggedInvoice(t, "S-void", 30, "void", "t-void")
	e.addTaggedInvoice(t, "S-deleted", 40, "deleted", "t-deleted")

	forecast := rows(model.SalesBreakdownForecast)
	require.Equal(t, model.Money(1000), forecast["t-live"])
	require.NotContains(t, forecast, "t-archived", "an archived invoice is not forecast, so it has no fact")
	require.NotContains(t, forecast, "t-void")
	require.NotContains(t, forecast, "t-deleted")

	// a payment on each of them is revenue whatever became of the invoice; only the live and archived invoices lend their tags
	for _, number := range []string{"S-live", "S-archived", "S-void", "S-deleted"} {
		_, err := e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date, status)
			SELECT customer_id, id, 'cash', 5, '2026-10-12', 'paid' FROM billing_documents WHERE parent_type = 'invoice' AND document_number = $1`, number)
		require.NoError(t, err)
	}
	revenue := rows(model.SalesBreakdownRevenue)
	require.Equal(t, model.Money(500), revenue["t-live"])
	require.Equal(t, model.Money(500), revenue["t-archived"], "an archived invoice still lends its tags")
	require.NotContains(t, revenue, "t-void", "a void invoice does not")
	require.NotContains(t, revenue, "t-deleted", "nor a deleted one")

	page := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC))
	require.Equal(t, model.Money(scalar[int64](t, e, revenueFactsCents)), page.Summary.Total, "the payments on void and deleted invoices are still revenue")
}

func TestPageInvoiceTagsOfTheSameNameAreOneRow(t *testing.T) {
	e := newEnv(t)
	e.addTaggedInvoice(t, "S-1", 10, "live", "Rush")
	e.addTaggedInvoice(t, "S-2", 15, "live", "rush")

	page := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC))

	var rush []model.SalesBreakdownRow
	for _, r := range page.Rows {
		if r.Name == "Rush" || r.Name == "rush" {
			rush = append(rush, r)
		}
	}
	require.Len(t, rush, 1, "'Rush' and 'rush' are one tag")
	require.Equal(t, "Rush", rush[0].Name, "named by the lowest id")
	require.Equal(t, model.Money(2500), rush[0].Total)
	require.Less(t, rush[0].ID, int64(0))

	opts, err := e.repo.GroupOptions(context.Background())
	require.NoError(t, err)
	n := 0
	for _, o := range opts.Tags {
		if o.Name == "Rush" || o.Name == "rush" {
			n++
			require.Equal(t, rush[0].ID, o.ID, "the filter option has the row's id")
		}
	}
	require.Equal(t, 1, n)

	f := filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC)
	f.GroupIDs = []int64{rush[0].ID}
	filtered := e.page(t, f)
	require.Len(t, filtered.Rows, 1)
	require.Equal(t, model.Money(2500), filtered.Summary.Total)
}

func TestPageItemSharesOfAnUnevenSplitAddUpToTheAmount(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// 10.00 paid on an invoice of three equal lines: thirds do not round to the cent, the first line takes the cent over
	invoice := e.addTaggedInvoice(t, "R-1", 30, "live")
	for _, name := range []string{"Item A", "Item B", "Item C"} {
		_, err := e.pool.Exec(ctx, `INSERT INTO items (name) VALUES ($1)`, name)
		require.NoError(t, err)
		_, err = e.pool.Exec(ctx, `INSERT INTO invoice_items (invoice_id, item_id, line_total)
			SELECT $1, id, 10 FROM items WHERE name = $2`, invoice, name)
		require.NoError(t, err)
	}
	_, err := e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date, status)
		SELECT customer_id, id, 'cash', 10, '2026-12-05', 'paid' FROM billing_documents WHERE id = $1`, invoice)
	require.NoError(t, err)

	page := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByItem, time.UTC))
	dec := map[string]model.Money{}
	for _, r := range page.Rows {
		dec[r.Name] = r.Months[11]
	}

	require.Equal(t, model.Money(334), dec["Item A"], "the lowest id among equal lines takes the remainder")
	require.Equal(t, model.Money(333), dec["Item B"])
	require.Equal(t, model.Money(333), dec["Item C"])
	require.Equal(t, model.Money(1000), dec["Item A"]+dec["Item B"]+dec["Item C"])
	byClient := e.page(t, filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC))
	require.Equal(t, byClient.Summary.Total, page.Summary.Total, "the item grouping totals the same as clients")
}
