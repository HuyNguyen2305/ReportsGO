package service_test

import (
	"context"
	"errors"
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

func noActiveJobsRows(n int) []model.NoActiveJobsRow {
	out := make([]model.NoActiveJobsRow, n)
	for i := range out {
		out[i] = model.NoActiveJobsRow{ID: int64(i + 1), SortValue: fmt.Sprintf("customer %02d", i+1)}
	}
	return out
}

// runNoActiveJobs calls List on a service whose repository returns rows, and returns the query it received.
func runNoActiveJobs(t *testing.T, grain model.NoActiveJobsGrain, p service.ListNoActiveJobsParams, rows []model.NoActiveJobsRow) (model.NoActiveJobsQuery, *service.ListNoActiveJobsResult) {
	t.Helper()

	repo := servicemocks.NewMockNoActiveJobsRepository(t)
	var got model.NoActiveJobsQuery
	// the summary is asked for on the first page only
	withSummary := strings.TrimSpace(p.Cursor) == ""
	page := model.NoActiveJobsPage{Rows: rows}
	if withSummary {
		page.Summary = &model.NoActiveJobsSummary{}
	}
	repo.EXPECT().Page(mock.Anything, mock.Anything, withSummary).
		Run(func(_ context.Context, q model.NoActiveJobsQuery, _ bool) { got = q }).
		Return(page, nil).Once()

	res, err := service.NewNoActiveJobsService(repo, fixedClock(october21)).List(context.Background(), grain, p)
	require.NoError(t, err)
	return got, res
}

// rejectsNoActiveJobs asserts that p is invalid and that the repository is never touched.
func rejectsNoActiveJobs(t *testing.T, grain model.NoActiveJobsGrain, p service.ListNoActiveJobsParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockNoActiveJobsRepository(t) // no expectations: any call fails the test
	_, err := service.NewNoActiveJobsService(repo, fixedClock(october21)).List(context.Background(), grain, p)
	require.Error(t, err)
	return fields(t, err)
}

var bothGrains = []model.NoActiveJobsGrain{model.NoActiveJobsCustomers, model.NoActiveJobsLocations}

func TestNoActiveJobsListDefaults(t *testing.T) {
	for _, grain := range bothGrains {
		t.Run(string(grain), func(t *testing.T) {
			q, res := runNoActiveJobs(t, grain, service.ListNoActiveJobsParams{}, nil)

			require.Equal(t, model.NoActiveJobsQuery{
				Grain: grain,
				Sort:  model.NoActiveJobsSort{Key: model.NoActiveJobsSortCustomer},
				Limit: service.DefaultPageSize,
			}, q, "the customer's name, A to Z")
			require.Equal(t, 50, res.Limit)
			require.NotNil(t, res.Summary, "the first page carries the count")
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
		})
	}
}

func TestNoActiveJobsListNormalizesFilters(t *testing.T) {
	q, _ := runNoActiveJobs(t, model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{
		Status: []string{" LEAD ", "inactive", "lead", ""},
		Search: "  saka ",
	}, nil)

	require.Equal(t, []model.CustomerStatus{model.CustomerStatusLead, model.CustomerStatusInactive}, q.Filter.Statuses)
	require.Equal(t, "saka", q.Filter.Search)
}

func TestNoActiveJobsListSort(t *testing.T) {
	tests := []struct {
		name        string
		grain       model.NoActiveJobsGrain
		sort, order string
		want        model.NoActiveJobsSort
	}{
		{"neither: customer A to Z", model.NoActiveJobsCustomers, "", "", model.NoActiveJobsSort{Key: model.NoActiveJobsSortCustomer}},
		{"a key alone is ascending", model.NoActiveJobsCustomers, "status", "", model.NoActiveJobsSort{Key: model.NoActiveJobsSortStatus}},
		{"a key and desc", model.NoActiveJobsCustomers, "last_service_date", "desc", model.NoActiveJobsSort{Key: model.NoActiveJobsSortLastServiceDate, Desc: true}},
		{"an order alone sorts by customer", model.NoActiveJobsLocations, "", "desc", model.NoActiveJobsSort{Key: model.NoActiveJobsSortCustomer, Desc: true}},
		{"a location key", model.NoActiveJobsLocations, " ZIP ", "", model.NoActiveJobsSort{Key: model.NoActiveJobsSortZip}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runNoActiveJobs(t, tt.grain, service.ListNoActiveJobsParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	for _, grain := range bothGrains {
		t.Run("every sort key of "+string(grain)+" is accepted", func(t *testing.T) {
			for _, key := range model.NoActiveJobsSortKeysFor(grain) {
				q, _ := runNoActiveJobs(t, grain, service.ListNoActiveJobsParams{Sort: string(key)}, nil)
				require.Equal(t, key, q.Sort.Key)
			}
		})
	}

	t.Run("address columns exist for locations only", func(t *testing.T) {
		got := rejectsNoActiveJobs(t, model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{Sort: "city"})
		require.Equal(t, "must be one of: status, account_number, customer, last_service_type, job_status, last_service_date", got["sort"])
	})
}

func TestNoActiveJobsListPaging(t *testing.T) {
	tests := []struct {
		name        string
		limit       string
		fromRepo    int // rows the repository returns
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
			_, res := runNoActiveJobs(t, model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{Limit: tt.limit}, noActiveJobsRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}

			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeNoActiveJobsCursor(res.NextCursor, model.NoActiveJobsSort{Key: model.NoActiveJobsSortCustomer})
			require.NoError(t, err)
			require.Equal(t, &model.NoActiveJobsCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestNoActiveJobsListFollowsACursor(t *testing.T) {
	_, page1 := runNoActiveJobs(t, model.NoActiveJobsLocations, service.ListNoActiveJobsParams{Limit: "2"}, noActiveJobsRows(3))
	require.NotEmpty(t, page1.NextCursor)

	q, page2 := runNoActiveJobs(t, model.NoActiveJobsLocations, service.ListNoActiveJobsParams{Limit: "2", Cursor: "  " + page1.NextCursor + " "}, noActiveJobsRows(1))

	require.Equal(t, &model.NoActiveJobsCursor{SortValue: "customer 02", ID: 2}, q.After)
	require.Nil(t, page2.Summary, "later pages carry no count")
}

func TestNoActiveJobsListReturnsLastServiceInTheClocksZone(t *testing.T) {
	zone := time.FixedZone("UTC+7", 7*3600)
	at := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC) // 03:00 on the 16th in UTC+7
	repo := servicemocks.NewMockNoActiveJobsRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.NoActiveJobsPage{Rows: []model.NoActiveJobsRow{
		{ID: 1, LastService: &model.LastService{ServiceType: "Pool", JobStatus: "completed", At: at}},
		{ID: 2},
	}}, nil).Once()

	res, err := service.NewNoActiveJobsService(repo, fixedClock(october21.In(zone))).List(context.Background(),
		model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{})

	require.NoError(t, err)
	require.Equal(t, "2026-10-16", res.Items[0].LastService.At.Format("2006-01-02"))
	require.True(t, at.Equal(res.Items[0].LastService.At), "same instant")
	require.Nil(t, res.Items[1].LastService)
}

func TestNoActiveJobsListRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		p     service.ListNoActiveJobsParams
		field string
		msg   string
	}{
		{"unknown status", service.ListNoActiveJobsParams{Status: []string{"vip"}}, "status", "must be one of: active, inactive, lead, archived"},
		{"search too long", service.ListNoActiveJobsParams{Search: strings.Repeat("a", 101)}, "q", "must be at most 100 characters long"},
		{"bad order", service.ListNoActiveJobsParams{Order: "up"}, "order", "must be asc or desc"},
		{"limit zero", service.ListNoActiveJobsParams{Limit: "0"}, "limit", "must be a whole number between 1 and 200"},
		{"garbage cursor", service.ListNoActiveJobsParams{Cursor: "not-a-cursor"}, "cursor", "is not a valid cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsNoActiveJobs(t, model.NoActiveJobsCustomers, tt.p)
			require.Equal(t, tt.msg, got[tt.field])
		})
	}

	t.Run("a cursor from another sort is rejected", func(t *testing.T) {
		_, page1 := runNoActiveJobs(t, model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{Limit: "1"}, noActiveJobsRows(2))
		got := rejectsNoActiveJobs(t, model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{Sort: "status", Cursor: page1.NextCursor})
		require.Equal(t, "does not match the requested sort", got["cursor"])
	})
}

func TestNoActiveJobsListWrapsStorageErrors(t *testing.T) {
	boom := errors.New("connection reset")
	repo := servicemocks.NewMockNoActiveJobsRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.NoActiveJobsPage{}, boom).Once()

	_, err := service.NewNoActiveJobsService(repo, fixedClock(october21)).List(context.Background(),
		model.NoActiveJobsLocations, service.ListNoActiveJobsParams{})

	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, model.ErrInvalidInput)
	require.Contains(t, err.Error(), "list locations without active jobs")
}

