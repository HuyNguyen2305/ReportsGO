//go:build integration

package creditrepo_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// env is a migrated, seeded throwaway database with a credit repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.CreditRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewCreditRepository(pool)}
}

func october() model.CreditFilter {
	return model.CreditFilter{DateFrom: day(2026, 10, 1), DateTo: day(2026, 10, 31)}
}

func query(f model.CreditFilter, key model.CreditSortKey, desc bool, limit int) model.CreditQuery {
	return model.CreditQuery{Filter: f, Sort: model.CreditSort{Key: key, Desc: desc}, Limit: limit}
}

func (e env) listAll(t *testing.T, f model.CreditFilter, key model.CreditSortKey, desc bool) []model.CreditRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), query(f, key, desc, 1000))
	require.NoError(t, err)
	return rows
}

func ids(rows []model.CreditRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// the 7 October credits in the seed (a credit dated 09-30 is outside this set; 8 credits in all).
const octoberCredits = 7

func TestListDefaultReturnsOctoberCreditsNewestFirst(t *testing.T) {
	e := newEnv(t)

	rows := e.listAll(t, october(), model.CreditSortDate, true)

	require.Len(t, rows, octoberCredits)
	require.Equal(t, "2026-10-21", rows[0].Date.Format("2006-01-02"))
	for i := 1; i < len(rows); i++ {
		require.False(t, rows[i].Date.After(rows[i-1].Date), "dates must not increase")
	}
}

func TestListReturnsAllReportFields(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = "4242"

	rows := e.listAll(t, f, model.CreditSortDate, true)

	require.Len(t, rows, 1)
	r := rows[0]
	require.NotZero(t, r.ID)
	require.NotZero(t, r.CustomerID)
	require.Equal(t, "Customer 1", r.CustomerName)
	require.Equal(t, "63733", r.AccountNumber)
	require.Equal(t, model.MethodCard, r.Method)
	require.Equal(t, "Stripe **** 4242 Visa", r.MethodDetail)
	require.Equal(t, model.Money(2000), r.Amount)
	require.Equal(t, "2026-10-15", r.Date.Format("2006-01-02"))
	require.NotNil(t, r.CreatedBy)
	require.Equal(t, "thon nguyen", *r.CreatedBy)
	require.Equal(t, "2026-10-15", r.SortValue)
}

func TestListHandlesMissingCreatorAndEmptyDetail(t *testing.T) {
	e := newEnv(t)

	f := october()
	f.Methods = []model.PaymentMethod{model.MethodOther}
	rows := e.listAll(t, f, model.CreditSortDate, true)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0].CreatedBy)
	require.Equal(t, "Wire transfer", rows[0].MethodDetail)

	f = october()
	f.Methods = []model.PaymentMethod{model.MethodCash}
	f.Search = "63717"
	rows = e.listAll(t, f, model.CreditSortDate, true)
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].MethodDetail, "an empty detail is an empty string, not an error")
}

var sortCast = map[model.CreditSortKey]string{
	model.CreditSortCustomer: "text", model.CreditSortAccountNumber: "text", model.CreditSortMethod: "text",
	model.CreditSortAmount: "numeric", model.CreditSortDate: "date", model.CreditSortCreatedBy: "text",
}

// compare asks Postgres to order two sort values, so text keys use the same collation as ORDER BY.
func (e env) compare(t *testing.T, key model.CreditSortKey, a, b string) int {
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
	require.Len(t, sortCast, len(model.CreditSortKeys), "every sort key must be covered")

	for _, key := range model.CreditSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				rows := e.listAll(t, october(), key, desc)
				require.Len(t, rows, octoberCredits)

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
				case model.CreditSortDate, model.CreditSortMethod, model.CreditSortCustomer, model.CreditSortAccountNumber:
					require.Positive(t, ties, "this key should have tied values in the data, or the tie-break goes untested")
				}
			})
		}
	}
}

func walk(t *testing.T, repo *repository.CreditRepository, key model.CreditSortKey, desc bool, limit int) []int64 {
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
		q.After = &model.CreditCursor{SortValue: last.SortValue, ID: last.ID}
	}
}

func TestKeysetPagingWalksEveryKeyAndDirectionWithoutGapsOrDuplicates(t *testing.T) {
	e := newEnv(t)

	for _, key := range model.CreditSortKeys {
		for _, desc := range []bool{false, true} {
			for _, limit := range []int{1, 3} {
				t.Run(fmt.Sprintf("%s/desc=%v/limit=%d", key, desc, limit), func(t *testing.T) {
					want := ids(e.listAll(t, october(), key, desc))

					got := walk(t, e.repo, key, desc, limit)

					require.Equal(t, want, got, "walking the pages must reproduce the full ordered list exactly")
					require.Len(t, got, octoberCredits)
				})
			}
		}
	}
}

func TestListFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	tests := []struct {
		name      string
		mutate    func(*model.CreditFilter)
		count     int
		wantTotal model.Money
	}{
		{"all of october", func(f *model.CreditFilter) {}, 7, 57525},
		{"cash", func(f *model.CreditFilter) { f.Methods = []model.PaymentMethod{model.MethodCash} }, 2, 6000},
		{"check", func(f *model.CreditFilter) { f.Methods = []model.PaymentMethod{model.MethodCheck} }, 2, 17525},
		{"card", func(f *model.CreditFilter) { f.Methods = []model.PaymentMethod{model.MethodCard} }, 2, 22000},
		{"other", func(f *model.CreditFilter) { f.Methods = []model.PaymentMethod{model.MethodOther} }, 1, 12000},
		{"card or other", func(f *model.CreditFilter) {
			f.Methods = []model.PaymentMethod{model.MethodCard, model.MethodOther}
		}, 3, 34000},
		{"one day", func(f *model.CreditFilter) { f.DateFrom, f.DateTo = day(2026, 10, 15), day(2026, 10, 15) }, 5, 38000},
		{"the last day is included", func(f *model.CreditFilter) { f.DateFrom, f.DateTo = day(2026, 10, 21), day(2026, 10, 21) }, 1, 12000},
		{"september", func(f *model.CreditFilter) { f.DateFrom, f.DateTo = day(2026, 9, 1), day(2026, 9, 30) }, 1, 3550},
		{"open start", func(f *model.CreditFilter) { f.DateFrom, f.DateTo = time.Time{}, day(2026, 10, 12) }, 2, 11075},
		{"open end", func(f *model.CreditFilter) { f.DateFrom, f.DateTo = day(2026, 10, 16), time.Time{} }, 1, 12000},
		{"no date limits at all", func(f *model.CreditFilter) { f.DateFrom, f.DateTo = time.Time{}, time.Time{} }, 8, 61075},
		{"empty range", func(f *model.CreditFilter) { f.DateFrom, f.DateTo = day(2027, 1, 1), day(2027, 1, 31) }, 0, 0},
		{"search by customer name", func(f *model.CreditFilter) { f.Search = "customer 1" }, 2, 14000},
		{"search by account number", func(f *model.CreditFilter) { f.Search = "63706" }, 2, 30000},
		{"search by method detail", func(f *model.CreditFilter) { f.Search = "stripe" }, 2, 22000},
		{"search by detail is case-insensitive", func(f *model.CreditFilter) { f.Search = "CHECK #" }, 1, 7525},
		{"search matches nothing", func(f *model.CreditFilter) { f.Search = "zzz-nothing" }, 0, 0},
		{"percent is literal", func(f *model.CreditFilter) { f.Search = "%" }, 0, 0},
		{"underscore is literal", func(f *model.CreditFilter) { f.Search = "Che_k" }, 0, 0},
		{"backslash is literal", func(f *model.CreditFilter) { f.Search = `\` }, 0, 0},
		{"sql injection text matches nothing", func(f *model.CreditFilter) { f.Search = `'; DROP TABLE credits; --` }, 0, 0},
		{"filters combine", func(f *model.CreditFilter) {
			f.Methods = []model.PaymentMethod{model.MethodCard}
			f.Search = "63733"
			f.DateFrom, f.DateTo = day(2026, 10, 15), day(2026, 10, 15)
		}, 1, 2000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := october()
			tt.mutate(&f)

			rows := e.listAll(t, f, model.CreditSortDate, false)
			require.Len(t, rows, tt.count)

			summary, err := e.repo.Summary(ctx, f)
			require.NoError(t, err)
			require.Equal(t, model.CreditSummary{Count: int64(tt.count), Total: tt.wantTotal}, summary,
				"the summary follows the same filter")

			var sum model.Money
			for _, r := range rows {
				sum += r.Amount
			}
			require.Equal(t, sum, summary.Total, "the total is the sum of the listed amounts")
		})
	}
}

func TestSummaryTotalMatchesAnIndependentQuery(t *testing.T) {
	e := newEnv(t)
	var count, total int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `
		SELECT count(*), (sum(amount) * 100)::bigint FROM credits
		WHERE credit_date BETWEEN '2026-10-01' AND '2026-10-31'`).Scan(&count, &total))

	s, err := e.repo.Summary(context.Background(), october())

	require.NoError(t, err)
	require.EqualValues(t, count, s.Count)
	require.EqualValues(t, total, s.Total)
}

