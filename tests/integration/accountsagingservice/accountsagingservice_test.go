//go:build integration

package accountsagingservice_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// december1 is a day after every seeded date, for the tests that read balances weeks after October.
var december1 = time.Date(2026, 12, 1, 9, 30, 0, 0, time.UTC)

func newService(t *testing.T, now ...time.Time) *service.AccountsAgingService {
	t.Helper()
	pool := testdb.SeededPool(t)
	return service.NewAccountsAgingService(repository.NewAccountsAgingRepository(pool), func() time.Time {
		if len(now) > 0 {
			return now[0]
		}
		return october21
	})
}

func TestListDefaultsToTodayAndTheCurrentBalances(t *testing.T) {
	svc := newService(t)

	res, err := svc.List(context.Background(), service.ListAgingParams{})

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC), res.AsOf)
	require.Len(t, res.Items, 2)
	require.Equal(t, "AAA1 ccc1", res.Items[0].CustomerName)
	require.Equal(t, model.AgingSummary{Count: 2, D0to30: 16900, Total: 16900, AvailableCredit: 30000}, *res.Summary)
	require.False(t, res.HasMore)
	require.Empty(t, res.NextCursor)
}

func TestListPointInTime(t *testing.T) {
	svc := newService(t)

	res, err := svc.List(context.Background(), service.ListAgingParams{AsOf: "2026-10-20"})

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), res.AsOf)
	require.Equal(t, model.Money(10000), res.Items[0].Total)
}

func TestListPagesWithCursorsAndSummaryOnlyOnTheFirstPage(t *testing.T) {
	svc := newService(t, december1)
	ctx := context.Background()

	first, err := svc.List(ctx, service.ListAgingParams{AsOf: "2026-11-30", Limit: "2"})
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.True(t, first.HasMore)
	require.NotEmpty(t, first.NextCursor)
	require.NotNil(t, first.Summary)
	require.Equal(t, int64(3), first.Summary.Count, "the summary covers all pages")

	second, err := svc.List(ctx, service.ListAgingParams{AsOf: "2026-11-30", Limit: "2", Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Equal(t, []string{"William Saliba"}, []string{second.Items[0].CustomerName})
	require.False(t, second.HasMore)
	require.Nil(t, second.Summary)
}

func TestListRejectsInvalidInputBeforeTouchingTheDatabase(t *testing.T) {
	svc := newService(t)

	for _, p := range []service.ListAgingParams{
		{AsOf: "2026-10-22"},
		{Days: "45"},
		{Sort: "name"},
		{Cursor: "garbage"},
	} {
		_, err := svc.List(context.Background(), p)
		require.ErrorIs(t, err, model.ErrInvalidInput, "%+v", p)
	}
}

func TestFilterOptionsListServiceTypes(t *testing.T) {
	svc := newService(t)

	opts, err := svc.FilterOptions(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, opts.ServiceTypes)
}
