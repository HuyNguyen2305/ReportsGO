//go:build integration

package accountsagingrepo_test

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

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

var (
	oct21 = day(2026, 10, 21) // the day the seed's reports are read at
	oct20 = day(2026, 10, 20)
	nov30 = day(2026, 11, 30)
)

// env is a migrated, seeded throwaway database with an Accounts Aging repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.AccountsAgingRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewAccountsAgingRepository(pool)}
}

func query(f model.AgingFilter, key model.AgingSortKey, desc bool, limit int) model.AgingQuery {
	return model.AgingQuery{Filter: f, Sort: model.AgingSort{Key: key, Desc: desc}, Limit: limit}
}

func (e env) page(t *testing.T, f model.AgingFilter) model.AgingPage {
	t.Helper()
	page, err := e.repo.Page(context.Background(), query(f, model.AgingSortCustomer, false, 1000), true)
	require.NoError(t, err)
	return page
}

func names(rows []model.AgingRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.CustomerName
	}
	return out
}

func (e env) customerID(t *testing.T, account string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, e.pool.QueryRow(context.Background(),
		`SELECT id FROM customers WHERE account_number = $1`, account).Scan(&id))
	return id
}

// addInvoice inserts an invoice without a job, so it falls under its customer with no location.
func (e env) addInvoice(t *testing.T, number, account string, date time.Time, total float64, status string, archived, deleted bool) {
	t.Helper()
	_, err := e.pool.Exec(context.Background(), `INSERT INTO billing_documents
		(parent_type, document_number, customer_id, document_date, total, status, archived_at, deleted_at)
		SELECT 'invoice', $1, c.id, $3::date, $4, $5::billing_status,
		       CASE WHEN $6 THEN now() END, CASE WHEN $7 THEN now() END
		FROM customers c WHERE c.account_number = $2`, number, account, date, total, status, archived, deleted)
	require.NoError(t, err)
}

// addAgedInvoices gives customer 5001 one sent invoice per bucket edge, dated so that each is exactly the given
// number of days old on oct21: 30 and 31, 60 and 61, 90 and 91 days.
func (e env) addAgedInvoices(t *testing.T) {
	t.Helper()
	for i, age := range []int{30, 31, 60, 61, 90, 91} {
		e.addInvoice(t, fmt.Sprintf("9%d", i), "5001", oct21.AddDate(0, 0, -age), float64(10*(i+1)), "sent", false, false)
	}
}

func TestPageCurrentSeedBalances(t *testing.T) {
	e := newEnv(t)

	page := e.page(t, model.AgingFilter{AsOf: oct21})

	// 85032 (100.00, part paid 30.00 on 10-21; the credit applications come on 10-22 and 10-25) and 85033 (99.00).
	require.Equal(t, []string{"AAA1 ccc1", "Bukayo Saka"}, names(page.Rows))
	first, second := page.Rows[0], page.Rows[1]
	require.Equal(t, model.Money(7000), first.D0to30)
	require.Equal(t, model.Money(7000), first.Total)
	require.Equal(t, model.Money(30000), first.AvailableCredit, "100.00 + 200.00, none applied yet")
	require.NotNil(t, first.Card)
	require.Equal(t, model.AgingCard{Processor: "stripe", Last4: "3222", ExpMonth: 12, ExpYear: 2032}, *first.Card, "the default card")
	require.NotNil(t, first.LocationID)
	require.NotNil(t, first.Location)
	require.Contains(t, *first.Location, "Miami")
	require.Equal(t, model.Money(9900), second.D0to30)
	require.Nil(t, second.Card)
	require.Equal(t, model.Money(0), second.AvailableCredit)

	require.NotNil(t, page.Summary)
	require.Equal(t, model.AgingSummary{Count: 2, D0to30: 16900, Total: 16900, AvailableCredit: 30000}, *page.Summary)
}

func TestPagePointInTimeReplaysPaymentsAndCredits(t *testing.T) {
	e := newEnv(t)

	t.Run("before the part payment and the credit applications", func(t *testing.T) {
		page := e.page(t, model.AgingFilter{AsOf: oct20})

		require.Equal(t, model.Money(10000), page.Rows[0].Total, "85032 is still in full")
		require.Equal(t, model.Money(30000), page.Rows[0].AvailableCredit)
	})

	t.Run("after both credit applications, 40 days later", func(t *testing.T) {
		page := e.page(t, model.AgingFilter{AsOf: nov30})

		require.Equal(t, []string{"AAA1 ccc1", "Bukayo Saka", "William Saliba"}, names(page.Rows))
		require.Equal(t, model.Money(2000), page.Rows[0].D31to60, "100.00 less 30.00 paid and 50.00 of credit")
		require.Equal(t, model.Money(25000), page.Rows[0].AvailableCredit, "300.00 less 50.00 applied")
		require.Equal(t, model.Money(0), page.Rows[0].D0to30)
		require.Equal(t, model.AgingSummary{Count: 3, D31to60: 21400, Total: 21400, AvailableCredit: 25000}, *page.Summary)
	})
}