func TestNewNoActiveJobsServiceDefaultsToUTC(t *testing.T) {
	at := time.Date(2026, 10, 15, 23, 30, 0, 0, time.UTC)
	repo := servicemocks.NewMockNoActiveJobsRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.NoActiveJobsPage{Rows: []model.NoActiveJobsRow{
		{ID: 1, LastService: &model.LastService{At: at.In(time.FixedZone("x", 9*3600))}},
	}}, nil).Once()

	res, err := service.NewNoActiveJobsService(repo, nil).List(context.Background(), model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{})

	require.NoError(t, err)
	require.Equal(t, time.UTC, res.Items[0].LastService.At.Location())
}

// the cursor tests -------------------------------------------------------------------------------------------------

var noActiveJobsSortValue = map[model.NoActiveJobsSortKey]string{
	model.NoActiveJobsSortStatus:          "inactive",
	model.NoActiveJobsSortAccountNumber:   "63716",
	model.NoActiveJobsSortCustomer:        "william saliba",
	model.NoActiveJobsSortAddress:         "12 alaba street",
	model.NoActiveJobsSortCity:            "austin",
	model.NoActiveJobsSortZip:             "78701",
	model.NoActiveJobsSortLastServiceType: "bi-monthly service",
	model.NoActiveJobsSortJobStatus:       "completed",
	model.NoActiveJobsSortLastServiceDate: "2026-10-15T08:00:00.000000Z",
}

func TestNoActiveJobsCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, noActiveJobsSortValue, len(model.NoActiveJobsLocationSortKeys), "every sort key needs a sample value")

	for _, key := range model.NoActiveJobsLocationSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.NoActiveJobsSort{Key: key, Desc: desc}
				r := model.NoActiveJobsRow{ID: 4242, SortValue: noActiveJobsSortValue[key]}

				raw := service.EncodeNoActiveJobsCursor(sort, r)
				got, err := service.DecodeNoActiveJobsCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.NoActiveJobsCursor{SortValue: noActiveJobsSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestNoActiveJobsCursorAcceptsTheNoJobValues(t *testing.T) {
	for _, tt := range []struct {
		key   model.NoActiveJobsSortKey
		value string
	}{
		{model.NoActiveJobsSortLastServiceDate, "0001-01-01T00:00:00.000000Z"},
		{model.NoActiveJobsSortLastServiceType, ""},
		{model.NoActiveJobsSortJobStatus, ""},
	} {
		t.Run(string(tt.key), func(t *testing.T) {
			sort := model.NoActiveJobsSort{Key: tt.key}
			raw := service.EncodeNoActiveJobsCursor(sort, model.NoActiveJobsRow{ID: 5, SortValue: tt.value})

			got, err := service.DecodeNoActiveJobsCursor(raw, sort)

			require.NoError(t, err)
			require.Equal(t, tt.value, got.SortValue)
		})
	}
}

func TestNoActiveJobsCursorRejectsInvalidCursors(t *testing.T) {
	customerAsc := model.NoActiveJobsSort{Key: model.NoActiveJobsSortCustomer}
	dateAsc := model.NoActiveJobsSort{Key: model.NoActiveJobsSortLastServiceDate}

	tests := []struct {
		name string
		raw  string
		sort model.NoActiveJobsSort
	}{
		{"not base64", "!!!", customerAsc},
		{"not json", encode("hello"), customerAsc},
		{"unknown field", encode(`{"k":"customer","d":false,"v":"a","id":1,"x":1}`), customerAsc},
		{"id missing", encode(`{"k":"customer","d":false,"v":"a"}`), customerAsc},
		{"another sort key", encode(`{"k":"status","d":false,"v":"a","id":1}`), customerAsc},
		{"another direction", encode(`{"k":"customer","d":true,"v":"a","id":1}`), customerAsc},
		{"date that is not a timestamp", encode(`{"k":"last_service_date","d":false,"v":"2026-10-15","id":1}`), dateAsc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.DecodeNoActiveJobsCursor(tt.raw, tt.sort)

			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
		})
	}
}
