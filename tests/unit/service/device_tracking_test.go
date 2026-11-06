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

func deviceTrackingRows(n int) []model.DeviceTrackingRow {
	out := make([]model.DeviceTrackingRow, n)
	for i := range out {
		out[i] = model.DeviceTrackingRow{
			ID: int64(i + 1), CustomerName: fmt.Sprint("Customer ", i),
			SortValue: fmt.Sprintf("2026-10-%02dT08:00:00.000000Z", i+1),
		}
	}
	return out
}

// runDeviceTracking calls List on a service whose repository returns rows, and returns the query it received.
func runDeviceTracking(t *testing.T, p service.ListDeviceTrackingParams, rows []model.DeviceTrackingRow) (model.DeviceTrackingQuery, *service.ListDeviceTrackingResult) {
	t.Helper()
	repo := servicemocks.NewMockDeviceTrackingRepository(t)
	var got model.DeviceTrackingQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.DeviceTrackingQuery) { got = q }).
		Return(rows, nil).Once()

	res, err := service.NewDeviceTrackingService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsDeviceTracking asserts that p is invalid and that the repository is never touched.
func rejectsDeviceTracking(t *testing.T, p service.ListDeviceTrackingParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockDeviceTrackingRepository(t) // no expectations: any call fails the test
	_, err := service.NewDeviceTrackingService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestDeviceTrackingListDefaults(t *testing.T) {
	q, res := runDeviceTracking(t, service.ListDeviceTrackingParams{}, nil)

	require.Equal(t, model.DeviceTrackingQuery{
		Filter: model.DeviceTrackingFilter{
			ScheduledFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			ScheduledBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		},
		Sort:  model.DeviceTrackingSort{Key: model.DeviceTrackingSortDate, Desc: true},
		Limit: service.DefaultPageSize,
	}, q, "the current month, every client, latest appointment first")
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
}

func TestDeviceTrackingListParsesFilters(t *testing.T) {
	q, _ := runDeviceTracking(t, service.ListDeviceTrackingParams{
		From: "2026-11-01", To: "2026-11-30", CustomerIDs: []string{"3", "3", "5"}, Search: "  bait ", Sort: "barcode", Limit: "10",
	}, nil)

	require.Equal(t, date(2026, 11, 1), q.Filter.ScheduledFrom)
	require.Equal(t, date(2026, 12, 1), q.Filter.ScheduledBefore, "the last day is included, so the bound is the next day")
	require.Equal(t, []int64{3, 5}, q.Filter.CustomerIDs)
	require.Equal(t, "bait", q.Filter.Search)
	require.Equal(t, model.DeviceTrackingSort{Key: model.DeviceTrackingSortBarcode}, q.Sort, "a sort key alone is ascending")
	require.Equal(t, 10, q.Limit)
}

func TestDeviceTrackingListEverySortKeyIsAccepted(t *testing.T) {
	require.Len(t, model.DeviceTrackingSortKeys, 19, "every column of the screen is sortable")
	for _, key := range model.DeviceTrackingSortKeys {
		q, _ := runDeviceTracking(t, service.ListDeviceTrackingParams{Sort: strings.ToUpper(string(key))}, nil)
		require.Equal(t, key, q.Sort.Key)
	}
}

func TestDeviceTrackingListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.DeviceTrackingSort
	}{
		{"neither: latest first", "", "", model.DeviceTrackingSort{Key: model.DeviceTrackingSortDate, Desc: true}},
		{"order alone flips the default key", "", "asc", model.DeviceTrackingSort{Key: model.DeviceTrackingSortDate}},
		{"key alone is ascending", "city", "", model.DeviceTrackingSort{Key: model.DeviceTrackingSortCity}},
		{"key and order", "check_in", "DESC", model.DeviceTrackingSort{Key: model.DeviceTrackingSortCheckIn, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runDeviceTracking(t, service.ListDeviceTrackingParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}
}

func TestDeviceTrackingListValidation(t *testing.T) {
	tests := []struct {
		name      string
		p         service.ListDeviceTrackingParams
		wantField string
	}{
		{"bad from", service.ListDeviceTrackingParams{From: "11/01/2026"}, "from"},
		{"to before from", service.ListDeviceTrackingParams{From: "2026-11-05", To: "2026-11-01"}, "to"},
		{"client id not a number", service.ListDeviceTrackingParams{CustomerIDs: []string{"abc"}}, "client_id"},
		{"client id zero", service.ListDeviceTrackingParams{CustomerIDs: []string{"0"}}, "client_id"},
		{"client id negative", service.ListDeviceTrackingParams{CustomerIDs: []string{"-1"}}, "client_id"},
		{"too many client ids", service.ListDeviceTrackingParams{CustomerIDs: make([]string, service.MaxIDsPerFilter+1)}, "client_id"},
		{"search too long", service.ListDeviceTrackingParams{Search: strings.Repeat("x", service.MaxSearchLength+1)}, "q"},
		{"unknown sort", service.ListDeviceTrackingParams{Sort: "address"}, "sort"},
		{"unknown order", service.ListDeviceTrackingParams{Order: "up"}, "order"},
		{"limit zero", service.ListDeviceTrackingParams{Limit: "0"}, "limit"},
		{"limit too big", service.ListDeviceTrackingParams{Limit: "201"}, "limit"},
		{"garbage cursor", service.ListDeviceTrackingParams{Cursor: "!!!"}, "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, rejectsDeviceTracking(t, tt.p), tt.wantField)
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsDeviceTracking(t, service.ListDeviceTrackingParams{CustomerIDs: []string{"x"}, Sort: "y", Limit: "0"})
		require.Contains(t, got, "client_id")
		require.Contains(t, got, "sort")
		require.Contains(t, got, "limit")
	})

	t.Run("a cursor is not checked when the sort is invalid", func(t *testing.T) {
		got := rejectsDeviceTracking(t, service.ListDeviceTrackingParams{Sort: "nope", Cursor: "!!!"})
		require.NotContains(t, got, "cursor")
	})
}

func TestDeviceTrackingListPaging(t *testing.T) {
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
			_, res := runDeviceTracking(t, service.ListDeviceTrackingParams{Limit: tt.limit}, deviceTrackingRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}
			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeDeviceTrackingCursor(res.NextCursor, model.DeviceTrackingSort{Key: model.DeviceTrackingSortDate, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.DeviceTrackingCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestDeviceTrackingListAppliesTheCursor(t *testing.T) {
	sort := model.DeviceTrackingSort{Key: model.DeviceTrackingSortDate, Desc: true}
	cursor := service.EncodeDeviceTrackingCursor(sort, model.DeviceTrackingRow{ID: 9, SortValue: "2026-10-15T14:30:15.123456Z"})

	q, _ := runDeviceTracking(t, service.ListDeviceTrackingParams{Cursor: cursor}, nil)

	require.Equal(t, &model.DeviceTrackingCursor{SortValue: "2026-10-15T14:30:15.123456Z", ID: 9}, q.After)
}

func TestDeviceTrackingListRejectsACursorOfAnotherSort(t *testing.T) {
	cursor := service.EncodeDeviceTrackingCursor(model.DeviceTrackingSort{Key: model.DeviceTrackingSortBarcode},
		model.DeviceTrackingRow{ID: 9, SortValue: "bc-1001"})

	got := rejectsDeviceTracking(t, service.ListDeviceTrackingParams{Cursor: cursor})

	require.Contains(t, got["cursor"], "does not match the requested sort")
}

func TestDeviceTrackingListWrapsRepositoryErrors(t *testing.T) {
	repo := servicemocks.NewMockDeviceTrackingRepository(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

	res, err := service.NewDeviceTrackingService(repo, fixedClock(october21)).List(context.Background(), service.ListDeviceTrackingParams{})

	require.Nil(t, res)
	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "list device tracking")
	require.NotErrorIs(t, err, model.ErrInvalidInput)
}

func TestDeviceTrackingFilterOptions(t *testing.T) {
	t.Run("a nil list becomes an empty list", func(t *testing.T) {
		repo := servicemocks.NewMockDeviceTrackingRepository(t)
		repo.EXPECT().Clients(mock.Anything).Return(nil, nil).Once()

		opts, err := service.NewDeviceTrackingService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.NotNil(t, opts.Clients)
	})

	t.Run("a failing lookup is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockDeviceTrackingRepository(t)
		repo.EXPECT().Clients(mock.Anything).Return(nil, errBoom).Once()

		opts, err := service.NewDeviceTrackingService(repo, nil).FilterOptions(context.Background())

		require.Nil(t, opts)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "load filter options")
	})
}

// deviceTrackingSortValue is what the repository's sort_value column looks like for each key.
var deviceTrackingSortValue = map[model.DeviceTrackingSortKey]string{
	model.DeviceTrackingSortCustomer:   "william saliba",
	model.DeviceTrackingSortService:    "bi-monthly service",
	model.DeviceTrackingSortDate:       "2026-11-02T08:15:30.123456Z",
	model.DeviceTrackingSortAddress1:   "west loop 1604 south",
	model.DeviceTrackingSortAddress2:   "",
	model.DeviceTrackingSortCity:       "san antonio",
	model.DeviceTrackingSortState:      "tx",
	model.DeviceTrackingSortZip:        "78264",
	model.DeviceTrackingSortDeviceName: "qa device 32d92e23",
	model.DeviceTrackingSortBarcode:    "bc-1001",
	model.DeviceTrackingSortCoordinate: "000115774266,000099869955",
	model.DeviceTrackingSortArea:       "area 1",
	model.DeviceTrackingSortDeviceType: "bait box",
	model.DeviceTrackingSortCheckIn:    "1793598000000",
	model.DeviceTrackingSortStatus:     "good",
	model.DeviceTrackingSortActivity:   "inspected",
	model.DeviceTrackingSortCheckOut:   "0",
	model.DeviceTrackingSortNotes:      "bait taken, refilled",
	model.DeviceTrackingSortAssignedTo: "thọ nguyễn quang",
}

func TestDeviceTrackingCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, deviceTrackingSortValue, len(model.DeviceTrackingSortKeys), "every sort key needs a sample value")

	for _, key := range model.DeviceTrackingSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.DeviceTrackingSort{Key: key, Desc: desc}
				row := model.DeviceTrackingRow{ID: 42, SortValue: deviceTrackingSortValue[key]}

				got, err := service.DecodeDeviceTrackingCursor(service.EncodeDeviceTrackingCursor(sort, row), sort)

				require.NoError(t, err)
				require.Equal(t, &model.DeviceTrackingCursor{SortValue: deviceTrackingSortValue[key], ID: 42}, got)
			})
		}
	}
}

