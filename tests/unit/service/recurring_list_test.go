package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
	servicemocks "reportsgo/tests/mocks/service"
)

func recurringRows(n int) []model.RecurringRow {
	out := make([]model.RecurringRow, n)
	for i := range out {
		out[i] = model.RecurringRow{ID: int64(i + 1), SortValue: fmt.Sprintf("2026-10-%02d", i+1)}
	}
	return out
}

func TestRecurringListPaging(t *testing.T) {
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
		{"limit one with more", "1", 2, 1, true},
		{"limit one without more", "1", 1, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, res := runRecurring(t, service.ListRecurringParams{Limit: tt.limit}, recurringRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}
			require.NotEmpty(t, res.NextCursor)

			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeRecurringCursor(res.NextCursor, model.RecurringSort{Key: model.RecurringSortStartedOn, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.RecurringCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestRecurringListPassesThePageSizeToTheRepository(t *testing.T) {
	q, _ := runRecurring(t, service.ListRecurringParams{Limit: "7"}, nil)
	require.Equal(t, 7, q.Limit, "the repository adds the extra row itself; the service passes the page size")
}

func TestRecurringListTurnsANilResultIntoAnEmptyList(t *testing.T) {
	_, res := runRecurring(t, service.ListRecurringParams{}, nil)

	require.NotNil(t, res.Items)
	require.Empty(t, res.Items)
}

func TestRecurringListSummaryOnFirstPageOnly(t *testing.T) {
	t.Run("first page has it, computed from the same filter", func(t *testing.T) {
		repo := servicemocks.NewMockRecurringRepository(t)
		var listFilter, summaryFilter model.RecurringFilter
		repo.EXPECT().List(mock.Anything, mock.Anything).
			Run(func(_ context.Context, q model.RecurringQuery) { listFilter = q.Filter }).
			Return(recurringRows(2), nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).
			Run(func(_ context.Context, f model.RecurringFilter) { summaryFilter = f }).
			Return(model.RecurringSummary{Count: 7, Total: 71349}, nil).Once()

		res, err := service.NewRecurringService(repo).List(context.Background(),
			service.ListRecurringParams{Status: []string{"active"}, Action: []string{"create_only"}, Search: "850"})

		require.NoError(t, err)
		require.NotNil(t, res.Summary)
		require.Equal(t, model.RecurringSummary{Count: 7, Total: 71349}, *res.Summary)
		require.Equal(t, listFilter, summaryFilter, "the totals cover exactly what the list filters on")
	})

	t.Run("later pages do not run it", func(t *testing.T) {
		cursor := service.EncodeRecurringCursor(model.RecurringSort{Key: model.RecurringSortStartedOn, Desc: true},
			model.RecurringRow{ID: 3, SortValue: "2026-10-03"})
		repo := servicemocks.NewMockRecurringRepository(t) // Summary has no expectation: calling it fails the test
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(recurringRows(1), nil).Once()

		res, err := service.NewRecurringService(repo).List(context.Background(),
			service.ListRecurringParams{Cursor: cursor})

		require.NoError(t, err)
		require.Nil(t, res.Summary)
	})
}

func TestRecurringListWrapsRepositoryErrors(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		repo := servicemocks.NewMockRecurringRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

		res, err := service.NewRecurringService(repo).List(context.Background(), service.ListRecurringParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "list recurring invoices")
		require.NotErrorIs(t, err, model.ErrInvalidInput)
	})

	t.Run("summary", func(t *testing.T) {
		repo := servicemocks.NewMockRecurringRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(recurringRows(1), nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.RecurringSummary{}, errBoom).Once()

		res, err := service.NewRecurringService(repo).List(context.Background(), service.ListRecurringParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "summarize recurring invoices")
	})

	t.Run("a cancelled context stays recognisable", func(t *testing.T) {
		repo := servicemocks.NewMockRecurringRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, context.Canceled).Once()

		_, err := service.NewRecurringService(repo).List(context.Background(), service.ListRecurringParams{})
		require.True(t, errors.Is(err, context.Canceled))
	})
}

func TestRecurringListPassesTheRequestContextToTheRepository(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	hasMarker := mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" })

	repo := servicemocks.NewMockRecurringRepository(t)
	repo.EXPECT().List(hasMarker, mock.Anything).Return(nil, nil).Once()
	repo.EXPECT().Summary(hasMarker, mock.Anything).Return(model.RecurringSummary{}, nil).Once()

	_, err := service.NewRecurringService(repo).List(ctx, service.ListRecurringParams{})
	require.NoError(t, err)
}

func TestRecurringListDoesNotCallTheRepositoryForInvalidInput(t *testing.T) {
	got := rejectsRecurring(t, service.ListRecurringParams{Limit: "0"})
	require.Contains(t, got, "limit")
}
