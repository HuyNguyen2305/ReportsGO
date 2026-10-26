//go:build integration

package documentrepo_test

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

func utc(y int, m time.Month, d, h, min, s, ns int) time.Time {
	return time.Date(y, m, d, h, min, s, ns, time.UTC)
}

// env is a migrated, seeded throwaway database with a document repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.DocumentRepository
}

// newEnv seeds the database. With withExtras it also adds three October documents: two created at the very
// same instant, and one without a creator, to exercise ties and empty sort values.
func newEnv(t *testing.T, withExtras bool) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	if withExtras {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO documents (document_number, document_type_id, customer_id, created_by, created_at)
			SELECT 'X' || g, dt.id, c.id, NULL, '2026-10-12 08:00:00+00'
			FROM generate_series(1, 3) g
			JOIN document_types dt ON dt.name = '1-Time Treatment (PestPros of MI)'
			JOIN customers c ON c.account_number = '63716'`)
		require.NoError(t, err)
	}
	return env{pool: pool, repo: repository.NewDocumentRepository(pool)}
}

func october() model.DocumentFilter {
	return model.DocumentFilter{
		Lifecycle:     model.LifecycleActive,
		CreatedFrom:   utc(2026, 10, 1, 0, 0, 0, 0),
		CreatedBefore: utc(2026, 11, 1, 0, 0, 0, 0),
	}
}

func query(f model.DocumentFilter, key model.DocumentSortKey, desc bool, limit int) model.DocumentQuery {
	return model.DocumentQuery{Filter: f, Sort: model.DocumentSort{Key: key, Desc: desc}, Limit: limit}
}

func listAll(t *testing.T, repo *repository.DocumentRepository, f model.DocumentFilter, key model.DocumentSortKey, desc bool) []model.DocumentRow {
	t.Helper()
	rows, err := repo.List(context.Background(), query(f, key, desc, 1000))
	require.NoError(t, err)
	return rows
}

func (e env) listAll(t *testing.T, f model.DocumentFilter, key model.DocumentSortKey, desc bool) []model.DocumentRow {
	t.Helper()
	return listAll(t, e.repo, f, key, desc)
}

func numbers(rows []model.DocumentRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.DocumentNumber
	}
	return out
}

func ids(rows []model.DocumentRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// the 18 active October documents in the seed: 15 drafts, one sent, two signed (5900 is deleted).
var octoberActiveNumbers = []string{
	"5979", "5978", "5977", "5972", "5957", "5936", "5931", "5924", "5923", "5910", "5859", "5805", "5751",
	"5746", "5743", "5980", "5981", "5982",
}

func TestListDefaultReturnsActiveOctoberDocumentsNewestFirst(t *testing.T) {
	e := newEnv(t, false)

	rows := e.listAll(t, october(), model.DocumentSortCreatedAt, true)

	require.ElementsMatch(t, octoberActiveNumbers, numbers(rows))
	require.Equal(t, "5982", rows[0].DocumentNumber, "10/22 is the newest")
	for i := 1; i < len(rows); i++ {
		require.False(t, rows[i].CreatedAt.After(rows[i-1].CreatedAt), "timestamps must not increase")
	}
}

func TestListReturnsAllReportFields(t *testing.T) {
	e := newEnv(t, false)
	f := october()
	f.Search = "5982"

	rows := e.listAll(t, f, model.DocumentSortCreatedAt, true)

	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "5982", r.DocumentNumber)
	require.NotZero(t, r.TypeID)
	require.Equal(t, "1-Time Treatment (PestPros of MI)", r.TypeName)
	require.NotZero(t, r.CustomerID)
	require.Equal(t, "Customer 1", r.CustomerName)
	require.Equal(t, "63733", r.AccountNumber)
	require.NotNil(t, r.CreatedBy)
	require.Equal(t, "Thọ Nguyễn Quang", *r.CreatedBy)
	require.Equal(t, utc(2026, 10, 22, 8, 15, 30, 123456000), r.CreatedAt, "microseconds survive, in UTC")
	require.Equal(t, time.UTC, r.CreatedAt.Location())
	require.Equal(t, model.DocumentSigned, r.Status)
	require.Equal(t, "2026-10-22T08:15:30.123456Z", r.SortValue)
}

func TestListHandlesMissingCreator(t *testing.T) {
	e := newEnv(t, true)
	f := october()
	f.Search = "X2"

	rows := e.listAll(t, f, model.DocumentSortCreatedAt, true)

	require.Len(t, rows, 1)
	require.Nil(t, rows[0].CreatedBy)
}

var sortCast = map[model.DocumentSortKey]string{
	model.DocumentSortNumber: "text", model.DocumentSortType: "text", model.DocumentSortAccountNumber: "text",
	model.DocumentSortCustomer: "text", model.DocumentSortCreatedBy: "text", model.DocumentSortCreatedAt: "timestamptz",
	model.DocumentSortStatus: "text",
}

// compare asks Postgres to order two sort values, so text keys use the same collation as ORDER BY.
func (e env) compare(t *testing.T, key model.DocumentSortKey, a, b string) int {
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
	require.Len(t, sortCast, len(model.DocumentSortKeys), "every sort key must be covered")

	for _, key := range model.DocumentSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				rows := e.listAll(t, october(), key, desc)
				require.Len(t, rows, 21)

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
				case model.DocumentSortCreatedAt, model.DocumentSortStatus, model.DocumentSortType, model.DocumentSortCustomer:
					require.Positive(t, ties, "this key should have tied values in the data, or the tie-break goes untested")
				}
			})
		}
	}
}

func walk(t *testing.T, repo *repository.DocumentRepository, key model.DocumentSortKey, desc bool, limit int) []int64 {
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
		q.After = &model.DocumentCursor{SortValue: last.SortValue, ID: last.ID}
	}
}

func TestKeysetPagingWalksEveryKeyAndDirectionWithoutGapsOrDuplicates(t *testing.T) {
	e := newEnv(t, true)

	for _, key := range model.DocumentSortKeys {
		for _, desc := range []bool{false, true} {
			for _, limit := range []int{1, 5} {
				t.Run(fmt.Sprintf("%s/desc=%v/limit=%d", key, desc, limit), func(t *testing.T) {
					want := ids(e.listAll(t, october(), key, desc))

					got := walk(t, e.repo, key, desc, limit)

					require.Equal(t, want, got, "walking the pages must reproduce the full ordered list exactly")
					require.Len(t, got, 21)
				})
			}
		}
	}
}

// A cursor's timestamp must mean the same instant whatever time zone the database session uses.
func TestCreatedAtCursorsAreIndependentOfTheSessionTimeZone(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	want := ids(e.listAll(t, october(), model.DocumentSortCreatedAt, true))

	for _, zone := range []string{"UTC", "Asia/Tokyo", "America/Los_Angeles", "Pacific/Kiritimati"} {
		t.Run(zone, func(t *testing.T) {
			conn, err := e.pool.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()
			_, err = conn.Exec(ctx, "SET TIME ZONE '"+zone+"'")
			require.NoError(t, err)

			repo := repository.NewDocumentRepository(conn.Conn())
			for _, limit := range []int{1, 4} {
				require.Equal(t, want, walk(t, repo, model.DocumentSortCreatedAt, true, limit), "limit %d", limit)
			}
			rows := listAll(t, repo, october(), model.DocumentSortCreatedAt, true)
			require.Equal(t, "2026-10-22T08:15:30.123456Z", rows[0].SortValue)
		})
	}
}

func TestListFilters(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	types, err := e.repo.DocumentTypes(ctx)
	require.NoError(t, err)
	typeID := map[string]int64{}
	for _, ty := range types {
		typeID[ty.Name] = ty.ID
	}
	treatment := typeID["1-Time Treatment (PestPros of MI)"]
	subTermite := typeID["1 Year SubTermite Agreement (LibertyPS)"]
	warranty := typeID["1 Year Termite Warranty (Spidermen)"]
	require.NotZero(t, treatment)

	tests := []struct {
		name   string
		mutate func(*model.DocumentFilter)
		want   []string // nil means "assert on count only"
		count  int
	}{
		{"sent", func(f *model.DocumentFilter) { f.Statuses = []model.DocumentStatus{model.DocumentSent} }, []string{"5980"}, 1},
		{"signed", func(f *model.DocumentFilter) { f.Statuses = []model.DocumentStatus{model.DocumentSigned} }, []string{"5981", "5982"}, 2},
		{"sent or signed", func(f *model.DocumentFilter) {
			f.Statuses = []model.DocumentStatus{model.DocumentSent, model.DocumentSigned}
		}, []string{"5980", "5981", "5982"}, 3},
		{"draft", func(f *model.DocumentFilter) { f.Statuses = []model.DocumentStatus{model.DocumentDraft} }, nil, 15},
		{"one type", func(f *model.DocumentFilter) { f.TypeIDs = []int64{warranty} }, []string{"5910", "5980"}, 2},
		{"two types", func(f *model.DocumentFilter) { f.TypeIDs = []int64{warranty, subTermite} }, nil, 8},
		{"every type", func(f *model.DocumentFilter) { f.TypeIDs = []int64{treatment, warranty, subTermite} }, nil, 18},
		{"unknown type id", func(f *model.DocumentFilter) { f.TypeIDs = []int64{999999} }, []string{}, 0},
		{"type and status", func(f *model.DocumentFilter) {
			f.TypeIDs = []int64{treatment}
			f.Statuses = []model.DocumentStatus{model.DocumentSigned}
		}, []string{"5982"}, 1},
		{"deleted tab", func(f *model.DocumentFilter) { f.Lifecycle = model.LifecycleDeleted }, []string{"5900"}, 1},
		{"one day", func(f *model.DocumentFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 21, 0, 0, 0, 0), utc(2026, 10, 22, 0, 0, 0, 0)
		}, []string{"5980", "5981"}, 2},
		{"open start", func(f *model.DocumentFilter) {
			f.CreatedFrom, f.CreatedBefore = time.Time{}, utc(2026, 10, 6, 0, 0, 0, 0)
		}, []string{"5751", "5746", "5743"}, 3},
		{"open end", func(f *model.DocumentFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2026, 10, 21, 0, 0, 0, 0), time.Time{}
		}, []string{"5980", "5981", "5982"}, 3},
		{"empty range", func(f *model.DocumentFilter) {
			f.CreatedFrom, f.CreatedBefore = utc(2027, 1, 1, 0, 0, 0, 0), utc(2027, 2, 1, 0, 0, 0, 0)
		}, []string{}, 0},
		{"search by number", func(f *model.DocumentFilter) { f.Search = "598" }, []string{"5980", "5981", "5982"}, 3},
		{"search by account number", func(f *model.DocumentFilter) { f.Search = "63733" }, []string{"5931", "5978", "5979", "5982"}, 4},
		{"search by customer name", func(f *model.DocumentFilter) { f.Search = "customer 1" }, []string{"5931", "5978", "5979", "5982"}, 4},
		{"search matches nothing", func(f *model.DocumentFilter) { f.Search = "zzz-nothing" }, []string{}, 0},
		{"percent is literal", func(f *model.DocumentFilter) { f.Search = "%" }, []string{}, 0},
		{"underscore is literal", func(f *model.DocumentFilter) { f.Search = "59_9" }, []string{}, 0},
		{"backslash is literal", func(f *model.DocumentFilter) { f.Search = `\` }, []string{}, 0},
		{"sql injection text matches nothing", func(f *model.DocumentFilter) { f.Search = `'; DROP TABLE documents; --` }, []string{}, 0},
		{"filters combine", func(f *model.DocumentFilter) {
			f.Statuses = []model.DocumentStatus{model.DocumentSigned}
			f.Search = "customer"
			f.CreatedFrom = utc(2026, 10, 22, 0, 0, 0, 0)
		}, []string{"5982"}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := october()
			tt.mutate(&f)

			rows := e.listAll(t, f, model.DocumentSortNumber, false)
			require.Len(t, rows, tt.count)
			if tt.want != nil {
				require.ElementsMatch(t, tt.want, numbers(rows))
			}

			summary, err := e.repo.Summary(ctx, f)
			require.NoError(t, err)
			require.EqualValues(t, tt.count, summary.Count, "the summary follows the same filter")
		})
	}

	// the database is still intact after the hostile search above
	var n int
	require.NoError(t, e.pool.QueryRow(ctx, "SELECT count(*) FROM documents").Scan(&n))
	require.Equal(t, 19, n)
}

