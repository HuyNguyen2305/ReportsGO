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

func serviceLookupRows(n int) []model.ServiceLookupRow {
	out := make([]model.ServiceLookupRow, n)
	for i := range out {
		out[i] = model.ServiceLookupRow{ID: int64(i + 1), SortValue: fmt.Sprintf("2026-10-%02dT08:00:00.000000Z", i+1)}
	}
	return out
}

// runServiceLookup calls List on a service whose repository returns rows, and returns the query it received.
func runServiceLookup(t *testing.T, p service.ListServiceLookupParams, rows []model.ServiceLookupRow) (model.ServiceLookupQuery, *service.ListServiceLookupResult) {
	t.Helper()

	repo := servicemocks.NewMockServiceLookupRepository(t)
	var got model.ServiceLookupQuery
	// the summary is asked for on the first page only
	withSummary := strings.TrimSpace(p.Cursor) == ""
	page := model.ServiceLookupPage{Rows: rows}
	if withSummary {
		page.Summary = &model.ServiceLookupSummary{}
	}
	repo.EXPECT().Page(mock.Anything, mock.Anything, withSummary).
		Run(func(_ context.Context, q model.ServiceLookupQuery, _ bool) { got = q }).
		Return(page, nil).Once()

	res, err := service.NewServiceLookupService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsServiceLookup asserts that p is invalid and that the repository is never touched.
func rejectsServiceLookup(t *testing.T, p service.ListServiceLookupParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockServiceLookupRepository(t) // no expectations: any call fails the test
	_, err := service.NewServiceLookupService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestServiceLookupListDefaults(t *testing.T) {
	q, res := runServiceLookup(t, service.ListServiceLookupParams{}, nil)

	require.Equal(t, model.ServiceLookupQuery{
		Filter: model.ServiceLookupFilter{
			Tab:             model.ServiceLookupActive,
			ScheduledFrom:   date(2026, 10, 1),
			ScheduledBefore: date(2026, 11, 1),
		},
		Sort:  model.ServiceLookupSort{Key: model.ServiceLookupSortScheduledAt},
		Limit: service.DefaultPageSize,
	}, q, "the Active tab, this month, earliest first")
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
	require.NotNil(t, res.Summary, "the first page carries the summary")
	require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
}

func TestServiceLookupListTabAndDateRange(t *testing.T) {
	tests := []struct {
		name          string
		tab, from, to string
		wantTab       model.ServiceLookupTab
		wantFrom      time.Time
		wantBefore    time.Time
	}{
		{"active defaults to the month", "", "", "", model.ServiceLookupActive, date(2026, 10, 1), date(2026, 11, 1)},
		{"deleted defaults to the month", "deleted", "", "", model.ServiceLookupDeleted, date(2026, 10, 1), date(2026, 11, 1)},
		{"work pool is open", "work_pool", "", "", model.ServiceLookupWorkPool, time.Time{}, time.Time{}},
		{"case and blanks are ignored", " WORK_POOL ", "", "", model.ServiceLookupWorkPool, time.Time{}, time.Time{}},
		{"an explicit range wins on the work pool", "work_pool", "2026-09-01", "2026-09-30", model.ServiceLookupWorkPool, date(2026, 9, 1), date(2026, 10, 1)},
		{"only from leaves the end open", "", "2026-10-05", "", model.ServiceLookupActive, date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", "", "", "2026-10-05", model.ServiceLookupActive, time.Time{}, date(2026, 10, 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runServiceLookup(t, service.ListServiceLookupParams{Tab: tt.tab, From: tt.from, To: tt.to}, nil)

			require.Equal(t, tt.wantTab, q.Filter.Tab)
			require.Equal(t, tt.wantFrom, q.Filter.ScheduledFrom)
			require.Equal(t, tt.wantBefore, q.Filter.ScheduledBefore)
		})
	}

	t.Run("the days are read in the clock's time zone", func(t *testing.T) {
		zone := time.FixedZone("UTC+7", 7*3600)
		repo := servicemocks.NewMockServiceLookupRepository(t)
		var got model.ServiceLookupQuery
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).
			Run(func(_ context.Context, q model.ServiceLookupQuery, _ bool) { got = q }).
			Return(model.ServiceLookupPage{}, nil).Once()

		_, err := service.NewServiceLookupService(repo, fixedClock(october21.In(zone))).List(context.Background(),
			service.ListServiceLookupParams{From: "2026-10-05", To: "2026-10-05"})

		require.NoError(t, err)
		require.True(t, time.Date(2026, 10, 5, 0, 0, 0, 0, zone).Equal(got.Filter.ScheduledFrom))
		require.True(t, time.Date(2026, 10, 6, 0, 0, 0, 0, zone).Equal(got.Filter.ScheduledBefore))
	})
}

