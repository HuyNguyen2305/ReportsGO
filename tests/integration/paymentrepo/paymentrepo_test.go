//go:build integration

package paymentrepo_test

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

// env is a migrated, seeded throwaway database with a payment repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.PaymentRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewPaymentRepository(pool)}
}

func october() model.PaymentFilter {
	return model.PaymentFilter{
		Lifecycle: model.LifecycleActive,
		DateFrom:  day(2026, 10, 1),
		DateTo:    day(2026, 10, 31),
	}
}

func query(f model.PaymentFilter, key model.PaymentSortKey, desc bool, limit int) model.PaymentQuery {
	return model.PaymentQuery{Filter: f, Sort: model.PaymentSort{Key: key, Desc: desc}, Limit: limit}
}

func (e env) listAll(t *testing.T, f model.PaymentFilter, key model.PaymentSortKey, desc bool) []model.PaymentRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), query(f, key, desc, 1000))
	require.NoError(t, err)
	return rows
}

func ids(rows []model.PaymentRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// the 15 active October payments in the seed: 12 original (all paid) plus a pending, a failed and a refunded
// one. A payment dated 09-30 and a deleted one are outside this set.
const octoberActive = 15

func TestListDefaultReturnsActiveOctoberPayments(t *testing.T) {
	e := newEnv(t)

	rows := e.listAll(t, october(), model.PaymentSortDate, true)

	require.Len(t, rows, octoberActive)
	require.Equal(t, day(2026, 10, 21).Format("2006-01-02"), rows[0].Date.Format("2006-01-02"), "10/21 is the newest")
	for i := 1; i < len(rows); i++ {
		require.False(t, rows[i].Date.After(rows[i-1].Date), "dates must not increase")
	}
}

func TestListReturnsAllReportFieldsForAnInvoicePayment(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = "85004"
	f.Statuses = []model.PaymentStatus{model.PaymentFailed}

	rows := e.listAll(t, f, model.PaymentSortDate, true)

	require.Len(t, rows, 1)
	r := rows[0]
	require.NotZero(t, r.ID)
	require.NotZero(t, r.CustomerID)
	require.NotEmpty(t, r.CustomerName)
	require.NotEmpty(t, r.AccountNumber)
	require.NotNil(t, r.InvoiceID)
	require.NotNil(t, r.InvoiceNumber)
	require.Equal(t, "85004", *r.InvoiceNumber)
	require.Equal(t, model.MethodCard, r.Method)
	require.Equal(t, "Visa", *r.CardBrand)
	require.Equal(t, "4242", *r.CardLast4)
	require.Equal(t, model.Money(2000), r.Amount)
	require.Equal(t, "2026-10-15", r.Date.Format("2006-01-02"))
	require.Equal(t, model.PaymentFailed, r.Status)
	require.Equal(t, "2026-10-15", r.SortValue)
}

func TestCreditPaymentsHaveNoInvoice(t *testing.T) {
	e := newEnv(t)

	var credits []model.PaymentRow
	for _, r := range e.listAll(t, october(), model.PaymentSortAmount, false) {
		if r.InvoiceID == nil {
			credits = append(credits, r)
		}
	}

	require.Len(t, credits, 2)
	for _, r := range credits {
		require.Nil(t, r.InvoiceNumber, "a credit has no invoice number")
	}
	require.Equal(t, model.Money(10000), credits[0].Amount)
	require.Equal(t, model.MethodCheck, credits[0].Method)
	require.Nil(t, credits[0].CardBrand)
	require.Nil(t, credits[0].CardLast4)
	require.Equal(t, model.Money(20000), credits[1].Amount)
	require.Equal(t, model.MethodCard, credits[1].Method)
	require.Equal(t, "Credit", *credits[1].CardBrand)
	require.Equal(t, "3222", *credits[1].CardLast4)
}

var sortCast = map[model.PaymentSortKey]string{
	model.PaymentSortPaymentFor: "text", model.PaymentSortCustomer: "text", model.PaymentSortMethod: "text",
	model.PaymentSortDate: "date", model.PaymentSortAmount: "numeric", model.PaymentSortStatus: "text",
}

// compare asks Postgres to order two sort values, so text keys use the same collation as ORDER BY.
func (e env) compare(t *testing.T, key model.PaymentSortKey, a, b string) int {
	t.Helper()
	cast := sortCast[key]
	var c int
	sql := fmt.Sprintf(`SELECT CASE WHEN $1::text::%[1]s < $2::text::%[1]s THEN -1
	                                WHEN $1::text::%[1]s = $2::text::%[1]s THEN 0 ELSE 1 END`, cast)
	require.NoError(t, e.pool.QueryRow(context.Background(), sql, a, b).Scan(&c))
	return c
}

func TestEverySortKeyOrdersCorrectlyInBothDirections(t *testing.T) {
	e := newEnv(t)
	require.Len(t, sortCast, len(model.PaymentSortKeys), "every sort key must be covered")

	for _, key := range model.PaymentSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				rows := e.listAll(t, october(), key, desc)
				require.Len(t, rows, octoberActive)

				ties := 0
				for i := 1; i < len(rows); i++ {
					prev, cur := rows[i-1], rows[i]
					c := e.compare(t, key, prev.SortValue, cur.SortValue)
					if desc {
						require.GreaterOrEqual(t, c, 0, "row %d (%s) must not sort above row %d (%s)", i, cur.SortValue, i-1, prev.SortValue)
					} else {
						require.LessOrEqual(t, c, 0, "row %d (%s) must not sort below row %d (%s)", i, cur.SortValue, i-1, prev.SortValue)
					}
					if c == 0 {
						ties++
						if desc {
							require.Greater(t, prev.ID, cur.ID, "ties are broken by id, same direction")
						} else {
							require.Less(t, prev.ID, cur.ID, "ties are broken by id, same direction")
						}
					}
				}
				switch key {
				case model.PaymentSortDate, model.PaymentSortStatus, model.PaymentSortMethod, model.PaymentSortPaymentFor:
					require.Positive(t, ties, "this key should have tied values in the data, or the tie-break goes untested")
				}
			})
		}
	}
}

