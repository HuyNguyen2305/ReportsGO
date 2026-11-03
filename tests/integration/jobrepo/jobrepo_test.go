//go:build integration

package jobrepo_test

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

// octoberJobs is how many of the 38 seeded jobs were created in October (UTC) and are not deleted; one is from 09-30, one from 11-01,
// one from 2025-12-01 and one October job is deleted.
const octoberJobs = 34

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
	model.JobSortPhone: "text", model.JobSortAddress: "text",
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
		{"all of october", func(f *model.JobFilter) {}, 34, 179626},
		{"unconfirmed", func(f *model.JobFilter) { f.Statuses = []model.JobStatus{model.JobUnconfirmed} }, 6, 28600},
		{"confirmed", func(f *model.JobFilter) { f.Statuses = []model.JobStatus{model.JobConfirmed} }, 9, 21666},
		{"completed", func(f *model.JobFilter) { f.Statuses = []model.JobStatus{model.JobCompleted} }, 18, 119860},
		{"cancelled", func(f *model.JobFilter) { f.Statuses = []model.JobStatus{model.JobCancelled} }, 1, 9500},
		{"completed or cancelled", func(f *model.JobFilter) {
			f.Statuses = []model.JobStatus{model.JobCompleted, model.JobCancelled}
		}, 19, 129360},
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
		{"open start", func(f *model.JobFilter) { f.CreatedFrom, f.CreatedBefore = time.Time{}, utc(2026, 10, 6, 0, 0, 0, 0) }, 13, 47130},
		{"open end", func(f *model.JobFilter) { f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 13, 0, 0, 0, 0), time.Time{} }, 5, 35500},
		{"september", func(f *model.JobFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 9, 1, 0, 0, 0, 0), utc(2026, 10, 1, 0, 0, 0, 0)
		}, 1, 6500},
		{"no date limits at all", func(f *model.JobFilter) { f.CreatedFrom, f.CreatedBefore = time.Time{}, time.Time{} }, 37, 192626},
		{"empty range", func(f *model.JobFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2027, 1, 1, 0, 0, 0, 0), utc(2027, 2, 1, 0, 0, 0, 0)
		}, 0, 0},
		{"search by customer name", func(f *model.JobFilter) { f.Search = "william" }, 15, 97500},
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
		WHERE deleted_at IS NULL AND created_at >= '2026-10-01 00:00:00+00' AND created_at < '2026-11-01 00:00:00+00'`).Scan(&count, &total))

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

// insertOctoberJob commits one more October job of 10.00.
func insertOctoberJob(t *testing.T, conn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO jobs (customer_id, location_id, service_type_id, invoice_value, created_at)
		SELECT l.customer_id, l.id, st.id, 10, '2026-10-19 08:00:00+00'
		FROM (SELECT id, customer_id FROM service_locations ORDER BY id LIMIT 1) l,
		     (SELECT id FROM service_types ORDER BY id LIMIT 1) st`)
	require.NoError(t, err)
}

func TestPage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := query(october(), model.JobSortCreatedAt, true, 5)

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
		require.EqualValues(t, octoberJobs, page.Summary.Count)
		require.Len(t, page.Rows, 6, "limit+1 rows, so the caller can tell another page follows")
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
		insertOctoberJob(t, tx)

		page, err := repository.NewJobRepository(tx).Page(ctx, q, true)
		require.NoError(t, err)
		require.EqualValues(t, octoberJobs+1, page.Summary.Count, "the caller's transaction is the snapshot and sees its own row")
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

// The count and total must describe the rows they are shown with. A job committed by someone else between
// reading the rows and reading the totals would otherwise make the two disagree.
func TestPageReadsRowsAndSummaryFromOneSnapshot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := query(october(), model.JobSortCreatedAt, true, 100)

	committed := 0
	pool := testdb.SnapshotPool{Pool: e.pool, BeforeQueryRow: func() {
		insertOctoberJob(t, e.pool)
		committed++
	}}

	page, err := repository.NewJobRepository(pool).Page(ctx, q, true)
	require.NoError(t, err)

	require.Equal(t, 1, committed, "the concurrent insert must have happened between the two reads")
	require.Len(t, page.Rows, octoberJobs)
	require.EqualValues(t, octoberJobs, page.Summary.Count, "the count ignores the job committed after the rows were read")
	var rowsTotal model.Money
	for _, r := range page.Rows {
		rowsTotal += r.InvoiceValue
	}
	require.Equal(t, rowsTotal, page.Summary.Total, "the total is the sum of the rows it describes")

	after, err := e.repo.Summary(ctx, q.Filter)
	require.NoError(t, err)
	require.EqualValues(t, octoberJobs+1, after.Count, "the committed job is real and visible to a later read")
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

func TestDeletedJobsAreNeitherListedNorCountedNorTotalled(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	ids := func(rows []model.JobRow) []int64 {
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.ID
		}
		return out
	}
	var seedDeleted int64
	require.NoError(t, e.pool.QueryRow(ctx, `SELECT id FROM jobs WHERE deleted_at IS NOT NULL`).Scan(&seedDeleted))
	require.NotContains(t, ids(e.listAll(t, october(), model.JobSortCreatedAt, true)), seedDeleted,
		"the seed's deleted job was created in October but is not listed")

	before, err := e.repo.Summary(ctx, october())
	require.NoError(t, err)

	// soft-delete the cancelled October job (95.00): it leaves the rows, the count and the total
	var cancelled int64
	require.NoError(t, e.pool.QueryRow(ctx, `UPDATE jobs SET deleted_at = '2026-10-23 00:00:00+00'
		WHERE status = 'cancelled' AND deleted_at IS NULL RETURNING id`).Scan(&cancelled))

	after, err := e.repo.Summary(ctx, october())
	require.NoError(t, err)
	require.NotContains(t, ids(e.listAll(t, october(), model.JobSortCreatedAt, true)), cancelled)
	require.Equal(t, before.Count-1, after.Count)
	require.Equal(t, before.Total-9500, after.Total)
}

