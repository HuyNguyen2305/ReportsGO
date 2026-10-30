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
	revenueFactsCents = `SELECT (COALESCE(SUM(amount), 0) * 100)::bigint FROM payments
		WHERE invoice_id IS NOT NULL AND status = 'paid' AND deleted_at IS NULL
		  AND payment_date BETWEEN '2026-01-01' AND '2026-12-31'`
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
	require.Equal(t, model.Money(183126), facts[model.SalesBreakdownForecast])
	require.Equal(t, model.Money(82106), facts[model.SalesBreakdownRevenue])

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
					// rounded line shares: the summary is their sum, within cents of the facts
					require.Equal(t, rowsPlusUnassigned(page), page.Summary.SalesBreakdownAmounts)
					require.InDelta(t, int64(facts[ty]), int64(page.Summary.Total), 5)
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
		require.Equal(t, model.Money(66880), page.Unassigned.Total)
		require.Nil(t, page.Rows[0].AccountNumber)
	})

	t.Run("revenue without tax", func(t *testing.T) {
		f := filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC)
		f.Measure = model.SalesBreakdownCollectedWithoutTax
		page := e.page(t, f)

		want := scalar[int64](t, e, `SELECT (COALESCE(SUM(CASE WHEN i.total = 0 THEN p.amount
				ELSE round(p.amount * (i.total - COALESCE((SELECT SUM(tax_amount) FROM invoice_tax_lines l WHERE l.invoice_id = i.id), 0)) / i.total, 2) END), 0) * 100)::bigint
			FROM payments p JOIN billing_documents i ON i.id = p.invoice_id
			WHERE p.status = 'paid' AND p.deleted_at IS NULL AND p.payment_date BETWEEN '2026-01-01' AND '2026-12-31'`)
		require.Equal(t, model.Money(want), page.Summary.Total)
		require.Less(t, int64(page.Summary.Total), int64(82106))
	})

	t.Run("revenue by payment method", func(t *testing.T) {
		f := filter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient, time.UTC)
		f.Methods = []model.PaymentMethod{model.MethodCard}
		page := e.page(t, f)
		require.Equal(t, model.Money(scalar[int64](t, e, revenueFactsCents+` AND method_type = 'card'`)), page.Summary.Total)
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
	jobID := scalar[int64](t, e, `INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, invoice_value)
		SELECT c.id, l.id, (SELECT min(id) FROM service_types), 'confirmed', '2026-10-20 08:00+00', 100
		FROM customers c JOIN service_locations l ON l.customer_id = c.id WHERE c.account_number = '5002' RETURNING id`)
	_, err := e.pool.Exec(context.Background(), `INSERT INTO job_tag_links (job_id, tag_id)
		SELECT $1, id FROM job_tags WHERE name IN ('VIP', 'Commercial')`, jobID)
	require.NoError(t, err)

	page := e.page(t, filter(model.SalesBreakdownForecast, model.SalesBreakdownByTag, time.UTC))

	var rowSum model.Money
	for _, r := range page.Rows {
		rowSum += r.Total
	}
	require.Equal(t, rowSum, page.Summary.Total+10000, "the two-tag job shows under both tags")
	require.Equal(t, model.Money(183126+10000), page.Summary.Total, "but the summary counts it once")
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
	require.Len(t, opts.Tags, 4)
}
