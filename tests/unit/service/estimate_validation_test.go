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

// rejectsEstimate asserts that p is invalid and that the repository is never touched.
func rejectsEstimate(t *testing.T, p service.ListEstimatesParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockEstimateRepository(t) // no expectations: any call fails the test
	_, err := service.NewEstimateService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

// runEstimates calls List on a service whose repository returns rows, and returns the query it received.
func runEstimates(t *testing.T, p service.ListEstimatesParams, rows []model.EstimateRow) (model.EstimateQuery, *service.ListEstimatesResult) {
	t.Helper()

	repo := servicemocks.NewMockEstimateRepository(t)
	var got model.EstimateQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.EstimateQuery) { got = q }).
		Return(rows, nil).Once()
	if strings.TrimSpace(p.Cursor) == "" {
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.EstimateSummary{}, nil).Once()
	}

	res, err := service.NewEstimateService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

func TestEstimateListDefaults(t *testing.T) {
	q, res := runEstimates(t, service.ListEstimatesParams{}, nil)

	require.Equal(t, model.EstimateQuery{
		Filter: model.EstimateFilter{
			DateFrom:  date(2026, 10, 1),
			DateTo:    date(2026, 10, 31),
			Lifecycle: model.LifecycleActive,
		},
		Sort:  model.EstimateSort{Key: model.EstimateSortDate, Desc: true},
		Limit: service.DefaultPageSize,
	}, q)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
	require.Equal(t, 50, res.Limit)
}

func TestEstimateListDateRange(t *testing.T) {
	tests := []struct {
		name     string
		now      time.Time
		from, to string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{"defaulted month", october21, "", "", date(2026, 10, 1), date(2026, 10, 31)},
		{"february of a leap year", time.Date(2028, 2, 15, 0, 0, 0, 0, time.UTC), "", "", date(2028, 2, 1), date(2028, 2, 29)},
		{"december", time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), "", "", date(2026, 12, 1), date(2026, 12, 31)},
		{"month is taken in UTC", time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("UTC+7", 7*3600)), "", "", date(2026, 10, 1), date(2026, 10, 31)},
		{"explicit", october21, "2026-09-01", "2026-09-30", date(2026, 9, 1), date(2026, 9, 30)},
		{"same day", october21, "2026-10-05", "2026-10-05", date(2026, 10, 5), date(2026, 10, 5)},
		{"only from leaves the end open", october21, "2026-10-05", "", date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", october21, "", "2026-10-05", time.Time{}, date(2026, 10, 5)},
		{"whitespace is trimmed", october21, " 2026-10-05 ", " 2026-10-06 ", date(2026, 10, 5), date(2026, 10, 6)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := servicemocks.NewMockEstimateRepository(t)
			var got model.EstimateQuery
			repo.EXPECT().List(mock.Anything, mock.Anything).
				Run(func(_ context.Context, q model.EstimateQuery) { got = q }).
				Return(nil, nil).Once()
			repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.EstimateSummary{}, nil).Once()

			res, err := service.NewEstimateService(repo, fixedClock(tt.now)).List(context.Background(),
				service.ListEstimatesParams{From: tt.from, To: tt.to})

			require.NoError(t, err)
			require.Equal(t, tt.wantFrom, got.Filter.DateFrom)
			require.Equal(t, tt.wantTo, got.Filter.DateTo)
			require.Equal(t, tt.wantFrom, res.From)
			require.Equal(t, tt.wantTo, res.To)
		})
	}
}

func TestEstimateListNormalizesFilters(t *testing.T) {
	q, _ := runEstimates(t, service.ListEstimatesParams{
		Lifecycle:       " ARCHIVED ",
		Status:          []string{"Accepted", " sent ", "", "accepted"},
		SignatureStatus: []string{"SIGNED", " signed "},
		Search:          "  Saliba  ",
		Sort:            " Phone ",
		Order:           "ASC",
		Limit:           " 25 ",
	}, nil)

	require.Equal(t, model.LifecycleArchived, q.Filter.Lifecycle)
	require.Equal(t, []model.EstimateStatus{model.EstimateAccepted, model.EstimateSent}, q.Filter.Statuses, "trimmed, lower-cased, de-duplicated, blanks dropped")
	require.Equal(t, []model.SignatureStatus{model.SignatureSigned}, q.Filter.SignatureStatus)
	require.Equal(t, "Saliba", q.Filter.Search, "trimmed, case kept")
	require.Equal(t, model.EstimateSort{Key: model.EstimateSortPhone}, q.Sort)
	require.Equal(t, 25, q.Limit)
}

func TestEstimateListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.EstimateSort
	}{
		{"nothing: newest first", "", "", model.EstimateSort{Key: model.EstimateSortDate, Desc: true}},
		{"only order asc", "", "asc", model.EstimateSort{Key: model.EstimateSortDate}},
		{"only order desc", "", "desc", model.EstimateSort{Key: model.EstimateSortDate, Desc: true}},
		{"only a key: ascending", "total", "", model.EstimateSort{Key: model.EstimateSortTotal}},
		{"key and order", "created_by", "desc", model.EstimateSort{Key: model.EstimateSortCreatedBy, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runEstimates(t, service.ListEstimatesParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	for _, key := range model.EstimateSortKeys {
		t.Run("accepts "+string(key), func(t *testing.T) {
			q, _ := runEstimates(t, service.ListEstimatesParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		})
	}
}

func TestEstimateListSearchAndLimitBoundaries(t *testing.T) {
	t.Run("search of exactly 100 characters", func(t *testing.T) {
		q, _ := runEstimates(t, service.ListEstimatesParams{Search: strings.Repeat("é", 100)}, nil)
		require.Len(t, []rune(q.Filter.Search), 100)
	})
	for _, limit := range []string{"1", "200"} {
		t.Run("limit "+limit, func(t *testing.T) {
			q, _ := runEstimates(t, service.ListEstimatesParams{Limit: limit}, nil)
			require.Contains(t, []int{1, 200}, q.Limit)
		})
	}
}

func TestEstimateListRejectsInvalidInputPerField(t *testing.T) {
	manyStatuses := make([]string, service.MaxEnumValues+1)
	for i := range manyStatuses {
		manyStatuses[i] = "sent"
	}

	tests := []struct {
		name    string
		params  service.ListEstimatesParams
		field   string
		message string
	}{
		{"from wrong separator", service.ListEstimatesParams{From: "2026/10/01"}, "from", "YYYY-MM-DD"},
		{"from impossible day", service.ListEstimatesParams{From: "2026-02-30"}, "from", "YYYY-MM-DD"},
		{"to not a date", service.ListEstimatesParams{To: "soon"}, "to", "YYYY-MM-DD"},
		{"to before from", service.ListEstimatesParams{From: "2026-10-10", To: "2026-10-09"}, "to", "must not be before from"},

		{"lifecycle unknown", service.ListEstimatesParams{Lifecycle: "everything"}, "lifecycle", "active, archived, deleted"},

		{"status unknown", service.ListEstimatesParams{Status: []string{"sent", "overdue"}}, "status", "draft, sent, accepted, declined"},
		{"invoice-only status paid", service.ListEstimatesParams{Status: []string{"paid"}}, "status", "draft, sent, accepted, declined"},
		{"invoice-only status void", service.ListEstimatesParams{Status: []string{"void"}}, "status", "draft, sent, accepted, declined"},
		{"status too many", service.ListEstimatesParams{Status: manyStatuses}, "status", "at most 10 values"},
		{"signature status unknown", service.ListEstimatesParams{SignatureStatus: []string{"maybe"}}, "signature_status", "draft, pending, signed, declined"},

		{"search with NUL", service.ListEstimatesParams{Search: "abc\x00def"}, "q", "NUL"},
		{"search invalid UTF-8", service.ListEstimatesParams{Search: "abc\xff"}, "q", "UTF-8"},
		{"search too long", service.ListEstimatesParams{Search: strings.Repeat("a", 101)}, "q", "at most 100 characters"},

		{"sort unknown", service.ListEstimatesParams{Sort: "id"}, "sort", "account_number"},
		{"sort injection attempt", service.ListEstimatesParams{Sort: "date; DROP TABLE billing_documents"}, "sort", "must be one of"},
		{"invoice-only sort discount", service.ListEstimatesParams{Sort: "discount"}, "sort", "estimate_number"},
		{"invoice-only sort job", service.ListEstimatesParams{Sort: "job"}, "sort", "estimate_number"},
		{"invoice-only sort payment_terms", service.ListEstimatesParams{Sort: "payment_terms"}, "sort", "estimate_number"},
		{"invoice-only sort invoice_number", service.ListEstimatesParams{Sort: "invoice_number"}, "sort", "estimate_number"},
		{"order unknown", service.ListEstimatesParams{Order: "up"}, "order", "asc or desc"},

		{"limit zero", service.ListEstimatesParams{Limit: "0"}, "limit", "between 1 and 200"},
		{"limit above maximum", service.ListEstimatesParams{Limit: "201"}, "limit", "between 1 and 200"},
		{"limit not a number", service.ListEstimatesParams{Limit: "many"}, "limit", "between 1 and 200"},

		{"cursor garbage", service.ListEstimatesParams{Cursor: "not-a-cursor!!"}, "cursor", "not a valid cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsEstimate(t, tt.params)
			require.Contains(t, got, tt.field)
			require.Contains(t, got[tt.field], tt.message)
			require.Len(t, got, 1, "only the offending field should be reported")
		})
	}
}

func TestEstimateListDoesNotAcceptInvoiceSpecificValues(t *testing.T) {
	// The sort list in the message must be the estimate one, never the invoice one.
	got := rejectsEstimate(t, service.ListEstimatesParams{Sort: "discount"})
	require.NotContains(t, got["sort"], "discount")
	require.Contains(t, got["sort"], "phone")
	require.Contains(t, got["sort"], "created_by")

	got = rejectsEstimate(t, service.ListEstimatesParams{Status: []string{"paid"}})
	require.NotContains(t, got["status"], "paid")
}

func TestEstimateListReportsEveryProblemTogether(t *testing.T) {
	got := rejectsEstimate(t, service.ListEstimatesParams{
		From: "x", To: "y", Lifecycle: "z", Status: []string{"paid"}, SignatureStatus: []string{"q"},
		Search: strings.Repeat("a", 101), Sort: "discount", Order: "up", Limit: "0",
	})

	require.ElementsMatch(t, []string{"from", "to", "lifecycle", "status", "signature_status", "q", "sort", "order", "limit"}, keys(got))
}

func TestEstimateListChecksTheCursorAgainstTheRequestedSort(t *testing.T) {
	sort := model.EstimateSort{Key: model.EstimateSortTotal, Desc: true}
	cursor := service.EncodeEstimateCursor(sort, model.EstimateRow{ID: 8, SortValue: "24.40"})

	t.Run("accepted with the same sort and passed to the repository", func(t *testing.T) {
		q, res := runEstimates(t, service.ListEstimatesParams{Sort: "total", Order: "desc", Cursor: cursor}, nil)
		require.Equal(t, &model.EstimateCursor{SortValue: "24.40", ID: 8}, q.After)
		require.Nil(t, res.Summary)
	})

	t.Run("rejected for another key", func(t *testing.T) {
		got := rejectsEstimate(t, service.ListEstimatesParams{Sort: "date", Order: "desc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("rejected for another direction", func(t *testing.T) {
		got := rejectsEstimate(t, service.ListEstimatesParams{Sort: "total", Order: "asc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("not decoded when the sort itself is invalid", func(t *testing.T) {
		got := rejectsEstimate(t, service.ListEstimatesParams{Sort: "bogus", Cursor: "garbage!"})
		require.Contains(t, got, "sort")
		require.NotContains(t, got, "cursor", "one root cause is reported once")
	})

	t.Run("a whitespace-only cursor means no cursor", func(t *testing.T) {
		q, res := runEstimates(t, service.ListEstimatesParams{Cursor: "   "}, nil)
		require.Nil(t, q.After)
		require.NotNil(t, res.Summary, "it is a first page")
	})
}
