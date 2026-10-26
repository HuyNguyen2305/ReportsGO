//go:build integration

package recurringrepo_test

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

// env is a migrated, seeded throwaway database with a recurring invoice repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.RecurringRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewRecurringRepository(pool)}
}

func query(f model.RecurringFilter, key model.RecurringSortKey, desc bool, limit int) model.RecurringQuery {
	return model.RecurringQuery{Filter: f, Sort: model.RecurringSort{Key: key, Desc: desc}, Limit: limit}
}

func (e env) listAll(t *testing.T, f model.RecurringFilter, key model.RecurringSortKey, desc bool) []model.RecurringRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), query(f, key, desc, 1000))
	require.NoError(t, err)
	return rows
}

func ids(rows []model.RecurringRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// the seed has 7 recurring invoices; none of the filters below limits the dates unless it says so.
const allRecurring = 7

// find returns the one row that matches the search term.
func (e env) find(t *testing.T, search string) model.RecurringRow {
	t.Helper()
	rows := e.listAll(t, model.RecurringFilter{Search: search}, model.RecurringSortStartedOn, true)
	require.Len(t, rows, 1, "search %q", search)
	return rows[0]
}

func TestListWithoutFiltersReturnsEveryRecurringInvoice(t *testing.T) {
	e := newEnv(t)

	rows := e.listAll(t, model.RecurringFilter{}, model.RecurringSortStartedOn, true)

	require.Len(t, rows, allRecurring, "there is no default date range")
	require.Equal(t, "2026-10-15", rows[0].StartedOn.Format("2006-01-02"), "newest start first")
	require.Equal(t, "2025-10-01", rows[len(rows)-1].StartedOn.Format("2006-01-02"))
}

func TestListReturnsAllReportFieldsForARecurringInvoiceWithANextInvoice(t *testing.T) {
	e := newEnv(t)

	r := e.find(t, "85032")

	require.NotZero(t, r.ID)
	require.NotZero(t, r.CustomerID)
	require.NotEmpty(t, r.CustomerName)
	require.Equal(t, "63733", r.AccountNumber)
	require.Equal(t, model.FrequencyWeek, r.Unit)
	require.Equal(t, 2, r.Interval)
	require.NotNil(t, r.Weekday)
	require.Equal(t, 2, *r.Weekday)
	require.NotNil(t, r.Occurrences)
	require.Equal(t, 6, *r.Occurrences)
	require.Equal(t, model.ActionSendByEmail, r.Action)
	require.Equal(t, model.RecurringFailed, r.Status)
	require.Equal(t, model.Money(4850), r.TotalValue)
	require.Equal(t, "2026-10-05", r.StartedOn.Format("2006-01-02"))
	require.Nil(t, r.EndsOn)
	require.NotNil(t, r.NextInvoiceID)
	require.Equal(t, "85032", *r.NextInvoiceNumber)
	require.Equal(t, "2026-10-19", r.NextInvoiceDate.Format("2006-01-02"))
	require.Equal(t, "2026-10-05", r.SortValue)
}

func TestListHandlesMissingOptionalParts(t *testing.T) {
	e := newEnv(t)

	t.Run("nothing optional is set", func(t *testing.T) {
		rows := e.listAll(t, model.RecurringFilter{Statuses: []model.RecurringStatus{model.RecurringPaused}},
			model.RecurringSortStartedOn, true)
		require.Len(t, rows, 1)
		r := rows[0]
		require.Equal(t, model.FrequencyMonth, r.Unit)
		require.Nil(t, r.Weekday)
		require.Nil(t, r.Occurrences)
		require.Nil(t, r.EndsOn)
		require.Nil(t, r.NextInvoiceID)
		require.Nil(t, r.NextInvoiceNumber)
		require.Nil(t, r.NextInvoiceDate)
		require.Equal(t, model.ActionCreateOnly, r.Action)
	})

	t.Run("an end date and no next invoice", func(t *testing.T) {
		rows := e.listAll(t, model.RecurringFilter{Statuses: []model.RecurringStatus{model.RecurringEnded}},
			model.RecurringSortStartedOn, true)
		require.Len(t, rows, 1)
		require.Equal(t, "2026-10-01", rows[0].EndsOn.Format("2006-01-02"))
		require.Nil(t, rows[0].NextInvoiceID)
	})

	t.Run("a due date without an invoice yet", func(t *testing.T) {
		rows := e.listAll(t, model.RecurringFilter{Statuses: []model.RecurringStatus{model.RecurringActive}},
			model.RecurringSortStartedOn, true)
		var found bool
		for _, r := range rows {
			if r.Unit == model.FrequencyDay {
				found = true
				require.Nil(t, r.NextInvoiceID)
				require.Nil(t, r.NextInvoiceNumber)
				require.NotNil(t, r.NextInvoiceDate)
				require.Equal(t, "2026-10-18", r.NextInvoiceDate.Format("2006-01-02"))
				require.Equal(t, 3, r.Interval)
			}
		}
		require.True(t, found)
	})
}

var sortCast = map[model.RecurringSortKey]string{
	model.RecurringSortCustomer: "text", model.RecurringSortAccountNumber: "text", model.RecurringSortRepeats: "text",
	model.RecurringSortAction: "text", model.RecurringSortStatus: "text", model.RecurringSortTotalValue: "numeric",
	model.RecurringSortStartedOn: "date", model.RecurringSortEndsOn: "date", model.RecurringSortNextInvoiceDate: "date",
}

// compare asks Postgres to order two sort values, so text keys use the same collation as ORDER BY.
func (e env) compare(t *testing.T, key model.RecurringSortKey, a, b string) int {
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
	require.Len(t, sortCast, len(model.RecurringSortKeys), "every sort key must be covered")

	for _, key := range model.RecurringSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				rows := e.listAll(t, model.RecurringFilter{}, key, desc)
				require.Len(t, rows, allRecurring)

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
				require.Positive(t, ties, "every key has tied values in the data, or the tie-break goes untested")
			})
		}
	}
}

