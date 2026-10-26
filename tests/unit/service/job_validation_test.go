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

// rejectsJob asserts that p is invalid and that the repository is never touched.
func rejectsJob(t *testing.T, p service.ListJobsParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockJobRepository(t) // no expectations: any call fails the test
	_, err := service.NewJobService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

// runJobs calls List on a service whose repository returns rows, and returns the query it received.
func runJobs(t *testing.T, p service.ListJobsParams, rows []model.JobRow) (model.JobQuery, *service.ListJobsResult) {
	t.Helper()

	repo := servicemocks.NewMockJobRepository(t)
	var got model.JobQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.JobQuery) { got = q }).
		Return(rows, nil).Once()
	if strings.TrimSpace(p.Cursor) == "" {
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.JobSummary{}, nil).Once()
	}

	res, err := service.NewJobService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

func TestJobListDefaults(t *testing.T) {
	q, res := runJobs(t, service.ListJobsParams{}, nil)

	require.Equal(t, model.JobQuery{
		Filter: model.JobFilter{
			CreatedFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			CreatedBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		},
		Sort:  model.JobSort{Key: model.JobSortCreatedAt, Desc: true},
		Limit: service.DefaultPageSize,
	}, q)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To, "the response shows the inclusive last day")
	require.Equal(t, 50, res.Limit)
}

func TestJobListDateRange(t *testing.T) {
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
		{"month is taken in the clock's zone", time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("UTC+7", 7*3600)), "", "", date(2026, 11, 1), date(2026, 12, 1), date(2026, 11, 1), date(2026, 11, 30)},
		{"the same instant read in UTC is still October", time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("UTC+7", 7*3600)).UTC(), "", "", date(2026, 10, 1), date(2026, 11, 1), date(2026, 10, 1), date(2026, 10, 31)},
		{"explicit", october21, "2026-09-01", "2026-09-30", date(2026, 9, 1), date(2026, 10, 1), date(2026, 9, 1), date(2026, 9, 30)},
		{"same day covers 24 hours", october21, "2026-10-05", "2026-10-05", date(2026, 10, 5), date(2026, 10, 6), date(2026, 10, 5), date(2026, 10, 5)},
		{"only from leaves the end open", october21, "2026-10-05", "", date(2026, 10, 5), time.Time{}, date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", october21, "", "2026-10-05", time.Time{}, date(2026, 10, 6), time.Time{}, date(2026, 10, 5)},
		{"whitespace is trimmed", october21, " 2026-10-05 ", " 2026-10-06 ", date(2026, 10, 5), date(2026, 10, 7), date(2026, 10, 5), date(2026, 10, 6)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := servicemocks.NewMockJobRepository(t)
			var got model.JobQuery
			repo.EXPECT().List(mock.Anything, mock.Anything).
				Run(func(_ context.Context, q model.JobQuery) { got = q }).
				Return(nil, nil).Once()
			repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.JobSummary{}, nil).Once()

			res, err := service.NewJobService(repo, fixedClock(tt.now)).List(context.Background(),
				service.ListJobsParams{From: tt.from, To: tt.to})

			require.NoError(t, err)
			require.Equal(t, tt.wantFrom, got.Filter.CreatedFrom)
			require.Equal(t, tt.wantBefore, got.Filter.CreatedBefore)
			require.Equal(t, tt.respFrom, res.From)
			require.Equal(t, tt.respTo, res.To)
		})
	}
}

func TestJobListNormalizesFilters(t *testing.T) {
	q, _ := runJobs(t, service.ListJobsParams{
		Status:         []string{"Completed", " cancelled ", "", "completed"},
		ServiceTypeIDs: []string{"3", " 1 ", "", "3"},
		LeadSourceIDs:  []string{"2", "2", " 5"},
		Search:         "  Saliba  ",
		Sort:           " Source ",
		Order:          "ASC",
		Limit:          " 25 ",
	}, nil)

	require.Equal(t, []model.JobStatus{model.JobCompleted, model.JobCancelled}, q.Filter.Statuses, "trimmed, lower-cased, de-duplicated, blanks dropped")
	require.Equal(t, []int64{3, 1}, q.Filter.ServiceTypeIDs)
	require.Equal(t, []int64{2, 5}, q.Filter.LeadSourceIDs)
	require.Equal(t, "Saliba", q.Filter.Search, "trimmed, case kept")
	require.Equal(t, model.JobSort{Key: model.JobSortSource}, q.Sort)
	require.Equal(t, 25, q.Limit)
}

func TestJobListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.JobSort
	}{
		{"nothing: newest first", "", "", model.JobSort{Key: model.JobSortCreatedAt, Desc: true}},
		{"only order asc", "", "asc", model.JobSort{Key: model.JobSortCreatedAt}},
		{"only order desc", "", "desc", model.JobSort{Key: model.JobSortCreatedAt, Desc: true}},
		{"only a key: ascending", "status", "", model.JobSort{Key: model.JobSortStatus}},
		{"key and order", "invoice_value", "desc", model.JobSort{Key: model.JobSortInvoiceValue, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runJobs(t, service.ListJobsParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	for _, key := range model.JobSortKeys {
		t.Run("accepts "+string(key), func(t *testing.T) {
			q, _ := runJobs(t, service.ListJobsParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		})
	}
}

func TestJobListSearchAndLimitBoundaries(t *testing.T) {
	t.Run("search of exactly 100 characters", func(t *testing.T) {
		q, _ := runJobs(t, service.ListJobsParams{Search: strings.Repeat("é", 100)}, nil)
		require.Len(t, []rune(q.Filter.Search), 100)
	})
	for _, limit := range []string{"1", "200"} {
		t.Run("limit "+limit, func(t *testing.T) {
			q, _ := runJobs(t, service.ListJobsParams{Limit: limit}, nil)
			require.Contains(t, []int{1, 200}, q.Limit)
		})
	}
}

func TestJobListRejectsInvalidInputPerField(t *testing.T) {
	manyIDs := make([]string, service.MaxIDsPerFilter+1)
	for i := range manyIDs {
		manyIDs[i] = "1"
	}
	manyStatuses := make([]string, service.MaxEnumValues+1)
	for i := range manyStatuses {
		manyStatuses[i] = "confirmed"
	}

	tests := []struct {
		name    string
		params  service.ListJobsParams
		field   string
		message string
	}{
		{"from wrong separator", service.ListJobsParams{From: "2026/10/01"}, "from", "YYYY-MM-DD"},
		{"from impossible day", service.ListJobsParams{From: "2026-02-30"}, "from", "YYYY-MM-DD"},
		{"to not a date", service.ListJobsParams{To: "soon"}, "to", "YYYY-MM-DD"},
		{"to before from", service.ListJobsParams{From: "2026-10-10", To: "2026-10-09"}, "to", "must not be before from"},

		{"status unknown", service.ListJobsParams{Status: []string{"confirmed", "overdue"}}, "status", "unconfirmed, confirmed, completed, cancelled"},
		{"invoice status paid", service.ListJobsParams{Status: []string{"paid"}}, "status", "unconfirmed, confirmed, completed, cancelled"},
		{"status too many", service.ListJobsParams{Status: manyStatuses}, "status", "at most 10 values"},

		{"service type id not a number", service.ListJobsParams{ServiceTypeIDs: []string{"abc"}}, "service_type_id", "positive whole numbers"},
		{"service type id zero", service.ListJobsParams{ServiceTypeIDs: []string{"0"}}, "service_type_id", "positive whole numbers"},
		{"service type id overflows", service.ListJobsParams{ServiceTypeIDs: []string{"99999999999999999999"}}, "service_type_id", "positive whole numbers"},
		{"too many service type ids", service.ListJobsParams{ServiceTypeIDs: manyIDs}, "service_type_id", "at most 100 values"},
		{"lead source id negative", service.ListJobsParams{LeadSourceIDs: []string{"-1"}}, "lead_source_id", "positive whole numbers"},
		{"one bad lead source among good", service.ListJobsParams{LeadSourceIDs: []string{"1", "x"}}, "lead_source_id", "positive whole numbers"},
		{"too many lead source ids", service.ListJobsParams{LeadSourceIDs: manyIDs}, "lead_source_id", "at most 100 values"},

		{"search with NUL", service.ListJobsParams{Search: "abc\x00def"}, "q", "NUL"},
		{"search invalid UTF-8", service.ListJobsParams{Search: "abc\xff"}, "q", "UTF-8"},
		{"search too long", service.ListJobsParams{Search: strings.Repeat("a", 101)}, "q", "at most 100 characters"},

		{"sort unknown", service.ListJobsParams{Sort: "id"}, "sort", "invoice_value"},
		{"sort injection attempt", service.ListJobsParams{Sort: "created_at; DROP TABLE jobs"}, "sort", "must be one of"},
		{"another report's sort", service.ListJobsParams{Sort: "document_number"}, "sort", "invoice_value"},
		{"order unknown", service.ListJobsParams{Order: "up"}, "order", "asc or desc"},

		{"limit zero", service.ListJobsParams{Limit: "0"}, "limit", "between 1 and 200"},
		{"limit above maximum", service.ListJobsParams{Limit: "201"}, "limit", "between 1 and 200"},
		{"limit not a number", service.ListJobsParams{Limit: "many"}, "limit", "between 1 and 200"},

		{"cursor garbage", service.ListJobsParams{Cursor: "not-a-cursor!!"}, "cursor", "not a valid cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsJob(t, tt.params)
			require.Contains(t, got, tt.field)
			require.Contains(t, got[tt.field], tt.message)
			require.Len(t, got, 1, "only the offending field should be reported")
		})
	}
}

func TestJobErrorMessagesListOnlyTheJobChoices(t *testing.T) {
	got := rejectsJob(t, service.ListJobsParams{Sort: "discount", Status: []string{"paid"}})

	require.Equal(t, "must be one of: unconfirmed, confirmed, completed, cancelled", got["status"])
	require.NotContains(t, got["sort"], "discount")
	require.Contains(t, got["sort"], "created_at")
	require.Contains(t, got["sort"], "account_number")
}

func TestJobListReportsEveryProblemTogether(t *testing.T) {
	got := rejectsJob(t, service.ListJobsParams{
		From: "x", To: "y", Status: []string{"paid"}, ServiceTypeIDs: []string{"q"}, LeadSourceIDs: []string{"r"},
		Search: strings.Repeat("a", 101), Sort: "discount", Order: "up", Limit: "0",
	})

	require.ElementsMatch(t, []string{"from", "to", "status", "service_type_id", "lead_source_id", "q", "sort", "order", "limit"}, keys(got))
}

func TestJobListChecksTheCursorAgainstTheRequestedSort(t *testing.T) {
	sort := model.JobSort{Key: model.JobSortCreatedAt, Desc: true}
	cursor := service.EncodeJobCursor(sort, model.JobRow{ID: 8, SortValue: "2026-10-12T08:00:00.000000Z"})

	t.Run("accepted with the same sort and passed to the repository", func(t *testing.T) {
		q, res := runJobs(t, service.ListJobsParams{Sort: "created_at", Order: "desc", Cursor: cursor}, nil)
		require.Equal(t, &model.JobCursor{SortValue: "2026-10-12T08:00:00.000000Z", ID: 8}, q.After)
		require.Nil(t, res.Summary)
	})

	t.Run("rejected for another key", func(t *testing.T) {
		got := rejectsJob(t, service.ListJobsParams{Sort: "status", Order: "desc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("rejected for another direction", func(t *testing.T) {
		got := rejectsJob(t, service.ListJobsParams{Sort: "created_at", Order: "asc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("not decoded when the sort itself is invalid", func(t *testing.T) {
		got := rejectsJob(t, service.ListJobsParams{Sort: "bogus", Cursor: "garbage!"})
		require.Contains(t, got, "sort")
		require.NotContains(t, got, "cursor", "one root cause is reported once")
	})

	t.Run("a whitespace-only cursor means no cursor", func(t *testing.T) {
		q, res := runJobs(t, service.ListJobsParams{Cursor: "   "}, nil)
		require.Nil(t, q.After)
		require.NotNil(t, res.Summary, "it is a first page")
	})
}