func TestPageBucketEdgesAndExcludedInvoices(t *testing.T) {
	e := newEnv(t)
	e.addAgedInvoices(t)
	// none of these is a receivable
	e.addInvoice(t, "9100", "5001", oct21.AddDate(0, 0, -100), 500, "draft", false, false)
	e.addInvoice(t, "9101", "5001", oct21.AddDate(0, 0, -100), 500, "void", false, false)
	e.addInvoice(t, "9102", "5001", oct21.AddDate(0, 0, -100), 500, "sent", true, false)
	e.addInvoice(t, "9103", "5001", oct21.AddDate(0, 0, -100), 500, "sent", false, true)
	e.addInvoice(t, "9104", "5001", oct21.AddDate(0, 0, 1), 500, "sent", false, false) // dated after as_of
	e.addInvoice(t, "9105", "5001", oct21.AddDate(0, 0, -100), 500, "paid", false, false)
	_, err := e.pool.Exec(context.Background(), `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date)
		SELECT customer_id, id, 'cash', 500, '2026-10-01' FROM billing_documents WHERE document_number = '9105'`)
	require.NoError(t, err)

	page := e.page(t, model.AgingFilter{AsOf: oct21})

	var row model.AgingRow
	for _, r := range page.Rows {
		if r.CustomerID == e.customerID(t, "5001") {
			row = r
		}
	}
	require.NotZero(t, row.ID, "customer 5001 has open invoices")
	require.Nil(t, row.LocationID, "invoices without a job fall under the customer alone")
	require.Nil(t, row.Location)
	require.Equal(t, model.Money(1000), row.D0to30, "30 days old")
	require.Equal(t, model.Money(2000+3000), row.D31to60, "31 and 60 days old")
	require.Equal(t, model.Money(4000+5000), row.D61to90, "61 and 90 days old")
	require.Equal(t, model.Money(6000), row.D90Plus, "91 days old")
	require.Equal(t, model.Money(21000), row.Total)
	require.Equal(t, row.D0to30+row.D31to60+row.D61to90+row.D90Plus, row.Total)
}

func TestPageDaysFilterKeepsRowsWithOlderBalances(t *testing.T) {
	e := newEnv(t)
	e.addAgedInvoices(t)                                                                 // 5001: balances in every bucket
	e.addInvoice(t, "9200", "63714", oct21.AddDate(0, 0, -90), 15, "sent", false, false) // 63714: exactly 90 days old

	tests := []struct {
		days int
		want []string
	}{
		{0, nil}, // checked below: everything
		{30, []string{"5001", "63714"}},
		{60, []string{"5001", "63714"}},
		{90, []string{"5001"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.days), func(t *testing.T) {
			page := e.page(t, model.AgingFilter{AsOf: oct21, MinAgeDays: tt.days})
			var got []int64
			for _, r := range page.Rows {
				got = append(got, r.CustomerID)
			}
			if tt.days == 0 {
				require.Len(t, page.Rows, 4, "the two seed customers and both new ones")
				return
			}
			want := []int64{e.customerID(t, tt.want[0])}
			if len(tt.want) > 1 {
				want = append(want, e.customerID(t, tt.want[1]))
			}
			require.ElementsMatch(t, want, got)
			require.Equal(t, int64(len(want)), page.Summary.Count)
		})
	}
}

func TestPageServiceTypeEmailAndSearchFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	var serviceType int64
	var email string
	require.NoError(t, e.pool.QueryRow(ctx, `SELECT j.service_type_id, b.billing_email
		FROM billing_documents b JOIN jobs j ON j.id = b.job_id WHERE b.document_number = '85033'`).Scan(&serviceType, &email))
	require.NotEmpty(t, email)

	t.Run("service type", func(t *testing.T) {
		page := e.page(t, model.AgingFilter{AsOf: oct21, ServiceTypeIDs: []int64{serviceType}})
		require.Contains(t, names(page.Rows), "Bukayo Saka")
		require.Equal(t, int64(len(page.Rows)), page.Summary.Count)

		none := e.page(t, model.AgingFilter{AsOf: oct21, ServiceTypeIDs: []int64{serviceType + 100000}})
		require.Empty(t, none.Rows)
		require.Equal(t, model.AgingSummary{}, *none.Summary)
	})

	t.Run("billing email, any case, as a substring", func(t *testing.T) {
		page := e.page(t, model.AgingFilter{AsOf: oct21, Email: email[1:4]})
		require.Contains(t, names(page.Rows), "Bukayo Saka")

		none := e.page(t, model.AgingFilter{AsOf: oct21, Email: "no-such-address"})
		require.Empty(t, none.Rows)
	})

	t.Run("search", func(t *testing.T) {
		page := e.page(t, model.AgingFilter{AsOf: oct21, Search: "SAKA"})
		require.Equal(t, []string{"Bukayo Saka"}, names(page.Rows))
		require.Equal(t, model.Money(9900), page.Summary.Total)

		byAddress := e.page(t, model.AgingFilter{AsOf: oct21, Search: "miami"})
		require.Equal(t, []string{"AAA1 ccc1"}, names(byAddress.Rows))

		wildcard := e.page(t, model.AgingFilter{AsOf: oct21, Search: "%"})
		require.Empty(t, wildcard.Rows, "LIKE wildcards are matched literally")
	})
}