func TestRepeatsSortByPeriodLength(t *testing.T) {
	e := newEnv(t)

	rows := e.listAll(t, model.RecurringFilter{}, model.RecurringSortRepeats, false)

	// every 3 days (3), every week (7, twice), every 2 weeks (14), every month (30), every 2 months (60), every year
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = fmt.Sprintf("%d %s", r.Interval, r.Unit)
	}
	require.Equal(t, []string{"3 day", "1 week", "1 week", "2 week", "1 month", "2 month", "1 year"}, got)
}

// The period is ordered with a bigint product, so an interval too large for 32-bit arithmetic (6,000,000 years is
// 2.19e9 days, past 2^31) still sorts, in both directions and across pages, instead of failing the whole request.
func TestRepeatsSortSurvivesHugeIntervals(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, interval := range []int{6_000_000, 2_147_483_647} {
		_, err := e.pool.Exec(ctx, `INSERT INTO recurring_invoices (customer_id, frequency_unit, frequency_interval, started_on)
			SELECT id, 'year', $1, '2026-10-19' FROM customers WHERE account_number = '63733'`, interval)
		require.NoError(t, err)
	}
	intervals := func(rows []model.RecurringRow) []int {
		out := make([]int, len(rows))
		for i, r := range rows {
			out[i] = r.Interval
		}
		return out
	}

	asc := e.listAll(t, model.RecurringFilter{}, model.RecurringSortRepeats, false)
	require.Len(t, asc, allRecurring+2)
	require.Equal(t, []int{6_000_000, 2_147_483_647}, intervals(asc[len(asc)-2:]), "ascending: the longest periods are last")
	require.Equal(t, 1, asc[len(asc)-3].Interval, "the seeded every-year rule comes right before them")

	desc := e.listAll(t, model.RecurringFilter{}, model.RecurringSortRepeats, true)
	require.Equal(t, []int{2_147_483_647, 6_000_000}, intervals(desc[:2]), "descending: the longest periods are first")

	for _, isDesc := range []bool{false, true} {
		var got []int64
		q := query(model.RecurringFilter{}, model.RecurringSortRepeats, isDesc, 2)
		for pages := 0; ; pages++ {
			require.Less(t, pages, 20, "paging must terminate")
			rows, err := e.repo.List(ctx, q)
			require.NoError(t, err)
			page := rows
			if len(rows) > q.Limit {
				page = rows[:q.Limit]
			}
			got = append(got, ids(page)...)
			if len(rows) <= q.Limit {
				break
			}
			last := page[len(page)-1]
			q.After = &model.RecurringCursor{SortValue: last.SortValue, ID: last.ID}
		}
		want := asc
		if isDesc {
			want = desc
		}
		require.Equal(t, ids(want), got, "paging in order desc=%v neither skips nor repeats a row", isDesc)
	}
}

