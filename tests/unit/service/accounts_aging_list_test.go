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

func agingRows(n int) []model.AgingRow {
	out := make([]model.AgingRow, n)
	for i := range out {
		out[i] = model.AgingRow{ID: int64(i+1) * 4294967296, SortValue: fmt.Sprintf("customer %02d", i+1)}
	}
	return out
}

// runAging calls List on a service whose repository returns rows, and returns the query it received.
func runAging(t *testing.T, p service.ListAgingParams, rows []model.AgingRow) (model.AgingQuery, *service.ListAgingResult) {
	t.Helper()

	repo := servicemocks.NewMockAccountsAgingRepository(t)
	var got model.AgingQuery
	// the summary is asked for on the first page only
	withSummary := strings.TrimSpace(p.Cursor) == ""
	page := model.AgingPage{Rows: rows}
	if withSummary {
		page.Summary = &model.AgingSummary{}
	}
	repo.EXPECT().Page(mock.Anything, mock.Anything, withSummary).
		Run(func(_ context.Context, q model.AgingQuery, _ bool) { got = q }).
		Return(page, nil).Once()

	res, err := service.NewAccountsAgingService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsAging asserts that p is invalid and that the repository is never touched.
func rejectsAging(t *testing.T, p service.ListAgingParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockAccountsAgingRepository(t) // no expectations: any call fails the test
	_, err := service.NewAccountsAgingService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestAgingListDefaults(t *testing.T) {
	q, res := runAging(t, service.ListAgingParams{}, nil)

	require.Equal(t, model.AgingQuery{
		Filter: model.AgingFilter{AsOf: date(2026, 10, 21)},
		Sort:   model.AgingSort{Key: model.AgingSortCustomer},
		Limit:  service.DefaultPageSize,
	}, q, "the Current tab: today, the customer's name A to Z")
	require.Equal(t, date(2026, 10, 21), res.AsOf)
	require.Equal(t, 50, res.Limit)
	require.NotNil(t, res.Summary, "the first page carries the summary")
}

func TestAgingListAsOf(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Time
	}{
		{"missing means today", "", date(2026, 10, 21)},
		{"blank means today", "   ", date(2026, 10, 21)},
		{"a past day", "2026-06-30", date(2026, 6, 30)},
		{"today itself", "2026-10-21", date(2026, 10, 21)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, res := runAging(t, service.ListAgingParams{AsOf: tt.raw}, nil)

			require.Equal(t, tt.want, q.Filter.AsOf)
			require.Equal(t, tt.want, res.AsOf)
		})
	}

	t.Run("today is read in the clock's own zone", func(t *testing.T) {
		zone := time.FixedZone("UTC+7", 7*3600)
		now := time.Date(2026, 10, 22, 1, 0, 0, 0, zone) // still Oct 21 in UTC
		repo := servicemocks.NewMockAccountsAgingRepository(t)
		var got model.AgingQuery
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).
			Run(func(_ context.Context, q model.AgingQuery, _ bool) { got = q }).
			Return(model.AgingPage{}, nil).Once()

		_, err := service.NewAccountsAgingService(repo, fixedClock(now)).List(context.Background(), service.ListAgingParams{})

		require.NoError(t, err)
		require.Equal(t, date(2026, 10, 22), got.Filter.AsOf)
	})
}

func TestAgingListDays(t *testing.T) {
	for raw, want := range map[string]int{"": 0, "30": 30, " 60 ": 60, "90": 90} {
		t.Run("accepts "+raw, func(t *testing.T) {
			q, _ := runAging(t, service.ListAgingParams{Days: raw}, nil)
			require.Equal(t, want, q.Filter.MinAgeDays)
		})
	}
	for _, bad := range []string{"0", "45", "-30", "abc", "30.5"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			got := rejectsAging(t, service.ListAgingParams{Days: bad})
			require.Equal(t, "must be one of: 30, 60, 90", got["days"])
		})
	}
}

func TestAgingListNormalizesFilters(t *testing.T) {
	q, _ := runAging(t, service.ListAgingParams{
		ServiceType: []string{"5", " 3 ", "5", ""},
		Email:       "  @Example.com ",
		Search:      "  saka ",
	}, nil)

	require.Equal(t, []int64{5, 3}, q.Filter.ServiceTypeIDs)
	require.Equal(t, "@Example.com", q.Filter.Email)
	require.Equal(t, "saka", q.Filter.Search)
}

