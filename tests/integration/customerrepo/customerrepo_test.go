//go:build integration

package customerrepo_test

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

// env is a migrated, seeded throwaway database with a customer repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.CustomerRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewCustomerRepository(pool)}
}

// october is the whole month in UTC: the first instant of October up to (not including) the first of November.
func october() model.CustomerFilter {
	return model.CustomerFilter{CreatedFrom: utc(2026, 10, 1, 0, 0, 0, 0), CreatedBefore: utc(2026, 11, 1, 0, 0, 0, 0)}
}

// octoberCustomers is how many of the 19 seeded customers were created in October (UTC). Four are from September
// (three original ones and 70004) and one is from 11-01.
const octoberCustomers = 14

func query(f model.CustomerFilter, key model.CustomerSortKey, desc bool, limit int) model.CustomerQuery {
	return model.CustomerQuery{Filter: f, Sort: model.CustomerSort{Key: key, Desc: desc}, Limit: limit}
}

func listAll(t *testing.T, repo *repository.CustomerRepository, f model.CustomerFilter, key model.CustomerSortKey, desc bool) []model.CustomerRow {
	t.Helper()
	rows, err := pageRows(context.Background(), repo, query(f, key, desc, 1000))
	require.NoError(t, err)
	return rows
}

func (e env) listAll(t *testing.T, f model.CustomerFilter, key model.CustomerSortKey, desc bool) []model.CustomerRow {
	t.Helper()
	return listAll(t, e.repo, f, key, desc)
}

func ids(rows []model.CustomerRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func accounts(rows []model.CustomerRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.AccountNumber
	}
	return out
}

func TestListDefaultReturnsOctoberCustomersNewestFirst(t *testing.T) {
	e := newEnv(t)

	rows := e.listAll(t, october(), model.CustomerSortCreatedAt, true)

	require.Len(t, rows, octoberCustomers)
	require.Equal(t, utc(2026, 10, 22, 8, 15, 30, 123456000), rows[0].CreatedAt, "the newest has microseconds")
	for i := 1; i < len(rows); i++ {
		require.False(t, rows[i].CreatedAt.After(rows[i-1].CreatedAt), "timestamps must not increase")
	}
}

func TestListReturnsAllReportFields(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = "70001"

	rows := e.listAll(t, f, model.CustomerSortCreatedAt, true)

	require.Len(t, rows, 1)
	r := rows[0]
	require.NotZero(t, r.ID)
	require.Equal(t, "Ada Lovelace", r.Name)
	require.Equal(t, "70001", r.AccountNumber)
	require.NotNil(t, r.Email)
	require.Equal(t, "ada@example.com", *r.Email)
	require.NotNil(t, r.Phone)
	require.Equal(t, "555-0101", *r.Phone, "the first of two phones")
	require.NotNil(t, r.SourceID)
	require.Equal(t, "Google", *r.Source)
	require.Equal(t, utc(2026, 10, 22, 8, 15, 30, 123456000), r.CreatedAt, "microseconds survive, in UTC")
	require.Equal(t, time.UTC, r.CreatedAt.Location())
	require.Equal(t, "2026-10-22T08:15:30.123456Z", r.SortValue)
}

func TestListHandlesMissingEmailPhoneAndSource(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = "63701"

	rows := e.listAll(t, f, model.CustomerSortCreatedAt, true)

	require.Len(t, rows, 1)
	require.Nil(t, rows[0].Email)
	require.Nil(t, rows[0].Phone)
	require.Nil(t, rows[0].SourceID)
	require.Nil(t, rows[0].Source)
}

func TestACustomerWithSeveralPhonesIsOneRow(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = "70001"

	require.Len(t, e.listAll(t, f, model.CustomerSortCreatedAt, true), 1)
	s, err := pageSummary(context.Background(), e.repo, f)
	require.NoError(t, err)
	require.EqualValues(t, 1, s.Count)
}

var sortCast = map[model.CustomerSortKey]string{
	model.CustomerSortCustomer: "text", model.CustomerSortAccountNumber: "text", model.CustomerSortEmail: "text",
	model.CustomerSortPhone: "text", model.CustomerSortAddress: "text", model.CustomerSortSource: "text",
	model.CustomerSortCreatedAt: "timestamptz",
}

