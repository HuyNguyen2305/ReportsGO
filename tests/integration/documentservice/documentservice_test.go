//go:build integration

package documentservice_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

type env struct {
	pool *pgxpool.Pool
	svc  *service.DocumentService
}

func newEnv(t *testing.T, withExtras bool) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	if withExtras {
		// three documents created at the same instant and without a creator: ties and empty sort values
		_, err := pool.Exec(context.Background(), `
			INSERT INTO documents (document_number, document_type_id, customer_id, created_by, created_at)
			SELECT 'X' || g, dt.id, c.id, NULL, '2026-10-12 08:00:00+00'
			FROM generate_series(1, 3) g
			JOIN document_types dt ON dt.name = '1-Time Treatment (PestPros of MI)'
			JOIN customers c ON c.account_number = '63716'`)
		require.NoError(t, err)
	}
	svc := service.NewDocumentService(repository.NewDocumentRepository(pool), func() time.Time { return october21 })
	return env{pool: pool, svc: svc}
}

func numbers(rows []model.DocumentRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.DocumentNumber
	}
	return out
}

func TestListDefault(t *testing.T) {
	e := newEnv(t, false)

	res, err := e.svc.List(context.Background(), service.ListDocumentsParams{})

	require.NoError(t, err)
	require.Len(t, res.Items, 18)
	require.Equal(t, 50, res.Limit)
	require.False(t, res.HasMore)
	require.Empty(t, res.NextCursor)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.NotNil(t, res.Summary)
	require.EqualValues(t, 18, res.Summary.Count)
	require.Equal(t, "5982", res.Items[0].DocumentNumber, "newest first")
}

func TestFollowingNextCursorWalksEveryKeyAndDirectionExactly(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()

	for _, key := range model.DocumentSortKeys {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 5} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", key, order, limit), func(t *testing.T) {
					all, err := e.svc.List(ctx, service.ListDocumentsParams{Sort: string(key), Order: order, Limit: "200"})
					require.NoError(t, err)
					require.Len(t, all.Items, 21)

					p := service.ListDocumentsParams{Sort: string(key), Order: order, Limit: fmt.Sprint(limit)}
					var got []model.DocumentRow
					for page := 0; ; page++ {
						require.Less(t, page, 100, "paging must terminate")

						res, err := e.svc.List(ctx, p)
						require.NoError(t, err)
						require.Equal(t, limit, res.Limit)
						if page == 0 {
							require.NotNil(t, res.Summary, "the first page carries the count")
							require.EqualValues(t, 21, res.Summary.Count)
						} else {
							require.Nil(t, res.Summary, "later pages do not")
						}
						got = append(got, res.Items...)
						if !res.HasMore {
							require.Empty(t, res.NextCursor)
							break
						}
						require.NotEmpty(t, res.NextCursor)
						p.Cursor = res.NextCursor
					}

					require.Equal(t, numbers(all.Items), numbers(got), "pages concatenate to the full ordered list")
				})
			}
		}
	}
}

func TestMicrosecondTimestampsSurviveTheCursorRoundTrip(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	// 5982 is created at 08:15:30.123456; a page boundary right after it must not lose or repeat it.
	first, err := e.svc.List(ctx, service.ListDocumentsParams{Limit: "1"})
	require.NoError(t, err)
	require.Equal(t, "5982", first.Items[0].DocumentNumber)
	require.True(t, first.HasMore)

	second, err := e.svc.List(ctx, service.ListDocumentsParams{Limit: "1", Cursor: first.NextCursor})
	require.NoError(t, err)
	require.NotEqual(t, "5982", second.Items[0].DocumentNumber, "the document before the boundary is not repeated")
}

func TestFiltersAgreeWithTheCount(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	opts, err := e.svc.FilterOptions(ctx)
	require.NoError(t, err)
	typeID := map[string]string{}
	for _, ty := range opts.Types {
		typeID[ty.Name] = fmt.Sprint(ty.ID)
	}

	tests := []struct {
		name      string
		params    service.ListDocumentsParams
		wantCount int
	}{
		{"sent", service.ListDocumentsParams{Status: []string{"sent"}}, 1},
		{"signed or sent, mixed case", service.ListDocumentsParams{Status: []string{"Signed", "SENT"}}, 3},
		{"one type", service.ListDocumentsParams{DocumentTypeIDs: []string{typeID["1 Year Termite Warranty (Spidermen)"]}}, 2},
		{"two types", service.ListDocumentsParams{DocumentTypeIDs: []string{
			typeID["1 Year Termite Warranty (Spidermen)"], typeID["1 Year SubTermite Agreement (LibertyPS)"]}}, 8},
		{"deleted tab", service.ListDocumentsParams{Lifecycle: "deleted"}, 1},
		{"one day", service.ListDocumentsParams{From: "2026-10-21", To: "2026-10-21"}, 2},
		{"the last day is included", service.ListDocumentsParams{From: "2026-10-22", To: "2026-10-22"}, 1},
		{"open start", service.ListDocumentsParams{To: "2026-10-05"}, 3},
		{"open end", service.ListDocumentsParams{From: "2026-10-21"}, 3},
		{"september is empty", service.ListDocumentsParams{From: "2026-09-01", To: "2026-09-30"}, 0},
		{"search by number", service.ListDocumentsParams{Search: "598"}, 3},
		{"search by customer", service.ListDocumentsParams{Search: "customer 1"}, 4},
		{"search with wildcards matches nothing", service.ListDocumentsParams{Search: "%_"}, 0},
		{"search with injection text matches nothing", service.ListDocumentsParams{Search: `'; DROP TABLE documents; --`}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.params.Limit = "200"
			res, err := e.svc.List(ctx, tt.params)

			require.NoError(t, err)
			require.Len(t, res.Items, tt.wantCount)
			require.NotNil(t, res.Summary)
			require.EqualValues(t, tt.wantCount, res.Summary.Count)
		})
	}
}

