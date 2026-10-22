//go:build integration

package paymentservice_test

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
	svc  *service.PaymentService
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	svc := service.NewPaymentService(repository.NewPaymentRepository(pool), func() time.Time { return october21 })
	return env{pool: pool, svc: svc}
}

func ids(rows []model.PaymentRow) []int64 {
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
		SELECT (sum(amount) * 100)::bigint FROM payments
		WHERE deleted_at IS NULL AND payment_date BETWEEN '2026-10-01' AND '2026-10-31'`).Scan(&wantTotal))

	res, err := e.svc.List(context.Background(), service.ListPaymentsParams{})

	require.NoError(t, err)
	require.Len(t, res.Items, 15)
	require.Equal(t, 50, res.Limit)
	require.False(t, res.HasMore)
	require.Empty(t, res.NextCursor)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.NotNil(t, res.Summary)
	require.EqualValues(t, 15, res.Summary.Count)
	require.EqualValues(t, wantTotal, res.Summary.Total, "the total matches an independent query")
	require.Equal(t, "2026-10-21", res.Items[0].Date.Format("2006-01-02"), "newest first")
}

func TestFollowingNextCursorWalksEveryKeyAndDirectionExactly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	for _, key := range model.PaymentSortKeys {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 4} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", key, order, limit), func(t *testing.T) {
					all, err := e.svc.List(ctx, service.ListPaymentsParams{Sort: string(key), Order: order, Limit: "200"})
					require.NoError(t, err)
					require.Len(t, all.Items, 15)

					p := service.ListPaymentsParams{Sort: string(key), Order: order, Limit: fmt.Sprint(limit)}
					var got []model.PaymentRow
					for page := 0; ; page++ {
						require.Less(t, page, 100, "paging must terminate")

						res, err := e.svc.List(ctx, p)
						require.NoError(t, err)
						require.Equal(t, limit, res.Limit)
						if page == 0 {
							require.NotNil(t, res.Summary, "the first page carries the count")
							require.EqualValues(t, 15, res.Summary.Count)
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
		params    service.ListPaymentsParams
		wantCount int
	}{
		{"paid", service.ListPaymentsParams{Status: []string{"paid"}}, 12},
		{"failed or refunded, mixed case", service.ListPaymentsParams{Status: []string{"Failed", "REFUNDED"}}, 2},
		{"card", service.ListPaymentsParams{Method: []string{"card"}}, 7},
		{"check or other", service.ListPaymentsParams{Method: []string{"check", "other"}}, 7},
		{"card and pending", service.ListPaymentsParams{Method: []string{"card"}, Status: []string{"pending"}}, 0},
		{"deleted tab", service.ListPaymentsParams{Lifecycle: "deleted"}, 1},
		{"one day", service.ListPaymentsParams{From: "2026-10-15", To: "2026-10-15"}, 8},
		{"the last day is included", service.ListPaymentsParams{From: "2026-10-21", To: "2026-10-21"}, 1},
		{"open start", service.ListPaymentsParams{To: "2026-10-10"}, 3},
		{"open end", service.ListPaymentsParams{From: "2026-10-16"}, 2},
		{"september", service.ListPaymentsParams{From: "2026-09-01", To: "2026-09-30"}, 1},
		{"search by invoice number", service.ListPaymentsParams{Search: "85004"}, 2},
		{"search with wildcards matches nothing", service.ListPaymentsParams{Search: "%_"}, 0},
		{"search with injection text matches nothing", service.ListPaymentsParams{Search: `'; DROP TABLE payments; --`}, 0},
		{"combined", service.ListPaymentsParams{Search: "85004", Status: []string{"paid"}, Method: []string{"card"}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.params.Limit = "200"
			res, err := e.svc.List(ctx, tt.params)

			require.NoError(t, err)
			require.Len(t, res.Items, tt.wantCount)
			require.NotNil(t, res.Summary)
			require.EqualValues(t, tt.wantCount, res.Summary.Count)

			var sum int64
			for _, r := range res.Items {
				sum += int64(r.Amount)
			}
			require.EqualValues(t, sum, res.Summary.Total)
		})
	}
}

func TestInvalidInputIsReportedPerFieldWithoutHittingTheData(t *testing.T) {
	e := newEnv(t)

	_, err := e.svc.List(context.Background(), service.ListPaymentsParams{
		Status: []string{"signed"}, Method: []string{"wire"}, Lifecycle: "archived", Sort: "discount", Limit: "0", Cursor: "garbage!",
	})

	require.ErrorIs(t, err, model.ErrInvalidInput)
	var in *model.InvalidInputError
	require.ErrorAs(t, err, &in)
	var got []string
	for _, f := range in.Fields {
		got = append(got, f.Field)
	}
	require.ElementsMatch(t, []string{"status", "method", "lifecycle", "sort", "limit"}, got, "the cursor is not judged against an invalid sort")
}

func TestCursorsAreBoundToTheirSortKeyAndDirection(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	first, err := e.svc.List(ctx, service.ListPaymentsParams{Sort: "amount", Order: "asc", Limit: "2"})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	for _, other := range []service.ListPaymentsParams{
		{Sort: "amount", Order: "desc"},
		{Sort: "payment_date", Order: "asc"},
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
			res, err := e.svc.List(context.Background(), service.ListPaymentsParams{From: tt.from, To: tt.to, Limit: "5"})

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

	_, err := e.svc.List(ctx, service.ListPaymentsParams{})
	require.ErrorIs(t, err, context.Canceled)
}