// compare asks Postgres to order two sort values, so text keys use the same collation as ORDER BY.
func (e env) compare(t *testing.T, key model.CustomerSortKey, a, b string) int {
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
	require.Len(t, sortCast, len(model.CustomerSortKeys), "every sort key must be covered")

	// The customer name and the account number are unique, so those keys never tie.
	tied := map[model.CustomerSortKey]bool{
		model.CustomerSortEmail: true, model.CustomerSortPhone: true, model.CustomerSortSource: true, model.CustomerSortCreatedAt: true,
	}

	for _, key := range model.CustomerSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				rows := e.listAll(t, october(), key, desc)
				require.Len(t, rows, octoberCustomers)

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
				if tied[key] {
					require.Positive(t, ties, "the data has tied values for this key, or the tie-break goes untested")
				} else {
					require.Zero(t, ties)
				}
			})
		}
	}
}

func TestMissingValuesSortFirstAscending(t *testing.T) {
	e := newEnv(t)

	for _, key := range []model.CustomerSortKey{model.CustomerSortEmail, model.CustomerSortPhone, model.CustomerSortSource} {
		rows := e.listAll(t, october(), key, false)

		require.Equal(t, "", rows[0].SortValue, string(key))
		require.NotEqual(t, "", rows[len(rows)-1].SortValue, "%s: the named values come after the empty ones", key)
	}
}

func walk(t *testing.T, repo *repository.CustomerRepository, key model.CustomerSortKey, desc bool, limit int) []int64 {
	t.Helper()
	var got []int64
	q := query(october(), key, desc, limit)
	for pages := 0; ; pages++ {
		require.Less(t, pages, 100, "paging must terminate")

		rows, err := pageRows(context.Background(), repo, q)
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
		q.After = &model.CustomerCursor{SortValue: last.SortValue, ID: last.ID}
	}
}

func TestKeysetPagingWalksEveryKeyAndDirectionWithoutGapsOrDuplicates(t *testing.T) {
	e := newEnv(t)

	for _, key := range model.CustomerSortKeys {
		for _, desc := range []bool{false, true} {
			for _, limit := range []int{1, 5} {
				t.Run(fmt.Sprintf("%s/desc=%v/limit=%d", key, desc, limit), func(t *testing.T) {
					want := ids(e.listAll(t, october(), key, desc))

					got := walk(t, e.repo, key, desc, limit)

					require.Equal(t, want, got, "walking the pages must reproduce the full ordered list exactly")
					require.Len(t, got, octoberCustomers)
				})
			}
		}
	}
}

// A cursor's timestamp must mean the same instant whatever time zone the database session uses.
func TestCreatedAtCursorsAreIndependentOfTheSessionTimeZone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	want := ids(e.listAll(t, october(), model.CustomerSortCreatedAt, true))

	for _, zone := range []string{"UTC", "Asia/Tokyo", "America/Los_Angeles", "Pacific/Kiritimati"} {
		t.Run(zone, func(t *testing.T) {
			conn, err := e.pool.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()
			_, err = conn.Exec(ctx, "SET TIME ZONE '"+zone+"'")
			require.NoError(t, err)

			repo := repository.NewCustomerRepository(conn.Conn())
			for _, limit := range []int{1, 4} {
				require.Equal(t, want, walk(t, repo, model.CustomerSortCreatedAt, true, limit), "limit %d", limit)
			}
			rows := listAll(t, repo, october(), model.CustomerSortCreatedAt, true)
			require.Equal(t, "2026-10-22T08:15:30.123456Z", rows[0].SortValue)
		})
	}
}

func TestListFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	sources, err := e.repo.LeadSources(ctx)
	require.NoError(t, err)
	sourceID := map[string]int64{}
	for _, so := range sources {
		sourceID[so.Name] = so.ID
	}
	require.NotZero(t, sourceID["Google"])

	tests := []struct {
		name   string
		mutate func(*model.CustomerFilter)
		count  int
	}{
		{"all of october", func(f *model.CustomerFilter) {}, 14},
		{"one source", func(f *model.CustomerFilter) { f.LeadSourceIDs = []int64{sourceID["Facebook"]} }, 3},
		{"google in october (the september one is out)", func(f *model.CustomerFilter) { f.LeadSourceIDs = []int64{sourceID["Google"]} }, 1},
		{"two sources", func(f *model.CustomerFilter) {
			f.LeadSourceIDs = []int64{sourceID["Referral"], sourceID["Google"]}
		}, 2},
		{"unknown source", func(f *model.CustomerFilter) { f.LeadSourceIDs = []int64{999999} }, 0},
		{"the day boundary: the first and last instant of 10-10", func(f *model.CustomerFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 10, 0, 0, 0, 0), utc(2026, 10, 11, 0, 0, 0, 0)
		}, 2},
		{"the instant after the day is out", func(f *model.CustomerFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 11, 0, 0, 0, 0), utc(2026, 10, 12, 0, 0, 0, 0)
		}, 0},
		{"two customers at the same instant", func(f *model.CustomerFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 12, 6, 0, 0, 0), utc(2026, 10, 12, 6, 0, 1, 0)
		}, 2},
		{"open start", func(f *model.CustomerFilter) {
			f.CreatedFrom, f.CreatedBefore = time.Time{}, utc(2026, 10, 6, 0, 0, 0, 0)
		}, 9},
		{"open end", func(f *model.CustomerFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 13, 0, 0, 0, 0), time.Time{}
		}, 5},
		{"september", func(f *model.CustomerFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 9, 1, 0, 0, 0, 0), utc(2026, 10, 1, 0, 0, 0, 0)
		}, 4},
		{"no date limits at all", func(f *model.CustomerFilter) { f.CreatedFrom, f.CreatedBefore = time.Time{}, time.Time{} }, 20},
		{"empty range", func(f *model.CustomerFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2027, 1, 1, 0, 0, 0, 0), utc(2027, 2, 1, 0, 0, 0, 0)
		}, 0},
		{"search by name", func(f *model.CustomerFilter) { f.Search = "LOVELACE" }, 1},
		{"search by account number", func(f *model.CustomerFilter) { f.Search = "7000" }, 6},
		{"search by email", func(f *model.CustomerFilter) { f.Search = "example.com" }, 4},
		{"search by phone", func(f *model.CustomerFilter) { f.Search = "0606" }, 1},
		{"search by the first phone only", func(f *model.CustomerFilter) { f.Search = "0101" }, 1},
		{"a second phone is not searched", func(f *model.CustomerFilter) { f.Search = "0102" }, 0},
		{"search across several phones", func(f *model.CustomerFilter) { f.Search = "555-" }, 4},
		{"search matches nothing", func(f *model.CustomerFilter) { f.Search = "zzz-nothing" }, 0},
		{"percent is literal", func(f *model.CustomerFilter) { f.Search = "50%" }, 1},
		{"a lone percent matches only the name that has one", func(f *model.CustomerFilter) { f.Search = "%" }, 1},
		{"underscore is literal", func(f *model.CustomerFilter) { f.Search = "a_b" }, 1},
		{"underscore is not a wildcard", func(f *model.CustomerFilter) { f.Search = "a?b" }, 0},
		{"backslash is literal", func(f *model.CustomerFilter) { f.Search = `\` }, 0},
		{"sql injection text matches nothing", func(f *model.CustomerFilter) { f.Search = `'; DROP TABLE customers; --` }, 0},
		{"source and search together", func(f *model.CustomerFilter) {
			f.LeadSourceIDs = []int64{sourceID["Facebook"]}
			f.Search = "tie"
		}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := october()
			tt.mutate(&f)

			rows := e.listAll(t, f, model.CustomerSortCreatedAt, false)
			require.Len(t, rows, tt.count, "got %v", accounts(rows))

			summary, err := pageSummary(ctx, e.repo, f)
			require.NoError(t, err)
			require.Equal(t, model.CustomerSummary{Count: int64(tt.count)}, summary, "the summary follows the same filter")
		})
	}
}