func TestServiceLookupListNormalizesFilters(t *testing.T) {
	q, _ := runServiceLookup(t, service.ListServiceLookupParams{
		Status:      []string{" CONFIRMED ", "completed", "confirmed", ""},
		ServiceType: []string{"5", " 3 ", "5"},
		AssignedTo:  []string{"2", "2", "1"},
		Tag:         []string{"4", "1", "4"},
		Search:      "  elm ",
	}, nil)

	require.Equal(t, []model.JobStatus{model.JobConfirmed, model.JobCompleted}, q.Filter.Statuses)
	require.Equal(t, []int64{5, 3}, q.Filter.ServiceTypeIDs)
	require.Equal(t, []int64{2, 1}, q.Filter.AssignedToIDs)
	require.Equal(t, []int64{4, 1}, q.Filter.TagIDs)
	require.Equal(t, "elm", q.Filter.Search)
}

func TestServiceLookupListSort(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.ServiceLookupSort
	}{
		{"neither: earliest scheduled first", "", "", model.ServiceLookupSort{Key: model.ServiceLookupSortScheduledAt}},
		{"a key alone is ascending", "invoice_value", "", model.ServiceLookupSort{Key: model.ServiceLookupSortInvoiceValue}},
		{"a key and desc", "without_tax", "desc", model.ServiceLookupSort{Key: model.ServiceLookupSortWithoutTax, Desc: true}},
		{"an order alone sorts by schedule", "", "desc", model.ServiceLookupSort{Key: model.ServiceLookupSortScheduledAt, Desc: true}},
		{"case is ignored", " Assigned_To ", " DESC ", model.ServiceLookupSort{Key: model.ServiceLookupSortAssignedTo, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runServiceLookup(t, service.ListServiceLookupParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}

	t.Run("every sort key is accepted", func(t *testing.T) {
		for _, key := range model.ServiceLookupSortKeys {
			q, _ := runServiceLookup(t, service.ListServiceLookupParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		}
	})
}

func TestServiceLookupListPaging(t *testing.T) {
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
			_, res := runServiceLookup(t, service.ListServiceLookupParams{Limit: tt.limit}, serviceLookupRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}

			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeServiceLookupCursor(res.NextCursor, model.ServiceLookupSort{Key: model.ServiceLookupSortScheduledAt})
			require.NoError(t, err)
			require.Equal(t, &model.ServiceLookupCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestServiceLookupListFollowsACursor(t *testing.T) {
	_, page1 := runServiceLookup(t, service.ListServiceLookupParams{Limit: "2"}, serviceLookupRows(3))
	require.NotEmpty(t, page1.NextCursor)

	q, page2 := runServiceLookup(t, service.ListServiceLookupParams{Limit: "2", Cursor: "  " + page1.NextCursor + " "}, serviceLookupRows(1))

	require.Equal(t, &model.ServiceLookupCursor{SortValue: "2026-10-02T08:00:00.000000Z", ID: 2}, q.After)
	require.Nil(t, page2.Summary, "later pages carry no summary")
}

func TestServiceLookupListSummaryComesFromTheRepositoryOnTheFirstPage(t *testing.T) {
	want := &model.ServiceLookupSummary{Count: 33, InvoiceValue: 161126, WithoutTax: 158260,
		Tabs: model.ServiceLookupTabCounts{Active: 33, WorkPool: 3, Deleted: 1}}
	repo := servicemocks.NewMockServiceLookupRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.ServiceLookupPage{Summary: want}, nil).Once()

	res, err := service.NewServiceLookupService(repo, fixedClock(october21)).List(context.Background(), service.ListServiceLookupParams{})

	require.NoError(t, err)
	require.Equal(t, want, res.Summary)
}

func TestServiceLookupListRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		p     service.ListServiceLookupParams
		field string
		msg   string
	}{
		{"unknown tab", service.ListServiceLookupParams{Tab: "archived"}, "tab", "must be one of: active, work_pool, deleted"},
		{"bad from", service.ListServiceLookupParams{From: "10/01/2026"}, "from", "must be a date in YYYY-MM-DD format"},
		{"to before from", service.ListServiceLookupParams{From: "2026-10-05", To: "2026-10-04"}, "to", "must not be before from"},
		{"unknown status", service.ListServiceLookupParams{Status: []string{"done"}}, "status", "must be one of: unconfirmed, confirmed, completed, cancelled"},
		{"service type not a number", service.ListServiceLookupParams{ServiceType: []string{"abc"}}, "service_type", "must be positive whole numbers"},
		{"assigned to zero", service.ListServiceLookupParams{AssignedTo: []string{"0"}}, "assigned_to", "must be positive whole numbers"},
		{"tag not a number", service.ListServiceLookupParams{Tag: []string{"x"}}, "tag", "must be positive whole numbers"},
		{"search too long", service.ListServiceLookupParams{Search: strings.Repeat("a", 101)}, "q", "must be at most 100 characters long"},
		{"unknown sort", service.ListServiceLookupParams{Sort: "total"}, "sort",
			"must be one of: customer, service_type, scheduled_at, address, city, zip, county, invoice_value, without_tax, status, assigned_to"},
		{"bad order", service.ListServiceLookupParams{Order: "up"}, "order", "must be asc or desc"},
		{"limit zero", service.ListServiceLookupParams{Limit: "0"}, "limit", "must be a whole number between 1 and 200"},
		{"garbage cursor", service.ListServiceLookupParams{Cursor: "not-a-cursor"}, "cursor", "is not a valid cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsServiceLookup(t, tt.p)
			require.Equal(t, tt.msg, got[tt.field])
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsServiceLookup(t, service.ListServiceLookupParams{Tab: "x", Status: []string{"x"}, Limit: "0", Order: "up"})
		require.Len(t, got, 4)
	})

	t.Run("a cursor from another sort is rejected", func(t *testing.T) {
		_, page1 := runServiceLookup(t, service.ListServiceLookupParams{Limit: "1"}, serviceLookupRows(2))
		got := rejectsServiceLookup(t, service.ListServiceLookupParams{Sort: "customer", Cursor: page1.NextCursor})
		require.Equal(t, "does not match the requested sort", got["cursor"])
	})
}

func TestServiceLookupListWrapsStorageErrors(t *testing.T) {
	boom := errors.New("connection reset")
	repo := servicemocks.NewMockServiceLookupRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.ServiceLookupPage{}, boom).Once()

	_, err := service.NewServiceLookupService(repo, fixedClock(october21)).List(context.Background(), service.ListServiceLookupParams{})

	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, model.ErrInvalidInput)
	require.Contains(t, err.Error(), "list service lookup")
}

