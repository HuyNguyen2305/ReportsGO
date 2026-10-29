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

func paymentCollectedRows(n int) []model.PaymentCollectedRow {
	out := make([]model.PaymentCollectedRow, n)
	for i := range out {
		out[i] = model.PaymentCollectedRow{ID: int64(i + 1), SortValue: fmt.Sprintf("2026-10-%02d", i+1)}
	}
	return out
}

// runPaymentsCollected calls List on a service whose repository returns rows, and returns the query it received.
func runPaymentsCollected(t *testing.T, p service.ListPaymentsCollectedParams, rows []model.PaymentCollectedRow) (model.PaymentCollectedQuery, *service.ListPaymentsCollectedResult) {
	t.Helper()

	repo := servicemocks.NewMockPaymentsCollectedRepository(t)
	var got model.PaymentCollectedQuery
	// the summary is asked for on the first page only
	withSummary := strings.TrimSpace(p.Cursor) == ""
	page := model.PaymentCollectedPage{Rows: rows}
	if withSummary {
		page.Summary = &model.PaymentCollectedSummary{}
	}
	repo.EXPECT().Page(mock.Anything, mock.Anything, withSummary).
		Run(func(_ context.Context, q model.PaymentCollectedQuery, _ bool) { got = q }).
		Return(page, nil).Once()

	res, err := service.NewPaymentsCollectedService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsPaymentsCollected asserts that p is invalid and that the repository is never touched.
func rejectsPaymentsCollected(t *testing.T, p service.ListPaymentsCollectedParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockPaymentsCollectedRepository(t) // no expectations: any call fails the test
	_, err := service.NewPaymentsCollectedService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestPaymentsCollectedListDefaults(t *testing.T) {
	q, res := runPaymentsCollected(t, service.ListPaymentsCollectedParams{}, nil)

	require.Equal(t, model.PaymentCollectedQuery{
		Filter: model.PaymentCollectedFilter{
			DateFrom:   date(2026, 10, 1),
			DateTo:     date(2026, 10, 31),
			PaymentFor: model.PaymentForInvoices,
		},
		Sort:  model.PaymentCollectedSort{Key: model.PaymentCollectedSortDate, Desc: true},
		Limit: service.DefaultPageSize,
	}, q)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
	require.Equal(t, 50, res.Limit)
	require.NotNil(t, res.Summary, "the first page carries the summary")
}

func TestPaymentsCollectedListDateRange(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{"defaulted month", "", "", date(2026, 10, 1), date(2026, 10, 31)},
		{"explicit", "2026-09-01", "2026-09-30", date(2026, 9, 1), date(2026, 9, 30)},
		{"only from leaves the end open", "2026-10-05", "", date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", "", "2026-10-05", time.Time{}, date(2026, 10, 5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, res := runPaymentsCollected(t, service.ListPaymentsCollectedParams{From: tt.from, To: tt.to}, nil)

			require.Equal(t, tt.wantFrom, q.Filter.DateFrom)
			require.Equal(t, tt.wantTo, q.Filter.DateTo)
			require.Equal(t, tt.wantFrom, res.From)
			require.Equal(t, tt.wantTo, res.To)
		})
	}
}

func TestPaymentsCollectedListPaymentFor(t *testing.T) {
	tests := []struct {
		raw  string
		want model.PaymentFor
	}{
		{"", model.PaymentForInvoices},
		{"invoices", model.PaymentForInvoices},
		{"credits", model.PaymentForCredits},
		{" CREDITS ", model.PaymentForCredits},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			q, _ := runPaymentsCollected(t, service.ListPaymentsCollectedParams{PaymentFor: tt.raw}, nil)
			require.Equal(t, tt.want, q.Filter.PaymentFor)
		})
	}

	for _, bad := range []string{"all", "invoice", "refunds"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			got := rejectsPaymentsCollected(t, service.ListPaymentsCollectedParams{PaymentFor: bad})
			require.Equal(t, "must be one of: invoices, credits", got["payment_for"])
		})
	}
}