func TestSummaryCountMatchesAnIndependentQuery(t *testing.T) {
	e := newEnv(t)
	var count int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM customers
		WHERE created_at >= '2026-10-01 00:00:00+00' AND created_at < '2026-11-01 00:00:00+00'`).Scan(&count))

	s, err := pageSummary(context.Background(), e.repo, october())

	require.NoError(t, err)
	require.EqualValues(t, count, s.Count)
	require.EqualValues(t, octoberCustomers, s.Count)
}

func TestSummaryOfNothingIsZero(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = "zzz-nothing"

	s, err := pageSummary(context.Background(), e.repo, f)

	require.NoError(t, err)
	require.Equal(t, model.CustomerSummary{}, s)
}

func TestLeadSourcesLookup(t *testing.T) {
	e := newEnv(t)

	sources, err := e.repo.LeadSources(context.Background())

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

	_, err := pageRows(ctx, e.repo, query(october(), "bogus", false, 5))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
	_, err = pageRows(ctx, e.repo, query(october(), model.CustomerSortCreatedAt, false, 0))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

// insertOctoberCustomer commits one more October customer.
func insertOctoberCustomer(t *testing.T, conn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO customers (account_number, display_name, created_at)
		VALUES ('SNAP-' || nextval(pg_get_serial_sequence('customers', 'id')), 'Snapshot Test', '2026-10-19 08:00:00+00')`)
	require.NoError(t, err)
}

