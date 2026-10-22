//go:build integration

package estimateservice_test

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
	svc  *service.EstimateService
}

func newEnv(t *testing.T, withExtras bool) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	if withExtras {
		// three tied October estimates without email, creator or phone, to exercise empty sort values
		_, err := pool.Exec(context.Background(), `
			INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total)
			SELECT 'estimate', 'EN' || g, c.id, '2026-10-10', 10 * g
			FROM generate_series(1, 3) g
			JOIN customers c ON c.account_number = '63716'`)
		require.NoError(t, err)
	}
	svc := service.NewEstimateService(repository.NewEstimateRepository(pool), func() time.Time { return october21 })
	return env{pool: pool, svc: svc}
}

func numbers(rows []model.EstimateRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.EstimateNumber
	}
	return out
}

func TestListDefault(t *testing.T) {
	e := newEnv(t, false)

	res, err := e.svc.List(context.Background(), service.ListEstimatesParams{})

	require.NoError(t, err)
	require.Len(t, res.Items, 19)
	require.Equal(t, 50, res.Limit)
	require.False(t, res.HasMore)
	require.Empty(t, res.NextCursor)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.NotNil(t, res.Summary)
	require.EqualValues(t, 19, res.Summary.Count)
	require.Equal(t, model.Money(69812), res.Summary.Total, "12 x 24.40 + 96.22 + 3.00 + 80.60 + 150.00 + 75.50")
	require.Equal(t, day(2026, 10, 21), res.Items[0].Date, "newest first")
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestFollowingNextCursorWalksEveryKeyAndDirectionExactly(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()

	for _, key := range model.EstimateSortKeys {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 5} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", key, order, limit), func(t *testing.T) {
					all, err := e.svc.List(ctx, service.ListEstimatesParams{Sort: string(key), Order: order, Limit: "200"})
					require.NoError(t, err)
					require.Len(t, all.Items, 22)

					p := service.ListEstimatesParams{Sort: string(key), Order: order, Limit: fmt.Sprint(limit)}
					var got []model.EstimateRow
					for page := 0; ; page++ {
						require.Less(t, page, 100, "paging must terminate")

						res, err := e.svc.List(ctx, p)
						require.NoError(t, err)
						require.Equal(t, limit, res.Limit)
						if page == 0 {
							require.NotNil(t, res.Summary, "the first page carries the summary")
							require.EqualValues(t, 22, res.Summary.Count)
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

func TestFiltersAgreeWithTheSummary(t *testing.T) {
	e := newEnv(t, false)

	tests := []struct {
		name      string
		params    service.ListEstimatesParams
		wantCount int
		wantTotal model.Money
	}{
		{"accepted", service.ListEstimatesParams{Status: []string{"accepted"}}, 1, 15000},
		{"accepted or declined", service.ListEstimatesParams{Status: []string{"Accepted", "declined"}}, 2, 22550},
		{"signed", service.ListEstimatesParams{SignatureStatus: []string{"signed"}}, 1, 15000},
		{"archived tab", service.ListEstimatesParams{Lifecycle: "archived"}, 1, 4000},
		{"deleted tab", service.ListEstimatesParams{Lifecycle: "deleted"}, 1, 1000},
		{"september is empty", service.ListEstimatesParams{From: "2026-09-01", To: "2026-09-30"}, 0, 0},
		{"open start", service.ListEstimatesParams{To: "2026-10-08"}, 5, 17982},
		{"search by customer", service.ListEstimatesParams{Search: "alaba"}, 3, 9922},
		{"search with wildcards matches nothing", service.ListEstimatesParams{Search: "%_"}, 0, 0},
		{"search with injection text matches nothing", service.ListEstimatesParams{Search: `'; DROP TABLE billing_documents; --`}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.params.Limit = "200"
			res, err := e.svc.List(context.Background(), tt.params)

			require.NoError(t, err)
			require.Len(t, res.Items, tt.wantCount)
			require.NotNil(t, res.Summary)
			require.EqualValues(t, tt.wantCount, res.Summary.Count)
			require.Equal(t, tt.wantTotal, res.Summary.Total)

			var sum model.Money
			for _, r := range res.Items {
				sum += r.Total
			}
			require.Equal(t, sum, res.Summary.Total, "the summary equals the sum of the listed rows")
		})
	}
}

func TestInvalidInputIsReportedPerFieldWithoutHittingTheData(t *testing.T) {
	e := newEnv(t, false)

	_, err := e.svc.List(context.Background(), service.ListEstimatesParams{
		Status: []string{"paid"}, Sort: "discount", Limit: "0", Cursor: "garbage!",
	})

	require.ErrorIs(t, err, model.ErrInvalidInput)
	var in *model.InvalidInputError
	require.ErrorAs(t, err, &in)
	var fields []string
	for _, f := range in.Fields {
		fields = append(fields, f.Field)
	}
	require.ElementsMatch(t, []string{"status", "sort", "limit"}, fields, "the cursor is not judged against an invalid sort")
}

func TestCursorsAreBoundToTheirSortKeyAndDirection(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	first, err := e.svc.List(ctx, service.ListEstimatesParams{Sort: "total", Order: "asc", Limit: "2"})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	_, err = e.svc.List(ctx, service.ListEstimatesParams{Sort: "total", Order: "desc", Limit: "2", Cursor: first.NextCursor})
	require.ErrorIs(t, err, model.ErrInvalidInput)

	_, err = e.svc.List(ctx, service.ListEstimatesParams{Sort: "date", Order: "asc", Limit: "2", Cursor: first.NextCursor})
	require.ErrorIs(t, err, model.ErrInvalidInput)

	invoiceCursor := service.EncodeInvoiceCursor(model.InvoiceSort{Key: model.SortTotal}, model.InvoiceRow{ID: 1, SortValue: "10.00"})
	_, err = e.svc.List(ctx, service.ListEstimatesParams{Sort: "total", Order: "asc", Cursor: invoiceCursor})
	require.NoError(t, err, "a cursor carries only a position, so one with the same key and direction is structurally valid")
}

func TestEstimateListNeverContainsInvoices(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	_, err := e.pool.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total)
		SELECT 'invoice', '123187', id, '2026-10-20', 777 FROM customers WHERE account_number = '63733'`)
	require.NoError(t, err)

	res, err := e.svc.List(ctx, service.ListEstimatesParams{Limit: "200"})
	require.NoError(t, err)
	require.Len(t, res.Items, 19)
	require.EqualValues(t, 19, res.Summary.Count)
	require.Equal(t, model.Money(69812), res.Summary.Total, "the 777.00 invoice is not counted")
}

func TestListHonorsContextCancellation(t *testing.T) {
	e := newEnv(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.svc.List(ctx, service.ListEstimatesParams{})

	require.ErrorIs(t, err, context.Canceled)
}
