//go:build integration

package estimaterepo_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// env is a migrated, seeded throwaway database with an estimate repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.EstimateRepository
}

// newEnv seeds the database. With withNulls it also adds three October estimates that have no billing email,
// no creator, no phone and a tied date, to exercise empty sort values and ties.
func newEnv(t *testing.T, withNulls bool) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	if withNulls {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total)
			SELECT 'estimate', 'EN' || g, c.id, '2026-10-10', 10 * g
			FROM generate_series(1, 3) g
			JOIN customers c ON c.account_number = '63716'`)
		require.NoError(t, err)
	}
	return env{pool: pool, repo: repository.NewEstimateRepository(pool)}
}

func octoberActive() model.EstimateFilter {
	return model.EstimateFilter{Lifecycle: model.LifecycleActive, DateFrom: day(2026, 10, 1), DateTo: day(2026, 10, 31)}
}

func query(f model.EstimateFilter, key model.EstimateSortKey, desc bool, limit int) model.EstimateQuery {
	return model.EstimateQuery{Filter: f, Sort: model.EstimateSort{Key: key, Desc: desc}, Limit: limit}
}

func (e env) listAll(t *testing.T, f model.EstimateFilter, key model.EstimateSortKey, desc bool) []model.EstimateRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), query(f, key, desc, 1000))
	require.NoError(t, err)
	return rows
}

func numbers(rows []model.EstimateRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.EstimateNumber
	}
	return out
}

func ids(rows []model.EstimateRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// the 19 active October estimates in the seed: 17 drafts, one accepted, one declined.
var octoberActiveNumbers = []string{
	"123187", "123184", "123186", "123185", "123183", "123175", "123174", "123173", "123172", "123168",
	"123170", "123169", "123156", "123159", "123158", "123157", "123155", "123188", "123189",
}

func TestListDefaultReturnsActiveOctoberEstimatesNewestFirst(t *testing.T) {
	e := newEnv(t, false)

	rows := e.listAll(t, octoberActive(), model.EstimateSortDate, true)

	require.ElementsMatch(t, octoberActiveNumbers, numbers(rows))
	require.Equal(t, day(2026, 10, 21), rows[0].Date, "10/21 is the newest")
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		require.False(t, cur.Date.After(prev.Date), "dates must not increase")
		if cur.Date.Equal(prev.Date) {
			require.Less(t, cur.ID, prev.ID, "ties are broken by id in the same direction")
		}
	}
}

func TestListReturnsAllReportFields(t *testing.T) {
	e := newEnv(t, false)
	f := octoberActive()
	f.Search = "123183"

	rows := e.listAll(t, f, model.EstimateSortDate, true)

	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "123183", r.EstimateNumber)
	require.Equal(t, "63714", r.AccountNumber)
	require.Equal(t, "#F Last Name", r.CustomerName)
	require.NotZero(t, r.CustomerID)
	require.NotNil(t, r.Phone)
	require.Equal(t, "012222222", *r.Phone)
	require.NotNil(t, r.BillingEmail)
	require.Equal(t, "d@cm.co", *r.BillingEmail)
	require.NotNil(t, r.CreatedBy)
	require.Equal(t, "Thọ Nguyễn Quang", *r.CreatedBy)
	require.Equal(t, day(2026, 10, 19), r.Date)
	require.Equal(t, model.Money(2440), r.Total)
	require.Equal(t, model.EstimateDraft, r.Status)
	require.Equal(t, model.SignatureDraft, r.Signature)
	require.Equal(t, "2026-10-19", r.SortValue)

	f.Search = "123188"
	rows = e.listAll(t, f, model.EstimateSortDate, true)
	require.Len(t, rows, 1)
	require.Equal(t, model.EstimateAccepted, rows[0].Status)
	require.Equal(t, model.SignatureSigned, rows[0].Signature)
	require.Equal(t, model.Money(15000), rows[0].Total)
	require.Nil(t, rows[0].Phone, "customer 63733 has no phone")
}

func TestListHandlesMissingEmailCreatorAndPhone(t *testing.T) {
	e := newEnv(t, true)
	f := octoberActive()
	f.Search = "EN2"

	rows := e.listAll(t, f, model.EstimateSortDate, true)

	require.Len(t, rows, 1)
	require.Nil(t, rows[0].Phone)
	require.Nil(t, rows[0].BillingEmail)
	require.Nil(t, rows[0].CreatedBy)
	require.Equal(t, model.Money(2000), rows[0].Total)
}

var sortCast = map[model.EstimateSortKey]string{
	model.EstimateSortAccountNumber: "text", model.EstimateSortNumber: "text", model.EstimateSortCustomer: "text",
	model.EstimateSortPhone: "text", model.EstimateSortBillingEmail: "text", model.EstimateSortCreatedBy: "text",
	model.EstimateSortDate: "date", model.EstimateSortTotal: "numeric", model.EstimateSortStatus: "text",
	model.EstimateSortSignatureStatus: "text",
}

// compare asks Postgres to order two sort values, so text keys use the same collation as ORDER BY.
func (e env) compare(t *testing.T, key model.EstimateSortKey, a, b string) int {
	t.Helper()
	cast := sortCast[key]
	var c int
	sql := fmt.Sprintf(`SELECT CASE WHEN $1::text::%[1]s < $2::text::%[1]s THEN -1
	                                WHEN $1::text::%[1]s = $2::text::%[1]s THEN 0 ELSE 1 END`, cast)
	require.NoError(t, e.pool.QueryRow(context.Background(), sql, a, b).Scan(&c))
	return c
}

func TestEverySortKeyOrdersCorrectlyInBothDirections(t *testing.T) {
	e := newEnv(t, true)
	require.Len(t, sortCast, len(model.EstimateSortKeys), "every sort key must be covered")

	for _, key := range model.EstimateSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				rows := e.listAll(t, octoberActive(), key, desc)
				require.Len(t, rows, 22)

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
				case model.EstimateSortDate, model.EstimateSortStatus, model.EstimateSortSignatureStatus, model.EstimateSortTotal:
					require.Positive(t, ties, "this key should have tied values in the data, or the tie-break goes untested")
				}
			})
		}
	}
}

func TestKeysetPagingWalksEveryKeyAndDirectionWithoutGapsOrDuplicates(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()

	for _, key := range model.EstimateSortKeys {
		for _, desc := range []bool{false, true} {
			for _, limit := range []int{1, 5} {
				t.Run(fmt.Sprintf("%s/desc=%v/limit=%d", key, desc, limit), func(t *testing.T) {
					want := ids(e.listAll(t, octoberActive(), key, desc))

					var got []int64
					q := query(octoberActive(), key, desc, limit)
					for pages := 0; ; pages++ {
						require.Less(t, pages, 100, "paging must terminate")

						rows, err := e.repo.List(ctx, q)
						require.NoError(t, err)
						page := rows
						hasMore := len(rows) > limit
						if hasMore {
							page = rows[:limit]
						}
						got = append(got, ids(page)...)
						if !hasMore {
							break
						}
						last := page[len(page)-1]
						q.After = &model.EstimateCursor{SortValue: last.SortValue, ID: last.ID}
					}

					require.Equal(t, want, got, "walking the pages must reproduce the full ordered list exactly")
					require.Len(t, got, 22)
				})
			}
		}
	}
}

func TestListFilters(t *testing.T) {
	e := newEnv(t, false)

	tests := []struct {
		name   string
		mutate func(*model.EstimateFilter)
		want   []string // nil means "assert on count only"
		count  int
	}{
		{"accepted", func(f *model.EstimateFilter) { f.Statuses = []model.EstimateStatus{model.EstimateAccepted} }, []string{"123188"}, 1},
		{"declined", func(f *model.EstimateFilter) { f.Statuses = []model.EstimateStatus{model.EstimateDeclined} }, []string{"123189"}, 1},
		{"accepted or declined", func(f *model.EstimateFilter) {
			f.Statuses = []model.EstimateStatus{model.EstimateAccepted, model.EstimateDeclined}
		}, []string{"123188", "123189"}, 2},
		{"draft", func(f *model.EstimateFilter) { f.Statuses = []model.EstimateStatus{model.EstimateDraft} }, nil, 17},
		{"sent is empty in the active tab", func(f *model.EstimateFilter) { f.Statuses = []model.EstimateStatus{model.EstimateSent} }, []string{}, 0},
		{"signed", func(f *model.EstimateFilter) { f.SignatureStatus = []model.SignatureStatus{model.SignatureSigned} }, []string{"123188"}, 1},
		{"declined signature", func(f *model.EstimateFilter) { f.SignatureStatus = []model.SignatureStatus{model.SignatureDeclined} }, []string{"123189"}, 1},
		{"archived tab", func(f *model.EstimateFilter) { f.Lifecycle = model.LifecycleArchived }, []string{"123150"}, 1},
		{"deleted tab", func(f *model.EstimateFilter) { f.Lifecycle = model.LifecycleDeleted }, []string{"123151"}, 1},
		{"one day", func(f *model.EstimateFilter) { f.DateFrom, f.DateTo = day(2026, 10, 21), day(2026, 10, 21) }, []string{"123188", "123189"}, 2},
		{"from only", func(f *model.EstimateFilter) { f.DateFrom, f.DateTo = day(2026, 10, 20), time.Time{} }, []string{"123187", "123184", "123188", "123189"}, 4},
		{"to only", func(f *model.EstimateFilter) { f.DateFrom, f.DateTo = time.Time{}, day(2026, 10, 8) }, []string{"123156", "123159", "123158", "123157", "123155"}, 5},
		{"empty range", func(f *model.EstimateFilter) { f.DateFrom, f.DateTo = day(2027, 1, 1), day(2027, 1, 31) }, []string{}, 0},
		{"search by number", func(f *model.EstimateFilter) { f.Search = "12318" }, nil, 7},
		{"search by account number", func(f *model.EstimateFilter) { f.Search = "5002" }, []string{"123159", "123158", "123157"}, 3},
		{"search by customer name", func(f *model.EstimateFilter) { f.Search = "alaba" }, []string{"123159", "123158", "123157"}, 3},
		{"search by billing email", func(f *model.EstimateFilter) { f.Search = "ALABA123@yopmail" }, []string{"123159", "123158", "123157"}, 3},
		{"search matches nothing", func(f *model.EstimateFilter) { f.Search = "zzz-nothing" }, []string{}, 0},
		{"percent is literal", func(f *model.EstimateFilter) { f.Search = "%" }, []string{}, 0},
		{"underscore is literal", func(f *model.EstimateFilter) { f.Search = "1_3" }, []string{}, 0},
		{"backslash is literal", func(f *model.EstimateFilter) { f.Search = `\` }, []string{}, 0},
		{"sql injection text matches nothing", func(f *model.EstimateFilter) { f.Search = `'; DROP TABLE billing_documents; --` }, []string{}, 0},
		{"filters combine", func(f *model.EstimateFilter) {
			f.Statuses = []model.EstimateStatus{model.EstimateDraft}
			f.Search = "5002"
			f.DateTo = day(2026, 10, 8)
		}, []string{"123159", "123158", "123157"}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := octoberActive()
			tt.mutate(&f)

			rows := e.listAll(t, f, model.EstimateSortNumber, false)
			require.Len(t, rows, tt.count)
			if tt.want != nil {
				require.ElementsMatch(t, tt.want, numbers(rows))
			}

			summary, err := e.repo.Summary(context.Background(), f)
			require.NoError(t, err)
			require.EqualValues(t, tt.count, summary.Count, "the summary follows the same filter")
		})
	}

	// the database is still intact after the hostile search above
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM billing_documents WHERE parent_type = 'estimate'").Scan(&n))
	require.Equal(t, 21, n)
}

