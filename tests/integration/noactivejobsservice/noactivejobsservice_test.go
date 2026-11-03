//go:build integration

package noactivejobsservice_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

func newService(t *testing.T, now time.Time) *service.NoActiveJobsService {
	t.Helper()
	pool := testdb.SeededPool(t)
	cancelSeedJobOf5001(t, pool)
	return service.NewNoActiveJobsService(repository.NewNoActiveJobsRepository(pool), func() time.Time { return now })
}

func TestListCustomersDefaultsToNameAscendingWithTheCount(t *testing.T) {
	svc := newService(t, october21)

	res, err := svc.List(context.Background(), model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{})

	require.NoError(t, err)
	require.Len(t, res.Items, 11)
	require.Equal(t, "Ada Lovelace", res.Items[0].CustomerName)
	require.Equal(t, &model.NoActiveJobsSummary{Count: 11}, res.Summary)
	require.False(t, res.HasMore)
}

func TestListLocations(t *testing.T) {
	svc := newService(t, october21)

	res, err := svc.List(context.Background(), model.NoActiveJobsLocations, service.ListNoActiveJobsParams{Sort: "city", Order: "desc"})

	require.NoError(t, err)
	require.Len(t, res.Items, 3)
	require.Equal(t, "San Antonio", res.Items[0].Address.City)
}

func TestListPagesWithCursorsAndSummaryOnlyOnTheFirstPage(t *testing.T) {
	svc := newService(t, october21)
	ctx := context.Background()

	first, err := svc.List(ctx, model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{Limit: "5", Sort: "last_service_date"})
	require.NoError(t, err)
	require.Len(t, first.Items, 5)
	require.True(t, first.HasMore)
	require.NotNil(t, first.Summary)
	require.Equal(t, int64(11), first.Summary.Count, "the summary covers all pages")

	second, err := svc.List(ctx, model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{
		Limit: "50", Sort: "last_service_date", Cursor: first.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, second.Items, 6)
	require.Nil(t, second.Summary)
	require.Equal(t, "5001", second.Items[5].AccountNumber, "the one customer with a job is last")
}

func TestListReturnsTheLastServiceInTheClocksTimeZone(t *testing.T) {
	// the 5001 job is at 2026-10-15 08:00 UTC; in UTC-10 that is still the 14th, in UTC+7 the 15th
	west := time.FixedZone("UTC-10", -10*3600)

	res, err := newService(t, october21.In(west)).List(context.Background(), model.NoActiveJobsCustomers,
		service.ListNoActiveJobsParams{Search: "5001"})

	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	at := res.Items[0].LastService.At
	require.Equal(t, west, at.Location())
	require.Equal(t, "2026-10-14", at.Format("2006-01-02"))
}

func TestListRejectsInvalidInputBeforeTouchingTheDatabase(t *testing.T) {
	svc := newService(t, october21)

	for _, tc := range []struct {
		grain model.NoActiveJobsGrain
		p     service.ListNoActiveJobsParams
	}{
		{model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{Status: []string{"vip"}}},
		{model.NoActiveJobsCustomers, service.ListNoActiveJobsParams{Sort: "city"}},
		{model.NoActiveJobsLocations, service.ListNoActiveJobsParams{Sort: "total"}},
		{model.NoActiveJobsLocations, service.ListNoActiveJobsParams{Cursor: "garbage"}},
	} {
		_, err := svc.List(context.Background(), tc.grain, tc.p)
		require.ErrorIs(t, err, model.ErrInvalidInput, "%s %+v", tc.grain, tc.p)
	}
}

// cancelSeedJobOf5001 cancels the seed's one completed job, that of customer 5001. A completed job counts as active, so
// 5001 would otherwise leave these reports; with the job cancelled it stays listed with a last service.
func cancelSeedJobOf5001(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE jobs SET status = 'cancelled'
		WHERE customer_id = (SELECT id FROM customers WHERE account_number = '5001')`)
	require.NoError(t, err)
}
