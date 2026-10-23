//go:build integration

package jobrepo_test

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

func utc(y int, m time.Month, d, h, min, s, ns int) time.Time {
	return time.Date(y, m, d, h, min, s, ns, time.UTC)
}

// env is a migrated, seeded throwaway database with a job repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.JobRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewJobRepository(pool)}
}

// october is the whole month in UTC: the first instant of October up to (not including) the first of November.
func october() model.JobFilter {
	return model.JobFilter{CreatedFrom: utc(2026, 10, 1, 0, 0, 0, 0), CreatedBefore: utc(2026, 11, 1, 0, 0, 0, 0)}
}

// octoberJobs is how many of the 37 seeded jobs were created in October (UTC); one is from 09-30 and one from 11-01.
const octoberJobs = 35

func query(f model.JobFilter, key model.JobSortKey, desc bool, limit int) model.JobQuery {
	return model.JobQuery{Filter: f, Sort: model.JobSort{Key: key, Desc: desc}, Limit: limit}
}

func listAll(t *testing.T, repo *repository.JobRepository, f model.JobFilter, key model.JobSortKey, desc bool) []model.JobRow {
	t.Helper()
	rows, err := repo.List(context.Background(), query(f, key, desc, 1000))
	require.NoError(t, err)
	return rows
}

func (e env) listAll(t *testing.T, f model.JobFilter, key model.JobSortKey, desc bool) []model.JobRow {
	t.Helper()
	return listAll(t, e.repo, f, key, desc)
}

func ids(rows []model.JobRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestListDefaultReturnsOctoberJobsNewestFirst(t *testing.T) {
	e := newEnv(t)

	rows := e.listAll(t, october(), model.JobSortCreatedAt, true)

	require.Len(t, rows, octoberJobs)
	require.Equal(t, utc(2026, 10, 22, 8, 15, 30, 123456000), rows[0].CreatedAt, "the newest has microseconds")
	for i := 1; i < len(rows); i++ {
		require.False(t, rows[i].CreatedAt.After(rows[i-1].CreatedAt), "timestamps must not increase")
	}
}

func TestListReturnsAllReportFields(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Statuses = []model.JobStatus{model.JobCancelled}
	f.Search = "63733"

	rows := e.listAll(t, f, model.JobSortCreatedAt, true)

	require.Len(t, rows, 1)
	r := rows[0]
	require.NotZero(t, r.ID)
	require.NotZero(t, r.CustomerID)
	require.Equal(t, "Customer 1", r.CustomerName)
	require.Equal(t, "63733", r.AccountNumber)
	require.NotZero(t, r.ServiceTypeID)
	require.Equal(t, "Initial Service", r.ServiceType)
	require.NotNil(t, r.SourceID)
	require.Equal(t, "Google", *r.Source)
	require.Equal(t, model.Money(9500), r.InvoiceValue)
	require.Equal(t, utc(2026, 10, 22, 8, 15, 30, 123456000), r.CreatedAt, "microseconds survive, in UTC")
	require.Equal(t, time.UTC, r.CreatedAt.Location())
	require.Equal(t, model.JobCancelled, r.Status)
	require.Equal(t, "2026-10-22T08:15:30.123456Z", r.SortValue)
}

func TestListHandlesAJobWithoutASource(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = "63702"

	rows := e.listAll(t, f, model.JobSortCreatedAt, true)

	require.Len(t, rows, 1)
	require.Nil(t, rows[0].SourceID)
	require.Nil(t, rows[0].Source)
}

var sortCast = map[model.JobSortKey]string{
	model.JobSortCustomer: "text", model.JobSortAccountNumber: "text", model.JobSortServiceType: "text",
	model.JobSortSource: "text", model.JobSortInvoiceValue: "numeric", model.JobSortCreatedAt: "timestamptz",
	model.JobSortStatus: "text",
}

// compare asks Postgres to order two sort values, so text keys use the same collation as ORDER BY.
func (e env) compare(t *testing.T, key model.JobSortKey, a, b string) int {
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
	require.Len(t, sortCast, len(model.JobSortKeys), "every sort key must be covered")

	for _, key := range model.JobSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				rows := e.listAll(t, october(), key, desc)
				require.Len(t, rows, octoberJobs)

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

func TestJobsWithoutASourceSortFirstAscending(t *testing.T) {
	e := newEnv(t)

	rows := e.listAll(t, october(), model.JobSortSource, false)

	require.Nil(t, rows[0].Source)
	require.Equal(t, "", rows[0].SortValue)
	require.NotNil(t, rows[len(rows)-1].Source, "the named sources come after the empty one")
}

func walk(t *testing.T, repo *repository.JobRepository, key model.JobSortKey, desc bool, limit int) []int64 {
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
		q.After = &model.JobCursor{SortValue: last.SortValue, ID: last.ID}
	}
}

func TestKeysetPagingWalksEveryKeyAndDirectionWithoutGapsOrDuplicates(t *testing.T) {
	e := newEnv(t)

	for _, key := range model.JobSortKeys {
		for _, desc := range []bool{false, true} {
			for _, limit := range []int{1, 7} {
				t.Run(fmt.Sprintf("%s/desc=%v/limit=%d", key, desc, limit), func(t *testing.T) {
					want := ids(e.listAll(t, october(), key, desc))

					got := walk(t, e.repo, key, desc, limit)

					require.Equal(t, want, got, "walking the pages must reproduce the full ordered list exactly")
					require.Len(t, got, octoberJobs)
				})
			}
		}
	}
}

// A cursor's timestamp must mean the same instant whatever time zone the database session uses.
func TestCreatedAtCursorsAreIndependentOfTheSessionTimeZone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	want := ids(e.listAll(t, october(), model.JobSortCreatedAt, true))

	for _, zone := range []string{"UTC", "Asia/Tokyo", "America/Los_Angeles", "Pacific/Kiritimati"} {
		t.Run(zone, func(t *testing.T) {
			conn, err := e.pool.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()
			_, err = conn.Exec(ctx, "SET TIME ZONE '"+zone+"'")
			require.NoError(t, err)

			repo := repository.NewJobRepository(conn.Conn())
			for _, limit := range []int{1, 4} {
				require.Equal(t, want, walk(t, repo, model.JobSortCreatedAt, true, limit), "limit %d", limit)
			}
			rows := listAll(t, repo, october(), model.JobSortCreatedAt, true)
			require.Equal(t, "2026-10-22T08:15:30.123456Z", rows[0].SortValue)
		})
	}
}

func TestListFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	serviceTypes, err := e.repo.ServiceTypes(ctx)
	require.NoError(t, err)
	typeID := map[string]int64{}
	for _, ty := range serviceTypes {
		typeID[ty.Name] = ty.ID
	}
	sources, err := e.repo.LeadSources(ctx)
	require.NoError(t, err)
	sourceID := map[string]int64{}
	for _, so := range sources {
		sourceID[so.Name] = so.ID
	}
	require.NotZero(t, typeID["Initial Service"])
	require.NotZero(t, sourceID["Google"])

	tests := []struct {
		name      string
		mutate    func(*model.JobFilter)
		count     int
		wantTotal model.Money
	}{
		{"all of october", func(f *model.JobFilter) {}, 35, 186126},
		{"unconfirmed", func(f *model.JobFilter) { f.Statuses = []model.JobStatus{model.JobUnconfirmed} }, 6, 28600},
		{"confirmed", func(f *model.JobFilter) { f.Statuses = []model.JobStatus{model.JobConfirmed} }, 9, 21666},
		{"completed", func(f *model.JobFilter) { f.Statuses = []model.JobStatus{model.JobCompleted} }, 18, 119860},
		{"cancelled", func(f *model.JobFilter) { f.Statuses = []model.JobStatus{model.JobCancelled} }, 2, 16000},
		{"completed or cancelled", func(f *model.JobFilter) {
			f.Statuses = []model.JobStatus{model.JobCompleted, model.JobCancelled}
		}, 20, 135860},
		{"one service type", func(f *model.JobFilter) { f.ServiceTypeIDs = []int64{typeID["Initial Service"]} }, 4, 38000},
		{"two service types", func(f *model.JobFilter) {
			f.ServiceTypeIDs = []int64{typeID["Full service"], typeID["Call Back Service"]}
		}, 4, 17926},
		{"unknown service type", func(f *model.JobFilter) { f.ServiceTypeIDs = []int64{999999} }, 0, 0},
		{"one source", func(f *model.JobFilter) { f.LeadSourceIDs = []int64{sourceID["Google"]} }, 2, 9500},
		{"two sources", func(f *model.JobFilter) {
			f.LeadSourceIDs = []int64{sourceID["Facebook"], sourceID["Referral"]}
		}, 3, 9063},
		{"status and source", func(f *model.JobFilter) {
			f.Statuses = []model.JobStatus{model.JobCancelled}
			f.LeadSourceIDs = []int64{sourceID["Google"]}
		}, 1, 9500},
		{"the day boundary: the first and last instant of 10-10", func(f *model.JobFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 10, 0, 0, 0, 0), utc(2026, 10, 11, 0, 0, 0, 0)
		}, 3, 15463},
		{"the instant after the day is out", func(f *model.JobFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 11, 0, 0, 0, 0), utc(2026, 10, 12, 0, 0, 0, 0)
		}, 1, 6500},
		{"two jobs at the same instant", func(f *model.JobFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 12, 6, 0, 0, 0), utc(2026, 10, 12, 6, 0, 1, 0)
		}, 2, 0},
		{"open start", func(f *model.JobFilter) { f.CreatedFrom, f.CreatedBefore = time.Time{}, utc(2026, 10, 6, 0, 0, 0, 0) }, 12, 47130},
		{"open end", func(f *model.JobFilter) { f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 13, 0, 0, 0, 0), time.Time{} }, 6, 42000},
		{"september", func(f *model.JobFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 9, 1, 0, 0, 0, 0), utc(2026, 10, 1, 0, 0, 0, 0)
		}, 1, 6500},
		{"no date limits at all", func(f *model.JobFilter) { f.CreatedFrom, f.CreatedBefore = time.Time{}, time.Time{} }, 37, 199126},
		{"empty range", func(f *model.JobFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2027, 1, 1, 0, 0, 0, 0), utc(2027, 2, 1, 0, 0, 0, 0)
		}, 0, 0},
		{"search by customer name", func(f *model.JobFilter) { f.Search = "william" }, 16, 104000},
		{"search by account number", func(f *model.JobFilter) { f.Search = "63706" }, 5, 15463},
		{"search by service type", func(f *model.JobFilter) { f.Search = "call back" }, 2, 2066},
		{"search by service type is case-insensitive", func(f *model.JobFilter) { f.Search = "INITIAL" }, 4, 38000},
		{"search matches nothing", func(f *model.JobFilter) { f.Search = "zzz-nothing" }, 0, 0},
		{"percent is literal", func(f *model.JobFilter) { f.Search = "%" }, 0, 0},
		{"underscore is literal", func(f *model.JobFilter) { f.Search = "Ful_ service" }, 0, 0},
		{"backslash is literal", func(f *model.JobFilter) { f.Search = `\` }, 0, 0},
		{"sql injection text matches nothing", func(f *model.JobFilter) { f.Search = `'; DROP TABLE jobs; --` }, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := october()
			tt.mutate(&f)

			rows := e.listAll(t, f, model.JobSortCreatedAt, false)
			require.Len(t, rows, tt.count)

			summary, err := e.repo.Summary(ctx, f)
			require.NoError(t, err)
			require.Equal(t, model.JobSummary{Count: int64(tt.count), Total: tt.wantTotal}, summary,
				"the summary follows the same filter")

			var sum model.Money
			for _, r := range rows {
				sum += r.InvoiceValue
			}
			require.Equal(t, sum, summary.Total, "the total is the sum of the listed values")
		})
	}
}