func TestSummary(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	t.Run("totals the whole filtered set", func(t *testing.T) {
		rows := e.listAll(t, octoberActive(), model.EstimateSortDate, true)
		var want model.Money
		for _, r := range rows {
			want += r.Total
		}

		s, err := e.repo.Summary(ctx, octoberActive())
		require.NoError(t, err)

		require.EqualValues(t, 19, s.Count)
		require.Equal(t, want, s.Total, "must equal the sum of the listed rows")
		// 12 x 24.40 + 0 + 96.22 + 3.00 + 0 + 80.60 + 150.00 + 75.50
		require.Equal(t, model.Money(29280+9622+300+8060+15000+7550), s.Total)
	})

	t.Run("totals include declined and draft estimates unless a status filter leaves them out", func(t *testing.T) {
		all, err := e.repo.Summary(ctx, octoberActive())
		require.NoError(t, err)

		f := octoberActive()
		f.Statuses = []model.EstimateStatus{model.EstimateDeclined}
		declined, err := e.repo.Summary(ctx, f)
		require.NoError(t, err)
		require.EqualValues(t, 1, declined.Count)
		require.Equal(t, model.Money(7550), declined.Total, "estimate 123189 is declined")

		f.Statuses = []model.EstimateStatus{model.EstimateDraft, model.EstimateSent, model.EstimateAccepted}
		others, err := e.repo.Summary(ctx, f)
		require.NoError(t, err)

		require.Equal(t, all.Count, others.Count+declined.Count)
		require.Equal(t, all.Total, others.Total+declined.Total, "the default total counts the declined estimate")
	})

	t.Run("archived and deleted tabs have their own totals", func(t *testing.T) {
		f := octoberActive()
		f.Lifecycle = model.LifecycleArchived
		s, err := e.repo.Summary(ctx, f)
		require.NoError(t, err)
		require.EqualValues(t, 1, s.Count)
		require.Equal(t, model.Money(4000), s.Total)

		f.Lifecycle = model.LifecycleDeleted
		s, err = e.repo.Summary(ctx, f)
		require.NoError(t, err)
		require.EqualValues(t, 1, s.Count)
		require.Equal(t, model.Money(1000), s.Total)
	})

	t.Run("an empty set totals zero", func(t *testing.T) {
		f := octoberActive()
		f.Search = "zzz-nothing"
		s, err := e.repo.Summary(ctx, f)
		require.NoError(t, err)
		require.Equal(t, model.EstimateSummary{}, s)
	})
}