func TestMissingDatesSortLastWhenAscending(t *testing.T) {
	e := newEnv(t)

	t.Run("end date", func(t *testing.T) {
		rows := e.listAll(t, model.RecurringFilter{}, model.RecurringSortEndsOn, false)
		require.NotNil(t, rows[0].EndsOn)
		require.Equal(t, "2026-10-01", rows[0].EndsOn.Format("2006-01-02"))
		require.NotNil(t, rows[1].EndsOn)
		for _, r := range rows[2:] {
			require.Nil(t, r.EndsOn, "no end date sorts after every real date")
		}
		require.Equal(t, "9999-12-31", rows[len(rows)-1].SortValue)
	})

	t.Run("next invoice date", func(t *testing.T) {
		rows := e.listAll(t, model.RecurringFilter{}, model.RecurringSortNextInvoiceDate, false)
		for _, r := range rows[:5] {
			require.NotNil(t, r.NextInvoiceDate)
		}
		for _, r := range rows[5:] {
			require.Nil(t, r.NextInvoiceDate)
		}
	})
}

func walk(t *testing.T, repo *repository.RecurringRepository, key model.RecurringSortKey, desc bool, limit int) []int64 {
	t.Helper()
	var got []int64
	q := query(model.RecurringFilter{}, key, desc, limit)
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
		q.After = &model.RecurringCursor{SortValue: last.SortValue, ID: last.ID}
	}
}

