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

// rejectsPayment asserts that p is invalid and that the repository is never touched.
func rejectsPayment(t *testing.T, p service.ListPaymentsParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockPaymentRepository(t) // no expectations: any call fails the test
	_, err := service.NewPaymentService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

// runPayments calls List on a service whose repository returns rows, and returns the query it received.
func runPayments(t *testing.T, p service.ListPaymentsParams, rows []model.PaymentRow) (model.PaymentQuery, *service.ListPaymentsResult) {
	t.Helper()

	repo := servicemocks.NewMockPaymentRepository(t)
	var got model.PaymentQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.PaymentQuery) { got = q }).
		Return(rows, nil).Once()
	if strings.TrimSpace(p.Cursor) == "" {
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.PaymentSummary{}, nil).Once()
	}

	res, err := service.NewPaymentService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

func TestPaymentListDefaults(t *testing.T) {
	q, res := runPayments(t, service.ListPaymentsParams{}, nil)

	require.Equal(t, model.PaymentQuery{
		Filter: model.PaymentFilter{
			DateFrom:  date(2026, 10, 1),
			DateTo:    date(2026, 10, 31),
			Lifecycle: model.LifecycleActive,
		},
		Sort:  model.PaymentSort{Key: model.PaymentSortDate, Desc: true},
		Limit: service.DefaultPageSize,
	}, q)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
	require.Equal(t, 50, res.Limit)
}

func TestPaymentListDateRange(t *testing.T) {
	tests := []struct {
		name     string
		now      time.Time
		from, to string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{"defaulted month", october21, "", "", date(2026, 10, 1), date(2026, 10, 31)},
		{"february of a leap year", time.Date(2028, 2, 15, 0, 0, 0, 0, time.UTC), "", "", date(2028, 2, 1), date(2028, 2, 29)},
		{"month is taken in UTC", time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("UTC+7", 7*3600)), "", "", date(2026, 10, 1), date(2026, 10, 31)},
		{"explicit", october21, "2026-09-01", "2026-09-30", date(2026, 9, 1), date(2026, 9, 30)},
		{"same day", october21, "2026-10-05", "2026-10-05", date(2026, 10, 5), date(2026, 10, 5)},
		{"only from leaves the end open", october21, "2026-10-05", "", date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", october21, "", "2026-10-05", time.Time{}, date(2026, 10, 5)},
		{"whitespace is trimmed", october21, " 2026-10-05 ", " 2026-10-06 ", date(2026, 10, 5), date(2026, 10, 6)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := servicemocks.NewMockPaymentRepository(t)
			var got model.PaymentQuery
			repo.EXPECT().List(mock.Anything, mock.Anything).
				Run(func(_ context.Context, q model.PaymentQuery) { got = q }).
				Return(nil, nil).Once()
			repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.PaymentSummary{}, nil).Once()

			res, err := service.NewPaymentService(repo, fixedClock(tt.now)).List(context.Background(),
				service.ListPaymentsParams{From: tt.from, To: tt.to})

			require.NoError(t, err)
			require.Equal(t, tt.wantFrom, got.Filter.DateFrom, "the payment date is a calendar date: no shifting")
			require.Equal(t, tt.wantTo, got.Filter.DateTo, "the last day is inclusive as given")
			require.Equal(t, tt.wantFrom, res.From)
			require.Equal(t, tt.wantTo, res.To)
		})
	}
}

func TestPaymentListNormalizesFilters(t *testing.T) {
	q, _ := runPayments(t, service.ListPaymentsParams{
		Lifecycle: " DELETED ",
		Status:    []string{"Failed", " paid ", "", "failed"},
		Method:    []string{"CARD", " check", "", "card"},
		Search:    "  85004  ",
		Sort:      " Amount ",
		Order:     "ASC",
		Limit:     " 25 ",
	}, nil)

	require.Equal(t, model.LifecycleDeleted, q.Filter.Lifecycle)
	require.Equal(t, []model.PaymentStatus{model.PaymentFailed, model.PaymentPaid}, q.Filter.Statuses, "trimmed, lower-cased, de-duplicated, blanks dropped")
	require.Equal(t, []model.PaymentMethod{model.MethodCard, model.MethodCheck}, q.Filter.Methods)
	require.Equal(t, "85004", q.Filter.Search, "trimmed")
	require.Equal(t, model.PaymentSort{Key: model.PaymentSortAmount}, q.Sort)
	require.Equal(t, 25, q.Limit)
}

func TestPaymentListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.PaymentSort
	}{
		{"nothing: newest first", "", "", model.PaymentSort{Key: model.PaymentSortDate, Desc: true}},
		{"only order asc", "", "asc", model.PaymentSort{Key: model.PaymentSortDate}},
		{"only order desc", "", "desc", model.PaymentSort{Key: model.PaymentSortDate, Desc: true}},
		{"only a key: ascending", "amount", "", model.PaymentSort{Key: model.PaymentSortAmount}},
		{"key and order", "customer", "desc", model.PaymentSort{Key: model.PaymentSortCustomer, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runPayments(t, service.ListPaymentsParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}
	for _, key := range model.PaymentSortKeys {
		t.Run("accepts "+string(key), func(t *testing.T) {
			q, _ := runPayments(t, service.ListPaymentsParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		})
	}
}

func TestPaymentListAcceptsEveryStatusMethodAndTab(t *testing.T) {
	for _, s := range model.PaymentStatuses {
		q, _ := runPayments(t, service.ListPaymentsParams{Status: []string{string(s)}}, nil)
		require.Equal(t, []model.PaymentStatus{s}, q.Filter.Statuses)
	}
	for _, m := range model.PaymentMethods {
		q, _ := runPayments(t, service.ListPaymentsParams{Method: []string{string(m)}}, nil)
		require.Equal(t, []model.PaymentMethod{m}, q.Filter.Methods)
	}
	for _, l := range model.PaymentLifecycles {
		q, _ := runPayments(t, service.ListPaymentsParams{Lifecycle: string(l)}, nil)
		require.Equal(t, l, q.Filter.Lifecycle)
	}
}

func TestPaymentListSearchAndLimitBoundaries(t *testing.T) {
	t.Run("search of exactly 100 characters", func(t *testing.T) {
		q, _ := runPayments(t, service.ListPaymentsParams{Search: strings.Repeat("é", 100)}, nil)
		require.Len(t, []rune(q.Filter.Search), 100)
	})
	for _, limit := range []string{"1", "200"} {
		t.Run("limit "+limit, func(t *testing.T) {
			q, _ := runPayments(t, service.ListPaymentsParams{Limit: limit}, nil)
			require.Contains(t, []int{1, 200}, q.Limit)
		})
	}
}

func TestPaymentListRejectsInvalidInputPerField(t *testing.T) {
	manyStatuses := make([]string, service.MaxEnumValues+1)
	for i := range manyStatuses {
		manyStatuses[i] = "paid"
	}

	tests := []struct {
		name    string
		params  service.ListPaymentsParams
		field   string
		message string
	}{
		{"from wrong separator", service.ListPaymentsParams{From: "2026/10/01"}, "from", "YYYY-MM-DD"},
		{"from impossible day", service.ListPaymentsParams{From: "2026-02-30"}, "from", "YYYY-MM-DD"},
		{"to not a date", service.ListPaymentsParams{To: "soon"}, "to", "YYYY-MM-DD"},
		{"to before from", service.ListPaymentsParams{From: "2026-10-10", To: "2026-10-09"}, "to", "must not be before from"},

		{"lifecycle unknown", service.ListPaymentsParams{Lifecycle: "everything"}, "lifecycle", "active, deleted"},
		{"payments cannot be archived", service.ListPaymentsParams{Lifecycle: "archived"}, "lifecycle", "active, deleted"},

		{"status unknown", service.ListPaymentsParams{Status: []string{"paid", "overdue"}}, "status", "paid, pending, failed, refunded"},
		{"document status draft", service.ListPaymentsParams{Status: []string{"draft"}}, "status", "paid, pending, failed, refunded"},
		{"document status signed", service.ListPaymentsParams{Status: []string{"signed"}}, "status", "paid, pending, failed, refunded"},
		{"invoice status void", service.ListPaymentsParams{Status: []string{"void"}}, "status", "paid, pending, failed, refunded"},
		{"estimate status accepted", service.ListPaymentsParams{Status: []string{"accepted"}}, "status", "paid, pending, failed, refunded"},
		{"status too many", service.ListPaymentsParams{Status: manyStatuses}, "status", "at most 10 values"},

		{"method unknown", service.ListPaymentsParams{Method: []string{"card", "bitcoin"}}, "method", "cash, check, card, other"},
		{"method too many", service.ListPaymentsParams{Method: manyStatuses}, "method", "at most 10 values"},

		{"search with NUL", service.ListPaymentsParams{Search: "abc\x00def"}, "q", "NUL"},
		{"search invalid UTF-8", service.ListPaymentsParams{Search: "abc\xff"}, "q", "UTF-8"},
		{"search too long", service.ListPaymentsParams{Search: strings.Repeat("a", 101)}, "q", "at most 100 characters"},

		{"sort unknown", service.ListPaymentsParams{Sort: "id"}, "sort", "payment_for"},
		{"sort injection attempt", service.ListPaymentsParams{Sort: "amount; DROP TABLE payments"}, "sort", "must be one of"},
		{"another report's sort key", service.ListPaymentsParams{Sort: "document_number"}, "sort", "payment_for"},
		{"sort by date is called payment_date", service.ListPaymentsParams{Sort: "date"}, "sort", "payment_date"},
		{"order unknown", service.ListPaymentsParams{Order: "up"}, "order", "asc or desc"},

		{"limit zero", service.ListPaymentsParams{Limit: "0"}, "limit", "between 1 and 200"},
		{"limit above maximum", service.ListPaymentsParams{Limit: "201"}, "limit", "between 1 and 200"},
		{"limit not a number", service.ListPaymentsParams{Limit: "many"}, "limit", "between 1 and 200"},

		{"cursor garbage", service.ListPaymentsParams{Cursor: "not-a-cursor!!"}, "cursor", "not a valid cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsPayment(t, tt.params)
			require.Contains(t, got, tt.field)
			require.Contains(t, got[tt.field], tt.message)
			require.Len(t, got, 1, "only the offending field should be reported")
		})
	}
}

func TestPaymentErrorMessagesListOnlyThePaymentChoices(t *testing.T) {
	got := rejectsPayment(t, service.ListPaymentsParams{
		Sort: "discount", Status: []string{"signed"}, Method: []string{"wire"}, Lifecycle: "archived",
	})

	require.Equal(t, "must be one of: paid, pending, failed, refunded", got["status"])
	require.Equal(t, "must be one of: cash, check, card, other", got["method"])
	require.Equal(t, "must be one of: active, deleted", got["lifecycle"])
	require.NotContains(t, got["sort"], "discount")
	require.Contains(t, got["sort"], "payment_date")
}

func TestPaymentListReportsEveryProblemTogether(t *testing.T) {
	got := rejectsPayment(t, service.ListPaymentsParams{
		From: "x", To: "y", Lifecycle: "z", Status: []string{"draft"}, Method: []string{"wire"},
		Search: strings.Repeat("a", 101), Sort: "discount", Order: "up", Limit: "0",
	})

	require.ElementsMatch(t, []string{"from", "to", "lifecycle", "status", "method", "q", "sort", "order", "limit"}, keys(got))
}

func TestPaymentListChecksTheCursorAgainstTheRequestedSort(t *testing.T) {
	sort := model.PaymentSort{Key: model.PaymentSortDate, Desc: true}
	cursor := service.EncodePaymentCursor(sort, model.PaymentRow{ID: 8, SortValue: "2026-10-12"})

	t.Run("accepted with the same sort and passed to the repository", func(t *testing.T) {
		q, res := runPayments(t, service.ListPaymentsParams{Sort: "payment_date", Order: "desc", Cursor: cursor}, nil)
		require.Equal(t, &model.PaymentCursor{SortValue: "2026-10-12", ID: 8}, q.After)
		require.Nil(t, res.Summary)
	})

	t.Run("rejected for another key", func(t *testing.T) {
		got := rejectsPayment(t, service.ListPaymentsParams{Sort: "status", Order: "desc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("rejected for another direction", func(t *testing.T) {
		got := rejectsPayment(t, service.ListPaymentsParams{Sort: "payment_date", Order: "asc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("not decoded when the sort itself is invalid", func(t *testing.T) {
		got := rejectsPayment(t, service.ListPaymentsParams{Sort: "bogus", Cursor: "garbage!"})
		require.Contains(t, got, "sort")
		require.NotContains(t, got, "cursor", "one root cause is reported once")
	})

	t.Run("a whitespace-only cursor means no cursor", func(t *testing.T) {
		q, res := runPayments(t, service.ListPaymentsParams{Cursor: "   "}, nil)
		require.Nil(t, q.After)
		require.NotNil(t, res.Summary, "it is a first page")
	})
}