// A customer with several phones must not multiply the estimate's rows; the first phone wins.
func TestPhoneIsTheCustomersFirstAndNeverDuplicatesRows(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	_, err := e.pool.Exec(ctx, `INSERT INTO customer_phones (customer_id, number)
		SELECT id, '999-NEW' FROM customers WHERE account_number = '63714'
		UNION ALL SELECT id, '000-NEWER' FROM customers WHERE account_number = '63714'`)
	require.NoError(t, err)

	f := octoberActive()
	f.Search = "63714"
	rows := e.listAll(t, f, model.EstimateSortNumber, false)

	require.Equal(t, []string{"123172", "123183"}, numbers(rows), "one row per estimate")
	for _, r := range rows {
		require.NotNil(t, r.Phone)
		require.Equal(t, "012222222", *r.Phone, "the earliest-added phone")
	}

	all := e.listAll(t, octoberActive(), model.EstimateSortDate, true)
	require.Len(t, all, 19)
	s, err := e.repo.Summary(ctx, octoberActive())
	require.NoError(t, err)
	require.EqualValues(t, 19, s.Count)
}

// billing_documents holds invoices next to estimates; this report must never see them, even when an invoice
// shares an estimate's number.
func TestInvoicesNeverAppearInEstimateResults(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	_, err := e.pool.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total)
		SELECT 'invoice', '123187', id, '2026-10-20', 777 FROM customers WHERE account_number = '63733'`)
	require.NoError(t, err)

	rows := e.listAll(t, octoberActive(), model.EstimateSortDate, true)
	require.Len(t, rows, 19, "invoices do not change the estimate count")
	f := octoberActive()
	f.Search = "123187"
	got := e.listAll(t, f, model.EstimateSortDate, true)
	require.Len(t, got, 1)
	require.Equal(t, model.Money(2440), got[0].Total, "the estimate, not the 777.00 invoice")

	s, err := e.repo.Summary(ctx, f)
	require.NoError(t, err)
	require.EqualValues(t, 1, s.Count)
	require.Equal(t, model.Money(2440), s.Total)

	f.Search = "85033" // a seeded invoice number
	require.Empty(t, e.listAll(t, f, model.EstimateSortDate, true))
}

func TestRepositoryWorksInsideATransaction(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	tx, err := e.pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total)
		SELECT 'estimate', 'TX1', id, '2026-10-15', 5 FROM customers WHERE account_number = '5001'`)
	require.NoError(t, err)

	inTx, err := repository.NewEstimateRepository(tx).Summary(ctx, octoberActive())
	require.NoError(t, err)
	outside, err := e.repo.Summary(ctx, octoberActive())
	require.NoError(t, err)

	require.EqualValues(t, 20, inTx.Count, "the transaction sees its own uncommitted row")
	require.EqualValues(t, 19, outside.Count, "other connections do not")

	rows, err := repository.NewEstimateRepository(tx).List(ctx, query(octoberActive(), model.EstimateSortNumber, false, 100))
	require.NoError(t, err)
	require.Contains(t, numbers(rows), "TX1")
}

func TestQueriesHonorContextCancellation(t *testing.T) {
	e := newEnv(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.repo.List(ctx, query(octoberActive(), model.EstimateSortDate, true, 10))
	require.ErrorIs(t, err, context.Canceled)

	_, err = e.repo.Summary(ctx, octoberActive())
	require.ErrorIs(t, err, context.Canceled)
}

func TestInvalidQueriesAreRejectedBeforeReachingTheDatabase(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	_, err := e.repo.List(ctx, query(octoberActive(), "nope", false, 10))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	_, err = e.repo.List(ctx, query(octoberActive(), model.EstimateSortDate, false, 0))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	bad := octoberActive()
	bad.Lifecycle = "bogus"
	_, err = e.repo.Summary(ctx, bad)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

func TestConcurrentCallsAgree(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([]int64, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := e.repo.Summary(ctx, octoberActive())
			results[i], errs[i] = s.Count, err
		}()
	}
	wg.Wait()

	for i := range results {
		require.NoError(t, errs[i])
		require.EqualValues(t, 19, results[i])
	}
}