func TestPageAGivenCustomerWithTwoLocationsHasTwoRowsButCreditCountsOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// a second location and job for customer 63706, with an open invoice of its own
	_, err := e.pool.Exec(ctx, `WITH loc AS (
			INSERT INTO service_locations (customer_id, address_line1, city)
			SELECT id, '9 Second Street', 'Tampa' FROM customers WHERE account_number = '63706' RETURNING id, customer_id),
		job AS (
			INSERT INTO jobs (customer_id, location_id, service_type_id)
			SELECT loc.customer_id, loc.id, (SELECT min(id) FROM service_types) FROM loc RETURNING id, customer_id)
		INSERT INTO billing_documents (parent_type, document_number, customer_id, job_id, document_date, total, status)
		SELECT 'invoice', '9300', job.customer_id, job.id, '2026-10-10', 40, 'sent' FROM job`)
	require.NoError(t, err)

	page := e.page(t, model.AgingFilter{AsOf: oct21})

	var mine []model.AgingRow
	for _, r := range page.Rows {
		if r.CustomerID == e.customerID(t, "63706") {
			mine = append(mine, r)
		}
	}
	require.Len(t, mine, 2)
	require.NotEqual(t, mine[0].ID, mine[1].ID, "each customer and location is its own row")
	require.Equal(t, model.Money(30000), mine[0].AvailableCredit)
	require.Equal(t, model.Money(30000), mine[1].AvailableCredit, "the customer's credit repeats on each of its rows")
	require.Equal(t, model.Money(30000), page.Summary.AvailableCredit, "but the summary counts it once")
	require.Equal(t, int64(3), page.Summary.Count)
	require.Equal(t, model.Money(7000+4000+9900), page.Summary.Total)
}

func TestPageSummaryAlwaysEqualsTheSumOfTheRows(t *testing.T) {
	e := newEnv(t)
	e.addAgedInvoices(t)

	for _, f := range []model.AgingFilter{
		{AsOf: oct21},
		{AsOf: nov30},
		{AsOf: oct21, MinAgeDays: 60},
		{AsOf: oct21, Search: "a"},
		{AsOf: day(2020, 1, 1)},
	} {
		page := e.page(t, f)

		var s model.AgingSummary
		credit := map[int64]model.Money{}
		for _, r := range page.Rows {
			s.D0to30 += r.D0to30
			s.D31to60 += r.D31to60
			s.D61to90 += r.D61to90
			s.D90Plus += r.D90Plus
			s.Total += r.Total
			credit[r.CustomerID] = r.AvailableCredit
		}
		for _, c := range credit {
			s.AvailableCredit += c
		}
		s.Count = int64(len(page.Rows))
		require.Equal(t, s, *page.Summary, "%+v", f)
	}
}

func TestPageKeysetPagingReproducesTheUnpagedOrderForEverySort(t *testing.T) {
	e := newEnv(t)
	e.addAgedInvoices(t)
	e.addInvoice(t, "9400", "63714", oct21.AddDate(0, 0, -45), 70, "sent", false, false)
	e.addInvoice(t, "9401", "63733", oct21.AddDate(0, 0, -5), 70, "sent", false, false) // ties with 9400 on total

	f := model.AgingFilter{AsOf: oct21}
	for _, key := range model.AgingSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s desc=%v", key, desc), func(t *testing.T) {
				all, err := e.repo.Page(context.Background(), query(f, key, desc, 1000), false)
				require.NoError(t, err)
				require.GreaterOrEqual(t, len(all.Rows), 5)

				var got []int64
				var after *model.AgingCursor
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
					after = &model.AgingCursor{SortValue: last.SortValue, ID: last.ID}
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

func TestPageOrdersBySortKey(t *testing.T) {
	e := newEnv(t)
	e.addAgedInvoices(t)

	page, err := e.repo.Page(context.Background(), query(model.AgingFilter{AsOf: oct21}, model.AgingSortTotal, true, 10), false)
	require.NoError(t, err)

	require.Equal(t, model.Money(21000), page.Rows[0].Total, "the biggest balance first")
	for i := 1; i < len(page.Rows); i++ {
		require.GreaterOrEqual(t, page.Rows[i-1].Total, page.Rows[i].Total)
	}
}

func TestPageRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	tests := []struct {
		name string
		q    model.AgingQuery
	}{
		{"no as_of", query(model.AgingFilter{}, model.AgingSortCustomer, false, 10)},
		{"unknown sort key", query(model.AgingFilter{AsOf: oct21}, "name; DROP TABLE customers", false, 10)},
		{"unknown days", query(model.AgingFilter{AsOf: oct21, MinAgeDays: 45}, model.AgingSortCustomer, false, 10)},
		{"no limit", query(model.AgingFilter{AsOf: oct21}, model.AgingSortCustomer, false, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.repo.Page(ctx, tt.q, true)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestServiceTypes(t *testing.T) {
	e := newEnv(t)

	types, err := e.repo.ServiceTypes(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, types)
}