func TestAgingListPaging(t *testing.T) {
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
			_, res := runAging(t, service.ListAgingParams{Limit: tt.limit}, agingRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}

			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeAgingCursor(res.NextCursor, model.AgingSort{Key: model.AgingSortCustomer})
			require.NoError(t, err)
			require.Equal(t, &model.AgingCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestAgingListFollowsACursor(t *testing.T) {
	_, page1 := runAging(t, service.ListAgingParams{Limit: "2"}, agingRows(3))
	require.NotEmpty(t, page1.NextCursor)

	q, page2 := runAging(t, service.ListAgingParams{Limit: "2", Cursor: "  " + page1.NextCursor + " "}, agingRows(1))

	require.Equal(t, &model.AgingCursor{SortValue: "customer 02", ID: 2 * 4294967296}, q.After)
	require.Nil(t, page2.Summary, "later pages carry no summary")
	require.False(t, page2.HasMore)
}

func TestAgingListSummaryComesFromTheRepositoryOnTheFirstPage(t *testing.T) {
	want := &model.AgingSummary{Count: 27, D0to30: 100, D31to60: 200, D61to90: 300, D90Plus: 400, Total: 1000, AvailableCredit: 5000}
	repo := servicemocks.NewMockAccountsAgingRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.AgingPage{Summary: want}, nil).Once()

	res, err := service.NewAccountsAgingService(repo, fixedClock(october21)).List(context.Background(), service.ListAgingParams{})

	require.NoError(t, err)
	require.Equal(t, want, res.Summary)
}

func TestAgingListSort(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.AgingSort
	}{
		{"neither: customer A to Z", "", "", model.AgingSort{Key: model.AgingSortCustomer}},
		{"a key alone is ascending", "total", "", model.AgingSort{Key: model.AgingSortTotal}},
		{"a key and desc", "d90_plus", "desc", model.AgingSort{Key: model.AgingSortD90Plus, Desc: true}},
		{"an order alone sorts by customer", "", "desc", model.AgingSort{Key: model.AgingSortCustomer, Desc: true}},
		{"case is ignored", " Available_Credit ", " DESC ", model.AgingSort{Key: model.AgingSortAvailableCredit, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runAging(t, service.ListAgingParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	t.Run("every sort key is accepted", func(t *testing.T) {
		for _, key := range model.AgingSortKeys {
			q, _ := runAging(t, service.ListAgingParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		}
	})
}

func TestAgingListRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		p     service.ListAgingParams
		field string
		msg   string
	}{
		{"bad as_of", service.ListAgingParams{AsOf: "10/01/2026"}, "as_of", "must be a date in YYYY-MM-DD format"},
		{"future as_of", service.ListAgingParams{AsOf: "2026-10-22"}, "as_of", "must not be in the future"},
		{"service type not a number", service.ListAgingParams{ServiceType: []string{"abc"}}, "service_type", "must be positive whole numbers"},
		{"email too long", service.ListAgingParams{Email: strings.Repeat("a", 101)}, "email", "must be at most 100 characters long"},
		{"email with NUL", service.ListAgingParams{Email: "a\x00b"}, "email", "must not contain NUL characters"},
		{"search too long", service.ListAgingParams{Search: strings.Repeat("a", 101)}, "q", "must be at most 100 characters long"},
		{"unknown sort", service.ListAgingParams{Sort: "status"}, "sort",
			"must be one of: customer, total, d0_30, d31_60, d61_90, d90_plus, available_credit"},
		{"bad order", service.ListAgingParams{Order: "up"}, "order", "must be asc or desc"},
		{"limit zero", service.ListAgingParams{Limit: "0"}, "limit", "must be a whole number between 1 and 200"},
		{"garbage cursor", service.ListAgingParams{Cursor: "not-a-cursor"}, "cursor", "is not a valid cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsAging(t, tt.p)
			require.Equal(t, tt.msg, got[tt.field])
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsAging(t, service.ListAgingParams{AsOf: "x", Days: "7", Limit: "0", Order: "up"})
		require.Len(t, got, 4)
	})

	t.Run("a cursor from another sort is rejected", func(t *testing.T) {
		_, page1 := runAging(t, service.ListAgingParams{Limit: "1"}, agingRows(2))
		got := rejectsAging(t, service.ListAgingParams{Sort: "total", Cursor: page1.NextCursor})
		require.Equal(t, "does not match the requested sort", got["cursor"])
	})
}

func TestAgingListWrapsStorageErrors(t *testing.T) {
	boom := errors.New("connection reset")
	repo := servicemocks.NewMockAccountsAgingRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.AgingPage{}, boom).Once()

	_, err := service.NewAccountsAgingService(repo, fixedClock(october21)).List(context.Background(), service.ListAgingParams{})

	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, model.ErrInvalidInput)
	require.Contains(t, err.Error(), "list accounts aging")
}

func TestAgingFilterOptions(t *testing.T) {
	t.Run("returns the service types", func(t *testing.T) {
		want := []model.ServiceType{{ID: 2, Name: "Bi-Monthly Service"}, {ID: 1, Name: "Pool Cleaning"}}
		repo := servicemocks.NewMockAccountsAgingRepository(t)
		repo.EXPECT().ServiceTypes(mock.Anything).Return(want, nil).Once()

		opts, err := service.NewAccountsAgingService(repo, fixedClock(october21)).FilterOptions(context.Background())

		require.NoError(t, err)
		require.Equal(t, want, opts.ServiceTypes)
	})

	t.Run("wraps storage errors", func(t *testing.T) {
		boom := errors.New("down")
		repo := servicemocks.NewMockAccountsAgingRepository(t)
		repo.EXPECT().ServiceTypes(mock.Anything).Return(nil, boom).Once()

		_, err := service.NewAccountsAgingService(repo, fixedClock(october21)).FilterOptions(context.Background())

		require.ErrorIs(t, err, boom)
		require.Contains(t, err.Error(), "load filter options")
	})
}

func TestNewAccountsAgingServiceDefaultsToTodayInUTC(t *testing.T) {
	repo := servicemocks.NewMockAccountsAgingRepository(t)
	var got model.AgingQuery
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).
		Run(func(_ context.Context, q model.AgingQuery, _ bool) { got = q }).
		Return(model.AgingPage{}, nil).Once()

	_, err := service.NewAccountsAgingService(repo, nil).List(context.Background(), service.ListAgingParams{})

	require.NoError(t, err)
	now := time.Now().UTC()
	require.Equal(t, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), got.Filter.AsOf)
}
