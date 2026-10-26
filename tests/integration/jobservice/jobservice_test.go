//go:build integration

package jobservice_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

func newService(t *testing.T) *service.JobService {
	t.Helper()
	pool := testdb.SeededPool(t)
	return service.NewJobService(repository.NewJobRepository(pool), func() time.Time { return october21 })
}

func ids(rows []model.JobRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestListDefault(t *testing.T) {
	svc := newService(t)

	res, err := svc.List(context.Background(), service.ListJobsParams{Limit: "200"})

	require.NoError(t, err)
	require.Len(t, res.Items, 35, "the October jobs only")
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.NotNil(t, res.Summary)
	require.EqualValues(t, 35, res.Summary.Count)
	require.Equal(t, model.Money(186126), res.Summary.Total)
	for i := 1; i < len(res.Items); i++ {
		require.False(t, res.Items[i].CreatedAt.After(res.Items[i-1].CreatedAt), "newest first")
	}
}

func TestFollowingNextCursorWalksEveryKeyAndDirectionExactly(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	for _, key := range model.JobSortKeys {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 7} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", key, order, limit), func(t *testing.T) {
					all, err := svc.List(ctx, service.ListJobsParams{Sort: string(key), Order: order, Limit: "200"})
					require.NoError(t, err)
					require.Len(t, all.Items, 35)

					p := service.ListJobsParams{Sort: string(key), Order: order, Limit: fmt.Sprint(limit)}
					var got []model.JobRow
					for page := 0; ; page++ {
						require.Less(t, page, 100, "paging must terminate")

						res, err := svc.List(ctx, p)
						require.NoError(t, err)
						require.Equal(t, limit, res.Limit)
						if page == 0 {
							require.NotNil(t, res.Summary, "the first page carries the totals")
							require.EqualValues(t, 35, res.Summary.Count)
							require.Equal(t, model.Money(186126), res.Summary.Total)
						} else {
							require.Nil(t, res.Summary, "later pages do not")
						}
						got = append(got, res.Items...)
						if !res.HasMore {
							require.Empty(t, res.NextCursor)
							break
						}
						require.NotEmpty(t, res.NextCursor)
						p.Cursor = res.NextCursor
					}

					require.Equal(t, ids(all.Items), ids(got), "pages concatenate to the full ordered list")
				})
			}
		}
	}
}

func TestMicrosecondTimestampsSurviveTheCursorRoundTrip(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	// The newest October job is created at 2026-10-22 08:15:30.123456. A page boundary right after it must
	// not lose or repeat it.
	first, err := svc.List(ctx, service.ListJobsParams{Limit: "1"})
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 22, 8, 15, 30, 123456000, time.UTC), first.Items[0].CreatedAt)
	require.True(t, first.HasMore)

	second, err := svc.List(ctx, service.ListJobsParams{Limit: "1", Cursor: first.NextCursor})
	require.NoError(t, err)
	require.NotEqual(t, first.Items[0].ID, second.Items[0].ID)
}

func TestFiltersAgreeWithTheCount(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	opts, err := svc.FilterOptions(ctx)
	require.NoError(t, err)
	typeID, sourceID := map[string]string{}, map[string]string{}
	for _, ty := range opts.ServiceTypes {
		typeID[ty.Name] = fmt.Sprint(ty.ID)
	}
	for _, s := range opts.LeadSources {
		sourceID[s.Name] = fmt.Sprint(s.ID)
	}
	require.Contains(t, typeID, "Bi-Monthly Service")
	require.Contains(t, typeID, "Initial Service")

	tests := []struct {
		name      string
		params    service.ListJobsParams
		wantCount int
	}{
		{"unconfirmed", service.ListJobsParams{Status: []string{"unconfirmed"}}, 6},
		{"cancelled", service.ListJobsParams{Status: []string{"cancelled"}}, 2},
		{"completed or cancelled, mixed case", service.ListJobsParams{Status: []string{"Completed", "CANCELLED"}}, 20},
		{"one service type", service.ListJobsParams{ServiceTypeIDs: []string{typeID["Initial Service"]}}, 4},
		{"two service types", service.ListJobsParams{ServiceTypeIDs: []string{typeID["Initial Service"], typeID["Bi-Monthly Service"]}}, 24},
		{"one source", service.ListJobsParams{LeadSourceIDs: []string{sourceID["Google"]}}, 2},
		{"two sources", service.ListJobsParams{LeadSourceIDs: []string{sourceID["Google"], sourceID["Referral"]}}, 3},
		{"status and source together", service.ListJobsParams{Status: []string{"cancelled"}, LeadSourceIDs: []string{sourceID["Google"]}}, 1},
		{"september 30 only", service.ListJobsParams{From: "2026-09-30", To: "2026-09-30"}, 1},
		{"november 1 only", service.ListJobsParams{From: "2026-11-01", To: "2026-11-01"}, 1},
		{"everything", service.ListJobsParams{From: "2000-01-01", To: "2100-01-01"}, 37},
		{"search by service type", service.ListJobsParams{Search: "initial serv"}, 4},
		{"search with wildcards matches nothing", service.ListJobsParams{Search: "%_"}, 0},
		{"search with injection text matches nothing", service.ListJobsParams{Search: `'; DROP TABLE jobs; --`}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.params.Limit = "200"
			res, err := svc.List(ctx, tt.params)

			require.NoError(t, err)
			require.Len(t, res.Items, tt.wantCount)
			require.NotNil(t, res.Summary)
			require.EqualValues(t, tt.wantCount, res.Summary.Count)
		})
	}
}