func TestTheCreatedRangeBoundariesAreHalfOpenInUTC(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	_, err := e.pool.Exec(ctx, `
		INSERT INTO documents (document_number, document_type_id, customer_id, created_by, created_at)
		SELECT v.num, dt.id, c.id, NULL, v.at::timestamptz
		FROM (VALUES ('B1', '2026-10-31 23:59:59.999999+00'), ('B2', '2026-11-01 00:00:00+00'),
		             ('B3', '2026-11-01 06:59:59+07'), ('B4', '2026-11-01 07:00:00+07')) AS v(num, at)
		JOIN document_types dt ON dt.name = '1-Time Treatment (PestPros of MI)'
		JOIN customers c ON c.account_number = '63716'`)
	require.NoError(t, err)

	f := model.DocumentFilter{
		Lifecycle:   model.LifecycleActive,
		CreatedFrom: utc(2026, 10, 31, 0, 0, 0, 0), CreatedBefore: utc(2026, 11, 1, 0, 0, 0, 0),
		Search: "B",
	}
	rows := e.listAll(t, f, model.DocumentSortNumber, false)

	// B1 and B3 (23:59:59 UTC on Oct 31) are in; B2 and B4 (exactly midnight UTC) belong to November.
	require.Equal(t, []string{"B1", "B3"}, numbers(rows))
}

