package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
	servicemocks "reportsgo/tests/mocks/service"
)

var dateDesc = model.InvoiceSort{Key: model.SortDate, Desc: true}

func TestListPagingSignalsAnotherPageWhenOneExtraRowComesBack(t *testing.T) {
	// The repository returns limit+1 rows when more exist.
	rows := []model.InvoiceRow{row(10, "2026-10-10"), row(9, "2026-10-09"), row(8, "2026-10-08"), row(7, "2026-10-07")}

	q, res := run(t, service.ListInvoicesParams{Limit: "3"}, rows)

	require.Equal(t, 3, q.Limit, "the service asks for the page size; the repository adds the extra row itself")
	require.Equal(t, 3, res.Limit, "the result reports the effective page size")
	require.True(t, res.HasMore)
	require.Equal(t, []int64{10, 9, 8}, []int64{res.Items[0].ID, res.Items[1].ID, res.Items[2].ID})
	require.Len(t, res.Items, 3, "the extra row is dropped")

	cur, err := service.DecodeInvoiceCursor(res.NextCursor, dateDesc)
	require.NoError(t, err)
	require.Equal(t, &model.InvoiceCursor{SortValue: "2026-10-08", ID: 8}, cur,
		"the cursor points at the last row kept, not at the extra row")
}

func TestListPagingHasNoCursorOnTheLastPage(t *testing.T) {
	tests := []struct {
		name string
		rows []model.InvoiceRow
		want int
	}{
		{"exactly a full page", []model.InvoiceRow{row(3, "2026-10-03"), row(2, "2026-10-02"), row(1, "2026-10-01")}, 3},
		{"a partial page", []model.InvoiceRow{row(2, "2026-10-02"), row(1, "2026-10-01")}, 2},
		{"a single row", []model.InvoiceRow{row(1, "2026-10-01")}, 1},
		{"no rows", []model.InvoiceRow{}, 0},
		{"nil rows", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, res := run(t, service.ListInvoicesParams{Limit: "3"}, tt.rows)

			require.False(t, res.HasMore)
			require.Empty(t, res.NextCursor)
			require.NotNil(t, res.Items, "an empty page is an empty list, never null")
			require.Len(t, res.Items, tt.want)
		})
	}
}

func TestListFollowsACursorToTheNextPage(t *testing.T) {
	first := []model.InvoiceRow{row(10, "2026-10-10"), row(9, "2026-10-09"), row(8, "2026-10-08")}
	_, page1 := run(t, service.ListInvoicesParams{Limit: "2"}, first)
	require.NotEmpty(t, page1.NextCursor)

	// Second request: the repository must receive the decoded cursor.
	q, page2 := run(t, service.ListInvoicesParams{Limit: "2", Cursor: page1.NextCursor}, []model.InvoiceRow{row(8, "2026-10-08")})

	require.Equal(t, &model.InvoiceCursor{SortValue: "2026-10-09", ID: 9}, q.After)
	require.False(t, page2.HasMore)
	require.Empty(t, page2.NextCursor)
}

func TestListCursorSurvivesSurroundingWhitespace(t *testing.T) {
	_, page1 := run(t, service.ListInvoicesParams{Limit: "1"}, []model.InvoiceRow{row(2, "2026-10-02"), row(1, "2026-10-01")})

	q, _ := run(t, service.ListInvoicesParams{Limit: "1", Cursor: "  " + page1.NextCursor + " "}, nil)

	require.NotNil(t, q.After)
	require.EqualValues(t, 2, q.After.ID, "page size 1 keeps row 2 and the cursor points at it")
}

func TestListRejectsACursorFromAnotherSort(t *testing.T) {
	_, page1 := run(t, service.ListInvoicesParams{Limit: "1"}, []model.InvoiceRow{row(2, "2026-10-02"), row(1, "2026-10-01")})

	tests := []struct {
		name   string
		params service.ListInvoicesParams
	}{
		{"different key", service.ListInvoicesParams{Sort: "total", Order: "desc", Cursor: page1.NextCursor}},
		{"same key, other direction", service.ListInvoicesParams{Sort: "date", Order: "asc", Cursor: page1.NextCursor}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejects(t, tt.params)
			require.Equal(t, map[string]string{"cursor": "does not match the requested sort"}, got)
		})
	}
}

func TestListSummary(t *testing.T) {
	summary := model.InvoiceSummary{Count: 21, Total: 242840, Discount: 123, Outstanding: 26400}

	t.Run("computed once, with the same filter, on the first page", func(t *testing.T) {
		repo := servicemocks.NewMockInvoiceRepository(t)
		var listed model.InvoiceQuery
		var summarized model.InvoiceFilter
		repo.EXPECT().List(mock.Anything, mock.Anything).
			Run(func(_ context.Context, q model.InvoiceQuery) { listed = q }).
			Return([]model.InvoiceRow{row(1, "2026-10-01")}, nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).
			Run(func(_ context.Context, f model.InvoiceFilter) { summarized = f }).
			Return(summary, nil).Once()

		res, err := service.NewInvoiceService(repo, fixedClock(october21)).List(context.Background(),
			service.ListInvoicesParams{Search: "saliba", Status: []string{"paid"}})

		require.NoError(t, err)
		require.NotNil(t, res.Summary)
		require.Equal(t, summary, *res.Summary)
		require.Equal(t, listed.Filter, summarized, "the summary must describe exactly the filtered set")
	})

	t.Run("not computed when a cursor is sent", func(t *testing.T) {
		_, page1 := run(t, service.ListInvoicesParams{Limit: "1"}, []model.InvoiceRow{row(2, "2026-10-02"), row(1, "2026-10-01")})

		repo := servicemocks.NewMockInvoiceRepository(t) // Summary has no expectation: calling it fails the test
		repo.EXPECT().List(mock.Anything, mock.Anything).Return([]model.InvoiceRow{row(1, "2026-10-01")}, nil).Once()

		res, err := service.NewInvoiceService(repo, fixedClock(october21)).List(context.Background(),
			service.ListInvoicesParams{Limit: "1", Cursor: page1.NextCursor})

		require.NoError(t, err)
		require.Nil(t, res.Summary)
	})

	t.Run("the summary ignores the cursor and the page size", func(t *testing.T) {
		repo := servicemocks.NewMockInvoiceRepository(t)
		var summarized model.InvoiceFilter
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).
			Run(func(_ context.Context, f model.InvoiceFilter) { summarized = f }).
			Return(summary, nil).Once()

		_, err := service.NewInvoiceService(repo, fixedClock(october21)).List(context.Background(),
			service.ListInvoicesParams{Limit: "5"})
		require.NoError(t, err)

		require.Equal(t, model.InvoiceFilter{
			DateFrom:  date(2026, 10, 1),
			DateTo:    date(2026, 10, 31),
			Lifecycle: model.LifecycleActive,
		}, summarized)
	})
}

