package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
	servicemocks "reportsgo/tests/mocks/service"
)

func totalSaleRows(n int) []model.TotalSaleRow {
	out := make([]model.TotalSaleRow, n)
	for i := range out {
		out[i] = model.TotalSaleRow{ID: int64(i + 1), SortValue: fmt.Sprintf("2026-10-%02d", i+1)}
	}
	return out
}

// runTotalSales calls List on a service whose repository returns rows, and returns the query it received.
func runTotalSales(t *testing.T, p service.ListTotalSalesParams, rows []model.TotalSaleRow) (model.TotalSaleQuery, *service.ListTotalSalesResult) {
	t.Helper()

	repo := servicemocks.NewMockTotalSalesRepository(t)
	var got model.TotalSaleQuery
	// the summary is asked for on the first page only
	withSummary := strings.TrimSpace(p.Cursor) == ""
	page := model.TotalSalePage{Rows: rows}
	if withSummary {
		page.Summary = &model.TotalSaleSummary{}
	}
	repo.EXPECT().Page(mock.Anything, mock.Anything, withSummary).
		Run(func(_ context.Context, q model.TotalSaleQuery, _ bool) { got = q }).
		Return(page, nil).Once()

	res, err := service.NewTotalSalesService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsTotalSales asserts that p is invalid and that the repository is never touched.
func rejectsTotalSales(t *testing.T, p service.ListTotalSalesParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockTotalSalesRepository(t) // no expectations: any call fails the test
	_, err := service.NewTotalSalesService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestTotalSalesListDefaults(t *testing.T) {
	q, res := runTotalSales(t, service.ListTotalSalesParams{}, nil)

	require.Equal(t, model.TotalSaleQuery{
		Filter: model.TotalSaleFilter{DateFrom: date(2026, 10, 1), DateTo: date(2026, 10, 31)},
		Sort:   model.TotalSaleSort{Key: model.TotalSaleSortDate, Desc: true},
		Limit:  service.DefaultPageSize,
	}, q)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
	require.Equal(t, 50, res.Limit)
	require.NotNil(t, res.Summary, "the first page carries the summary")
}

func TestTotalSalesListDateRange(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{"defaulted month", "", "", date(2026, 10, 1), date(2026, 10, 31)},
		{"explicit", "2026-09-01", "2026-09-30", date(2026, 9, 1), date(2026, 9, 30)},
		{"same day", "2026-10-05", "2026-10-05", date(2026, 10, 5), date(2026, 10, 5)},
		{"only from leaves the end open", "2026-10-05", "", date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", "", "2026-10-05", time.Time{}, date(2026, 10, 5)},
		{"whitespace is trimmed", " 2026-10-05 ", " 2026-10-06 ", date(2026, 10, 5), date(2026, 10, 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, res := runTotalSales(t, service.ListTotalSalesParams{From: tt.from, To: tt.to}, nil)

			require.Equal(t, tt.wantFrom, q.Filter.DateFrom)
			require.Equal(t, tt.wantTo, q.Filter.DateTo)
			require.Equal(t, tt.wantFrom, res.From)
			require.Equal(t, tt.wantTo, res.To)
		})
	}
}

func TestTotalSalesListPaging(t *testing.T) {
	tests := []struct {
		name        string
		limit       string
		fromRepo    int // rows the repository returns
		wantItems   int
		wantHasMore bool
	}{
		{"short page", "5", 3, 3, false},
		{"exactly a full page", "5", 5, 5, false},
		{"one row beyond: more pages", "5", 6, 5, true},
		{"empty", "5", 0, 0, false},
		{"limit one with more", "1", 2, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, res := runTotalSales(t, service.ListTotalSalesParams{Limit: tt.limit}, totalSaleRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}

			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeTotalSaleCursor(res.NextCursor, model.TotalSaleSort{Key: model.TotalSaleSortDate, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.TotalSaleCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestTotalSalesListFollowsACursor(t *testing.T) {
	_, page1 := runTotalSales(t, service.ListTotalSalesParams{Limit: "2"}, totalSaleRows(3))
	require.NotEmpty(t, page1.NextCursor)

	q, page2 := runTotalSales(t, service.ListTotalSalesParams{Limit: "2", Cursor: "  " + page1.NextCursor + " "}, totalSaleRows(1))

	require.Equal(t, &model.TotalSaleCursor{SortValue: "2026-10-02", ID: 2}, q.After)
	require.Nil(t, page2.Summary, "later pages carry no summary")
	require.False(t, page2.HasMore)
}

func TestTotalSalesListSummaryComesFromTheRepositoryOnTheFirstPage(t *testing.T) {
	want := &model.TotalSaleSummary{Count: 28, Amount: 200635, WithoutTax: 179480}
	repo := servicemocks.NewMockTotalSalesRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).
		Return(model.TotalSalePage{Rows: nil, Summary: want}, nil).Once()

	res, err := service.NewTotalSalesService(repo, fixedClock(october21)).List(context.Background(), service.ListTotalSalesParams{})

	require.NoError(t, err)
	require.Equal(t, want, res.Summary)
}

func TestTotalSalesListNormalizesFilters(t *testing.T) {
	q, _ := runTotalSales(t, service.ListTotalSalesParams{
		Status:      []string{" PAID ", "partial", "paid", ""},
		CustomerIDs: []string{"5", " 3 ", "5"},
		Search:      "  85004 ",
	}, nil)

	require.Equal(t, []model.TotalSaleStatus{model.TotalSalePaid, model.TotalSalePartial}, q.Filter.Statuses)
	require.Equal(t, []int64{5, 3}, q.Filter.CustomerIDs)
	require.Equal(t, "85004", q.Filter.Search)
}

func TestTotalSalesListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.TotalSaleSort
	}{
		{"neither: newest first", "", "", model.TotalSaleSort{Key: model.TotalSaleSortDate, Desc: true}},
		{"a key alone is ascending", "amount", "", model.TotalSaleSort{Key: model.TotalSaleSortAmount}},
		{"a key and desc", "without_tax", "desc", model.TotalSaleSort{Key: model.TotalSaleSortWithoutTax, Desc: true}},
		{"an order alone sorts by date", "", "asc", model.TotalSaleSort{Key: model.TotalSaleSortDate}},
		{"case is ignored", " Status ", " DESC ", model.TotalSaleSort{Key: model.TotalSaleSortStatus, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runTotalSales(t, service.ListTotalSalesParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	t.Run("every sort key is accepted", func(t *testing.T) {
		for _, key := range model.TotalSaleSortKeys {
			q, _ := runTotalSales(t, service.ListTotalSalesParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		}
	})
}

func TestTotalSalesListRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		p     service.ListTotalSalesParams
		field string
		msg   string
	}{
		{"bad from", service.ListTotalSalesParams{From: "10/01/2026"}, "from", "must be a date in YYYY-MM-DD format"},
		{"bad to", service.ListTotalSalesParams{To: "2026-13-01"}, "to", "must be a date in YYYY-MM-DD format"},
		{"to before from", service.ListTotalSalesParams{From: "2026-10-05", To: "2026-10-04"}, "to", "must not be before from"},
		{"unknown status", service.ListTotalSalesParams{Status: []string{"void"}}, "status", "must be one of: paid, partial, sent"},
		{"draft is not a sale status", service.ListTotalSalesParams{Status: []string{"draft"}}, "status", "must be one of: paid, partial, sent"},
		{"customer id not a number", service.ListTotalSalesParams{CustomerIDs: []string{"abc"}}, "customer_id", "must be positive whole numbers"},
		{"customer id zero", service.ListTotalSalesParams{CustomerIDs: []string{"0"}}, "customer_id", "must be positive whole numbers"},
		{"search too long", service.ListTotalSalesParams{Search: strings.Repeat("a", 101)}, "q", "must be at most 100 characters long"},
		{"unknown sort", service.ListTotalSalesParams{Sort: "total"}, "sort", "must be one of: invoice_number, customer, date, status, amount, without_tax"},
		{"bad order", service.ListTotalSalesParams{Order: "up"}, "order", "must be asc or desc"},
		{"limit zero", service.ListTotalSalesParams{Limit: "0"}, "limit", "must be a whole number between 1 and 200"},
		{"limit too big", service.ListTotalSalesParams{Limit: "201"}, "limit", "must be a whole number between 1 and 200"},
		{"limit not a number", service.ListTotalSalesParams{Limit: "ten"}, "limit", "must be a whole number between 1 and 200"},
		{"garbage cursor", service.ListTotalSalesParams{Cursor: "not-a-cursor"}, "cursor", "is not a valid cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsTotalSales(t, tt.p)
			require.Equal(t, tt.msg, got[tt.field])
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsTotalSales(t, service.ListTotalSalesParams{From: "x", Status: []string{"void"}, Limit: "0", Order: "up"})
		require.Len(t, got, 4)
	})

	t.Run("a cursor from another sort is rejected", func(t *testing.T) {
		_, page1 := runTotalSales(t, service.ListTotalSalesParams{Limit: "1"}, totalSaleRows(2))
		got := rejectsTotalSales(t, service.ListTotalSalesParams{Sort: "amount", Cursor: page1.NextCursor})
		require.Equal(t, "does not match the requested sort", got["cursor"])
	})
}

func TestTotalSalesListWrapsStorageErrors(t *testing.T) {
	boom := errors.New("connection reset")
	repo := servicemocks.NewMockTotalSalesRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.TotalSalePage{}, boom).Once()

	_, err := service.NewTotalSalesService(repo, fixedClock(october21)).List(context.Background(), service.ListTotalSalesParams{})

	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, model.ErrInvalidInput)
	require.Contains(t, err.Error(), "list total sales")
}

func TestNewTotalSalesServiceDefaultsToTheCurrentMonthInUTC(t *testing.T) {
	repo := servicemocks.NewMockTotalSalesRepository(t)
	var got model.TotalSaleQuery
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).
		Run(func(_ context.Context, q model.TotalSaleQuery, _ bool) { got = q }).
		Return(model.TotalSalePage{}, nil).Once()

	_, err := service.NewTotalSalesService(repo, nil).List(context.Background(), service.ListTotalSalesParams{})

	require.NoError(t, err)
	now := time.Now().UTC()
	thisMonth := date(now.Year(), now.Month(), 1)
	// the test could straddle midnight on the last day of a month
	require.Contains(t, []time.Time{thisMonth, thisMonth.AddDate(0, -1, 0)}, got.Filter.DateFrom)
}