func TestServiceLookupFilterOptions(t *testing.T) {
	types := []model.ServiceType{{ID: 1, Name: "Pool Cleaning"}}
	staff := []model.StaffOption{{ID: 2, Name: "thon nguyen"}}
	tags := []model.JobTag{{ID: 3, Name: "VIP"}}

	t.Run("returns all three lists", func(t *testing.T) {
		repo := servicemocks.NewMockServiceLookupRepository(t)
		repo.EXPECT().ServiceTypes(mock.Anything).Return(types, nil).Once()
		repo.EXPECT().Staff(mock.Anything).Return(staff, nil).Once()
		repo.EXPECT().Tags(mock.Anything).Return(tags, nil).Once()

		opts, err := service.NewServiceLookupService(repo, fixedClock(october21)).FilterOptions(context.Background())

		require.NoError(t, err)
		require.Equal(t, &service.ServiceLookupFilterOptions{ServiceTypes: types, Staff: staff, Tags: tags}, opts)
	})

	boom := errors.New("down")
	failures := map[string]func(*servicemocks.MockServiceLookupRepository){
		"service types": func(r *servicemocks.MockServiceLookupRepository) {
			r.EXPECT().ServiceTypes(mock.Anything).Return(nil, boom).Once()
		},
		"staff": func(r *servicemocks.MockServiceLookupRepository) {
			r.EXPECT().ServiceTypes(mock.Anything).Return(types, nil).Once()
			r.EXPECT().Staff(mock.Anything).Return(nil, boom).Once()
		},
		"tags": func(r *servicemocks.MockServiceLookupRepository) {
			r.EXPECT().ServiceTypes(mock.Anything).Return(types, nil).Once()
			r.EXPECT().Staff(mock.Anything).Return(staff, nil).Once()
			r.EXPECT().Tags(mock.Anything).Return(nil, boom).Once()
		},
	}
	for name, setup := range failures {
		t.Run("wraps a "+name+" error", func(t *testing.T) {
			repo := servicemocks.NewMockServiceLookupRepository(t)
			setup(repo)

			_, err := service.NewServiceLookupService(repo, fixedClock(october21)).FilterOptions(context.Background())

			require.ErrorIs(t, err, boom)
			require.Contains(t, err.Error(), "load filter options")
		})
	}
}

