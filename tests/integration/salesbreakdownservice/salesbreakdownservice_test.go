//go:build integration

package salesbreakdownservice_test

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

func newService(t *testing.T) *service.SalesBreakdownService {
	t.Helper()
	pool := testdb.SeededPool(t)
	return service.NewSalesBreakdownService(repository.NewSalesBreakdownRepository(pool), func() time.Time { return october21 })
}

func TestListDefaultsToThisYearByName(t *testing.T) {
	svc := newService(t)

	res, err := svc.List(context.Background(), service.ListSalesBreakdownParams{Type: "forecast", By: "service"})

	require.NoError(t, err)
	require.Equal(t, 2026, res.Year)
	require.Equal(t, model.SalesBreakdownForecast, res.Type)
	require.Equal(t, model.SalesBreakdownByService, res.By)
	require.Len(t, res.Items, 5)
	require.Equal(t, "Bi-Monthly Service", res.Items[0].Name)
	require.Equal(t, model.Money(183126), res.Summary.Total)
	require.Nil(t, res.Unassigned, "every forecast job has a service")
}

func TestListAnotherYearIsEmpty(t *testing.T) {
	svc := newService(t)

	res, err := svc.List(context.Background(), service.ListSalesBreakdownParams{Type: "revenue", By: "client", Year: "2025"})

	require.NoError(t, err)
	require.Empty(t, res.Items)
	require.Equal(t, model.SalesBreakdownSummary{}, *res.Summary)
	require.Nil(t, res.Unassigned)
}

func TestListRevenueMeasureAndPaging(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	first, err := svc.List(ctx, service.ListSalesBreakdownParams{Type: "revenue", By: "item", Measure: "collected_without_tax",
		Sort: "total", Order: "desc", Limit: "2"})
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.True(t, first.HasMore)
	require.NotNil(t, first.Summary)
	require.Less(t, int64(first.Summary.Total), int64(82106), "without tax is less than collected")

	second, err := svc.List(ctx, service.ListSalesBreakdownParams{Type: "revenue", By: "item", Measure: "collected_without_tax",
		Sort: "total", Order: "desc", Limit: "2", Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Items, 2)
	require.Nil(t, second.Summary)
	require.Nil(t, second.Unassigned)
	require.GreaterOrEqual(t, first.Items[1].Total, second.Items[0].Total)
}

func TestListRejectsInvalidInputBeforeTouchingTheDatabase(t *testing.T) {
	svc := newService(t)

	for _, p := range []service.ListSalesBreakdownParams{
		{By: "client"},
		{Type: "revenue"},
		{Type: "forecast", By: "client", Measure: "collected"},
		{Type: "forecast", By: "client", Method: []string{"card"}},
		{Type: "revenue", By: "item", Sort: "account_number"},
		{Type: "revenue", By: "client", Year: "1999"},
		{Type: "revenue", By: "client", Cursor: "garbage"},
	} {
		_, err := svc.List(context.Background(), p)
		require.ErrorIs(t, err, model.ErrInvalidInput, "%+v", p)
	}
}

func TestFilterOptions(t *testing.T) {
	svc := newService(t)

	opts, err := svc.FilterOptions(context.Background())

	require.NoError(t, err)
	require.Len(t, opts.Tags, 4)
	require.Len(t, opts.Staff, 2)
}