func TestKeysetPagingWalksEveryKeyAndDirectionWithoutGapsOrDuplicates(t *testing.T) {
	e := newEnv(t)

	for _, key := range model.RecurringSortKeys {
		for _, desc := range []bool{false, true} {
			for _, limit := range []int{1, 3} {
				t.Run(fmt.Sprintf("%s/desc=%v/limit=%d", key, desc, limit), func(t *testing.T) {
					want := ids(e.listAll(t, model.RecurringFilter{}, key, desc))

					got := walk(t, e.repo, key, desc, limit)

					require.Equal(t, want, got, "walking the pages must reproduce the full ordered list exactly")
					require.Len(t, got, allRecurring)
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
		filter    model.RecurringFilter
		count     int
		wantTotal model.Money
	}{
		{"no filter", model.RecurringFilter{}, 7, 71349},
		{"active", model.RecurringFilter{Statuses: []model.RecurringStatus{model.RecurringActive}}, 4, 24499},
		{"paused", model.RecurringFilter{Statuses: []model.RecurringStatus{model.RecurringPaused}}, 1, 12000},
		{"failed", model.RecurringFilter{Statuses: []model.RecurringStatus{model.RecurringFailed}}, 1, 4850},
		{"ended", model.RecurringFilter{Statuses: []model.RecurringStatus{model.RecurringEnded}}, 1, 30000},
		{"paused or failed", model.RecurringFilter{Statuses: []model.RecurringStatus{model.RecurringPaused, model.RecurringFailed}}, 2, 16850},
		{"create only", model.RecurringFilter{Actions: []model.RecurringAction{model.ActionCreateOnly}}, 2, 13500},
		{"send by email", model.RecurringFilter{Actions: []model.RecurringAction{model.ActionSendByEmail}}, 5, 57849},
		{"both actions", model.RecurringFilter{Actions: model.RecurringActions}, 7, 71349},
		{"active and send by email", model.RecurringFilter{
			Statuses: []model.RecurringStatus{model.RecurringActive}, Actions: []model.RecurringAction{model.ActionSendByEmail},
		}, 3, 22999},
		{"started from", model.RecurringFilter{DateFrom: day(2026, 9, 14)}, 4, 19350},
		{"started up to", model.RecurringFilter{DateTo: day(2026, 8, 31)}, 3, 51999},
		{"one start date", model.RecurringFilter{DateFrom: day(2026, 9, 14), DateTo: day(2026, 9, 14)}, 2, 13000},
		{"the last day is included", model.RecurringFilter{DateFrom: day(2026, 10, 15), DateTo: day(2026, 10, 15)}, 1, 1500},
		{"empty range", model.RecurringFilter{DateFrom: day(2030, 1, 1), DateTo: day(2030, 1, 31)}, 0, 0},
		{"search by next invoice number", model.RecurringFilter{Search: "85032"}, 1, 4850},
		{"search by the original next invoice", model.RecurringFilter{Search: "84890"}, 1, 6500},
		{"search by account number", model.RecurringFilter{Search: "63733"}, 2, 14849},
		{"search matches nothing", model.RecurringFilter{Search: "zzz-nothing"}, 0, 0},
		{"percent is literal", model.RecurringFilter{Search: "%"}, 0, 0},
		{"underscore is literal", model.RecurringFilter{Search: "850_2"}, 0, 0},
		{"backslash is literal", model.RecurringFilter{Search: `\`}, 0, 0},
		{"sql injection text matches nothing", model.RecurringFilter{Search: `'; DROP TABLE recurring_invoices; --`}, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := e.listAll(t, tt.filter, model.RecurringSortStartedOn, false)
			require.Len(t, rows, tt.count)

			summary, err := e.repo.Summary(ctx, tt.filter)
			require.NoError(t, err)
			require.Equal(t, model.RecurringSummary{Count: int64(tt.count), Total: tt.wantTotal}, summary,
				"the summary follows the same filter")

			var sum model.Money
			for _, r := range rows {
				sum += r.TotalValue
			}
			require.Equal(t, sum, summary.Total, "the total is the sum of the listed values")
		})
	}
}

func TestSummaryTotalMatchesAnIndependentQuery(t *testing.T) {
	e := newEnv(t)
	var count, total int64
	require.NoError(t, e.pool.QueryRow(context.Background(),
		`SELECT count(*), (sum(total_value) * 100)::bigint FROM recurring_invoices`).Scan(&count, &total))

	s, err := e.repo.Summary(context.Background(), model.RecurringFilter{})

	require.NoError(t, err)
	require.EqualValues(t, count, s.Count)
	require.EqualValues(t, total, s.Total)
}

func TestSummaryOfNothingIsZero(t *testing.T) {
	e := newEnv(t)

	s, err := e.repo.Summary(context.Background(), model.RecurringFilter{Search: "zzz-nothing"})

	require.NoError(t, err)
	require.Equal(t, model.RecurringSummary{}, s)
}

func TestRepositoryRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.repo.List(ctx, query(model.RecurringFilter{}, "bogus", false, 5))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
	_, err = e.repo.List(ctx, query(model.RecurringFilter{}, model.RecurringSortStartedOn, false, 0))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

// insertRecurring commits one more recurring invoice of 10.00.
func insertRecurring(t *testing.T, conn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO recurring_invoices (customer_id, frequency_unit, total_value, started_on)
		SELECT id, 'month', 10, '2026-10-19' FROM customers WHERE account_number = '63733'`)
	require.NoError(t, err)
}

func TestPage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := query(model.RecurringFilter{}, model.RecurringSortStartedOn, true, 3)

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
		require.EqualValues(t, allRecurring, page.Summary.Count)
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
		insertRecurring(t, tx)

		page, err := repository.NewRecurringRepository(tx).Page(ctx, q, true)
		require.NoError(t, err)
		require.EqualValues(t, allRecurring+1, page.Summary.Count, "the caller's transaction is the snapshot and sees its own row")
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

// The count and total must describe the rows they are shown with. A recurring invoice committed by someone else
// between reading the rows and reading the totals would otherwise make the two disagree.
func TestPageReadsRowsAndSummaryFromOneSnapshot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := query(model.RecurringFilter{}, model.RecurringSortStartedOn, true, 100)

	committed := 0
	pool := testdb.SnapshotPool{Pool: e.pool, BeforeQueryRow: func() {
		insertRecurring(t, e.pool)
		committed++
	}}

	page, err := repository.NewRecurringRepository(pool).Page(ctx, q, true)
	require.NoError(t, err)

	require.Equal(t, 1, committed, "the concurrent insert must have happened between the two reads")
	require.Len(t, page.Rows, allRecurring)
	require.EqualValues(t, allRecurring, page.Summary.Count, "the count ignores the recurring invoice committed after the rows were read")
	var rowsTotal model.Money
	for _, r := range page.Rows {
		rowsTotal += r.TotalValue
	}
	require.Equal(t, rowsTotal, page.Summary.Total, "the total is the sum of the rows it describes")

	after, err := e.repo.Summary(ctx, q.Filter)
	require.NoError(t, err)
	require.EqualValues(t, allRecurring+1, after.Count, "the committed recurring invoice is real and visible to a later read")
}

func TestCancelledContextIsReturnedAsAnError(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.repo.List(ctx, query(model.RecurringFilter{}, model.RecurringSortStartedOn, false, 5))
	require.ErrorIs(t, err, context.Canceled)
	_, err = e.repo.Summary(ctx, model.RecurringFilter{})
	require.ErrorIs(t, err, context.Canceled)
}
