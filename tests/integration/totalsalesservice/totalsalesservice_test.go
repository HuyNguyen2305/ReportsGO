//go:build integration

package totalsalesservice_test

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

func newService(t *testing.T) *service.TotalSalesService {
	t.Helper()
	pool := testdb.SeededPool(t)
	return service.NewTotalSalesService(repository.NewTotalSalesRepository(pool), func() time.Time { return october21 })
}

func numbers(rows []model.TotalSaleRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.InvoiceNumber
	}
	return out
}

func TestListDefaultsToThisMonthNewestFirstWithASummary(t *testing.T) {
	svc := newService(t)

	res, err := svc.List(context.Background(), service.ListTotalSalesParams{})

	require.NoError(t, err)
	require.Len(t, res.Items, 12)
	require.False(t, res.HasMore)
	require.Empty(t, res.NextCursor)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.Equal(t, &model.TotalSaleSummary{Count: 12, Amount: 107506, WithoutTax: 91136}, res.Summary)
	require.Equal(t, "85031", res.Items[0].InvoiceNumber, "the newest sale first")
}

func TestListWalksEveryPageThroughTheCursor(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	var got []string
	cursor := ""
	var summaries int
	for pages := 0; pages < 10; pages++ {
		res, err := svc.List(ctx, service.ListTotalSalesParams{Limit: "5", Sort: "amount", Order: "desc", Cursor: cursor})
		require.NoError(t, err)
		if res.Summary != nil {
			summaries++
			require.Equal(t, int64(12), res.Summary.Count, "the summary covers the whole set, not the page")
		}
		got = append(got, numbers(res.Items)...)
		if !res.HasMore {
			require.Empty(t, res.NextCursor)
			break
		}
		cursor = res.NextCursor
	}

	require.Len(t, got, 12)
	require.ElementsMatch(t, got, numbers(mustList(t, svc, service.ListTotalSalesParams{Limit: "200"})), "every sale exactly once")
	require.Equal(t, 1, summaries, "only the first page carries the summary")
}

func mustList(t *testing.T, svc *service.TotalSalesService, p service.ListTotalSalesParams) []model.TotalSaleRow {
	t.Helper()
	res, err := svc.List(context.Background(), p)
	require.NoError(t, err)
	return res.Items
}

func TestListFiltersByDerivedStatusAndSortsByIt(t *testing.T) {
	svc := newService(t)

	partial := mustList(t, svc, service.ListTotalSalesParams{Status: []string{"partial"}})
	require.Equal(t, []string{"85032"}, numbers(partial))

	open := mustList(t, svc, service.ListTotalSalesParams{Status: []string{"sent", "partial"}, Sort: "status", Order: "desc"})
	require.Equal(t, []model.TotalSaleStatus{model.TotalSaleSent, model.TotalSaleSent, model.TotalSalePartial},
		[]model.TotalSaleStatus{open[0].Status, open[1].Status, open[2].Status})
}

func TestListRespectsTheDateRangeAndAnEmptyRangeHasAZeroSummary(t *testing.T) {
	svc := newService(t)

	res, err := svc.List(context.Background(), service.ListTotalSalesParams{From: "2026-09-01", To: "2026-09-30"})

	require.NoError(t, err)
	require.Empty(t, res.Items)
	require.NotNil(t, res.Items, "an empty page is an empty list")
	require.Equal(t, &model.TotalSaleSummary{}, res.Summary)
}

func TestListRejectsInvalidInputBeforeTouchingTheDatabase(t *testing.T) {
	svc := newService(t)

	_, err := svc.List(context.Background(), service.ListTotalSalesParams{Status: []string{"void"}, Limit: "0"})

	require.ErrorIs(t, err, model.ErrInvalidInput)
}

func TestListRejectsACursorFromAnotherSort(t *testing.T) {
	svc := newService(t)
	res, err := svc.List(context.Background(), service.ListTotalSalesParams{Limit: "2"})
	require.NoError(t, err)
	require.True(t, res.HasMore)

	_, err = svc.List(context.Background(), service.ListTotalSalesParams{Limit: "2", Sort: "amount", Cursor: res.NextCursor})

	require.ErrorIs(t, err, model.ErrInvalidInput)
}