func TestCreditsSortAsTheWordCredit(t *testing.T) {
	e := newEnv(t)

	asc := e.listAll(t, october(), model.PaymentSortPaymentFor, false)

	// Invoice numbers are digits and "credit" is a word, so in ascending order every invoice payment comes
	// first and the two credits close the list.
	require.Nil(t, asc[len(asc)-1].InvoiceID)
	require.Nil(t, asc[len(asc)-2].InvoiceID)
	for _, r := range asc[:len(asc)-2] {
		require.NotNil(t, r.InvoiceID)
	}
	require.Equal(t, "credit", asc[len(asc)-1].SortValue)
}

func walk(t *testing.T, repo *repository.PaymentRepository, key model.PaymentSortKey, desc bool, limit int) []int64 {
	t.Helper()
	var got []int64
	q := query(october(), key, desc, limit)
	for pages := 0; ; pages++ {
		require.Less(t, pages, 100, "paging must terminate")

		rows, err := repo.List(context.Background(), q)
		require.NoError(t, err)
		page := rows
		hasMore := len(rows) > limit
		if hasMore {
			page = rows[:limit]
		}
		got = append(got, ids(page)...)
		if !hasMore {
			return got
		}
		last := page[len(page)-1]
		q.After = &model.PaymentCursor{SortValue: last.SortValue, ID: last.ID}
	}
}

func TestKeysetPagingWalksEveryKeyAndDirectionWithoutGapsOrDuplicates(t *testing.T) {
	e := newEnv(t)

	for _, key := range model.PaymentSortKeys {
		for _, desc := range []bool{false, true} {
			for _, limit := range []int{1, 4} {
				t.Run(fmt.Sprintf("%s/desc=%v/limit=%d", key, desc, limit), func(t *testing.T) {
					want := ids(e.listAll(t, october(), key, desc))

					got := walk(t, e.repo, key, desc, limit)

					require.Equal(t, want, got, "walking the pages must reproduce the full ordered list exactly")
					require.Len(t, got, octoberActive)
				})
			}
		}
	}
}

func TestListFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		mutate func(*model.PaymentFilter)
		count  int
	}{
		{"all", func(f *model.PaymentFilter) {}, 15},
		{"paid", func(f *model.PaymentFilter) { f.Statuses = []model.PaymentStatus{model.PaymentPaid} }, 12},
		{"pending", func(f *model.PaymentFilter) { f.Statuses = []model.PaymentStatus{model.PaymentPending} }, 1},
		{"failed", func(f *model.PaymentFilter) { f.Statuses = []model.PaymentStatus{model.PaymentFailed} }, 1},
		{"refunded", func(f *model.PaymentFilter) { f.Statuses = []model.PaymentStatus{model.PaymentRefunded} }, 1},
		{"pending or failed", func(f *model.PaymentFilter) {
			f.Statuses = []model.PaymentStatus{model.PaymentPending, model.PaymentFailed}
		}, 2},
		{"cash", func(f *model.PaymentFilter) { f.Methods = []model.PaymentMethod{model.MethodCash} }, 1},
		{"check", func(f *model.PaymentFilter) { f.Methods = []model.PaymentMethod{model.MethodCheck} }, 6},
		{"card", func(f *model.PaymentFilter) { f.Methods = []model.PaymentMethod{model.MethodCard} }, 7},
		{"other", func(f *model.PaymentFilter) { f.Methods = []model.PaymentMethod{model.MethodOther} }, 1},
		{"card or other", func(f *model.PaymentFilter) {
			f.Methods = []model.PaymentMethod{model.MethodCard, model.MethodOther}
		}, 8},
		{"card and failed", func(f *model.PaymentFilter) {
			f.Methods = []model.PaymentMethod{model.MethodCard}
			f.Statuses = []model.PaymentStatus{model.PaymentFailed}
		}, 1},
		{"cash and failed", func(f *model.PaymentFilter) {
			f.Methods = []model.PaymentMethod{model.MethodCash}
			f.Statuses = []model.PaymentStatus{model.PaymentFailed}
		}, 0},
		{"deleted tab", func(f *model.PaymentFilter) { f.Lifecycle = model.LifecycleDeleted }, 1},
		{"one day", func(f *model.PaymentFilter) { f.DateFrom, f.DateTo = day(2026, 10, 15), day(2026, 10, 15) }, 8},
		{"last day is included", func(f *model.PaymentFilter) { f.DateFrom, f.DateTo = day(2026, 10, 21), day(2026, 10, 21) }, 1},
		{"september", func(f *model.PaymentFilter) { f.DateFrom, f.DateTo = day(2026, 9, 1), day(2026, 9, 30) }, 1},
		{"open start", func(f *model.PaymentFilter) { f.DateFrom, f.DateTo = time.Time{}, day(2026, 10, 10) }, 3},
		{"open end", func(f *model.PaymentFilter) { f.DateFrom, f.DateTo = day(2026, 10, 16), time.Time{} }, 2},
		{"empty range", func(f *model.PaymentFilter) { f.DateFrom, f.DateTo = day(2027, 1, 1), day(2027, 1, 31) }, 0},
		{"search by invoice number", func(f *model.PaymentFilter) { f.Search = "85004" }, 2},
		{"search matches nothing", func(f *model.PaymentFilter) { f.Search = "zzz-nothing" }, 0},
		{"percent is literal", func(f *model.PaymentFilter) { f.Search = "%" }, 0},
		{"underscore is literal", func(f *model.PaymentFilter) { f.Search = "850_4" }, 0},
		{"backslash is literal", func(f *model.PaymentFilter) { f.Search = `\` }, 0},
		{"sql injection text matches nothing", func(f *model.PaymentFilter) { f.Search = `'; DROP TABLE payments; --` }, 0},
		{"filters combine", func(f *model.PaymentFilter) {
			f.Search = "85004"
			f.Statuses = []model.PaymentStatus{model.PaymentPaid}
		}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := october()
			tt.mutate(&f)

			rows := e.listAll(t, f, model.PaymentSortDate, false)
			require.Len(t, rows, tt.count)

			summary, err := e.repo.Summary(ctx, f)
			require.NoError(t, err)
			require.EqualValues(t, tt.count, summary.Count, "the summary follows the same filter")

			var sum int64
			for _, r := range rows {
				sum += int64(r.Amount)
			}
			require.EqualValues(t, sum, summary.Total, "the summary total is the sum of the listed amounts")
		})
	}
}

func TestSearchMatchesAccountNumberAndCustomerName(t *testing.T) {
	e := newEnv(t)

	var account, name string
	require.NoError(t, e.pool.QueryRow(context.Background(), `
		SELECT c.account_number, c.display_name FROM payments p JOIN customers c ON c.id = p.customer_id
		WHERE p.credit_id IS NOT NULL ORDER BY p.id LIMIT 1`).Scan(&account, &name))

	for _, term := range []string{account, name, name[:3]} {
		f := october()
		f.Search = term
		rows := e.listAll(t, f, model.PaymentSortDate, true)
		require.NotEmpty(t, rows, term)
		var credits int
		for _, r := range rows {
			if r.InvoiceID == nil {
				credits++
			}
		}
		require.Positive(t, credits, "a credit is found through its customer: %q", term)
	}
}

func TestSummaryTotalMatchesAnIndependentQuery(t *testing.T) {
	e := newEnv(t)
	var count, total int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `
		SELECT count(*), (sum(amount) * 100)::bigint FROM payments
		WHERE deleted_at IS NULL AND payment_date BETWEEN '2026-10-01' AND '2026-10-31'`).Scan(&count, &total))

	s, err := e.repo.Summary(context.Background(), october())

	require.NoError(t, err)
	require.EqualValues(t, count, s.Count)
	require.EqualValues(t, total, s.Total)
	require.EqualValues(t, octoberActive, s.Count)
}

func TestSummaryOfNothingIsZero(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = "zzz-nothing"

	s, err := e.repo.Summary(context.Background(), f)

	require.NoError(t, err)
	require.Equal(t, model.PaymentSummary{}, s)
}

func TestRepositoryRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.repo.List(ctx, query(october(), "bogus", false, 5))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
	_, err = e.repo.List(ctx, query(october(), model.PaymentSortDate, false, 0))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
	_, err = e.repo.Summary(ctx, model.PaymentFilter{Lifecycle: model.LifecycleArchived})
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

func TestCancelledContextIsReturnedAsAnError(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.repo.List(ctx, query(october(), model.PaymentSortDate, false, 5))
	require.ErrorIs(t, err, context.Canceled)
	_, err = e.repo.Summary(ctx, october())
	require.ErrorIs(t, err, context.Canceled)
}
