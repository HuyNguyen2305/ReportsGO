package service_test

import (
	"context"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // so the zones in the date-range table load on machines without OS zone data

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
	servicemocks "reportsgo/tests/mocks/service"
)

// rejectsCustomer asserts that p is invalid and that the repository is never touched.
func rejectsCustomer(t *testing.T, p service.ListCustomersParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockCustomerRepository(t) // no expectations: any call fails the test
	_, err := service.NewCustomerService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

// runCustomers calls List on a service whose repository returns rows, and returns the query it received.
func runCustomers(t *testing.T, p service.ListCustomersParams, rows []model.CustomerRow) (model.CustomerQuery, *service.ListCustomersResult) {
	t.Helper()

	repo := servicemocks.NewMockCustomerRepository(t)
	var got model.CustomerQuery
	// the summary is asked for on the first page only
	withSummary := strings.TrimSpace(p.Cursor) == ""
	page := model.CustomerPage{Rows: rows}
	if withSummary {
		page.Summary = &model.CustomerSummary{}
	}
	repo.EXPECT().Page(mock.Anything, mock.Anything, withSummary).
		Run(func(_ context.Context, q model.CustomerQuery, _ bool) { got = q }).
		Return(page, nil).Once()

	res, err := service.NewCustomerService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

func TestCustomerListDefaults(t *testing.T) {
	q, res := runCustomers(t, service.ListCustomersParams{}, nil)

	require.Equal(t, model.CustomerQuery{
		Filter: model.CustomerFilter{
			CreatedFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			CreatedBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		},
		Sort:  model.CustomerSort{Key: model.CustomerSortCreatedAt, Desc: true},
		Limit: service.DefaultPageSize,
	}, q)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To, "the response shows the inclusive last day")
	require.Equal(t, 50, res.Limit)
}

func TestCustomerListDateRange(t *testing.T) {
	utc7 := time.FixedZone("UTC+7", 7*3600)
	la, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)

	tests := []struct {
		name       string
		now        time.Time
		from, to   string
		wantFrom   time.Time
		wantBefore time.Time
		respFrom   time.Time
		respTo     time.Time
	}{
		{"defaulted month", october21, "", "", date(2026, 10, 1), date(2026, 11, 1), date(2026, 10, 1), date(2026, 10, 31)},
		{"february of a leap year", time.Date(2028, 2, 15, 0, 0, 0, 0, time.UTC), "", "", date(2028, 2, 1), date(2028, 3, 1), date(2028, 2, 1), date(2028, 2, 29)},
		{"december rolls into the next year", time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), "", "", date(2026, 12, 1), date(2027, 1, 1), date(2026, 12, 1), date(2026, 12, 31)},
		{"month and day boundaries are taken in the clock's zone", time.Date(2026, 11, 1, 3, 0, 0, 0, utc7), "", "", time.Date(2026, 11, 1, 0, 0, 0, 0, utc7), time.Date(2026, 12, 1, 0, 0, 0, 0, utc7), date(2026, 11, 1), date(2026, 11, 30)},
		{"an explicit day is read in the clock's zone", time.Date(2026, 10, 21, 9, 30, 0, 0, utc7), "2026-10-05", "2026-10-05", time.Date(2026, 10, 5, 0, 0, 0, 0, utc7), time.Date(2026, 10, 6, 0, 0, 0, 0, utc7), date(2026, 10, 5), date(2026, 10, 5)},
		{"open ends stay open in another zone", time.Date(2026, 10, 21, 9, 30, 0, 0, utc7), "2026-10-05", "", time.Date(2026, 10, 5, 0, 0, 0, 0, utc7), time.Time{}, date(2026, 10, 5), time.Time{}},
		// Daylight saving: the spring-forward day lasts 23 hours (08:00Z to 07:00Z) and the fall-back day 25.
		{"a 23-hour day in Los Angeles", time.Date(2026, 3, 20, 12, 0, 0, 0, la), "2026-03-08", "2026-03-08", time.Date(2026, 3, 8, 8, 0, 0, 0, time.UTC), time.Date(2026, 3, 9, 7, 0, 0, 0, time.UTC), date(2026, 3, 8), date(2026, 3, 8)},
		{"a 25-hour day in Los Angeles", time.Date(2026, 11, 20, 12, 0, 0, 0, la), "2026-11-01", "2026-11-01", time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC), date(2026, 11, 1), date(2026, 11, 1)},
		{"the same instant read in UTC is still October", time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("UTC+7", 7*3600)).UTC(), "", "", date(2026, 10, 1), date(2026, 11, 1), date(2026, 10, 1), date(2026, 10, 31)},
		{"explicit", october21, "2026-09-01", "2026-09-30", date(2026, 9, 1), date(2026, 10, 1), date(2026, 9, 1), date(2026, 9, 30)},
		{"same day covers 24 hours", october21, "2026-10-05", "2026-10-05", date(2026, 10, 5), date(2026, 10, 6), date(2026, 10, 5), date(2026, 10, 5)},
		{"only from leaves the end open", october21, "2026-10-05", "", date(2026, 10, 5), time.Time{}, date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", october21, "", "2026-10-05", time.Time{}, date(2026, 10, 6), time.Time{}, date(2026, 10, 5)},
		{"whitespace is trimmed", october21, " 2026-10-05 ", " 2026-10-06 ", date(2026, 10, 5), date(2026, 10, 7), date(2026, 10, 5), date(2026, 10, 6)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := servicemocks.NewMockCustomerRepository(t)
			var got model.CustomerQuery
			repo.EXPECT().Page(mock.Anything, mock.Anything, true).
				Run(func(_ context.Context, q model.CustomerQuery, _ bool) { got = q }).
				Return(model.CustomerPage{}, nil).Once()

			res, err := service.NewCustomerService(repo, fixedClock(tt.now)).List(context.Background(),
				service.ListCustomersParams{From: tt.from, To: tt.to})

			require.NoError(t, err)
			require.True(t, tt.wantFrom.Equal(got.Filter.CreatedFrom), "first instant: want %s, got %s", tt.wantFrom, got.Filter.CreatedFrom)
			require.True(t, tt.wantBefore.Equal(got.Filter.CreatedBefore), "first excluded instant: want %s, got %s", tt.wantBefore, got.Filter.CreatedBefore)
			require.Equal(t, tt.respFrom, res.From)
			require.Equal(t, tt.respTo, res.To)
		})
	}
}

