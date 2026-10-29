//go:build integration

package servicelookupservice_test

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

func newService(t *testing.T, now time.Time) *service.ServiceLookupService {
	t.Helper()
	pool := testdb.SeededPool(t)
	return service.NewServiceLookupService(repository.NewServiceLookupRepository(pool), func() time.Time { return now })
}

func TestListDefaultsToTheActiveTabThisMonthEarliestFirst(t *testing.T) {
	svc := newService(t, october21)

	res, err := svc.List(context.Background(), service.ListServiceLookupParams{Limit: "200"})

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.Len(t, res.Items, 31, "33 active jobs, two of them outside October")
	require.Equal(t, int64(31), res.Summary.Count)
	// the tab counts use the same October range: the three pooled jobs are scheduled in October, and so is the deleted job
	require.Equal(t, model.ServiceLookupTabCounts{Active: 31, WorkPool: 3, Deleted: 1}, res.Summary.Tabs)
}

func TestListWorkPoolHasAnOpenRangeByDefault(t *testing.T) {
	svc := newService(t, october21)

	res, err := svc.List(context.Background(), service.ListServiceLookupParams{Tab: "work_pool"})

	require.NoError(t, err)
	require.True(t, res.From.IsZero())
	require.True(t, res.To.IsZero())
	require.Len(t, res.Items, 3)
	require.Equal(t, model.Money(28500), res.Summary.InvoiceValue)
}

func TestListDeletedTabAndExplicitRange(t *testing.T) {
	svc := newService(t, october21)

	res, err := svc.List(context.Background(), service.ListServiceLookupParams{Tab: "deleted", From: "2026-10-22", To: "2026-10-22"})

	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.Equal(t, model.JobCancelled, res.Items[0].Status)
	require.Equal(t, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), res.From)
}

func TestListPagesWithCursorsAndSummaryOnlyOnTheFirstPage(t *testing.T) {
	svc := newService(t, october21)
	ctx := context.Background()

	first, err := svc.List(ctx, service.ListServiceLookupParams{Limit: "20", Sort: "invoice_value", Order: "desc"})
	require.NoError(t, err)
	require.Len(t, first.Items, 20)
	require.True(t, first.HasMore)
	require.NotNil(t, first.Summary)

	second, err := svc.List(ctx, service.ListServiceLookupParams{Limit: "20", Sort: "invoice_value", Order: "desc", Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Items, 11)
	require.False(t, second.HasMore)
	require.Nil(t, second.Summary)
	require.GreaterOrEqual(t, first.Items[19].InvoiceValue, second.Items[0].InvoiceValue)
}

func TestListRejectsInvalidInputBeforeTouchingTheDatabase(t *testing.T) {
	svc := newService(t, october21)

	for _, p := range []service.ListServiceLookupParams{
		{Tab: "archived"},
		{Status: []string{"done"}},
		{Tag: []string{"x"}},
		{Sort: "total"},
		{Cursor: "garbage"},
		{From: "2026-10-05", To: "2026-10-04"},
	} {
		_, err := svc.List(context.Background(), p)
		require.ErrorIs(t, err, model.ErrInvalidInput, "%+v", p)
	}
}

func TestFilterOptions(t *testing.T) {
	svc := newService(t, october21)

	opts, err := svc.FilterOptions(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, opts.ServiceTypes)
	require.Len(t, opts.Staff, 2)
	require.Len(t, opts.Tags, 4)
}
