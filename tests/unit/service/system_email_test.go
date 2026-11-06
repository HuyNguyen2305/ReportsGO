package service_test

import (
	"context"
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

func systemEmailRows(n int) []model.SystemEmailRow {
	out := make([]model.SystemEmailRow, n)
	for i := range out {
		out[i] = model.SystemEmailRow{
			ID: int64(i + 1), CustomerName: fmt.Sprint("Customer ", i), Email: "a@example.com",
			Type: model.EmailInvoice, Status: model.EmailConfirmed,
			SortValue: fmt.Sprintf("2026-10-%02dT08:00:00.000000Z", i+1),
		}
	}
	return out
}

// runSystemEmails calls List on a service whose repository returns rows, and returns the query it received.
func runSystemEmails(t *testing.T, p service.ListSystemEmailsParams, rows []model.SystemEmailRow) (model.SystemEmailQuery, *service.ListSystemEmailsResult) {
	t.Helper()
	repo := servicemocks.NewMockSystemEmailRepository(t)
	var got model.SystemEmailQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.SystemEmailQuery) { got = q }).
		Return(rows, nil).Once()

	res, err := service.NewSystemEmailService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsSystemEmails asserts that p is invalid and that the repository is never touched.
func rejectsSystemEmails(t *testing.T, p service.ListSystemEmailsParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockSystemEmailRepository(t) // no expectations: any call fails the test
	_, err := service.NewSystemEmailService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestSystemEmailListDefaults(t *testing.T) {
	q, res := runSystemEmails(t, service.ListSystemEmailsParams{}, nil)

	require.Equal(t, model.SystemEmailQuery{
		Filter: model.SystemEmailFilter{
			SentFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			SentBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		},
		NameFormat: model.NameFirst,
		Sort:       model.SystemEmailSort{Key: model.SystemEmailSortTimeStamp, Desc: true},
		Limit:      service.DefaultPageSize,
	}, q, "the current month, every status, first names, newest first")
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
}

func TestSystemEmailListParsesFilters(t *testing.T) {
	q, _ := runSystemEmails(t, service.ListSystemEmailsParams{
		From: "2026-11-01", To: "2026-11-30", Status: []string{" Confirmed ", "cancelled", "confirmed"},
		CustomerIDs: []string{"3", "3", "5"}, NameFormat: " Full_Name ", Search: "  saliba ", Sort: "email", Limit: "10",
	}, nil)

	require.Equal(t, date(2026, 11, 1), q.Filter.SentFrom)
	require.Equal(t, date(2026, 12, 1), q.Filter.SentBefore, "the last day is included, so the bound is the next day")
	require.Equal(t, []model.EmailStatus{model.EmailConfirmed, model.EmailCancelled}, q.Filter.Statuses)
	require.Equal(t, []int64{3, 5}, q.Filter.CustomerIDs)
	require.Equal(t, "saliba", q.Filter.Search)
	require.Equal(t, model.NameFull, q.NameFormat)
	require.Equal(t, model.SystemEmailSort{Key: model.SystemEmailSortEmail}, q.Sort, "a sort key alone is ascending")
	require.Equal(t, 10, q.Limit)
}

func TestSystemEmailListEveryNameFormatIsAccepted(t *testing.T) {
	for _, f := range model.NameFormats {
		q, _ := runSystemEmails(t, service.ListSystemEmailsParams{NameFormat: string(f)}, nil)
		require.Equal(t, f, q.NameFormat)
	}
}

func TestSystemEmailListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.SystemEmailSort
	}{
		{"neither: newest first", "", "", model.SystemEmailSort{Key: model.SystemEmailSortTimeStamp, Desc: true}},
		{"order alone flips the default key", "", "asc", model.SystemEmailSort{Key: model.SystemEmailSortTimeStamp}},
		{"key alone is ascending", "customer", "", model.SystemEmailSort{Key: model.SystemEmailSortCustomer}},
		{"key and order", "status", "DESC", model.SystemEmailSort{Key: model.SystemEmailSortStatus, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runSystemEmails(t, service.ListSystemEmailsParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}
}

func TestSystemEmailListValidation(t *testing.T) {
	tests := []struct {
		name      string
		p         service.ListSystemEmailsParams
		wantField string
	}{
		{"bad from", service.ListSystemEmailsParams{From: "11/01/2026"}, "from"},
		{"to before from", service.ListSystemEmailsParams{From: "2026-11-05", To: "2026-11-01"}, "to"},
		{"unknown status", service.ListSystemEmailsParams{Status: []string{"sent"}}, "status"},
		{"too many statuses", service.ListSystemEmailsParams{Status: make([]string, service.MaxEnumValues+1)}, "status"},
		{"client id not a number", service.ListSystemEmailsParams{CustomerIDs: []string{"abc"}}, "client_id"},
		{"client id zero", service.ListSystemEmailsParams{CustomerIDs: []string{"0"}}, "client_id"},
		{"too many client ids", service.ListSystemEmailsParams{CustomerIDs: make([]string, service.MaxIDsPerFilter+1)}, "client_id"},
		{"unknown name format", service.ListSystemEmailsParams{NameFormat: "nickname"}, "name_format"},
		{"search too long", service.ListSystemEmailsParams{Search: strings.Repeat("x", service.MaxSearchLength+1)}, "q"},
		{"unknown sort", service.ListSystemEmailsParams{Sort: "customer_id"}, "sort"},
		{"unknown order", service.ListSystemEmailsParams{Order: "up"}, "order"},
		{"limit zero", service.ListSystemEmailsParams{Limit: "0"}, "limit"},
		{"limit too big", service.ListSystemEmailsParams{Limit: "201"}, "limit"},
		{"garbage cursor", service.ListSystemEmailsParams{Cursor: "!!!"}, "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, rejectsSystemEmails(t, tt.p), tt.wantField)
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsSystemEmails(t, service.ListSystemEmailsParams{Status: []string{"x"}, NameFormat: "y", Limit: "0"})
		require.Contains(t, got, "status")
		require.Contains(t, got, "name_format")
		require.Contains(t, got, "limit")
	})

	t.Run("a cursor is not checked when the sort is invalid", func(t *testing.T) {
		got := rejectsSystemEmails(t, service.ListSystemEmailsParams{Sort: "nope", Cursor: "!!!"})
		require.NotContains(t, got, "cursor")
	})
}

func TestSystemEmailListPaging(t *testing.T) {
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
			_, res := runSystemEmails(t, service.ListSystemEmailsParams{Limit: tt.limit}, systemEmailRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}
			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeSystemEmailCursor(res.NextCursor, model.SystemEmailSort{Key: model.SystemEmailSortTimeStamp, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.SystemEmailCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestSystemEmailListAppliesTheCursor(t *testing.T) {
	sort := model.SystemEmailSort{Key: model.SystemEmailSortTimeStamp, Desc: true}
	cursor := service.EncodeSystemEmailCursor(sort, model.SystemEmailRow{ID: 9, SortValue: "2026-10-15T14:30:15.123456Z"})

	q, _ := runSystemEmails(t, service.ListSystemEmailsParams{Cursor: cursor}, nil)

	require.Equal(t, &model.SystemEmailCursor{SortValue: "2026-10-15T14:30:15.123456Z", ID: 9}, q.After)
}

func TestSystemEmailListRejectsACursorOfAnotherSort(t *testing.T) {
	cursor := service.EncodeSystemEmailCursor(model.SystemEmailSort{Key: model.SystemEmailSortEmail},
		model.SystemEmailRow{ID: 9, SortValue: "a@example.com"})

	got := rejectsSystemEmails(t, service.ListSystemEmailsParams{Cursor: cursor})

	require.Contains(t, got["cursor"], "does not match the requested sort")
}

func TestSystemEmailListWrapsRepositoryErrors(t *testing.T) {
	repo := servicemocks.NewMockSystemEmailRepository(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

	res, err := service.NewSystemEmailService(repo, fixedClock(october21)).List(context.Background(), service.ListSystemEmailsParams{})

	require.Nil(t, res)
	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "list system emails")
	require.NotErrorIs(t, err, model.ErrInvalidInput)
}

func TestSystemEmailFilterOptions(t *testing.T) {
	t.Run("a nil list becomes an empty list", func(t *testing.T) {
		repo := servicemocks.NewMockSystemEmailRepository(t)
		repo.EXPECT().Clients(mock.Anything).Return(nil, nil).Once()

		opts, err := service.NewSystemEmailService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.NotNil(t, opts.Clients)
	})

	t.Run("a failing lookup is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockSystemEmailRepository(t)
		repo.EXPECT().Clients(mock.Anything).Return(nil, errBoom).Once()

		opts, err := service.NewSystemEmailService(repo, nil).FilterOptions(context.Background())

		require.Nil(t, opts)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "load filter options")
	})
}

// systemEmailSortValue is what the repository's sort_value column looks like for each key.
var systemEmailSortValue = map[model.SystemEmailSortKey]string{
	model.SystemEmailSortTimeStamp: "2026-11-02T08:15:30.123456Z",
	model.SystemEmailSortCustomer:  "william saliba",
	model.SystemEmailSortEmail:     "billing.alt@example.com",
	model.SystemEmailSortType:      "appointment_reminder",
	model.SystemEmailSortStatus:    "unconfirmed",
}

func TestSystemEmailCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, systemEmailSortValue, len(model.SystemEmailSortKeys), "every sort key needs a sample value")

	for _, key := range model.SystemEmailSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.SystemEmailSort{Key: key, Desc: desc}
				row := model.SystemEmailRow{ID: 42, SortValue: systemEmailSortValue[key]}

				got, err := service.DecodeSystemEmailCursor(service.EncodeSystemEmailCursor(sort, row), sort)

				require.NoError(t, err)
				require.Equal(t, &model.SystemEmailCursor{SortValue: systemEmailSortValue[key], ID: 42}, got)
			})
		}
	}
}