func TestTheLastDayIsIncludedToTheMicrosecond(t *testing.T) {
	svc := newService(t)

	res, err := svc.List(context.Background(), service.ListJobsParams{From: "2026-10-10", To: "2026-10-10", Limit: "200"})

	require.NoError(t, err)
	var starts, ends bool
	for _, r := range res.Items {
		starts = starts || r.CreatedAt.Equal(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
		ends = ends || r.CreatedAt.Equal(time.Date(2026, 10, 10, 23, 59, 59, 999999000, time.UTC))
	}
	require.True(t, starts, "the first instant of the day is in")
	require.True(t, ends, "the last microsecond of the day is in")
}

func TestFilterOptionsListTheSeededLookups(t *testing.T) {
	svc := newService(t)

	opts, err := svc.FilterOptions(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, opts.ServiceTypes)
	require.Len(t, opts.LeadSources, 3)
	for _, s := range opts.LeadSources {
		require.NotZero(t, s.ID)
		require.NotEmpty(t, s.Name)
	}
}

func TestInvalidInputIsReportedPerFieldWithoutHittingTheData(t *testing.T) {
	svc := newService(t)

	_, err := svc.List(context.Background(), service.ListJobsParams{
		Status: []string{"paid"}, ServiceTypeIDs: []string{"x"}, Sort: "discount", Limit: "0", Cursor: "garbage!",
	})

	require.ErrorIs(t, err, model.ErrInvalidInput)
	var in *model.InvalidInputError
	require.ErrorAs(t, err, &in)
	var got []string
	for _, f := range in.Fields {
		got = append(got, f.Field)
	}
	require.ElementsMatch(t, []string{"status", "service_type_id", "sort", "limit"}, got, "the cursor is not judged against an invalid sort")
}

func TestCursorsAreBoundToTheirSortKeyAndDirection(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	first, err := svc.List(ctx, service.ListJobsParams{Sort: "status", Order: "asc", Limit: "2"})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	for _, other := range []service.ListJobsParams{
		{Sort: "status", Order: "desc"},
		{Sort: "source", Order: "asc"},
	} {
		other.Limit, other.Cursor = "2", first.NextCursor
		_, err = svc.List(ctx, other)
		require.ErrorIs(t, err, model.ErrInvalidInput, "%+v", other)
	}
}

func TestListHonorsContextCancellation(t *testing.T) {
	svc := newService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.List(ctx, service.ListJobsParams{})
	require.ErrorIs(t, err, context.Canceled)

	_, err = svc.FilterOptions(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// The day boundaries of the created range follow the clock's time zone (APP_TIMEZONE in the app), not UTC.
// One record is created at 01:00 on Nov 1 in UTC+7 (still Oct 31 in UTC); another at 23:59 on Oct 31 in UTC+7.
func TestCreatedDaysFollowTheClocksTimezone(t *testing.T) {
	ctx := context.Background()
	pool := testdb.SeededPool(t)
	insert := func(at string) int64 {
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO jobs (customer_id, location_id, service_type_id, created_at)
			SELECT l.customer_id, l.id, st.id, $1::timestamptz
			FROM (SELECT id, customer_id FROM service_locations ORDER BY id LIMIT 1) l,
			     (SELECT id FROM service_types ORDER BY id LIMIT 1) st RETURNING id`, at).Scan(&id))
		return id
	}
	early := insert("2026-10-31 18:00:00+00") // Nov 1, 01:00 in UTC+7
	late := insert("2026-10-31 16:59:00+00")  // Oct 31, 23:59 in UTC+7

	utc7 := time.FixedZone("UTC+7", 7*3600)
	list := func(now time.Time, p service.ListJobsParams) []int64 {
		t.Helper()
		p.Limit = "200"
		svc := service.NewJobService(repository.NewJobRepository(pool), func() time.Time { return now })
		res, err := svc.List(ctx, p)
		require.NoError(t, err)
		return ids(res.Items)
	}
	october := service.ListJobsParams{From: "2026-10-01", To: "2026-10-31"}

	t.Run("a UTC+7 clock reads the default month, and explicit days, in UTC+7", func(t *testing.T) {
		november := list(time.Date(2026, 11, 15, 12, 0, 0, 0, utc7), service.ListJobsParams{})
		require.Contains(t, november, early)
		require.NotContains(t, november, late)

		inOctober := list(time.Date(2026, 11, 15, 12, 0, 0, 0, utc7), october)
		require.Contains(t, inOctober, late)
		require.NotContains(t, inOctober, early)
	})

	t.Run("a UTC clock reads the same days in UTC", func(t *testing.T) {
		november := list(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC), service.ListJobsParams{})
		require.NotContains(t, november, early)
		require.NotContains(t, november, late)

		inOctober := list(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC), october)
		require.Contains(t, inOctober, early)
		require.Contains(t, inOctober, late)
	})
}
