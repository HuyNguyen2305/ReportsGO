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

func onlineBookingRows(n int) []model.OnlineBookingRow {
	out := make([]model.OnlineBookingRow, n)
	for i := range out {
		out[i] = model.OnlineBookingRow{
			ID: int64(i + 1), CustomerName: fmt.Sprint("Customer ", i),
			SortValue: fmt.Sprintf("2026-10-%02dT08:00:00.000000Z", i+1),
		}
	}
	return out
}

// runOnlineBookings calls List on a service whose repository returns rows, and returns the query it received.
func runOnlineBookings(t *testing.T, p service.ListOnlineBookingsParams, rows []model.OnlineBookingRow) (model.OnlineBookingQuery, *service.ListOnlineBookingsResult) {
	t.Helper()
	repo := servicemocks.NewMockOnlineBookingRepository(t)
	var got model.OnlineBookingQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.OnlineBookingQuery) { got = q }).
		Return(rows, nil).Once()

	res, err := service.NewOnlineBookingService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsOnlineBookings asserts that p is invalid and that the repository is never touched.
func rejectsOnlineBookings(t *testing.T, p service.ListOnlineBookingsParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockOnlineBookingRepository(t) // no expectations: any call fails the test
	_, err := service.NewOnlineBookingService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestOnlineBookingListDefaults(t *testing.T) {
	q, res := runOnlineBookings(t, service.ListOnlineBookingsParams{}, nil)

	require.Equal(t, model.OnlineBookingQuery{
		Filter: model.OnlineBookingFilter{
			ScheduledFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			ScheduledBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		},
		NameFormat: model.NameFirst,
		Sort:       model.OnlineBookingSort{Key: model.OnlineBookingSortScheduledAt, Desc: true},
		Limit:      service.DefaultPageSize,
	}, q, "the current month, every source, first names, latest appointment first")
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
}

func TestOnlineBookingListParsesFilters(t *testing.T) {
	q, _ := runOnlineBookings(t, service.ListOnlineBookingsParams{
		From: "2026-11-01", To: "2026-11-30", SourceIDs: []string{"2", " NONE ", "2", "5"},
		NameFormat: " Full_Name ", Search: "  ants ", Sort: "service", Limit: "10",
	}, nil)

	require.Equal(t, date(2026, 11, 1), q.Filter.ScheduledFrom)
	require.Equal(t, date(2026, 12, 1), q.Filter.ScheduledBefore, "the last day is included, so the bound is the next day")
	require.Equal(t, []int64{2, 5}, q.Filter.LeadSourceIDs)
	require.True(t, q.Filter.NoSource, `"none" is the customers without a source`)
	require.Equal(t, "ants", q.Filter.Search)
	require.Equal(t, model.NameFull, q.NameFormat)
	require.Equal(t, model.OnlineBookingSort{Key: model.OnlineBookingSortService}, q.Sort, "a sort key alone is ascending")
	require.Equal(t, 10, q.Limit)
}

func TestOnlineBookingListSourceFilterForms(t *testing.T) {
	tests := []struct {
		name     string
		raw      []string
		wantIDs  []int64
		wantNone bool
	}{
		{"nothing", nil, nil, false},
		{"ids only", []string{"3", "4"}, []int64{3, 4}, false},
		{"none only", []string{"none"}, nil, true},
		{"ids and none", []string{"1", "None"}, []int64{1}, true},
		{"blanks are ignored", []string{"", " "}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runOnlineBookings(t, service.ListOnlineBookingsParams{SourceIDs: tt.raw}, nil)

			require.Equal(t, tt.wantIDs, q.Filter.LeadSourceIDs)
			require.Equal(t, tt.wantNone, q.Filter.NoSource)
		})
	}
}

func TestOnlineBookingListEveryNameFormatIsAccepted(t *testing.T) {
	for _, f := range model.NameFormats {
		q, _ := runOnlineBookings(t, service.ListOnlineBookingsParams{NameFormat: string(f)}, nil)
		require.Equal(t, f, q.NameFormat)
	}
}

func TestOnlineBookingListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.OnlineBookingSort
	}{
		{"neither: latest first", "", "", model.OnlineBookingSort{Key: model.OnlineBookingSortScheduledAt, Desc: true}},
		{"order alone flips the default key", "", "asc", model.OnlineBookingSort{Key: model.OnlineBookingSortScheduledAt}},
		{"key alone is ascending", "customer", "", model.OnlineBookingSort{Key: model.OnlineBookingSortCustomer}},
		{"key and order", "status", "DESC", model.OnlineBookingSort{Key: model.OnlineBookingSortStatus, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runOnlineBookings(t, service.ListOnlineBookingsParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}
}

func TestOnlineBookingListValidation(t *testing.T) {
	tests := []struct {
		name      string
		p         service.ListOnlineBookingsParams
		wantField string
	}{
		{"bad from", service.ListOnlineBookingsParams{From: "11/01/2026"}, "from"},
		{"to before from", service.ListOnlineBookingsParams{From: "2026-11-05", To: "2026-11-01"}, "to"},
		{"source id not a number", service.ListOnlineBookingsParams{SourceIDs: []string{"abc"}}, "source_id"},
		{"source id zero", service.ListOnlineBookingsParams{SourceIDs: []string{"0"}}, "source_id"},
		{"source id negative", service.ListOnlineBookingsParams{SourceIDs: []string{"-1"}}, "source_id"},
		{"too many source ids", service.ListOnlineBookingsParams{SourceIDs: make([]string, service.MaxIDsPerFilter+1)}, "source_id"},
		{"unknown name format", service.ListOnlineBookingsParams{NameFormat: "nickname"}, "name_format"},
		{"search too long", service.ListOnlineBookingsParams{Search: strings.Repeat("x", service.MaxSearchLength+1)}, "q"},
		{"unknown sort", service.ListOnlineBookingsParams{Sort: "source"}, "sort"},
		{"date and time are one column", service.ListOnlineBookingsParams{Sort: "time"}, "sort"},
		{"unknown order", service.ListOnlineBookingsParams{Order: "up"}, "order"},
		{"limit zero", service.ListOnlineBookingsParams{Limit: "0"}, "limit"},
		{"limit too big", service.ListOnlineBookingsParams{Limit: "201"}, "limit"},
		{"garbage cursor", service.ListOnlineBookingsParams{Cursor: "!!!"}, "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, rejectsOnlineBookings(t, tt.p), tt.wantField)
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsOnlineBookings(t, service.ListOnlineBookingsParams{SourceIDs: []string{"x"}, NameFormat: "y", Limit: "0"})
		require.Contains(t, got, "source_id")
		require.Contains(t, got, "name_format")
		require.Contains(t, got, "limit")
	})

	t.Run("a cursor is not checked when the sort is invalid", func(t *testing.T) {
		got := rejectsOnlineBookings(t, service.ListOnlineBookingsParams{Sort: "nope", Cursor: "!!!"})
		require.NotContains(t, got, "cursor")
	})
}

func TestOnlineBookingListPaging(t *testing.T) {
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
			_, res := runOnlineBookings(t, service.ListOnlineBookingsParams{Limit: tt.limit}, onlineBookingRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}
			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeOnlineBookingCursor(res.NextCursor, model.OnlineBookingSort{Key: model.OnlineBookingSortScheduledAt, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.OnlineBookingCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestOnlineBookingListAppliesTheCursor(t *testing.T) {
	sort := model.OnlineBookingSort{Key: model.OnlineBookingSortScheduledAt, Desc: true}
	cursor := service.EncodeOnlineBookingCursor(sort, model.OnlineBookingRow{ID: 9, SortValue: "2026-10-15T14:30:15.123456Z"})

	q, _ := runOnlineBookings(t, service.ListOnlineBookingsParams{Cursor: cursor}, nil)

	require.Equal(t, &model.OnlineBookingCursor{SortValue: "2026-10-15T14:30:15.123456Z", ID: 9}, q.After)
}

func TestOnlineBookingListRejectsACursorOfAnotherSort(t *testing.T) {
	cursor := service.EncodeOnlineBookingCursor(model.OnlineBookingSort{Key: model.OnlineBookingSortEmail},
		model.OnlineBookingRow{ID: 9, SortValue: "a@example.com"})

	got := rejectsOnlineBookings(t, service.ListOnlineBookingsParams{Cursor: cursor})

	require.Contains(t, got["cursor"], "does not match the requested sort")
}

func TestOnlineBookingListWrapsRepositoryErrors(t *testing.T) {
	repo := servicemocks.NewMockOnlineBookingRepository(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

	res, err := service.NewOnlineBookingService(repo, fixedClock(october21)).List(context.Background(), service.ListOnlineBookingsParams{})

	require.Nil(t, res)
	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "list online bookings")
	require.NotErrorIs(t, err, model.ErrInvalidInput)
}

func TestOnlineBookingFilterOptions(t *testing.T) {
	t.Run("a nil list becomes an empty list", func(t *testing.T) {
		repo := servicemocks.NewMockOnlineBookingRepository(t)
		repo.EXPECT().LeadSources(mock.Anything).Return(nil, nil).Once()

		opts, err := service.NewOnlineBookingService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.NotNil(t, opts.Sources)
	})

	t.Run("a failing lookup is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockOnlineBookingRepository(t)
		repo.EXPECT().LeadSources(mock.Anything).Return(nil, errBoom).Once()

		opts, err := service.NewOnlineBookingService(repo, nil).FilterOptions(context.Background())

		require.Nil(t, opts)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "load filter options")
	})
}

// onlineBookingSortValue is what the repository's sort_value column looks like for each key.
var onlineBookingSortValue = map[model.OnlineBookingSortKey]string{
	model.OnlineBookingSortCustomer:    "william saliba",
	model.OnlineBookingSortScheduledAt: "2026-11-02T08:15:30.123456Z",
	model.OnlineBookingSortService:     "bi-monthly service",
	model.OnlineBookingSortEmail:       "mia.torres@example.com",
	model.OnlineBookingSortPhone:       "(555) 010-0001",
	model.OnlineBookingSortAddress:     "1 main st",
	model.OnlineBookingSortNotes:       "asked for a quote",
	model.OnlineBookingSortStatus:      "confirmed",
}

func TestOnlineBookingCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, onlineBookingSortValue, len(model.OnlineBookingSortKeys), "every sort key needs a sample value")

	for _, key := range model.OnlineBookingSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.OnlineBookingSort{Key: key, Desc: desc}
				row := model.OnlineBookingRow{ID: 42, SortValue: onlineBookingSortValue[key]}

				got, err := service.DecodeOnlineBookingCursor(service.EncodeOnlineBookingCursor(sort, row), sort)

				require.NoError(t, err)
				require.Equal(t, &model.OnlineBookingCursor{SortValue: onlineBookingSortValue[key], ID: 42}, got)
			})
		}
	}
}