// the cursor tests -------------------------------------------------------------------------------------------------

var serviceLookupSortValue = map[model.ServiceLookupSortKey]string{
	model.ServiceLookupSortCustomer:     "william saliba",
	model.ServiceLookupSortServiceType:  "bi-monthly service",
	model.ServiceLookupSortScheduledAt:  "2026-10-15T08:00:00.000000Z",
	model.ServiceLookupSortAddress:      "12 alaba street",
	model.ServiceLookupSortCity:         "austin",
	model.ServiceLookupSortZip:          "78701",
	model.ServiceLookupSortCounty:       "travis county",
	model.ServiceLookupSortInvoiceValue: "1234.50",
	model.ServiceLookupSortWithoutTax:   "1121.00",
	model.ServiceLookupSortStatus:       "confirmed",
	model.ServiceLookupSortAssignedTo:   "thọ nguyễn quang",
}

func TestServiceLookupCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, serviceLookupSortValue, len(model.ServiceLookupSortKeys), "every sort key needs a sample value")

	for _, key := range model.ServiceLookupSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.ServiceLookupSort{Key: key, Desc: desc}
				r := model.ServiceLookupRow{ID: 4242, SortValue: serviceLookupSortValue[key]}

				raw := service.EncodeServiceLookupCursor(sort, r)
				got, err := service.DecodeServiceLookupCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.ServiceLookupCursor{SortValue: serviceLookupSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestServiceLookupCursorAcceptsTheUnscheduledAndEmptyValues(t *testing.T) {
	for _, tt := range []struct {
		key   model.ServiceLookupSortKey
		value string
	}{
		{model.ServiceLookupSortScheduledAt, "0001-01-01T00:00:00.000000Z"},
		{model.ServiceLookupSortAssignedTo, ""},
		{model.ServiceLookupSortCounty, ""},
	} {
		t.Run(string(tt.key), func(t *testing.T) {
			sort := model.ServiceLookupSort{Key: tt.key}
			raw := service.EncodeServiceLookupCursor(sort, model.ServiceLookupRow{ID: 5, SortValue: tt.value})

			got, err := service.DecodeServiceLookupCursor(raw, sort)

			require.NoError(t, err)
			require.Equal(t, tt.value, got.SortValue)
		})
	}
}

func TestServiceLookupCursorRejectsInvalidCursors(t *testing.T) {
	customerAsc := model.ServiceLookupSort{Key: model.ServiceLookupSortCustomer}
	scheduledAsc := model.ServiceLookupSort{Key: model.ServiceLookupSortScheduledAt}
	valueAsc := model.ServiceLookupSort{Key: model.ServiceLookupSortInvoiceValue}
	withoutTaxAsc := model.ServiceLookupSort{Key: model.ServiceLookupSortWithoutTax}

	tests := []struct {
		name string
		raw  string
		sort model.ServiceLookupSort
	}{
		{"not base64", "!!!", customerAsc},
		{"not json", encode("hello"), customerAsc},
		{"unknown field", encode(`{"k":"customer","d":false,"v":"a","id":1,"x":1}`), customerAsc},
		{"id missing", encode(`{"k":"customer","d":false,"v":"a"}`), customerAsc},
		{"another sort key", encode(`{"k":"status","d":false,"v":"a","id":1}`), customerAsc},
		{"another direction", encode(`{"k":"customer","d":true,"v":"a","id":1}`), customerAsc},
		{"scheduled that is not a timestamp", encode(`{"k":"scheduled_at","d":false,"v":"2026-10-15","id":1}`), scheduledAsc},
		{"value that is not money", encode(`{"k":"invoice_value","d":false,"v":"abc","id":1}`), valueAsc},
		{"without tax with three decimals", encode(`{"k":"without_tax","d":false,"v":"1.234","id":1}`), withoutTaxAsc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.DecodeServiceLookupCursor(tt.raw, tt.sort)

			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
		})
	}
}