func TestListErrors(t *testing.T) {
	t.Run("a List failure is wrapped, not turned into a validation error, and skips the summary", func(t *testing.T) {
		repo := servicemocks.NewMockInvoiceRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

		_, err := service.NewInvoiceService(repo, fixedClock(october21)).List(context.Background(), service.ListInvoicesParams{})

		require.ErrorIs(t, err, errBoom)
		require.NotErrorIs(t, err, model.ErrInvalidInput)
		require.ErrorContains(t, err, "list invoices")
	})

	t.Run("a Summary failure is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockInvoiceRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return([]model.InvoiceRow{}, nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.InvoiceSummary{}, errBoom).Once()

		res, err := service.NewInvoiceService(repo, fixedClock(october21)).List(context.Background(), service.ListInvoicesParams{})

		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "summarize invoices")
		require.Nil(t, res, "no partial result when the summary fails")
	})

	t.Run("context errors stay recognizable", func(t *testing.T) {
		for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
			repo := servicemocks.NewMockInvoiceRepository(t)
			repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, ctxErr).Once()

			_, err := service.NewInvoiceService(repo, fixedClock(october21)).List(context.Background(), service.ListInvoicesParams{})
			require.ErrorIs(t, err, ctxErr)
		}
	})

	t.Run("the caller's context reaches the repository", func(t *testing.T) {
		type key struct{}
		ctx := context.WithValue(context.Background(), key{}, "marker")

		repo := servicemocks.NewMockInvoiceRepository(t)
		repo.EXPECT().List(mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" }), mock.Anything).
			Return([]model.InvoiceRow{}, nil).Once()
		repo.EXPECT().Summary(mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" }), mock.Anything).
			Return(model.InvoiceSummary{}, nil).Once()

		_, err := service.NewInvoiceService(repo, fixedClock(october21)).List(ctx, service.ListInvoicesParams{})
		require.NoError(t, err)
	})
}

func TestNewInvoiceServiceWithoutClockUsesTheRealOne(t *testing.T) {
	repo := newRepoReturning(t, nil)

	res, err := service.NewInvoiceService(repo, nil).List(context.Background(), service.ListInvoicesParams{})

	require.NoError(t, err)
	now := time.Now().UTC()
	require.Equal(t, date(now.Year(), now.Month(), 1), res.From)
}

func TestFilterOptions(t *testing.T) {
	serviceTypes := []model.ServiceType{{ID: 1, Name: "Bi-Monthly Service"}}
	tags := []model.InvoiceTag{{ID: 2, Name: "a"}}
	terms := []model.PaymentTerm{{ID: 3, Name: "Net 30 Terms", Description: "30 days"}}

	t.Run("returns all three lists", func(t *testing.T) {
		repo := servicemocks.NewMockInvoiceRepository(t)
		repo.EXPECT().ServiceTypes(mock.Anything).Return(serviceTypes, nil).Once()
		repo.EXPECT().Tags(mock.Anything).Return(tags, nil).Once()
		repo.EXPECT().PaymentTerms(mock.Anything).Return(terms, nil).Once()

		got, err := service.NewInvoiceService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.Equal(t, &service.InvoiceFilterOptions{ServiceTypes: serviceTypes, Tags: tags, PaymentTerms: terms}, got)
	})

	tests := []struct {
		name  string
		setup func(r *servicemocks.MockInvoiceRepository)
	}{
		{"service types fail", func(r *servicemocks.MockInvoiceRepository) {
			r.EXPECT().ServiceTypes(mock.Anything).Return(nil, errBoom).Once()
		}},
		{"tags fail", func(r *servicemocks.MockInvoiceRepository) {
			r.EXPECT().ServiceTypes(mock.Anything).Return(serviceTypes, nil).Once()
			r.EXPECT().Tags(mock.Anything).Return(nil, errBoom).Once()
		}},
		{"payment terms fail", func(r *servicemocks.MockInvoiceRepository) {
			r.EXPECT().ServiceTypes(mock.Anything).Return(serviceTypes, nil).Once()
			r.EXPECT().Tags(mock.Anything).Return(tags, nil).Once()
			r.EXPECT().PaymentTerms(mock.Anything).Return(nil, errBoom).Once()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := servicemocks.NewMockInvoiceRepository(t) // later lookups have no expectation: they must not run
			tt.setup(repo)

			got, err := service.NewInvoiceService(repo, nil).FilterOptions(context.Background())

			require.Nil(t, got)
			require.ErrorIs(t, err, errBoom)
			require.ErrorContains(t, err, "load filter options")
			require.False(t, errors.Is(err, model.ErrInvalidInput))
		})
	}
}