func TestCustomerListNormalizesFilters(t *testing.T) {
	q, _ := runCustomers(t, service.ListCustomersParams{
		LeadSourceIDs: []string{"3", " 1 ", "", "3"},
		Search:        "  Lovelace  ",
		Sort:          " Email ",
		Order:         "ASC",
		Limit:         " 25 ",
	}, nil)

	require.Equal(t, []int64{3, 1}, q.Filter.LeadSourceIDs, "trimmed, de-duplicated, blanks dropped")
	require.Equal(t, "Lovelace", q.Filter.Search, "trimmed, case kept")
	require.Equal(t, model.CustomerSort{Key: model.CustomerSortEmail}, q.Sort)
	require.Equal(t, 25, q.Limit)
}

func TestCustomerListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.CustomerSort
	}{
		{"nothing: newest first", "", "", model.CustomerSort{Key: model.CustomerSortCreatedAt, Desc: true}},
		{"only order asc", "", "asc", model.CustomerSort{Key: model.CustomerSortCreatedAt}},
		{"only order desc", "", "desc", model.CustomerSort{Key: model.CustomerSortCreatedAt, Desc: true}},
		{"only a key: ascending", "phone", "", model.CustomerSort{Key: model.CustomerSortPhone}},
		{"key and order", "source", "desc", model.CustomerSort{Key: model.CustomerSortSource, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runCustomers(t, service.ListCustomersParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	for _, key := range model.CustomerSortKeys {
		t.Run("accepts "+string(key), func(t *testing.T) {
			q, _ := runCustomers(t, service.ListCustomersParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		})
	}
}

func TestCustomerListSearchAndLimitBoundaries(t *testing.T) {
	t.Run("search of exactly 100 characters", func(t *testing.T) {
		q, _ := runCustomers(t, service.ListCustomersParams{Search: strings.Repeat("é", 100)}, nil)
		require.Len(t, []rune(q.Filter.Search), 100)
	})
	for _, limit := range []string{"1", "200"} {
		t.Run("limit "+limit, func(t *testing.T) {
			q, _ := runCustomers(t, service.ListCustomersParams{Limit: limit}, nil)
			require.Contains(t, []int{1, 200}, q.Limit)
		})
	}
}

func TestCustomerListRejectsInvalidInputPerField(t *testing.T) {
	manyIDs := make([]string, service.MaxIDsPerFilter+1)
	for i := range manyIDs {
		manyIDs[i] = "1"
	}

	tests := []struct {
		name    string
		params  service.ListCustomersParams
		field   string
		message string
	}{
		{"from wrong separator", service.ListCustomersParams{From: "2026/10/01"}, "from", "YYYY-MM-DD"},
		{"from impossible day", service.ListCustomersParams{From: "2026-02-30"}, "from", "YYYY-MM-DD"},
		{"to not a date", service.ListCustomersParams{To: "soon"}, "to", "YYYY-MM-DD"},
		{"to before from", service.ListCustomersParams{From: "2026-10-10", To: "2026-10-09"}, "to", "must not be before from"},

		{"lead source id not a number", service.ListCustomersParams{LeadSourceIDs: []string{"abc"}}, "lead_source_id", "positive whole numbers"},
		{"lead source id zero", service.ListCustomersParams{LeadSourceIDs: []string{"0"}}, "lead_source_id", "positive whole numbers"},
		{"lead source id negative", service.ListCustomersParams{LeadSourceIDs: []string{"-1"}}, "lead_source_id", "positive whole numbers"},
		{"lead source id overflows", service.ListCustomersParams{LeadSourceIDs: []string{"99999999999999999999"}}, "lead_source_id", "positive whole numbers"},
		{"one bad id among good", service.ListCustomersParams{LeadSourceIDs: []string{"1", "x"}}, "lead_source_id", "positive whole numbers"},
		{"too many ids", service.ListCustomersParams{LeadSourceIDs: manyIDs}, "lead_source_id", "at most 100 values"},

		{"search with NUL", service.ListCustomersParams{Search: "abc\x00def"}, "q", "NUL"},
		{"search invalid UTF-8", service.ListCustomersParams{Search: "abc\xff"}, "q", "UTF-8"},
		{"search too long", service.ListCustomersParams{Search: strings.Repeat("a", 101)}, "q", "at most 100 characters"},

		{"sort unknown", service.ListCustomersParams{Sort: "id"}, "sort", "account_number"},
		{"sort injection attempt", service.ListCustomersParams{Sort: "created_at; DROP TABLE customers"}, "sort", "must be one of"},
		{"another report's sort", service.ListCustomersParams{Sort: "invoice_value"}, "sort", "account_number"},
		{"order unknown", service.ListCustomersParams{Order: "up"}, "order", "asc or desc"},

		{"limit zero", service.ListCustomersParams{Limit: "0"}, "limit", "between 1 and 200"},
		{"limit above maximum", service.ListCustomersParams{Limit: "201"}, "limit", "between 1 and 200"},
		{"limit not a number", service.ListCustomersParams{Limit: "many"}, "limit", "between 1 and 200"},

		{"cursor garbage", service.ListCustomersParams{Cursor: "not-a-cursor!!"}, "cursor", "not a valid cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsCustomer(t, tt.params)
			require.Contains(t, got, tt.field)
			require.Contains(t, got[tt.field], tt.message)
			require.Len(t, got, 1, "only the offending field should be reported")
		})
	}
}

func TestCustomerErrorMessagesListOnlyTheCustomerChoices(t *testing.T) {
	got := rejectsCustomer(t, service.ListCustomersParams{Sort: "status"})

	require.NotContains(t, got["sort"], "status")
	require.NotContains(t, got["sort"], "invoice_value")
	for _, key := range model.CustomerSortKeys {
		require.Contains(t, got["sort"], string(key))
	}
}

func TestCustomerListReportsEveryProblemTogether(t *testing.T) {
	got := rejectsCustomer(t, service.ListCustomersParams{
		From: "x", To: "y", LeadSourceIDs: []string{"r"},
		Search: strings.Repeat("a", 101), Sort: "discount", Order: "up", Limit: "0",
	})

	require.ElementsMatch(t, []string{"from", "to", "lead_source_id", "q", "sort", "order", "limit"}, keys(got))
}

func TestCustomerListChecksTheCursorAgainstTheRequestedSort(t *testing.T) {
	sort := model.CustomerSort{Key: model.CustomerSortCreatedAt, Desc: true}
	cursor := service.EncodeCustomerCursor(sort, model.CustomerRow{ID: 8, SortValue: "2026-10-12T08:00:00.000000Z"})

	t.Run("accepted with the same sort and passed to the repository", func(t *testing.T) {
		q, res := runCustomers(t, service.ListCustomersParams{Sort: "created_at", Order: "desc", Cursor: cursor}, nil)
		require.Equal(t, &model.CustomerCursor{SortValue: "2026-10-12T08:00:00.000000Z", ID: 8}, q.After)
		require.Nil(t, res.Summary)
	})

	t.Run("rejected for another key", func(t *testing.T) {
		got := rejectsCustomer(t, service.ListCustomersParams{Sort: "email", Order: "desc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("rejected for another direction", func(t *testing.T) {
		got := rejectsCustomer(t, service.ListCustomersParams{Sort: "created_at", Order: "asc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("not decoded when the sort itself is invalid", func(t *testing.T) {
		got := rejectsCustomer(t, service.ListCustomersParams{Sort: "bogus", Cursor: "garbage!"})
		require.Contains(t, got, "sort")
		require.NotContains(t, got, "cursor", "one root cause is reported once")
	})

	t.Run("a whitespace-only cursor means no cursor", func(t *testing.T) {
		q, res := runCustomers(t, service.ListCustomersParams{Cursor: "   "}, nil)
		require.Nil(t, q.After)
		require.NotNil(t, res.Summary, "it is a first page")
	})
}
