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

// rejectsCredit asserts that p is invalid and that the repository is never touched.
func rejectsCredit(t *testing.T, p service.ListCreditsParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockCreditRepository(t) // no expectations: any call fails the test
	_, err := service.NewCreditService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

// runCredits calls List on a service whose repository returns rows, and returns the query it received.
func runCredits(t *testing.T, p service.ListCreditsParams, rows []model.CreditRow) (model.CreditQuery, *service.ListCreditsResult) {
	t.Helper()

	repo := servicemocks.NewMockCreditRepository(t)
	var got model.CreditQuery
	// the summary is asked for on the first page only
	withSummary := strings.TrimSpace(p.Cursor) == ""
	page := model.CreditPage{Rows: rows}
	if withSummary {
		page.Summary = &model.CreditSummary{}
	}
	repo.EXPECT().Page(mock.Anything, mock.Anything, withSummary).
		Run(func(_ context.Context, q model.CreditQuery, _ bool) { got = q }).
		Return(page, nil).Once()

	res, err := service.NewCreditService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

func TestCreditListDefaults(t *testing.T) {
	q, res := runCredits(t, service.ListCreditsParams{}, nil)

	require.Equal(t, model.CreditQuery{
		Filter: model.CreditFilter{DateFrom: date(2026, 10, 1), DateTo: date(2026, 10, 31)},
		Sort:   model.CreditSort{Key: model.CreditSortDate, Desc: true},
		Limit:  service.DefaultPageSize,
	}, q)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
	require.Equal(t, 50, res.Limit)
}

func TestCreditListDateRange(t *testing.T) {
	tests := []struct {
		name     string
		now      time.Time
		from, to string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{"defaulted month", october21, "", "", date(2026, 10, 1), date(2026, 10, 31)},
		{"february of a leap year", time.Date(2028, 2, 15, 0, 0, 0, 0, time.UTC), "", "", date(2028, 2, 1), date(2028, 2, 29)},
		{"month is taken in the clock's zone", time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("UTC+7", 7*3600)), "", "", date(2026, 11, 1), date(2026, 11, 30)},
		{"the same instant read in UTC is still October", time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("UTC+7", 7*3600)).UTC(), "", "", date(2026, 10, 1), date(2026, 10, 31)},
		{"explicit", october21, "2026-09-01", "2026-09-30", date(2026, 9, 1), date(2026, 9, 30)},
		{"same day", october21, "2026-10-05", "2026-10-05", date(2026, 10, 5), date(2026, 10, 5)},
		{"only from leaves the end open", october21, "2026-10-05", "", date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", october21, "", "2026-10-05", time.Time{}, date(2026, 10, 5)},
		{"whitespace is trimmed", october21, " 2026-10-05 ", " 2026-10-06 ", date(2026, 10, 5), date(2026, 10, 6)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := servicemocks.NewMockCreditRepository(t)
			var got model.CreditQuery
			repo.EXPECT().Page(mock.Anything, mock.Anything, true).
				Run(func(_ context.Context, q model.CreditQuery, _ bool) { got = q }).
				Return(model.CreditPage{}, nil).Once()

			res, err := service.NewCreditService(repo, fixedClock(tt.now)).List(context.Background(),
				service.ListCreditsParams{From: tt.from, To: tt.to})

			require.NoError(t, err)
			require.Equal(t, tt.wantFrom, got.Filter.DateFrom, "the credit date is a calendar date: no shifting")
			require.Equal(t, tt.wantTo, got.Filter.DateTo, "the last day is inclusive as given")
			require.Equal(t, tt.wantFrom, res.From)
			require.Equal(t, tt.wantTo, res.To)
		})
	}
}

func TestCreditListNormalizesFilters(t *testing.T) {
	q, _ := runCredits(t, service.ListCreditsParams{
		Method: []string{"CARD", " check", "", "card"},
		Search: "  stripe  ",
		Sort:   " Amount ",
		Order:  "ASC",
		Limit:  " 25 ",
	}, nil)

	require.Equal(t, []model.PaymentMethod{model.MethodCard, model.MethodCheck}, q.Filter.Methods, "trimmed, lower-cased, de-duplicated, blanks dropped")
	require.Equal(t, "stripe", q.Filter.Search, "trimmed")
	require.Equal(t, model.CreditSort{Key: model.CreditSortAmount}, q.Sort)
	require.Equal(t, 25, q.Limit)
}

func TestCreditListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.CreditSort
	}{
		{"nothing: newest first", "", "", model.CreditSort{Key: model.CreditSortDate, Desc: true}},
		{"only order asc", "", "asc", model.CreditSort{Key: model.CreditSortDate}},
		{"only order desc", "", "desc", model.CreditSort{Key: model.CreditSortDate, Desc: true}},
		{"only a key: ascending", "amount", "", model.CreditSort{Key: model.CreditSortAmount}},
		{"key and order", "customer", "desc", model.CreditSort{Key: model.CreditSortCustomer, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runCredits(t, service.ListCreditsParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}
	for _, key := range model.CreditSortKeys {
		t.Run("accepts "+string(key), func(t *testing.T) {
			q, _ := runCredits(t, service.ListCreditsParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		})
	}
}

func TestCreditListAcceptsEveryMethod(t *testing.T) {
	for _, m := range model.PaymentMethods {
		q, _ := runCredits(t, service.ListCreditsParams{Method: []string{string(m)}}, nil)
		require.Equal(t, []model.PaymentMethod{m}, q.Filter.Methods)
	}
}

func TestCreditListSearchAndLimitBoundaries(t *testing.T) {
	t.Run("search of exactly 100 characters", func(t *testing.T) {
		q, _ := runCredits(t, service.ListCreditsParams{Search: strings.Repeat("é", 100)}, nil)
		require.Len(t, []rune(q.Filter.Search), 100)
	})
	for _, limit := range []string{"1", "200"} {
		t.Run("limit "+limit, func(t *testing.T) {
			q, _ := runCredits(t, service.ListCreditsParams{Limit: limit}, nil)
			require.Contains(t, []int{1, 200}, q.Limit)
		})
	}
}

func TestCreditListRejectsInvalidInputPerField(t *testing.T) {
	manyMethods := make([]string, service.MaxEnumValues+1)
	for i := range manyMethods {
		manyMethods[i] = "cash"
	}

	tests := []struct {
		name    string
		params  service.ListCreditsParams
		field   string
		message string
	}{
		{"from wrong separator", service.ListCreditsParams{From: "2026/10/01"}, "from", "YYYY-MM-DD"},
		{"from impossible day", service.ListCreditsParams{From: "2026-02-30"}, "from", "YYYY-MM-DD"},
		{"to not a date", service.ListCreditsParams{To: "soon"}, "to", "YYYY-MM-DD"},
		{"to before from", service.ListCreditsParams{From: "2026-10-10", To: "2026-10-09"}, "to", "must not be before from"},

		{"method unknown", service.ListCreditsParams{Method: []string{"card", "bitcoin"}}, "method", "cash, check, card, other"},
		{"a payment status is not a method", service.ListCreditsParams{Method: []string{"paid"}}, "method", "cash, check, card, other"},
		{"a document status is not a method", service.ListCreditsParams{Method: []string{"signed"}}, "method", "cash, check, card, other"},
		{"method too many", service.ListCreditsParams{Method: manyMethods}, "method", "at most 10 values"},

		{"search with NUL", service.ListCreditsParams{Search: "abc\x00def"}, "q", "NUL"},
		{"search invalid UTF-8", service.ListCreditsParams{Search: "abc\xff"}, "q", "UTF-8"},
		{"search too long", service.ListCreditsParams{Search: strings.Repeat("a", 101)}, "q", "at most 100 characters"},

		{"sort unknown", service.ListCreditsParams{Sort: "id"}, "sort", "credit_date"},
		{"sort injection attempt", service.ListCreditsParams{Sort: "amount; DROP TABLE credits"}, "sort", "must be one of"},
		{"another report's sort key", service.ListCreditsParams{Sort: "payment_date"}, "sort", "credit_date"},
		{"sort by date is called credit_date", service.ListCreditsParams{Sort: "date"}, "sort", "credit_date"},
		{"sort by status does not exist", service.ListCreditsParams{Sort: "status"}, "sort", "credit_date"},
		{"order unknown", service.ListCreditsParams{Order: "up"}, "order", "asc or desc"},

		{"limit zero", service.ListCreditsParams{Limit: "0"}, "limit", "between 1 and 200"},
		{"limit above maximum", service.ListCreditsParams{Limit: "201"}, "limit", "between 1 and 200"},
		{"limit not a number", service.ListCreditsParams{Limit: "many"}, "limit", "between 1 and 200"},

		{"cursor garbage", service.ListCreditsParams{Cursor: "not-a-cursor!!"}, "cursor", "not a valid cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsCredit(t, tt.params)
			require.Contains(t, got, tt.field)
			require.Contains(t, got[tt.field], tt.message)
			require.Len(t, got, 1, "only the offending field should be reported")
		})
	}
}

func TestCreditErrorMessagesListOnlyTheCreditChoices(t *testing.T) {
	got := rejectsCredit(t, service.ListCreditsParams{Sort: "status", Method: []string{"wire"}})

	require.Equal(t, "must be one of: cash, check, card, other", got["method"])
	require.NotContains(t, got["sort"], "status")
	require.NotContains(t, got["sort"], "payment_date")
	require.Contains(t, got["sort"], "credit_date")
}

func TestCreditListReportsEveryProblemTogether(t *testing.T) {
	got := rejectsCredit(t, service.ListCreditsParams{
		From: "x", To: "y", Method: []string{"wire"},
		Search: strings.Repeat("a", 101), Sort: "status", Order: "up", Limit: "0",
	})

	require.ElementsMatch(t, []string{"from", "to", "method", "q", "sort", "order", "limit"}, keys(got))
}

func TestCreditListChecksTheCursorAgainstTheRequestedSort(t *testing.T) {
	sort := model.CreditSort{Key: model.CreditSortDate, Desc: true}
	cursor := service.EncodeCreditCursor(sort, model.CreditRow{ID: 8, SortValue: "2026-10-12"})

	t.Run("accepted with the same sort and passed to the repository", func(t *testing.T) {
		q, res := runCredits(t, service.ListCreditsParams{Sort: "credit_date", Order: "desc", Cursor: cursor}, nil)
		require.Equal(t, &model.CreditCursor{SortValue: "2026-10-12", ID: 8}, q.After)
		require.Nil(t, res.Summary)
	})

	t.Run("rejected for another key", func(t *testing.T) {
		got := rejectsCredit(t, service.ListCreditsParams{Sort: "method", Order: "desc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("rejected for another direction", func(t *testing.T) {
		got := rejectsCredit(t, service.ListCreditsParams{Sort: "credit_date", Order: "asc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("not decoded when the sort itself is invalid", func(t *testing.T) {
		got := rejectsCredit(t, service.ListCreditsParams{Sort: "bogus", Cursor: "garbage!"})
		require.Contains(t, got, "sort")
		require.NotContains(t, got, "cursor", "one root cause is reported once")
	})

	t.Run("a whitespace-only cursor means no cursor", func(t *testing.T) {
		q, res := runCredits(t, service.ListCreditsParams{Cursor: "   "}, nil)
		require.Nil(t, q.After)
		require.NotNil(t, res.Summary, "it is a first page")
	})
}
