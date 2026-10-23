package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
	servicemocks "reportsgo/tests/mocks/service"
)

// rejectsRecurring asserts that p is invalid and that the repository is never touched.
func rejectsRecurring(t *testing.T, p service.ListRecurringParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockRecurringRepository(t) // no expectations: any call fails the test
	_, err := service.NewRecurringService(repo).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

// runRecurring calls List on a service whose repository returns rows, and returns the query it received.
func runRecurring(t *testing.T, p service.ListRecurringParams, rows []model.RecurringRow) (model.RecurringQuery, *service.ListRecurringResult) {
	t.Helper()

	repo := servicemocks.NewMockRecurringRepository(t)
	var got model.RecurringQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.RecurringQuery) { got = q }).
		Return(rows, nil).Once()
	if strings.TrimSpace(p.Cursor) == "" {
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.RecurringSummary{}, nil).Once()
	}

	res, err := service.NewRecurringService(repo).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

func TestRecurringListDefaults(t *testing.T) {
	q, res := runRecurring(t, service.ListRecurringParams{}, nil)

	require.Equal(t, model.RecurringQuery{
		Sort:  model.RecurringSort{Key: model.RecurringSortStartedOn, Desc: true},
		Limit: service.DefaultPageSize,
	}, q, "no date limit, no status or action filter")
	require.True(t, res.From.IsZero(), "unlike the other reports there is no default month")
	require.True(t, res.To.IsZero())
	require.Equal(t, 50, res.Limit)
}

func TestRecurringListDateRange(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{"neither: both ends open", "", "", time.Time{}, time.Time{}},
		{"only whitespace: both ends open", "  ", " ", time.Time{}, time.Time{}},
		{"explicit", "2026-09-01", "2026-09-30", date(2026, 9, 1), date(2026, 9, 30)},
		{"same day", "2026-10-05", "2026-10-05", date(2026, 10, 5), date(2026, 10, 5)},
		{"only from leaves the end open", "2026-10-05", "", date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", "", "2026-10-05", time.Time{}, date(2026, 10, 5)},
		{"whitespace is trimmed", " 2026-10-05 ", " 2026-10-06 ", date(2026, 10, 5), date(2026, 10, 6)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, res := runRecurring(t, service.ListRecurringParams{From: tt.from, To: tt.to}, nil)

			require.Equal(t, tt.wantFrom, q.Filter.DateFrom, "the start date is a calendar date: no shifting")
			require.Equal(t, tt.wantTo, q.Filter.DateTo, "the last day is inclusive as given")
			require.Equal(t, tt.wantFrom, res.From)
			require.Equal(t, tt.wantTo, res.To)
		})
	}
}

// The other reports share the validator's date handling; their default month must not have changed.
func TestOtherReportsStillDefaultToTheCurrentMonth(t *testing.T) {
	q, res := runCredits(t, service.ListCreditsParams{}, nil)

	require.Equal(t, date(2026, 10, 1), q.Filter.DateFrom)
	require.Equal(t, date(2026, 10, 31), q.Filter.DateTo)
	require.Equal(t, date(2026, 10, 1), res.From)
}

func TestRecurringListNormalizesFilters(t *testing.T) {
	q, _ := runRecurring(t, service.ListRecurringParams{
		Status: []string{"PAUSED", " active", "", "paused"},
		Action: []string{"Create_Only", " send_by_email ", "create_only"},
		Search: "  85032  ",
		Sort:   " Repeats ",
		Order:  "ASC",
		Limit:  " 25 ",
	}, nil)

	require.Equal(t, []model.RecurringStatus{model.RecurringPaused, model.RecurringActive}, q.Filter.Statuses, "trimmed, lower-cased, de-duplicated, blanks dropped")
	require.Equal(t, []model.RecurringAction{model.ActionCreateOnly, model.ActionSendByEmail}, q.Filter.Actions)
	require.Equal(t, "85032", q.Filter.Search, "trimmed")
	require.Equal(t, model.RecurringSort{Key: model.RecurringSortRepeats}, q.Sort)
	require.Equal(t, 25, q.Limit)
}

func TestRecurringListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.RecurringSort
	}{
		{"nothing: newest start first", "", "", model.RecurringSort{Key: model.RecurringSortStartedOn, Desc: true}},
		{"only order asc", "", "asc", model.RecurringSort{Key: model.RecurringSortStartedOn}},
		{"only order desc", "", "desc", model.RecurringSort{Key: model.RecurringSortStartedOn, Desc: true}},
		{"only a key: ascending", "total_value", "", model.RecurringSort{Key: model.RecurringSortTotalValue}},
		{"key and order", "customer", "desc", model.RecurringSort{Key: model.RecurringSortCustomer, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runRecurring(t, service.ListRecurringParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}
	for _, key := range model.RecurringSortKeys {
		t.Run("accepts "+string(key), func(t *testing.T) {
			q, _ := runRecurring(t, service.ListRecurringParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		})
	}
}

func TestRecurringListAcceptsEveryStatusAndAction(t *testing.T) {
	for _, s := range model.RecurringStatuses {
		q, _ := runRecurring(t, service.ListRecurringParams{Status: []string{string(s)}}, nil)
		require.Equal(t, []model.RecurringStatus{s}, q.Filter.Statuses)
	}
	for _, a := range model.RecurringActions {
		q, _ := runRecurring(t, service.ListRecurringParams{Action: []string{string(a)}}, nil)
		require.Equal(t, []model.RecurringAction{a}, q.Filter.Actions)
	}
}

func TestRecurringListSearchAndLimitBoundaries(t *testing.T) {
	t.Run("search of exactly 100 characters", func(t *testing.T) {
		q, _ := runRecurring(t, service.ListRecurringParams{Search: strings.Repeat("é", 100)}, nil)
		require.Len(t, []rune(q.Filter.Search), 100)
	})
	for _, limit := range []string{"1", "200"} {
		t.Run("limit "+limit, func(t *testing.T) {
			q, _ := runRecurring(t, service.ListRecurringParams{Limit: limit}, nil)
			require.Contains(t, []int{1, 200}, q.Limit)
		})
	}
}

func TestRecurringListRejectsInvalidInputPerField(t *testing.T) {
	manyValues := make([]string, service.MaxEnumValues+1)
	for i := range manyValues {
		manyValues[i] = "active"
	}

	tests := []struct {
		name    string
		params  service.ListRecurringParams
		field   string
		message string
	}{
		{"from wrong separator", service.ListRecurringParams{From: "2026/10/01"}, "from", "YYYY-MM-DD"},
		{"from impossible day", service.ListRecurringParams{From: "2026-02-30"}, "from", "YYYY-MM-DD"},
		{"to not a date", service.ListRecurringParams{To: "soon"}, "to", "YYYY-MM-DD"},
		{"to before from", service.ListRecurringParams{From: "2026-10-10", To: "2026-10-09"}, "to", "must not be before from"},

		{"status unknown", service.ListRecurringParams{Status: []string{"active", "overdue"}}, "status", "active, paused, failed, ended"},
		{"a payment status is not a status", service.ListRecurringParams{Status: []string{"paid"}}, "status", "active, paused, failed, ended"},
		{"a document status is not a status", service.ListRecurringParams{Status: []string{"draft"}}, "status", "active, paused, failed, ended"},
		{"status too many", service.ListRecurringParams{Status: manyValues}, "status", "at most 10 values"},

		{"action unknown", service.ListRecurringParams{Action: []string{"send_by_email", "print"}}, "action", "send_by_email, create_only"},
		{"a payment method is not an action", service.ListRecurringParams{Action: []string{"card"}}, "action", "send_by_email, create_only"},
		{"action too many", service.ListRecurringParams{Action: manyValues}, "action", "at most 10 values"},

		{"search with NUL", service.ListRecurringParams{Search: "abc\x00def"}, "q", "NUL"},
		{"search invalid UTF-8", service.ListRecurringParams{Search: "abc\xff"}, "q", "UTF-8"},
		{"search too long", service.ListRecurringParams{Search: strings.Repeat("a", 101)}, "q", "at most 100 characters"},

		{"sort unknown", service.ListRecurringParams{Sort: "id"}, "sort", "started_on"},
		{"sort injection attempt", service.ListRecurringParams{Sort: "status; DROP TABLE recurring_invoices"}, "sort", "must be one of"},
		{"another report's sort key", service.ListRecurringParams{Sort: "credit_date"}, "sort", "started_on"},
		{"sort by date is called started_on", service.ListRecurringParams{Sort: "date"}, "sort", "started_on"},
		{"order unknown", service.ListRecurringParams{Order: "up"}, "order", "asc or desc"},

		{"limit zero", service.ListRecurringParams{Limit: "0"}, "limit", "between 1 and 200"},
		{"limit above maximum", service.ListRecurringParams{Limit: "201"}, "limit", "between 1 and 200"},
		{"limit not a number", service.ListRecurringParams{Limit: "many"}, "limit", "between 1 and 200"},

		{"cursor garbage", service.ListRecurringParams{Cursor: "not-a-cursor!!"}, "cursor", "not a valid cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsRecurring(t, tt.params)
			require.Contains(t, got, tt.field)
			require.Contains(t, got[tt.field], tt.message)
			require.Len(t, got, 1, "only the offending field should be reported")
		})
	}
}

func TestRecurringErrorMessagesListOnlyTheRecurringChoices(t *testing.T) {
	got := rejectsRecurring(t, service.ListRecurringParams{
		Sort: "credit_date", Status: []string{"paid"}, Action: []string{"card"},
	})

	require.Equal(t, "must be one of: active, paused, failed, ended", got["status"])
	require.Equal(t, "must be one of: send_by_email, create_only", got["action"])
	require.NotContains(t, got["sort"], "credit_date")
	require.Contains(t, got["sort"], "started_on")
	require.Contains(t, got["sort"], "next_invoice_date")
}

func TestRecurringListReportsEveryProblemTogether(t *testing.T) {
	got := rejectsRecurring(t, service.ListRecurringParams{
		From: "x", To: "y", Status: []string{"paid"}, Action: []string{"card"},
		Search: strings.Repeat("a", 101), Sort: "credit_date", Order: "up", Limit: "0",
	})

	require.ElementsMatch(t, []string{"from", "to", "status", "action", "q", "sort", "order", "limit"}, keys(got))
}

func TestRecurringListChecksTheCursorAgainstTheRequestedSort(t *testing.T) {
	sort := model.RecurringSort{Key: model.RecurringSortStartedOn, Desc: true}
	cursor := service.EncodeRecurringCursor(sort, model.RecurringRow{ID: 8, SortValue: "2026-10-12"})

	t.Run("accepted with the same sort and passed to the repository", func(t *testing.T) {
		q, res := runRecurring(t, service.ListRecurringParams{Sort: "started_on", Order: "desc", Cursor: cursor}, nil)
		require.Equal(t, &model.RecurringCursor{SortValue: "2026-10-12", ID: 8}, q.After)
		require.Nil(t, res.Summary)
	})

	t.Run("rejected for another key", func(t *testing.T) {
		got := rejectsRecurring(t, service.ListRecurringParams{Sort: "ends_on", Order: "desc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("rejected for another direction", func(t *testing.T) {
		got := rejectsRecurring(t, service.ListRecurringParams{Sort: "started_on", Order: "asc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("not decoded when the sort itself is invalid", func(t *testing.T) {
		got := rejectsRecurring(t, service.ListRecurringParams{Sort: "bogus", Cursor: "garbage!"})
		require.Contains(t, got, "sort")
		require.NotContains(t, got, "cursor", "one root cause is reported once")
	})

	t.Run("a whitespace-only cursor means no cursor", func(t *testing.T) {
		q, res := runRecurring(t, service.ListRecurringParams{Cursor: "   "}, nil)
		require.Nil(t, q.After)
		require.NotNil(t, res.Summary, "it is a first page")
	})
}
