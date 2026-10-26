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

// rejectsDocument asserts that p is invalid and that the repository is never touched.
func rejectsDocument(t *testing.T, p service.ListDocumentsParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockDocumentRepository(t) // no expectations: any call fails the test
	_, err := service.NewDocumentService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

// runDocuments calls List on a service whose repository returns rows, and returns the query it received.
func runDocuments(t *testing.T, p service.ListDocumentsParams, rows []model.DocumentRow) (model.DocumentQuery, *service.ListDocumentsResult) {
	t.Helper()

	repo := servicemocks.NewMockDocumentRepository(t)
	var got model.DocumentQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.DocumentQuery) { got = q }).
		Return(rows, nil).Once()
	if strings.TrimSpace(p.Cursor) == "" {
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.DocumentSummary{}, nil).Once()
	}

	res, err := service.NewDocumentService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

func TestDocumentListDefaults(t *testing.T) {
	q, res := runDocuments(t, service.ListDocumentsParams{}, nil)

	require.Equal(t, model.DocumentQuery{
		Filter: model.DocumentFilter{
			CreatedFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			CreatedBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			Lifecycle:     model.LifecycleActive,
		},
		Sort:  model.DocumentSort{Key: model.DocumentSortCreatedAt, Desc: true},
		Limit: service.DefaultPageSize,
	}, q)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To, "the response shows the inclusive last day")
	require.Equal(t, 50, res.Limit)
}

func TestDocumentListDateRange(t *testing.T) {
	tests := []struct {
		name       string
		now        time.Time
		from, to   string
		wantFrom   time.Time // the filter's first instant
		wantBefore time.Time // the filter's first excluded instant
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
			repo := servicemocks.NewMockDocumentRepository(t)
			var got model.DocumentQuery
			repo.EXPECT().List(mock.Anything, mock.Anything).
				Run(func(_ context.Context, q model.DocumentQuery) { got = q }).
				Return(nil, nil).Once()
			repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.DocumentSummary{}, nil).Once()

			res, err := service.NewDocumentService(repo, fixedClock(tt.now)).List(context.Background(),
				service.ListDocumentsParams{From: tt.from, To: tt.to})

			require.NoError(t, err)
			require.Equal(t, tt.wantFrom, got.Filter.CreatedFrom)
			require.Equal(t, tt.wantBefore, got.Filter.CreatedBefore)
			require.Equal(t, tt.respFrom, res.From)
			require.Equal(t, tt.respTo, res.To)
		})
	}
}

func TestDocumentListNormalizesFilters(t *testing.T) {
	q, _ := runDocuments(t, service.ListDocumentsParams{
		Lifecycle:       " DELETED ",
		Status:          []string{"Signed", " sent ", "", "signed"},
		DocumentTypeIDs: []string{"3", " 1 ", "", "3"},
		Search:          "  Saliba  ",
		Sort:            " Type ",
		Order:           "ASC",
		Limit:           " 25 ",
	}, nil)

	require.Equal(t, model.LifecycleDeleted, q.Filter.Lifecycle)
	require.Equal(t, []model.DocumentStatus{model.DocumentSigned, model.DocumentSent}, q.Filter.Statuses, "trimmed, lower-cased, de-duplicated, blanks dropped")
	require.Equal(t, []int64{3, 1}, q.Filter.TypeIDs)
	require.Equal(t, "Saliba", q.Filter.Search, "trimmed, case kept")
	require.Equal(t, model.DocumentSort{Key: model.DocumentSortType}, q.Sort)
	require.Equal(t, 25, q.Limit)
}

func TestDocumentListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.DocumentSort
	}{
		{"nothing: newest first", "", "", model.DocumentSort{Key: model.DocumentSortCreatedAt, Desc: true}},
		{"only order asc", "", "asc", model.DocumentSort{Key: model.DocumentSortCreatedAt}},
		{"only order desc", "", "desc", model.DocumentSort{Key: model.DocumentSortCreatedAt, Desc: true}},
		{"only a key: ascending", "status", "", model.DocumentSort{Key: model.DocumentSortStatus}},
		{"key and order", "customer", "desc", model.DocumentSort{Key: model.DocumentSortCustomer, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runDocuments(t, service.ListDocumentsParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	for _, key := range model.DocumentSortKeys {
		t.Run("accepts "+string(key), func(t *testing.T) {
			q, _ := runDocuments(t, service.ListDocumentsParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		})
	}
}

func TestDocumentListSearchAndLimitBoundaries(t *testing.T) {
	t.Run("search of exactly 100 characters", func(t *testing.T) {
		q, _ := runDocuments(t, service.ListDocumentsParams{Search: strings.Repeat("é", 100)}, nil)
		require.Len(t, []rune(q.Filter.Search), 100)
	})
	for _, limit := range []string{"1", "200"} {
		t.Run("limit "+limit, func(t *testing.T) {
			q, _ := runDocuments(t, service.ListDocumentsParams{Limit: limit}, nil)
			require.Contains(t, []int{1, 200}, q.Limit)
		})
	}
}

func TestDocumentListRejectsInvalidInputPerField(t *testing.T) {
	manyIDs := make([]string, service.MaxIDsPerFilter+1)
	for i := range manyIDs {
		manyIDs[i] = "1"
	}
	manyStatuses := make([]string, service.MaxEnumValues+1)
	for i := range manyStatuses {
		manyStatuses[i] = "sent"
	}

	tests := []struct {
		name    string
		params  service.ListDocumentsParams
		field   string
		message string
	}{
		{"from wrong separator", service.ListDocumentsParams{From: "2026/10/01"}, "from", "YYYY-MM-DD"},
		{"from impossible day", service.ListDocumentsParams{From: "2026-02-30"}, "from", "YYYY-MM-DD"},
		{"to not a date", service.ListDocumentsParams{To: "soon"}, "to", "YYYY-MM-DD"},
		{"to before from", service.ListDocumentsParams{From: "2026-10-10", To: "2026-10-09"}, "to", "must not be before from"},

		{"lifecycle unknown", service.ListDocumentsParams{Lifecycle: "everything"}, "lifecycle", "active, deleted"},
		{"documents cannot be archived", service.ListDocumentsParams{Lifecycle: "archived"}, "lifecycle", "active, deleted"},

		{"status unknown", service.ListDocumentsParams{Status: []string{"sent", "overdue"}}, "status", "draft, sent, signed"},
		{"invoice status paid", service.ListDocumentsParams{Status: []string{"paid"}}, "status", "draft, sent, signed"},
		{"estimate status accepted", service.ListDocumentsParams{Status: []string{"accepted"}}, "status", "draft, sent, signed"},
		{"status too many", service.ListDocumentsParams{Status: manyStatuses}, "status", "at most 10 values"},

		{"type id not a number", service.ListDocumentsParams{DocumentTypeIDs: []string{"abc"}}, "document_type_id", "positive whole numbers"},
		{"type id zero", service.ListDocumentsParams{DocumentTypeIDs: []string{"0"}}, "document_type_id", "positive whole numbers"},
		{"type id negative", service.ListDocumentsParams{DocumentTypeIDs: []string{"-1"}}, "document_type_id", "positive whole numbers"},
		{"type id decimal", service.ListDocumentsParams{DocumentTypeIDs: []string{"1.5"}}, "document_type_id", "positive whole numbers"},
		{"type id overflows int64", service.ListDocumentsParams{DocumentTypeIDs: []string{"99999999999999999999"}}, "document_type_id", "positive whole numbers"},
		{"one bad id among good", service.ListDocumentsParams{DocumentTypeIDs: []string{"1", "2", "x"}}, "document_type_id", "positive whole numbers"},
		{"too many ids", service.ListDocumentsParams{DocumentTypeIDs: manyIDs}, "document_type_id", "at most 100 values"},

		{"search with NUL", service.ListDocumentsParams{Search: "abc\x00def"}, "q", "NUL"},
		{"search invalid UTF-8", service.ListDocumentsParams{Search: "abc\xff"}, "q", "UTF-8"},
		{"search too long", service.ListDocumentsParams{Search: strings.Repeat("a", 101)}, "q", "at most 100 characters"},

		{"sort unknown", service.ListDocumentsParams{Sort: "id"}, "sort", "document_number"},
		{"sort injection attempt", service.ListDocumentsParams{Sort: "created_at; DROP TABLE documents"}, "sort", "must be one of"},
		{"estimate-only sort phone", service.ListDocumentsParams{Sort: "phone"}, "sort", "document_number"},
		{"invoice-only sort discount", service.ListDocumentsParams{Sort: "discount"}, "sort", "document_number"},
		{"sort by date is called created_at", service.ListDocumentsParams{Sort: "date"}, "sort", "created_at"},
		{"order unknown", service.ListDocumentsParams{Order: "up"}, "order", "asc or desc"},

		{"limit zero", service.ListDocumentsParams{Limit: "0"}, "limit", "between 1 and 200"},
		{"limit above maximum", service.ListDocumentsParams{Limit: "201"}, "limit", "between 1 and 200"},
		{"limit not a number", service.ListDocumentsParams{Limit: "many"}, "limit", "between 1 and 200"},

		{"cursor garbage", service.ListDocumentsParams{Cursor: "not-a-cursor!!"}, "cursor", "not a valid cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsDocument(t, tt.params)
			require.Contains(t, got, tt.field)
			require.Contains(t, got[tt.field], tt.message)
			require.Len(t, got, 1, "only the offending field should be reported")
		})
	}
}

func TestDocumentErrorMessagesListOnlyTheDocumentChoices(t *testing.T) {
	got := rejectsDocument(t, service.ListDocumentsParams{Sort: "discount", Status: []string{"paid"}, Lifecycle: "archived"})

	require.Equal(t, "must be one of: draft, sent, signed", got["status"])
	require.Equal(t, "must be one of: active, deleted", got["lifecycle"])
	require.NotContains(t, got["sort"], "discount")
	require.NotContains(t, got["sort"], "phone")
	require.Contains(t, got["sort"], "created_at")
}

func TestDocumentListReportsEveryProblemTogether(t *testing.T) {
	got := rejectsDocument(t, service.ListDocumentsParams{
		From: "x", To: "y", Lifecycle: "z", Status: []string{"paid"}, DocumentTypeIDs: []string{"q"},
		Search: strings.Repeat("a", 101), Sort: "discount", Order: "up", Limit: "0",
	})

	require.ElementsMatch(t, []string{"from", "to", "lifecycle", "status", "document_type_id", "q", "sort", "order", "limit"}, keys(got))
}

func TestDocumentListChecksTheCursorAgainstTheRequestedSort(t *testing.T) {
	sort := model.DocumentSort{Key: model.DocumentSortCreatedAt, Desc: true}
	cursor := service.EncodeDocumentCursor(sort, model.DocumentRow{ID: 8, SortValue: "2026-10-12T08:00:00.000000Z"})

	t.Run("accepted with the same sort and passed to the repository", func(t *testing.T) {
		q, res := runDocuments(t, service.ListDocumentsParams{Sort: "created_at", Order: "desc", Cursor: cursor}, nil)
		require.Equal(t, &model.DocumentCursor{SortValue: "2026-10-12T08:00:00.000000Z", ID: 8}, q.After)
		require.Nil(t, res.Summary)
	})

	t.Run("rejected for another key", func(t *testing.T) {
		got := rejectsDocument(t, service.ListDocumentsParams{Sort: "status", Order: "desc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("rejected for another direction", func(t *testing.T) {
		got := rejectsDocument(t, service.ListDocumentsParams{Sort: "created_at", Order: "asc", Cursor: cursor})
		require.Contains(t, got["cursor"], "does not match the requested sort")
	})

	t.Run("not decoded when the sort itself is invalid", func(t *testing.T) {
		got := rejectsDocument(t, service.ListDocumentsParams{Sort: "bogus", Cursor: "garbage!"})
		require.Contains(t, got, "sort")
		require.NotContains(t, got, "cursor", "one root cause is reported once")
	})

	t.Run("a whitespace-only cursor means no cursor", func(t *testing.T) {
		q, res := runDocuments(t, service.ListDocumentsParams{Cursor: "   "}, nil)
		require.Nil(t, q.After)
		require.NotNil(t, res.Summary, "it is a first page")
	})
}