func TestSummaryTotalMatchesAnIndependentQuery(t *testing.T) {
	e := newEnv(t)
	var count, total int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `
		SELECT count(*), (sum(invoice_value) * 100)::bigint FROM jobs
		WHERE created_at >= '2026-10-01 00:00:00+00' AND created_at < '2026-11-01 00:00:00+00'`).Scan(&count, &total))

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
	require.Equal(t, model.JobSummary{}, s)
}

func TestLookups(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	types, err := e.repo.ServiceTypes(ctx)
	require.NoError(t, err)
	require.Len(t, types, 5)
	for _, ty := range types {
		require.NotZero(t, ty.ID)
		require.NotEmpty(t, ty.Name)
	}

	sources, err := e.repo.LeadSources(ctx)
	require.NoError(t, err)
	names := make([]string, len(sources))
	for i, s := range sources {
		require.NotZero(t, s.ID)
		names[i] = s.Name
	}
	require.Equal(t, []string{"Facebook", "Google", "Referral"}, names, "ordered by name")
}

func TestRepositoryRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.repo.List(ctx, query(october(), "bogus", false, 5))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
	_, err = e.repo.List(ctx, query(october(), model.JobSortCreatedAt, false, 0))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

func TestCancelledContextIsReturnedAsAnError(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.repo.List(ctx, query(october(), model.JobSortCreatedAt, false, 5))
	require.ErrorIs(t, err, context.Canceled)
	_, err = e.repo.Summary(ctx, october())
	require.ErrorIs(t, err, context.Canceled)
	_, err = e.repo.ServiceTypes(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = e.repo.LeadSources(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