func TestDeviceTrackingCursorRejectsInvalidCursors(t *testing.T) {
	dateAsc := model.DeviceTrackingSort{Key: model.DeviceTrackingSortDate}
	checkInAsc := model.DeviceTrackingSort{Key: model.DeviceTrackingSortCheckIn}
	barcodeAsc := model.DeviceTrackingSort{Key: model.DeviceTrackingSortBarcode}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.DeviceTrackingSort
		wantReason string
	}{
		{"empty", "", dateAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", dateAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"date","d":false,"v":"2026-10-12T08:00:00.000000Z","id":0}`), dateAsc, "not a valid cursor"},
		{"other sort key", cursorWith("notes", "x"), dateAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"date","d":true,"v":"2026-10-12T08:00:00.000000Z","id":1}`), dateAsc, "does not match the requested sort"},
		{"timestamp without microseconds", cursorWith("date", "2026-10-12T08:00:00Z"), dateAsc, "not a valid cursor"},
		{"timestamp with injection", cursorWith("date", "2026-10-12T08:00:00.000000Z'; DROP TABLE jobs;--"), dateAsc, "not a valid cursor"},
		{"check in not a number", cursorWith("check_in", "abc"), checkInAsc, "not a valid cursor"},
		{"check in with injection", cursorWith("check_in", "1; DROP TABLE jobs"), checkInAsc, "not a valid cursor"},
		{"check in negative", cursorWith("check_in", "-1"), checkInAsc, "not a valid cursor"},
		{"check in empty", cursorWith("check_in", ""), checkInAsc, "not a valid cursor"},
		{"text with NUL", encode(`{"k":"barcode","d":false,"v":"a\u0000b","id":1}`), barcodeAsc, "not a valid cursor"},
		{"text too long", cursorWith("barcode", strings.Repeat("x", model.MaxSortTextLength+1)), barcodeAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), barcodeAsc, "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeDeviceTrackingCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestDeviceTrackingCursorAcceptsEmptyAndZeroValues(t *testing.T) {
	for _, tt := range []struct {
		key   model.DeviceTrackingSortKey
		value string
	}{
		{model.DeviceTrackingSortAddress2, ""},
		{model.DeviceTrackingSortCoordinate, ""},
		{model.DeviceTrackingSortAssignedTo, ""},
		{model.DeviceTrackingSortCheckIn, "0"},
		{model.DeviceTrackingSortCheckOut, "0"},
	} {
		sort := model.DeviceTrackingSort{Key: tt.key}
		raw := service.EncodeDeviceTrackingCursor(sort, model.DeviceTrackingRow{ID: 5, SortValue: tt.value})

		got, err := service.DecodeDeviceTrackingCursor(raw, sort)

		require.NoError(t, err, tt.key)
		require.Equal(t, tt.value, got.SortValue, "%s: a device never checked, or without a coordinate, sorts first", tt.key)
	}
}