func TestPaymentsCollectedListPaging(t *testing.T) {
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
			_, res := runPaymentsCollected(t, service.ListPaymentsCollectedParams{Limit: tt.limit}, paymentCollectedRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}

			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodePaymentCollectedCursor(res.NextCursor, model.PaymentCollectedSort{Key: model.PaymentCollectedSortDate, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.PaymentCollectedCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestPaymentsCollectedListFollowsACursor(t *testing.T) {
	_, page1 := runPaymentsCollected(t, service.ListPaymentsCollectedParams{Limit: "2"}, paymentCollectedRows(3))
	require.NotEmpty(t, page1.NextCursor)

	q, page2 := runPaymentsCollected(t, service.ListPaymentsCollectedParams{Limit: "2", Cursor: "  " + page1.NextCursor + " "}, paymentCollectedRows(1))

	require.Equal(t, &model.PaymentCollectedCursor{SortValue: "2026-10-02", ID: 2}, q.After)
	require.Nil(t, page2.Summary, "later pages carry no summary")
	require.False(t, page2.HasMore)
}

func TestPaymentsCollectedListSummaryComesFromTheRepositoryOnTheFirstPage(t *testing.T) {
	want := &model.PaymentCollectedSummary{Count: 27, Amount: 189670, WithoutTax: 169522}
	repo := servicemocks.NewMockPaymentsCollectedRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).
		Return(model.PaymentCollectedPage{Summary: want}, nil).Once()

	res, err := service.NewPaymentsCollectedService(repo, fixedClock(october21)).List(context.Background(), service.ListPaymentsCollectedParams{})

	require.NoError(t, err)
	require.Equal(t, want, res.Summary)
}

func TestPaymentsCollectedListNormalizesFilters(t *testing.T) {
	q, _ := runPaymentsCollected(t, service.ListPaymentsCollectedParams{
		Method:      []string{" CARD ", "check", "card", ""},
		CustomerIDs: []string{"5", " 3 ", "5"},
		StaffIDs:    []string{"2", "2", "1"},
		Search:      "  85004 ",
	}, nil)

	require.Equal(t, []model.PaymentMethod{model.MethodCard, model.MethodCheck}, q.Filter.Methods)
	require.Equal(t, []int64{5, 3}, q.Filter.CustomerIDs)
	require.Equal(t, []int64{2, 1}, q.Filter.StaffIDs)
	require.Equal(t, "85004", q.Filter.Search)
}

func TestPaymentsCollectedListSort(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.PaymentCollectedSort
	}{
		{"neither: newest first", "", "", model.PaymentCollectedSort{Key: model.PaymentCollectedSortDate, Desc: true}},
		{"a key alone is ascending", "amount", "", model.PaymentCollectedSort{Key: model.PaymentCollectedSortAmount}},
		{"a key and desc", "without_tax", "desc", model.PaymentCollectedSort{Key: model.PaymentCollectedSortWithoutTax, Desc: true}},
		{"an order alone sorts by date", "", "asc", model.PaymentCollectedSort{Key: model.PaymentCollectedSortDate}},
		{"case is ignored", " Staff ", " DESC ", model.PaymentCollectedSort{Key: model.PaymentCollectedSortStaff, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runPaymentsCollected(t, service.ListPaymentsCollectedParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	t.Run("every sort key is accepted", func(t *testing.T) {
		for _, key := range model.PaymentCollectedSortKeys {
			q, _ := runPaymentsCollected(t, service.ListPaymentsCollectedParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		}
	})
}

func TestPaymentsCollectedListRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		p     service.ListPaymentsCollectedParams
		field string
		msg   string
	}{
		{"bad from", service.ListPaymentsCollectedParams{From: "10/01/2026"}, "from", "must be a date in YYYY-MM-DD format"},
		{"to before from", service.ListPaymentsCollectedParams{From: "2026-10-05", To: "2026-10-04"}, "to", "must not be before from"},
		{"unknown method", service.ListPaymentsCollectedParams{Method: []string{"wire"}}, "method", "must be one of: cash, check, card, other"},
		{"customer id not a number", service.ListPaymentsCollectedParams{CustomerIDs: []string{"abc"}}, "customer_id", "must be positive whole numbers"},
		{"staff id zero", service.ListPaymentsCollectedParams{StaffIDs: []string{"0"}}, "staff_id", "must be positive whole numbers"},
		{"search too long", service.ListPaymentsCollectedParams{Search: strings.Repeat("a", 101)}, "q", "must be at most 100 characters long"},
		{"unknown sort", service.ListPaymentsCollectedParams{Sort: "status"}, "sort",
			"must be one of: payment_for, customer, method, service_state, staff, date, amount, without_tax"},
		{"bad order", service.ListPaymentsCollectedParams{Order: "up"}, "order", "must be asc or desc"},
		{"limit zero", service.ListPaymentsCollectedParams{Limit: "0"}, "limit", "must be a whole number between 1 and 200"},
		{"limit too big", service.ListPaymentsCollectedParams{Limit: "201"}, "limit", "must be a whole number between 1 and 200"},
		{"garbage cursor", service.ListPaymentsCollectedParams{Cursor: "not-a-cursor"}, "cursor", "is not a valid cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsPaymentsCollected(t, tt.p)
			require.Equal(t, tt.msg, got[tt.field])
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsPaymentsCollected(t, service.ListPaymentsCollectedParams{From: "x", PaymentFor: "all", Limit: "0", Order: "up"})
		require.Len(t, got, 4)
	})

	t.Run("a cursor from another sort is rejected", func(t *testing.T) {
		_, page1 := runPaymentsCollected(t, service.ListPaymentsCollectedParams{Limit: "1"}, paymentCollectedRows(2))
		got := rejectsPaymentsCollected(t, service.ListPaymentsCollectedParams{Sort: "amount", Cursor: page1.NextCursor})
		require.Equal(t, "does not match the requested sort", got["cursor"])
	})
}

func TestPaymentsCollectedListWrapsStorageErrors(t *testing.T) {
	boom := errors.New("connection reset")
	repo := servicemocks.NewMockPaymentsCollectedRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.PaymentCollectedPage{}, boom).Once()

	_, err := service.NewPaymentsCollectedService(repo, fixedClock(october21)).List(context.Background(), service.ListPaymentsCollectedParams{})

	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, model.ErrInvalidInput)
	require.Contains(t, err.Error(), "list payments collected")
}

func TestPaymentsCollectedFilterOptions(t *testing.T) {
	t.Run("returns the staff", func(t *testing.T) {
		want := []model.StaffOption{{ID: 2, Name: "thon nguyen"}, {ID: 1, Name: "Thọ Nguyễn Quang"}}
		repo := servicemocks.NewMockPaymentsCollectedRepository(t)
		repo.EXPECT().Staff(mock.Anything).Return(want, nil).Once()

		opts, err := service.NewPaymentsCollectedService(repo, fixedClock(october21)).FilterOptions(context.Background())

		require.NoError(t, err)
		require.Equal(t, want, opts.Staff)
	})

	t.Run("wraps storage errors", func(t *testing.T) {
		boom := errors.New("down")
		repo := servicemocks.NewMockPaymentsCollectedRepository(t)
		repo.EXPECT().Staff(mock.Anything).Return(nil, boom).Once()

		_, err := service.NewPaymentsCollectedService(repo, fixedClock(october21)).FilterOptions(context.Background())

		require.ErrorIs(t, err, boom)
		require.Contains(t, err.Error(), "load filter options")
	})
}

func TestNewPaymentsCollectedServiceDefaultsToTheCurrentMonthInUTC(t *testing.T) {
	repo := servicemocks.NewMockPaymentsCollectedRepository(t)
	var got model.PaymentCollectedQuery
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).
		Run(func(_ context.Context, q model.PaymentCollectedQuery, _ bool) { got = q }).
		Return(model.PaymentCollectedPage{}, nil).Once()

	_, err := service.NewPaymentsCollectedService(repo, nil).List(context.Background(), service.ListPaymentsCollectedParams{})

	require.NoError(t, err)
	now := time.Now().UTC()
	thisMonth := date(now.Year(), now.Month(), 1)
	// the test could straddle midnight on the last day of a month
	require.Contains(t, []time.Time{thisMonth, thisMonth.AddDate(0, -1, 0)}, got.Filter.DateFrom)
}