func TestSummary(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	t.Run("counts the whole filtered set", func(t *testing.T) {
		s, err := e.repo.Summary(ctx, october())
		require.NoError(t, err)
		require.EqualValues(t, 18, s.Count)
		require.EqualValues(t, len(e.listAll(t, october(), model.DocumentSortNumber, false)), s.Count)
	})

	t.Run("deleted tab", func(t *testing.T) {
		f := october()
		f.Lifecycle = model.LifecycleDeleted
		s, err := e.repo.Summary(ctx, f)
		require.NoError(t, err)
		require.EqualValues(t, 1, s.Count)
	})

	t.Run("an empty set counts zero", func(t *testing.T) {
		f := october()
		f.Search = "zzz-nothing"
		s, err := e.repo.Summary(ctx, f)
		require.NoError(t, err)
		require.Equal(t, model.DocumentSummary{}, s)
	})
}

func TestDocumentTypes(t *testing.T) {
	e := newEnv(t, false)

	types, err := e.repo.DocumentTypes(context.Background())

	require.NoError(t, err)
	names := make([]string, len(types))
	for i, ty := range types {
		require.NotZero(t, ty.ID)
		names[i] = ty.Name
	}
	// the order is the database's own collation order by name, not byte order
	rows, err := e.pool.Query(context.Background(), `SELECT name FROM document_types ORDER BY name, id`)
	require.NoError(t, err)
	defer rows.Close()
	var want []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		want = append(want, name)
	}
	require.NoError(t, rows.Err())
	require.Len(t, want, 3)
	require.Equal(t, want, names, "ordered by name")
}

