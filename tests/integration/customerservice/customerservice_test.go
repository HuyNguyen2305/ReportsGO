//go:build integration

package customerservice_test

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

func newService(t *testing.T) *service.CustomerService {
	t.Helper()
	pool := testdb.SeededPool(t)
	return service.NewCustomerService(repository.NewCustomerRepository(pool), func() time.Time { return october21 })
}

func ids(rows []model.CustomerRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestListDefault(t *testing.T) {
	svc := newService(t)

	res, err := svc.List(context.Background(), service.ListCustomersParams{Limit: "200"})

	require.NoError(t, err)
	require.Len(t, res.Items, 14, "the October customers only")
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.NotNil(t, res.Summary)
	require.EqualValues(t, 14, res.Summary.Count)
	for i := 1; i < len(res.Items); i++ {
		require.False(t, res.Items[i].CreatedAt.After(res.Items[i-1].CreatedAt), "newest first")
	}
}

func TestFollowingNextCursorWalksEveryKeyAndDirectionExactly(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	for _, key := range model.CustomerSortKeys {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 5} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", key, order, limit), func(t *testing.T) {
					all, err := svc.List(ctx, service.ListCustomersParams{Sort: string(key), Order: order, Limit: "200"})
					require.NoError(t, err)
					require.Len(t, all.Items, 14)

					p := service.ListCustomersParams{Sort: string(key), Order: order, Limit: fmt.Sprint(limit)}
					var got []model.CustomerRow
					for page := 0; ; page++ {
						require.Less(t, page, 100, "paging must terminate")

						res, err := svc.List(ctx, p)
						require.NoError(t, err)
						require.Equal(t, limit, res.Limit)
						if page == 0 {
							require.NotNil(t, res.Summary, "the first page carries the count")
							require.EqualValues(t, 14, res.Summary.Count)
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

	// The newest October customer is created at 2026-10-22 08:15:30.123456. A page boundary right after it must
	// not lose or repeat it.
	first, err := svc.List(ctx, service.ListCustomersParams{Limit: "1"})
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 22, 8, 15, 30, 123456000, time.UTC), first.Items[0].CreatedAt)
	require.True(t, first.HasMore)

	second, err := svc.List(ctx, service.ListCustomersParams{Limit: "1", Cursor: first.NextCursor})
	require.NoError(t, err)
	require.NotEqual(t, first.Items[0].ID, second.Items[0].ID)
}

func TestFiltersAgreeWithTheCount(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	opts, err := svc.FilterOptions(ctx)
	require.NoError(t, err)
	sourceID := map[string]string{}
	for _, s := range opts.LeadSources {
		sourceID[s.Name] = fmt.Sprint(s.ID)
	}
	require.Contains(t, sourceID, "Google")
	require.Contains(t, sourceID, "Facebook")

	tests := []struct {
		name      string
		params    service.ListCustomersParams
		wantCount int
	}{
		{"one source", service.ListCustomersParams{LeadSourceIDs: []string{sourceID["Facebook"]}}, 3},
		{"google in october", service.ListCustomersParams{LeadSourceIDs: []string{sourceID["Google"]}}, 1},
		{"two sources", service.ListCustomersParams{LeadSourceIDs: []string{sourceID["Referral"], sourceID["Google"]}}, 2},
		{"september", service.ListCustomersParams{From: "2026-09-01", To: "2026-09-30"}, 4},
		{"september 30 only", service.ListCustomersParams{From: "2026-09-30", To: "2026-09-30"}, 1},
		{"november 1 only", service.ListCustomersParams{From: "2026-11-01", To: "2026-11-01"}, 1},
		{"everything", service.ListCustomersParams{From: "2000-01-01", To: "2100-01-01"}, 19},
		{"search by name", service.ListCustomersParams{Search: "lovelace"}, 1},
		{"search by email", service.ListCustomersParams{Search: "EXAMPLE.com"}, 4},
		{"search by phone", service.ListCustomersParams{Search: "0606"}, 1},
		{"percent is literal", service.ListCustomersParams{Search: "50%"}, 1},
		{"a wildcard does not match everything", service.ListCustomersParams{Search: "a%b"}, 0},
		{"underscore is literal", service.ListCustomersParams{Search: "a_b"}, 1},
		{"injection text matches nothing", service.ListCustomersParams{Search: `'; DROP TABLE customers; --`}, 0},
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

	res, err := svc.List(context.Background(), service.ListCustomersParams{From: "2026-10-10", To: "2026-10-10", Limit: "200"})

	require.NoError(t, err)
	var starts, ends bool
	for _, r := range res.Items {
		starts = starts || r.CreatedAt.Equal(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
		ends = ends || r.CreatedAt.Equal(time.Date(2026, 10, 10, 23, 59, 59, 999999000, time.UTC))
	}
	require.True(t, starts, "the first instant of the day is in")
	require.True(t, ends, "the last microsecond of the day is in")
}

func TestFilterOptionsListTheSeededLeadSources(t *testing.T) {
	svc := newService(t)

	opts, err := svc.FilterOptions(context.Background())

	require.NoError(t, err)
	require.Len(t, opts.LeadSources, 3)
	for _, s := range opts.LeadSources {
		require.NotZero(t, s.ID)
		require.NotEmpty(t, s.Name)
	}
}

func TestInvalidInputIsReportedPerFieldWithoutHittingTheData(t *testing.T) {
	svc := newService(t)

	_, err := svc.List(context.Background(), service.ListCustomersParams{
		LeadSourceIDs: []string{"x"}, Sort: "discount", Limit: "0", Cursor: "garbage!",
	})

	require.ErrorIs(t, err, model.ErrInvalidInput)
	var in *model.InvalidInputError
	require.ErrorAs(t, err, &in)
	var got []string
	for _, f := range in.Fields {
		got = append(got, f.Field)
	}
	require.ElementsMatch(t, []string{"lead_source_id", "sort", "limit"}, got, "the cursor is not judged against an invalid sort")
}

func TestCursorsAreBoundToTheirSortKeyAndDirection(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	first, err := svc.List(ctx, service.ListCustomersParams{Sort: "email", Order: "asc", Limit: "2"})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	for _, other := range []service.ListCustomersParams{
		{Sort: "email", Order: "desc"},
		{Sort: "phone", Order: "asc"},
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

	_, err := svc.List(ctx, service.ListCustomersParams{})
	require.ErrorIs(t, err, context.Canceled)

	_, err = svc.FilterOptions(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