func TestOnlineBookingCursorAcceptsEmptyValues(t *testing.T) {
	for _, key := range []model.OnlineBookingSortKey{
		model.OnlineBookingSortEmail, model.OnlineBookingSortPhone, model.OnlineBookingSortNotes,
	} {
		sort := model.OnlineBookingSort{Key: key}
		raw := service.EncodeOnlineBookingCursor(sort, model.OnlineBookingRow{ID: 5, SortValue: ""})

		got, err := service.DecodeOnlineBookingCursor(raw, sort)

		require.NoError(t, err, key)
		require.Equal(t, "", got.SortValue, "%s: a customer without one sorts as an empty value", key)
	}
}

func TestOnlineBookingCursorRejectsInvalidCursors(t *testing.T) {
	scheduledAsc := model.OnlineBookingSort{Key: model.OnlineBookingSortScheduledAt}
	emailAsc := model.OnlineBookingSort{Key: model.OnlineBookingSortEmail}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.OnlineBookingSort
		wantReason string
	}{
		{"empty", "", scheduledAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", scheduledAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"scheduled_at","d":false,"v":"2026-10-12T08:00:00.000000Z","id":0}`), scheduledAsc, "not a valid cursor"},
		{"other sort key", cursorWith("notes", "x"), scheduledAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"scheduled_at","d":true,"v":"2026-10-12T08:00:00.000000Z","id":1}`), scheduledAsc, "does not match the requested sort"},
		{"timestamp without microseconds", cursorWith("scheduled_at", "2026-10-12T08:00:00Z"), scheduledAsc, "not a valid cursor"},
		{"timestamp with an offset", cursorWith("scheduled_at", "2026-10-12T08:00:00.000000+07:00"), scheduledAsc, "not a valid cursor"},
		{"timestamp with injection", cursorWith("scheduled_at", "2026-10-12T08:00:00.000000Z'; DROP TABLE jobs;--"), scheduledAsc, "not a valid cursor"},
		{"text with NUL", encode(`{"k":"email","d":false,"v":"a\u0000b","id":1}`), emailAsc, "not a valid cursor"},
		{"text too long", cursorWith("email", strings.Repeat("x", model.MaxSortTextLength+1)), emailAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), emailAsc, "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeOnlineBookingCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}