func TestACancelledJobThatIsNotDeletedIsCountedAndTotalled(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Statuses = []model.JobStatus{model.JobCancelled}

	s, err := e.repo.Summary(context.Background(), f)

	require.NoError(t, err)
	require.Equal(t, model.JobSummary{Count: 1, Total: 9500}, s)
}

func TestAssignedToFilterMatchesTheAssignedStaff(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	staff, err := e.repo.Staff(ctx)
	require.NoError(t, err)
	require.Len(t, staff, 2)
	staffID := map[string]int64{}
	for _, s := range staff {
		staffID[s.Name] = s.ID
	}

	// independent counts and totals of the live October jobs assigned to any of ids
	want := func(ids []int64) model.JobSummary {
		var s model.JobSummary
		require.NoError(t, e.pool.QueryRow(ctx, `
			SELECT count(*), COALESCE(SUM(invoice_value * 100), 0)::bigint FROM jobs
			WHERE deleted_at IS NULL AND assigned_to = ANY($1)
			  AND created_at >= '2026-10-01 00:00:00+00' AND created_at < '2026-11-01 00:00:00+00'`, ids).Scan(&s.Count, &s.Total))
		return s
	}

	tho, thon := staffID["Thọ Nguyễn Quang"], staffID["thon nguyen"]
	tests := []struct {
		name string
		ids  []int64
	}{
		{"one staff member", []int64{tho}},
		{"the other staff member", []int64{thon}},
		{"both staff members", []int64{tho, thon}},
		{"an unknown staff member", []int64{999999}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := october()
			f.AssignedToIDs = tt.ids

			rows := e.listAll(t, f, model.JobSortCreatedAt, true)
			s, err := e.repo.Summary(ctx, f)

			require.NoError(t, err)
			require.Equal(t, want(tt.ids), s)
			require.Len(t, rows, int(s.Count))
		})
	}

	t.Run("unassigned jobs only match without the filter", func(t *testing.T) {
		f := october()
		f.AssignedToIDs = []int64{tho, thon}
		assigned, err := e.repo.Summary(ctx, f)
		require.NoError(t, err)

		require.Positive(t, assigned.Count)
		require.Less(t, assigned.Count, int64(octoberJobs), "the seed has unassigned October jobs")
	})
}

func TestStaffListsEveryUser(t *testing.T) {
	e := newEnv(t)

	staff, err := e.repo.Staff(context.Background())

	require.NoError(t, err)
	names := make([]string, len(staff))
	for i, s := range staff {
		require.Positive(t, s.ID)
		names[i] = s.Name
	}
	require.ElementsMatch(t, []string{"thon nguyen", "Thọ Nguyễn Quang"}, names, "the order follows the database collation")
}

func TestRowsCarryTheJobsAddressAndTheCustomersFirstPhone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// a second, later phone for AAA1 ccc1: the report keeps showing the first one
	_, err := e.pool.Exec(ctx, `INSERT INTO customer_phones (customer_id, number, kind)
		SELECT id, '555-9999', 'home' FROM customers WHERE account_number = '63706'`)
	require.NoError(t, err)

	byAccount := func(account string) model.JobRow {
		t.Helper()
		f := october()
		f.Search = account
		rows := e.listAll(t, f, model.JobSortCreatedAt, false)
		require.NotEmpty(t, rows)
		return rows[0]
	}

	withPhone := byAccount("63706")
	require.NotNil(t, withPhone.Phone)
	require.Equal(t, "(098) 765-4321", *withPhone.Phone, "the customer's first phone")
	require.Equal(t, model.LookupAddress{Line1: "Miami Beach Boardwalk 2", City: "Miami", State: "FL", Zip: "33139", County: "Miami-Dade County"},
		withPhone.Address)

	withoutPhone := byAccount("5001")
	require.Nil(t, withoutPhone.Phone, "William Saliba has no phone")
	require.Equal(t, model.LookupAddress{Line1: "West Loop 1604 South", City: "San Antonio", State: "TX", Zip: "78264", County: "Bexar County"},
		withoutPhone.Address)
}

func TestSearchMatchesThePhoneAndTheAddress(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// independent count of the live October jobs whose customer or location matches
	want := func(cond string) int64 {
		var n int64
		require.NoError(t, e.pool.QueryRow(ctx, `
			SELECT count(*) FROM jobs j
			JOIN customers c ON c.id = j.customer_id
			JOIN service_locations sl ON sl.id = j.location_id
			WHERE j.deleted_at IS NULL
			  AND j.created_at >= '2026-10-01 00:00:00+00' AND j.created_at < '2026-11-01 00:00:00+00'
			  AND `+cond).Scan(&n))
		return n
	}

	tests := []struct {
		name, search, cond string
	}{
		{"part of a phone number", "765-43", `c.account_number = '63706'`},
		{"part of an address line, any case", "BOARDWALK", `sl.address_line1 = 'Miami Beach Boardwalk 2'`},
		{"a city", "san antonio", `sl.city = 'San Antonio'`},
		{"a second phone is not searched", "555-9999", `false`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := october()
			f.Search = tt.search

			rows := e.listAll(t, f, model.JobSortCreatedAt, false)
			s, err := e.repo.Summary(ctx, f)

			require.NoError(t, err)
			require.Equal(t, want(tt.cond), s.Count, "the summary follows the same search")
			require.Len(t, rows, int(s.Count))
		})
	}
}
