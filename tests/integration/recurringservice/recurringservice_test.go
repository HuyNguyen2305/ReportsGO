//go:build integration

package recurringservice_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

type env struct {
	pool *pgxpool.Pool
	svc  *service.RecurringService
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, svc: service.NewRecurringService(repository.NewRecurringRepository(pool))}
}

func ids(rows []model.RecurringRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestListDefault(t *testing.T) {
	e := newEnv(t)
	var wantTotal int64
	require.NoError(t, e.pool.QueryRow(context.Background(),
		`SELECT (sum(total_value) * 100)::bigint FROM recurring_invoices`).Scan(&wantTotal))

	res, err := e.svc.List(context.Background(), service.ListRecurringParams{})

	require.NoError(t, err)
	require.Len(t, res.Items, 7, "there is no default date range")
	require.Equal(t, 50, res.Limit)
	require.False(t, res.HasMore)
	require.Empty(t, res.NextCursor)
	require.True(t, res.From.IsZero())
	require.True(t, res.To.IsZero())
	require.NotNil(t, res.Summary)
	require.EqualValues(t, 7, res.Summary.Count)
	require.EqualValues(t, wantTotal, res.Summary.Total, "the total matches an independent query")
	require.Equal(t, "2026-10-15", res.Items[0].StartedOn.Format("2006-01-02"), "newest start first")
}

func TestFollowingNextCursorWalksEveryKeyAndDirectionExactly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	for _, key := range model.RecurringSortKeys {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 3} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", key, order, limit), func(t *testing.T) {
					all, err := e.svc.List(ctx, service.ListRecurringParams{Sort: string(key), Order: order, Limit: "200"})
					require.NoError(t, err)
					require.Len(t, all.Items, 7)

					p := service.ListRecurringParams{Sort: string(key), Order: order, Limit: fmt.Sprint(limit)}
					var got []model.RecurringRow
					for page := 0; ; page++ {
						require.Less(t, page, 100, "paging must terminate")

						res, err := e.svc.List(ctx, p)
						require.NoError(t, err)
						require.Equal(t, limit, res.Limit)
						if page == 0 {
							require.NotNil(t, res.Summary, "the first page carries the count")
							require.EqualValues(t, 7, res.Summary.Count)
							require.Equal(t, all.Summary.Total, res.Summary.Total)
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

					require.Equal(t, ids(all.Items), ids(got), "pages concatenate to the full ordered list")
				})
			}
		}
	}
}

func TestFiltersAgreeWithTheCountAndTotal(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	tests := []struct {
		name      string
		params    service.ListRecurringParams
		wantCount int
	}{
		{"active", service.ListRecurringParams{Status: []string{"active"}}, 4},
		{"paused or failed, mixed case", service.ListRecurringParams{Status: []string{"Paused", "FAILED"}}, 2},
		{"ended", service.ListRecurringParams{Status: []string{"ended"}}, 1},
		{"create only", service.ListRecurringParams{Action: []string{"create_only"}}, 2},
		{"send by email", service.ListRecurringParams{Action: []string{"send_by_email"}}, 5},
		{"active and send by email", service.ListRecurringParams{Status: []string{"active"}, Action: []string{"send_by_email"}}, 3},
		{"one start date", service.ListRecurringParams{From: "2026-09-14", To: "2026-09-14"}, 2},
		{"the last day is included", service.ListRecurringParams{From: "2026-10-15", To: "2026-10-15"}, 1},
		{"open start", service.ListRecurringParams{To: "2026-08-31"}, 3},
		{"open end", service.ListRecurringParams{From: "2026-09-14"}, 4},
		{"empty range", service.ListRecurringParams{From: "2030-01-01", To: "2030-01-31"}, 0},
		{"search by next invoice number", service.ListRecurringParams{Search: "85032"}, 1},
		{"search by account number", service.ListRecurringParams{Search: "63733"}, 2},
		{"search with wildcards matches nothing", service.ListRecurringParams{Search: "%_"}, 0},
		{"search with injection text matches nothing", service.ListRecurringParams{Search: `'; DROP TABLE recurring_invoices; --`}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.params.Limit = "200"
			res, err := e.svc.List(ctx, tt.params)

			require.NoError(t, err)
			require.Len(t, res.Items, tt.wantCount)
			require.NotNil(t, res.Summary)
			require.EqualValues(t, tt.wantCount, res.Summary.Count)

			var sum model.Money
			for _, r := range res.Items {
				sum += r.TotalValue
			}
			require.Equal(t, sum, res.Summary.Total)
		})
	}
}

func TestInvalidInputIsReportedPerFieldWithoutHittingTheData(t *testing.T) {
	e := newEnv(t)

	_, err := e.svc.List(context.Background(), service.ListRecurringParams{
		Status: []string{"paid"}, Action: []string{"card"}, Sort: "credit_date", Limit: "0", Cursor: "garbage!",
	})

	require.ErrorIs(t, err, model.ErrInvalidInput)
	var in *model.InvalidInputError
	require.ErrorAs(t, err, &in)
	var got []string
	for _, f := range in.Fields {
		got = append(got, f.Field)
	}
	require.ElementsMatch(t, []string{"status", "action", "sort", "limit"}, got, "the cursor is not judged against an invalid sort")
}

func TestCursorsAreBoundToTheirSortKeyAndDirection(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	first, err := e.svc.List(ctx, service.ListRecurringParams{Sort: "ends_on", Order: "asc", Limit: "2"})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	for _, other := range []service.ListRecurringParams{
		{Sort: "ends_on", Order: "desc"},
		{Sort: "started_on", Order: "asc"},
	} {
		other.Limit, other.Cursor = "2", first.NextCursor
		_, err = e.svc.List(ctx, other)
		require.ErrorIs(t, err, model.ErrInvalidInput, "%+v", other)
	}
}

// Extreme but well-formed dates must give an answer or a field error, never a server error.
func TestExtremeDatesNeverCauseAServerError(t *testing.T) {
	e := newEnv(t)

	tests := []struct{ name, from, to string }{
		{"year 1", "0001-01-01", "0001-01-01"},
		{"year 9999", "9999-12-31", "9999-12-31"},
		{"the whole range", "0001-01-01", "9999-12-31"},
		{"year 0", "0000-01-01", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := e.svc.List(context.Background(), service.ListRecurringParams{From: tt.from, To: tt.to, Limit: "5"})

			if err != nil {
				require.ErrorIs(t, err, model.ErrInvalidInput, "a database failure here would be a 500 for the client")
				return
			}
			require.NotNil(t, res.Summary)
		})
	}
}

func TestListHonorsContextCancellation(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.svc.List(ctx, service.ListRecurringParams{})
	require.ErrorIs(t, err, context.Canceled)
}
