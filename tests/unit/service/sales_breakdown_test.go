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

func salesBreakdownRows(n int) []model.SalesBreakdownRow {
	out := make([]model.SalesBreakdownRow, n)
	for i := range out {
		out[i] = model.SalesBreakdownRow{ID: int64(i + 1), Name: fmt.Sprintf("Group %02d", i+1), SortValue: fmt.Sprintf("group %02d", i+1)}
	}
	return out
}

// runSalesBreakdown calls List on a service whose repository returns rows, and returns the query it received.
func runSalesBreakdown(t *testing.T, p service.ListSalesBreakdownParams, rows []model.SalesBreakdownRow) (model.SalesBreakdownQuery, *service.ListSalesBreakdownResult) {
	t.Helper()

	repo := servicemocks.NewMockSalesBreakdownRepository(t)
	var got model.SalesBreakdownQuery
	// the summary is asked for on the first page only
	withSummary := strings.TrimSpace(p.Cursor) == ""
	page := model.SalesBreakdownPage{Rows: rows}
	if withSummary {
		page.Summary = &model.SalesBreakdownSummary{}
	}
	repo.EXPECT().Page(mock.Anything, mock.Anything, withSummary).
		Run(func(_ context.Context, q model.SalesBreakdownQuery, _ bool) { got = q }).
		Return(page, nil).Once()

	res, err := service.NewSalesBreakdownService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsSalesBreakdown asserts that p is invalid and that the repository is never touched.
func rejectsSalesBreakdown(t *testing.T, p service.ListSalesBreakdownParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockSalesBreakdownRepository(t) // no expectations: any call fails the test
	_, err := service.NewSalesBreakdownService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func revenueBy(by string) service.ListSalesBreakdownParams {
	return service.ListSalesBreakdownParams{Type: "revenue", By: by}
}

func TestSalesBreakdownListDefaults(t *testing.T) {
	q, res := runSalesBreakdown(t, service.ListSalesBreakdownParams{Type: " Forecast ", By: "SERVICE"}, nil)

	require.Equal(t, model.SalesBreakdownForecast, q.Filter.Type)
	require.Equal(t, model.SalesBreakdownByService, q.Filter.By)
	require.Equal(t, 2026, q.Filter.Year)
	require.Equal(t, date(2026, 1, 1), q.Filter.YearFrom)
	require.Equal(t, date(2026, 12, 31), q.Filter.YearTo)
	require.Equal(t, date(2026, 1, 1), q.Filter.MonthStarts[0])
	require.Equal(t, date(2026, 10, 1), q.Filter.MonthStarts[9])
	require.Equal(t, date(2027, 1, 1), q.Filter.MonthStarts[12])
	require.Empty(t, q.Filter.Measure, "the forecast has no measure")
	require.Equal(t, model.SalesBreakdownSort{Key: model.SalesBreakdownSortName}, q.Sort, "name, A to Z")
	require.Equal(t, service.DefaultPageSize, q.Limit)
	require.Equal(t, 2026, res.Year)
	require.Equal(t, model.SalesBreakdownForecast, res.Type)
	require.Equal(t, model.SalesBreakdownByService, res.By)
	require.NotNil(t, res.Summary)
	require.NotNil(t, res.Items)
}

func TestSalesBreakdownListRevenueDefaultsToCollected(t *testing.T) {
	q, _ := runSalesBreakdown(t, revenueBy("client"), nil)

	require.Equal(t, model.SalesBreakdownCollected, q.Filter.Measure)
	require.Empty(t, q.Filter.Methods)
}

func TestSalesBreakdownListMonthsAreReadInTheClocksZone(t *testing.T) {
	zone := time.FixedZone("UTC+7", 7*3600)
	repo := servicemocks.NewMockSalesBreakdownRepository(t)
	var got model.SalesBreakdownQuery
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).
		Run(func(_ context.Context, q model.SalesBreakdownQuery, _ bool) { got = q }).
		Return(model.SalesBreakdownPage{}, nil).Once()

	// 2026-12-31 20:00 UTC is already 2027 in UTC+7
	_, err := service.NewSalesBreakdownService(repo, fixedClock(time.Date(2026, 12, 31, 20, 0, 0, 0, time.UTC).In(zone))).
		List(context.Background(), revenueBy("client"))

	require.NoError(t, err)
	require.Equal(t, 2027, got.Filter.Year)
	require.True(t, time.Date(2027, 1, 1, 0, 0, 0, 0, zone).Equal(got.Filter.MonthStarts[0]))
	require.Equal(t, zone, got.Filter.MonthStarts[5].Location())
}

func TestSalesBreakdownListNormalizesFilters(t *testing.T) {
	q, _ := runSalesBreakdown(t, service.ListSalesBreakdownParams{
		Type: "revenue", By: "staff", Year: " 2025 ", Group: []string{"2", " 1 ", "2"},
		Measure: " COLLECTED_WITHOUT_TAX ", Method: []string{"Card", "cash", "card"}, Search: "  thon ",
	}, nil)

	require.Equal(t, 2025, q.Filter.Year)
	require.Equal(t, []int64{2, 1}, q.Filter.GroupIDs)
	require.Equal(t, model.SalesBreakdownCollectedWithoutTax, q.Filter.Measure)
	require.Equal(t, []model.PaymentMethod{model.MethodCard, model.MethodCash}, q.Filter.Methods)
	require.Equal(t, "thon", q.Filter.Search)
}

func TestSalesBreakdownListSort(t *testing.T) {
	tests := []struct {
		name        string
		by          string
		sort, order string
		want        model.SalesBreakdownSort
	}{
		{"neither: name A to Z", "client", "", "", model.SalesBreakdownSort{Key: model.SalesBreakdownSortName}},
		{"a month alone is ascending", "client", "oct", "", model.SalesBreakdownSort{Key: model.SalesBreakdownSortOct}},
		{"the total, biggest first", "tag", "total", "desc", model.SalesBreakdownSort{Key: model.SalesBreakdownSortTotal, Desc: true}},
		{"an order alone sorts by name", "item", "", "desc", model.SalesBreakdownSort{Key: model.SalesBreakdownSortName, Desc: true}},
		{"the account number of clients", "client", "Account_Number", "", model.SalesBreakdownSort{Key: model.SalesBreakdownSortAccountNumber}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runSalesBreakdown(t, service.ListSalesBreakdownParams{Type: "revenue", By: tt.by, Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	for _, by := range model.SalesBreakdownBys {
		t.Run("every sort key of "+string(by)+" is accepted", func(t *testing.T) {
			for _, key := range model.SalesBreakdownSortKeysFor(by) {
				q, _ := runSalesBreakdown(t, service.ListSalesBreakdownParams{Type: "forecast", By: string(by), Sort: string(key)}, nil)
				require.Equal(t, key, q.Sort.Key)
			}
		})
	}
}

func TestSalesBreakdownListPaging(t *testing.T) {
	tests := []struct {
		name        string
		limit       string
		fromRepo    int
		wantItems   int
		wantHasMore bool
	}{
		{"short page", "5", 3, 3, false},
		{"exactly a full page", "5", 5, 5, false},
		{"one row beyond: more pages", "5", 6, 5, true},
		{"empty", "5", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := revenueBy("client")
			p.Limit = tt.limit
			_, res := runSalesBreakdown(t, p, salesBreakdownRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}

			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeSalesBreakdownCursor(res.NextCursor, model.SalesBreakdownSort{Key: model.SalesBreakdownSortName})
			require.NoError(t, err)
			require.Equal(t, &model.SalesBreakdownCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestSalesBreakdownListFollowsACursor(t *testing.T) {
	p := revenueBy("client")
	p.Limit = "2"
	_, page1 := runSalesBreakdown(t, p, salesBreakdownRows(3))
	require.NotEmpty(t, page1.NextCursor)

	p.Cursor = "  " + page1.NextCursor + " "
	q, page2 := runSalesBreakdown(t, p, salesBreakdownRows(1))

	require.Equal(t, &model.SalesBreakdownCursor{SortValue: "group 02", ID: 2}, q.After)
	require.Nil(t, page2.Summary, "later pages carry no summary")
	require.Nil(t, page2.Unassigned)
}

func TestSalesBreakdownListPassesTheSummaryAndUnassignedLine(t *testing.T) {
	summary := &model.SalesBreakdownSummary{Count: 2, SalesBreakdownAmounts: model.SalesBreakdownAmounts{Total: 82106}}
	unassigned := &model.SalesBreakdownAmounts{Total: 66880}
	repo := servicemocks.NewMockSalesBreakdownRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).
		Return(model.SalesBreakdownPage{Summary: summary, Unassigned: unassigned}, nil).Once()

	res, err := service.NewSalesBreakdownService(repo, fixedClock(october21)).List(context.Background(), revenueBy("source"))

	require.NoError(t, err)
	require.Equal(t, summary, res.Summary)
	require.Equal(t, unassigned, res.Unassigned)
}

func TestSalesBreakdownListRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		p     service.ListSalesBreakdownParams
		field string
		msg   string
	}{
		{"type is required", service.ListSalesBreakdownParams{By: "client"}, "type", "is required"},
		{"by is required", service.ListSalesBreakdownParams{Type: "revenue"}, "by", "is required"},
		{"unknown type", service.ListSalesBreakdownParams{Type: "budget", By: "client"}, "type", "must be one of: forecast, revenue"},
		{"unknown by", service.ListSalesBreakdownParams{Type: "revenue", By: "region"}, "by", "must be one of: client, service, item, staff, source, tag"},
		{"year not a number", service.ListSalesBreakdownParams{Type: "revenue", By: "client", Year: "next"}, "year", "must be a year between 2000 and 2100"},
		{"year too early", service.ListSalesBreakdownParams{Type: "revenue", By: "client", Year: "1999"}, "year", "must be a year between 2000 and 2100"},
		{"year too late", service.ListSalesBreakdownParams{Type: "revenue", By: "client", Year: "2101"}, "year", "must be a year between 2000 and 2100"},
		{"measure on the forecast", service.ListSalesBreakdownParams{Type: "forecast", By: "client", Measure: "collected"}, "measure", "can only be used with type revenue"},
		{"method on the forecast", service.ListSalesBreakdownParams{Type: "forecast", By: "client", Method: []string{"card"}}, "method", "can only be used with type revenue"},
		{"unknown measure", service.ListSalesBreakdownParams{Type: "revenue", By: "client", Measure: "invoiced"}, "measure", "must be one of: collected, collected_without_tax"},
		{"unknown method", service.ListSalesBreakdownParams{Type: "revenue", By: "client", Method: []string{"wire"}}, "method", "must be one of: cash, check, card, other"},
		{"group not a number", service.ListSalesBreakdownParams{Type: "revenue", By: "tag", Group: []string{"x"}}, "group", "must be positive whole numbers"},
		{"search too long", service.ListSalesBreakdownParams{Type: "revenue", By: "client", Search: strings.Repeat("a", 101)}, "q", "must be at most 100 characters long"},
		{"account number outside clients", service.ListSalesBreakdownParams{Type: "revenue", By: "item", Sort: "account_number"}, "sort",
			"must be one of: name, jan, feb, mar, apr, may, jun, jul, aug, sep, oct, nov, dec, total"},
		{"bad order", service.ListSalesBreakdownParams{Type: "revenue", By: "client", Order: "up"}, "order", "must be asc or desc"},
		{"limit zero", service.ListSalesBreakdownParams{Type: "revenue", By: "client", Limit: "0"}, "limit", "must be a whole number between 1 and 200"},
		{"garbage cursor", service.ListSalesBreakdownParams{Type: "revenue", By: "client", Cursor: "not-a-cursor"}, "cursor", "is not a valid cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsSalesBreakdown(t, tt.p)
			require.Equal(t, tt.msg, got[tt.field])
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsSalesBreakdown(t, service.ListSalesBreakdownParams{Year: "x", Limit: "0"})
		require.Len(t, got, 4, "type, by, year and limit")
	})

	t.Run("a cursor from another sort is rejected", func(t *testing.T) {
		p := revenueBy("client")
		p.Limit = "1"
		_, page1 := runSalesBreakdown(t, p, salesBreakdownRows(2))
		got := rejectsSalesBreakdown(t, service.ListSalesBreakdownParams{Type: "revenue", By: "client", Sort: "total", Cursor: page1.NextCursor})
		require.Equal(t, "does not match the requested sort", got["cursor"])
	})
}

func TestSalesBreakdownListWrapsStorageErrors(t *testing.T) {
	boom := errors.New("connection reset")
	repo := servicemocks.NewMockSalesBreakdownRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.SalesBreakdownPage{}, boom).Once()

	_, err := service.NewSalesBreakdownService(repo, fixedClock(october21)).List(context.Background(), revenueBy("client"))

	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, model.ErrInvalidInput)
	require.Contains(t, err.Error(), "list sales breakdown")
}

func TestSalesBreakdownFilterOptions(t *testing.T) {
	t.Run("returns the group options", func(t *testing.T) {
		want := model.SalesBreakdownGroupOptions{Tags: []model.JobTag{{ID: 1, Name: "VIP"}}}
		repo := servicemocks.NewMockSalesBreakdownRepository(t)
		repo.EXPECT().GroupOptions(mock.Anything).Return(want, nil).Once()

		opts, err := service.NewSalesBreakdownService(repo, fixedClock(october21)).FilterOptions(context.Background())

		require.NoError(t, err)
		require.Equal(t, &want, opts)
	})

	t.Run("wraps storage errors", func(t *testing.T) {
		boom := errors.New("down")
		repo := servicemocks.NewMockSalesBreakdownRepository(t)
		repo.EXPECT().GroupOptions(mock.Anything).Return(model.SalesBreakdownGroupOptions{}, boom).Once()

		_, err := service.NewSalesBreakdownService(repo, fixedClock(october21)).FilterOptions(context.Background())

		require.ErrorIs(t, err, boom)
		require.Contains(t, err.Error(), "load filter options")
	})
}

// the cursor tests -------------------------------------------------------------------------------------------------

func TestSalesBreakdownCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	for _, key := range model.SalesBreakdownSortKeysFor(model.SalesBreakdownByClient) {
		value := "1234.50"
		if key == model.SalesBreakdownSortName || key == model.SalesBreakdownSortAccountNumber {
			value = "william saliba"
		}
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.SalesBreakdownSort{Key: key, Desc: desc}

				raw := service.EncodeSalesBreakdownCursor(sort, model.SalesBreakdownRow{ID: 42, SortValue: value})
				got, err := service.DecodeSalesBreakdownCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.SalesBreakdownCursor{SortValue: value, ID: 42}, got)
			})
		}
	}
}

func TestSalesBreakdownCursorRejectsInvalidCursors(t *testing.T) {
	nameAsc := model.SalesBreakdownSort{Key: model.SalesBreakdownSortName}
	octAsc := model.SalesBreakdownSort{Key: model.SalesBreakdownSortOct}

	tests := []struct {
		name string
		raw  string
		sort model.SalesBreakdownSort
	}{
		{"not base64", "!!!", nameAsc},
		{"not json", encode("hello"), nameAsc},
		{"id missing", encode(`{"k":"name","d":false,"v":"a"}`), nameAsc},
		{"another sort key", encode(`{"k":"total","d":false,"v":"1.00","id":1}`), nameAsc},
		{"another direction", encode(`{"k":"name","d":true,"v":"a","id":1}`), nameAsc},
		{"a month that is not money", encode(`{"k":"oct","d":false,"v":"abc","id":1}`), octAsc},
		{"a month with three decimals", encode(`{"k":"oct","d":false,"v":"1.234","id":1}`), octAsc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.DecodeSalesBreakdownCursor(tt.raw, tt.sort)

			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
		})
	}
}