func TestSystemEmailCursorRejectsInvalidCursors(t *testing.T) {
	stampAsc := model.SystemEmailSort{Key: model.SystemEmailSortTimeStamp}
	emailAsc := model.SystemEmailSort{Key: model.SystemEmailSortEmail}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.SystemEmailSort
		wantReason string
	}{
		{"empty", "", stampAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", stampAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"time_stamp","d":false,"v":"2026-10-12T08:00:00.000000Z","id":0}`), stampAsc, "not a valid cursor"},
		{"other sort key", cursorWith("status", "confirmed"), stampAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"time_stamp","d":true,"v":"2026-10-12T08:00:00.000000Z","id":1}`), stampAsc, "does not match the requested sort"},
		{"timestamp without microseconds", cursorWith("time_stamp", "2026-10-12T08:00:00Z"), stampAsc, "not a valid cursor"},
		{"timestamp with an offset", cursorWith("time_stamp", "2026-10-12T08:00:00.000000+07:00"), stampAsc, "not a valid cursor"},
		{"timestamp with injection", cursorWith("time_stamp", "2026-10-12T08:00:00.000000Z'; DROP TABLE system_emails;--"), stampAsc, "not a valid cursor"},
		{"text with NUL", encode(`{"k":"email","d":false,"v":"a\u0000b","id":1}`), emailAsc, "not a valid cursor"},
		{"text too long", cursorWith("email", strings.Repeat("x", model.MaxSortTextLength+1)), emailAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), emailAsc, "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeSystemEmailCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestSystemEmailCursorAcceptsBoundaryTimestamps(t *testing.T) {
	sort := model.SystemEmailSort{Key: model.SystemEmailSortTimeStamp}
	for _, v := range []string{"0001-01-01T00:00:00.000000Z", "9999-12-31T23:59:59.999999Z", "2026-10-31T23:59:59.999999Z"} {
		raw := service.EncodeSystemEmailCursor(sort, model.SystemEmailRow{ID: 1, SortValue: v})
		got, err := service.DecodeSystemEmailCursor(raw, sort)
		require.NoError(t, err, v)
		require.Equal(t, v, got.SortValue)
	}
}