func TestFilterOptionsListTheSeededTypes(t *testing.T) {
	e := newEnv(t, false)

	opts, err := e.svc.FilterOptions(context.Background())

	require.NoError(t, err)
	require.Len(t, opts.Types, 3)
	for _, ty := range opts.Types {
		require.NotZero(t, ty.ID)
		require.NotEmpty(t, ty.Name)
	}
}

func TestInvalidInputIsReportedPerFieldWithoutHittingTheData(t *testing.T) {
	e := newEnv(t, false)

	_, err := e.svc.List(context.Background(), service.ListDocumentsParams{
		Status: []string{"paid"}, Lifecycle: "archived", Sort: "discount", Limit: "0", Cursor: "garbage!",
	})

	require.ErrorIs(t, err, model.ErrInvalidInput)
	var in *model.InvalidInputError
	require.ErrorAs(t, err, &in)
	var got []string
	for _, f := range in.Fields {
		got = append(got, f.Field)
	}
	require.ElementsMatch(t, []string{"status", "lifecycle", "sort", "limit"}, got, "the cursor is not judged against an invalid sort")
}

func TestCursorsAreBoundToTheirSortKeyAndDirection(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	first, err := e.svc.List(ctx, service.ListDocumentsParams{Sort: "status", Order: "asc", Limit: "2"})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	for _, other := range []service.ListDocumentsParams{
		{Sort: "status", Order: "desc"},
		{Sort: "type", Order: "asc"},
	} {
		other.Limit, other.Cursor = "2", first.NextCursor
		_, err = e.svc.List(ctx, other)
		require.ErrorIs(t, err, model.ErrInvalidInput, "%+v", other)
	}

	estimate := service.EncodeEstimateCursor(model.EstimateSort{Key: model.EstimateSortDate},
		model.EstimateRow{ID: 1, SortValue: "2026-10-01"})
	_, err = e.svc.List(ctx, service.ListDocumentsParams{Sort: "created_at", Order: "asc", Cursor: estimate})
	require.ErrorIs(t, err, model.ErrInvalidInput, "an estimate cursor means nothing here")
}

// Extreme but well-formed dates must give an answer or a field error, never a server error.
func TestExtremeDatesNeverCauseAServerError(t *testing.T) {
	e := newEnv(t, false)

	tests := []struct{ name, from, to string }{
		{"year 1", "0001-01-01", "0001-01-01"},
		{"year 9999", "9999-12-31", "9999-12-31"},
		{"the whole range", "0001-01-01", "9999-12-31"},
		{"year 0", "0000-01-01", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := e.svc.List(context.Background(), service.ListDocumentsParams{From: tt.from, To: tt.to, Limit: "5"})

			if err != nil {
				require.ErrorIs(t, err, model.ErrInvalidInput, "a database failure here would be a 500 for the client")
				return
			}
			require.NotNil(t, res.Summary)
		})
	}

	t.Run("the whole range returns everything", func(t *testing.T) {
		res, err := e.svc.List(context.Background(), service.ListDocumentsParams{From: "0001-01-01", To: "9999-12-31", Limit: "200"})
		if err != nil {
			t.Skipf("rejected as invalid input: %v", errors.Unwrap(err))
		}
		require.EqualValues(t, 18, res.Summary.Count)
	})
}

func TestListHonorsContextCancellation(t *testing.T) {
	e := newEnv(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.svc.List(ctx, service.ListDocumentsParams{})
	require.ErrorIs(t, err, context.Canceled)

	_, err = e.svc.FilterOptions(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestDocumentListNeverContainsOtherReportsRows(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	// An invoice and an estimate that share a document's number must not show up here.
	_, err := e.pool.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total)
		SELECT t.kind::document_parent_type, '5982', c.id, '2026-10-22', 1 FROM customers c, (VALUES ('invoice'), ('estimate')) t(kind)
		WHERE c.account_number = '63733'`)
	require.NoError(t, err)

	res, err := e.svc.List(ctx, service.ListDocumentsParams{Search: "5982"})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.EqualValues(t, 1, res.Summary.Count)
}