func TestSummaryOfNothingIsZero(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = "zzz-nothing"

	s, err := e.repo.Summary(context.Background(), f)

	require.NoError(t, err)
	require.Equal(t, model.CreditSummary{}, s)
}

func TestTheCreditsAreNotDuplicatedByTheirPayments(t *testing.T) {
	e := newEnv(t)

	// Two of the credits also appear in the Payments report; joining must not multiply the credit rows.
	rows := e.listAll(t, october(), model.CreditSortDate, true)

	seen := map[int64]bool{}
	for _, r := range rows {
		require.False(t, seen[r.ID], "credit %d listed twice", r.ID)
		seen[r.ID] = true
	}
}

func TestRepositoryRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.repo.List(ctx, query(october(), "bogus", false, 5))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
	_, err = e.repo.List(ctx, query(october(), model.CreditSortDate, false, 0))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

// insertOctoberCredit commits one more October credit of 10.00.
func insertOctoberCredit(t *testing.T, conn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO credits (customer_id, amount, method_type, credit_date)
		SELECT id, 10, 'cash', '2026-10-19' FROM customers WHERE account_number = '63733'`)
	require.NoError(t, err)
}

func TestPage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := query(october(), model.CreditSortDate, true, 3)

	t.Run("a first page carries the rows and the totals of the same set", func(t *testing.T) {
		page, err := e.repo.Page(ctx, q, true)
		require.NoError(t, err)

		rows, err := e.repo.List(ctx, q)
		require.NoError(t, err)
		summary, err := e.repo.Summary(ctx, q.Filter)
		require.NoError(t, err)

		require.Equal(t, rows, page.Rows)
		require.NotNil(t, page.Summary)
		require.Equal(t, summary, *page.Summary)
		require.EqualValues(t, octoberCredits, page.Summary.Count)
		require.Len(t, page.Rows, 4, "limit+1 rows, so the caller can tell another page follows")
	})

	t.Run("without a summary it is just the rows", func(t *testing.T) {
		page, err := e.repo.Page(ctx, q, false)
		require.NoError(t, err)

		rows, err := e.repo.List(ctx, q)
		require.NoError(t, err)
		require.Equal(t, rows, page.Rows)
		require.Nil(t, page.Summary)
	})

	t.Run("works on a repository bound to the caller's transaction", func(t *testing.T) {
		tx, err := e.pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		insertOctoberCredit(t, tx)

		page, err := repository.NewCreditRepository(tx).Page(ctx, q, true)
		require.NoError(t, err)
		require.EqualValues(t, octoberCredits+1, page.Summary.Count, "the caller's transaction is the snapshot and sees its own row")
	})

	t.Run("invalid queries and cancelled contexts are errors, not partial pages", func(t *testing.T) {
		bad := q
		bad.Limit = 0
		_, err := e.repo.Page(ctx, bad, true)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)

		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		page, err := e.repo.Page(cancelled, q, true)
		require.ErrorIs(t, err, context.Canceled)
		require.Empty(t, page.Rows)
		require.Nil(t, page.Summary)
	})
}

// The count and total must describe the rows they are shown with. A credit committed by someone else between
// reading the rows and reading the totals would otherwise make the two disagree.
func TestPageReadsRowsAndSummaryFromOneSnapshot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := query(october(), model.CreditSortDate, true, 100)

	committed := 0
	pool := testdb.SnapshotPool{Pool: e.pool, BeforeQueryRow: func() {
		insertOctoberCredit(t, e.pool)
		committed++
	}}

	page, err := repository.NewCreditRepository(pool).Page(ctx, q, true)
	require.NoError(t, err)

	require.Equal(t, 1, committed, "the concurrent insert must have happened between the two reads")
	require.Len(t, page.Rows, octoberCredits)
	require.EqualValues(t, octoberCredits, page.Summary.Count, "the count ignores the credit committed after the rows were read")
	var rowsTotal model.Money
	for _, r := range page.Rows {
		rowsTotal += r.Amount
	}
	require.Equal(t, rowsTotal, page.Summary.Total, "the total is the sum of the rows it describes")

	after, err := e.repo.Summary(ctx, q.Filter)
	require.NoError(t, err)
	require.EqualValues(t, octoberCredits+1, after.Count, "the committed credit is real and visible to a later read")
}

func TestCancelledContextIsReturnedAsAnError(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.repo.List(ctx, query(october(), model.CreditSortDate, false, 5))
	require.ErrorIs(t, err, context.Canceled)
	_, err = e.repo.Summary(ctx, october())
	require.ErrorIs(t, err, context.Canceled)
}