func TestPage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := query(october(), model.CustomerSortCreatedAt, true, 5)

	t.Run("a first page carries the rows and the count of the same set", func(t *testing.T) {
		page, err := e.repo.Page(ctx, q, true)
		require.NoError(t, err)

		rows, err := pageRows(ctx, e.repo, q)
		require.NoError(t, err)
		summary, err := pageSummary(ctx, e.repo, q.Filter)
		require.NoError(t, err)

		require.Equal(t, rows, page.Rows)
		require.NotNil(t, page.Summary)
		require.Equal(t, summary, *page.Summary)
		require.EqualValues(t, octoberCustomers, page.Summary.Count)
		require.Len(t, page.Rows, 6, "limit+1 rows, so the caller can tell another page follows")
	})

	t.Run("without a summary it is just the rows", func(t *testing.T) {
		page, err := e.repo.Page(ctx, q, false)
		require.NoError(t, err)

		rows, err := pageRows(ctx, e.repo, q)
		require.NoError(t, err)
		require.Equal(t, rows, page.Rows)
		require.Nil(t, page.Summary)
	})

	t.Run("works on a repository bound to the caller's transaction", func(t *testing.T) {
		tx, err := e.pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		insertOctoberCustomer(t, tx)

		page, err := repository.NewCustomerRepository(tx).Page(ctx, q, true)
		require.NoError(t, err)
		require.EqualValues(t, octoberCustomers+1, page.Summary.Count, "the caller's transaction is the snapshot and sees its own row")
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

// The count must describe the rows it is shown with. A customer committed by someone else between reading the
// rows and reading the count would otherwise make the two disagree.
func TestPageReadsRowsAndSummaryFromOneSnapshot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := query(october(), model.CustomerSortCreatedAt, true, 100)

	committed := 0
	pool := testdb.SnapshotPool{Pool: e.pool, BeforeQueryRow: func() {
		insertOctoberCustomer(t, e.pool)
		committed++
	}}

	page, err := repository.NewCustomerRepository(pool).Page(ctx, q, true)
	require.NoError(t, err)

	require.Equal(t, 1, committed, "the concurrent insert must have happened between the two reads")
	require.Len(t, page.Rows, octoberCustomers)
	require.EqualValues(t, octoberCustomers, page.Summary.Count, "the count ignores the customer committed after the rows were read")

	after, err := pageSummary(ctx, e.repo, q.Filter)
	require.NoError(t, err)
	require.EqualValues(t, octoberCustomers+1, after.Count, "the committed customer is real and visible to a later read")
}

func TestCancelledContextIsReturnedAsAnError(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := pageRows(ctx, e.repo, query(october(), model.CustomerSortCreatedAt, false, 5))
	require.ErrorIs(t, err, context.Canceled)
	_, err = pageSummary(ctx, e.repo, october())
	require.ErrorIs(t, err, context.Canceled)
	_, err = e.repo.LeadSources(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// customer returns the report row of one customer, whatever its creation date.
func (e env) customer(t *testing.T, account string) model.CustomerRow {
	t.Helper()
	for _, r := range e.listAll(t, model.CustomerFilter{Search: account}, model.CustomerSortCreatedAt, false) {
		if r.AccountNumber == account {
			return r
		}
	}
	t.Fatalf("customer %s is not listed", account)
	return model.CustomerRow{}
}

func TestRowsCarryTheTopNoteAddressFirstJobAndCompletedJobs(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("every row agrees with independent SQL", func(t *testing.T) {
		for _, r := range e.listAll(t, model.CustomerFilter{}, model.CustomerSortCreatedAt, false) {
			var (
				topNote   string
				line1     *string
				firstJob  *int64
				completed int64
			)
			require.NoError(t, e.pool.QueryRow(ctx, `SELECT c.top_note,
				(SELECT address_line1 FROM service_locations WHERE customer_id = c.id ORDER BY id LIMIT 1),
				(SELECT id FROM jobs WHERE customer_id = c.id AND deleted_at IS NULL ORDER BY created_at, id LIMIT 1),
				(SELECT count(*) FROM jobs WHERE customer_id = c.id AND deleted_at IS NULL AND status = 'completed')
				FROM customers c WHERE c.id = $1`, r.ID).Scan(&topNote, &line1, &firstJob, &completed))

			require.Equal(t, topNote, r.TopNote, r.AccountNumber)
			if line1 == nil {
				require.Nil(t, r.Address, r.AccountNumber)
			} else {
				require.NotNil(t, r.Address, r.AccountNumber)
				require.Equal(t, *line1, r.Address.Line1, r.AccountNumber)
			}
			if firstJob == nil {
				require.Nil(t, r.FirstJob, r.AccountNumber)
			} else {
				require.NotNil(t, r.FirstJob, r.AccountNumber)
				require.Equal(t, *firstJob, r.FirstJob.ID, r.AccountNumber)
			}
			require.Equal(t, completed, r.CompletedJobs, r.AccountNumber)
		}
	})

	t.Run("the screenshot's customer #1", func(t *testing.T) {
		r := e.customer(t, "63749")
		require.Empty(t, r.TopNote)
		require.Equal(t, &model.LookupAddress{Line1: "1 2", City: "3", State: "4", Zip: "5"}, r.Address)
		require.NotNil(t, r.FirstJob)
		require.Equal(t, "Blank Service 123", r.FirstJob.ServiceType)
		require.Equal(t, utc(2026, 11, 2, 3, 42, 0, 0), r.FirstJob.CreatedAt)
		require.Equal(t, time.UTC, r.FirstJob.CreatedAt.Location())
		require.Zero(t, r.CompletedJobs)
	})

	t.Run("a top note, and completed jobs counted", func(t *testing.T) {
		r := e.customer(t, "63714")
		require.Equal(t, "Here's top note #F", r.TopNote)
		require.Equal(t, "S1 S2", r.Address.Line1)
		require.Equal(t, int64(1), r.CompletedJobs, "the Full service job of 10-10")
	})

	t.Run("no location and no job", func(t *testing.T) {
		r := e.customer(t, "70001")
		require.Nil(t, r.Address)
		require.Nil(t, r.FirstJob)
		require.Zero(t, r.CompletedJobs)
	})

	t.Run("a deleted job is neither the first job nor counted", func(t *testing.T) {
		before := e.customer(t, "63714")
		require.NotNil(t, before.FirstJob)
		_, err := e.pool.Exec(ctx, `UPDATE jobs SET deleted_at = now() WHERE id = $1 OR (customer_id = $2 AND status = 'completed')`,
			before.FirstJob.ID, before.ID)
		require.NoError(t, err)

		after := e.customer(t, "63714")
		if after.FirstJob != nil {
			require.NotEqual(t, before.FirstJob.ID, after.FirstJob.ID)
		}
		require.Zero(t, after.CompletedJobs)
	})
}

// pageRows reads one page of rows through Page, the repository's only public read path.
func pageRows(ctx context.Context, r *repository.CustomerRepository, q model.CustomerQuery) ([]model.CustomerRow, error) {
	page, err := r.Page(ctx, q, false)
	return page.Rows, err
}

// pageSummary reads the totals of a filtered set through Page.
func pageSummary(ctx context.Context, r *repository.CustomerRepository, f model.CustomerFilter) (model.CustomerSummary, error) {
	page, err := r.Page(ctx, model.CustomerQuery{Filter: f, Sort: model.CustomerSort{Key: model.CustomerSortCreatedAt}, Limit: 1}, true)
	if err != nil {
		return model.CustomerSummary{}, err
	}
	return *page.Summary, nil
}