func TestRepositoryWorksInsideATransaction(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	tx, err := e.pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `INSERT INTO documents (document_number, document_type_id, customer_id, created_at)
		SELECT 'TX1', dt.id, c.id, '2026-10-15 00:00:00+00'
		FROM document_types dt, customers c WHERE c.account_number = '5001' LIMIT 1`)
	require.NoError(t, err)

	inTx, err := repository.NewDocumentRepository(tx).Summary(ctx, october())
	require.NoError(t, err)
	outside, err := e.repo.Summary(ctx, october())
	require.NoError(t, err)

	require.EqualValues(t, 19, inTx.Count, "the transaction sees its own uncommitted row")
	require.EqualValues(t, 18, outside.Count, "other connections do not")

	rows, err := repository.NewDocumentRepository(tx).List(ctx, query(october(), model.DocumentSortNumber, false, 100))
	require.NoError(t, err)
	require.Contains(t, numbers(rows), "TX1")
}

func TestPage(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	q := query(october(), model.DocumentSortCreatedAt, true, 5)

	t.Run("a first page carries the rows and the count of the same set", func(t *testing.T) {
		page, err := e.repo.Page(ctx, q, true)
		require.NoError(t, err)

		rows, err := e.repo.List(ctx, q)
		require.NoError(t, err)
		summary, err := e.repo.Summary(ctx, q.Filter)
		require.NoError(t, err)

		require.Equal(t, rows, page.Rows)
		require.NotNil(t, page.Summary)
		require.Equal(t, summary, *page.Summary)
		require.EqualValues(t, 21, page.Summary.Count, "18 seeded October documents plus 3 added by newEnv")
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

		_, err = tx.Exec(ctx, `INSERT INTO documents (document_number, document_type_id, customer_id, created_at)
			SELECT 'TXP1', dt.id, c.id, '2026-10-15 00:00:00+00'
			FROM document_types dt, customers c WHERE c.account_number = '5001' LIMIT 1`)
		require.NoError(t, err)

		page, err := repository.NewDocumentRepository(tx).Page(ctx, q, true)
		require.NoError(t, err)
		require.EqualValues(t, 22, page.Summary.Count, "the caller's transaction is the snapshot and sees its own row")
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

// The count must describe the rows it is shown with. A change committed by someone else between reading the
// rows and reading the count would otherwise make the two disagree.
func TestPageReadsRowsAndSummaryFromOneSnapshot(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	q := query(october(), model.DocumentSortCreatedAt, true, 100)

	committed := 0
	pool := testdb.SnapshotPool{Pool: e.pool, BeforeQueryRow: func() {
		_, err := e.pool.Exec(ctx, `INSERT INTO documents (document_number, document_type_id, customer_id, created_at)
			SELECT 'SNAP1', dt.id, c.id, '2026-10-15 00:00:00+00'
			FROM document_types dt, customers c WHERE c.account_number = '5001' LIMIT 1`)
		require.NoError(t, err)
		committed++
	}}

	page, err := repository.NewDocumentRepository(pool).Page(ctx, q, true)
	require.NoError(t, err)

	require.Equal(t, 1, committed, "the concurrent insert must have happened between the two reads")
	require.Len(t, page.Rows, 18)
	require.EqualValues(t, 18, page.Summary.Count, "the count ignores the document committed after the rows were read")

	after, err := e.repo.Summary(ctx, q.Filter)
	require.NoError(t, err)
	require.EqualValues(t, 19, after.Count, "the committed document is real and visible to a later read")
}

func TestQueriesHonorContextCancellation(t *testing.T) {
	e := newEnv(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.repo.List(ctx, query(october(), model.DocumentSortCreatedAt, true, 10))
	require.ErrorIs(t, err, context.Canceled)

	_, err = e.repo.Summary(ctx, october())
	require.ErrorIs(t, err, context.Canceled)

	_, err = e.repo.DocumentTypes(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestInvalidQueriesAreRejectedBeforeReachingTheDatabase(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	_, err := e.repo.List(ctx, query(october(), "nope", false, 10))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	_, err = e.repo.List(ctx, query(october(), model.DocumentSortCreatedAt, false, 0))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	bad := october()
	bad.Lifecycle = model.LifecycleArchived
	_, err = e.repo.Summary(ctx, bad)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

func TestConcurrentCallsAgree(t *testing.T) {
	e := newEnv(t, false)

	var wg sync.WaitGroup
	counts := make([]int64, 4)
	errs := make([]error, 4)
	for i := range counts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := e.repo.Summary(context.Background(), october())
			counts[i], errs[i] = s.Count, err
		}()
	}
	wg.Wait()

	for i := range counts {
		require.NoError(t, errs[i])
		require.EqualValues(t, 18, counts[i])
	}
}
