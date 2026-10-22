//go:build integration

package creditservice_test

import (
	"context"
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
	svc  *service.CreditService
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	svc := service.NewCreditService(repository.NewCreditRepository(pool), func() time.Time { return october21 })
	return env{pool: pool, svc: svc}
}

func ids(rows []model.CreditRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestListDefault(t *testing.T) {
	e := newEnv(t)
	var wantTotal int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `
		SELECT (sum(amount) * 100)::bigint FROM credits
		WHERE credit_date BETWEEN '2026-10-01' AND '2026-10-31'`).Scan(&wantTotal))

	res, err := e.svc.List(context.Background(), service.ListCreditsParams{})

	require.NoError(t, err)
	require.Len(t, res.Items, 7)
	require.Equal(t, 50, res.Limit)
	require.False(t, res.HasMore)
	require.Empty(t, res.NextCursor)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.NotNil(t, res.Summary)
	require.EqualValues(t, 7, res.Summary.Count)
	require.EqualValues(t, wantTotal, res.Summary.Total, "the total matches an independent query")
	require.Equal(t, "2026-10-21", res.Items[0].Date.Format("2006-01-02"), "newest first")
}

func TestFollowingNextCursorWalksEveryKeyAndDirectionExactly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	for _, key := range model.CreditSortKeys {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 3} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", key, order, limit), func(t *testing.T) {
					all, err := e.svc.List(ctx, service.ListCreditsParams{Sort: string(key), Order: order, Limit: "200"})
					require.NoError(t, err)
					require.Len(t, all.Items, 7)

					p := service.ListCreditsParams{Sort: string(key), Order: order, Limit: fmt.Sprint(limit)}
					var got []model.CreditRow
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
		params    service.ListCreditsParams
		wantCount int
	}{
		{"cash", service.ListCreditsParams{Method: []string{"cash"}}, 2},
		{"check or other, mixed case", service.ListCreditsParams{Method: []string{"Check", "OTHER"}}, 3},
		{"card", service.ListCreditsParams{Method: []string{"card"}}, 2},
		{"one day", service.ListCreditsParams{From: "2026-10-15", To: "2026-10-15"}, 5},
		{"the last day is included", service.ListCreditsParams{From: "2026-10-21", To: "2026-10-21"}, 1},
		{"open start", service.ListCreditsParams{To: "2026-10-12"}, 2},
		{"open end", service.ListCreditsParams{From: "2026-10-16"}, 1},
		{"september", service.ListCreditsParams{From: "2026-09-01", To: "2026-09-30"}, 1},
		{"search by customer", service.ListCreditsParams{Search: "customer 1"}, 2},
		{"search by account number", service.ListCreditsParams{Search: "63706"}, 2},
		{"search by method detail", service.ListCreditsParams{Search: "STRIPE"}, 2},
		{"search with wildcards matches nothing", service.ListCreditsParams{Search: "%_"}, 0},
		{"search with injection text matches nothing", service.ListCreditsParams{Search: `'; DROP TABLE credits; --`}, 0},
		{"combined", service.ListCreditsParams{Method: []string{"card"}, Search: "63733", From: "2026-10-15", To: "2026-10-15"}, 1},
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
				sum += r.Amount
			}
			require.Equal(t, sum, res.Summary.Total)
		})
	}
}

func TestInvalidInputIsReportedPerFieldWithoutHittingTheData(t *testing.T) {
	e := newEnv(t)

	_, err := e.svc.List(context.Background(), service.ListCreditsParams{
		Method: []string{"wire"}, Sort: "status", Limit: "0", Cursor: "garbage!",
	})

	require.ErrorIs(t, err, model.ErrInvalidInput)
	var in *model.InvalidInputError
	require.ErrorAs(t, err, &in)
	var got []string
	for _, f := range in.Fields {
		got = append(got, f.Field)
	}
	require.ElementsMatch(t, []string{"method", "sort", "limit"}, got, "the cursor is not judged against an invalid sort")
}

func TestCursorsAreBoundToTheirSortKeyAndDirection(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	first, err := e.svc.List(ctx, service.ListCreditsParams{Sort: "amount", Order: "asc", Limit: "2"})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	for _, other := range []service.ListCreditsParams{
		{Sort: "amount", Order: "desc"},
		{Sort: "credit_date", Order: "asc"},
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
			res, err := e.svc.List(context.Background(), service.ListCreditsParams{From: tt.from, To: tt.to, Limit: "5"})

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

	_, err := e.svc.List(ctx, service.ListCreditsParams{})
	require.ErrorIs(t, err, context.Canceled)
}
