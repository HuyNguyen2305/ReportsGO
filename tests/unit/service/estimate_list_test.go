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

func estimateRow(id int64, sortValue string) model.EstimateRow {
	return model.EstimateRow{ID: id, EstimateNumber: "EST" + sortValue, SortValue: sortValue}
}

func estimateRows(n int) []model.EstimateRow {
	out := make([]model.EstimateRow, n)
	for i := range out {
		out[i] = estimateRow(int64(i+1), fmt.Sprintf("2026-10-%02d", i+1))
	}
	return out
}

func TestEstimateListPaging(t *testing.T) {
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
			_, res := runEstimates(t, service.ListEstimatesParams{Limit: tt.limit}, estimateRows(tt.fromRepo))

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
			cur, err := service.DecodeEstimateCursor(res.NextCursor, model.EstimateSort{Key: model.EstimateSortDate, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.EstimateCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestEstimateListAsksTheRepositoryForOneExtraRow(t *testing.T) {
	q, _ := runEstimates(t, service.ListEstimatesParams{Limit: "7"}, nil)
	require.Equal(t, 7, q.Limit, "the repository adds the extra row itself; the service passes the page size")
}

func TestEstimateListSummaryOnFirstPageOnly(t *testing.T) {
	want := model.EstimateSummary{Count: 19, Total: 69812}

	t.Run("first page has it, computed from the same filter", func(t *testing.T) {
		repo := servicemocks.NewMockEstimateRepository(t)
		var listFilter, summaryFilter model.EstimateFilter
		repo.EXPECT().List(mock.Anything, mock.Anything).
			Run(func(_ context.Context, q model.EstimateQuery) { listFilter = q.Filter }).
			Return(estimateRows(2), nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).
			Run(func(_ context.Context, f model.EstimateFilter) { summaryFilter = f }).
			Return(want, nil).Once()

		res, err := service.NewEstimateService(repo, fixedClock(october21)).List(context.Background(),
			service.ListEstimatesParams{Status: []string{"draft"}, Search: "alaba"})

		require.NoError(t, err)
		require.NotNil(t, res.Summary)
		require.Equal(t, want, *res.Summary)
		require.Equal(t, listFilter, summaryFilter, "the totals cover exactly what the list filters on")
	})

	t.Run("later pages do not run it", func(t *testing.T) {
		cursor := service.EncodeEstimateCursor(model.EstimateSort{Key: model.EstimateSortDate, Desc: true}, estimateRow(3, "2026-10-03"))
		repo := servicemocks.NewMockEstimateRepository(t) // Summary has no expectation: calling it fails the test
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(estimateRows(1), nil).Once()

		res, err := service.NewEstimateService(repo, fixedClock(october21)).List(context.Background(),
			service.ListEstimatesParams{Cursor: cursor})

		require.NoError(t, err)
		require.Nil(t, res.Summary)
	})
}

func TestEstimateListWrapsRepositoryErrors(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		repo := servicemocks.NewMockEstimateRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

		res, err := service.NewEstimateService(repo, fixedClock(october21)).List(context.Background(), service.ListEstimatesParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "list estimates")
		require.NotErrorIs(t, err, model.ErrInvalidInput)
	})

	t.Run("summary", func(t *testing.T) {
		repo := servicemocks.NewMockEstimateRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(estimateRows(1), nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.EstimateSummary{}, errBoom).Once()

		res, err := service.NewEstimateService(repo, fixedClock(october21)).List(context.Background(), service.ListEstimatesParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "summarize estimates")
	})

	t.Run("a cancelled context stays recognisable", func(t *testing.T) {
		repo := servicemocks.NewMockEstimateRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, context.Canceled).Once()

		_, err := service.NewEstimateService(repo, nil).List(context.Background(), service.ListEstimatesParams{})
		require.True(t, errors.Is(err, context.Canceled))
	})
}

func TestEstimateListPassesTheRequestContextToTheRepository(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")

	repo := servicemocks.NewMockEstimateRepository(t)
	repo.EXPECT().List(mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" }), mock.Anything).
		Return(nil, nil).Once()
	repo.EXPECT().Summary(mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" }), mock.Anything).
		Return(model.EstimateSummary{}, nil).Once()

	_, err := service.NewEstimateService(repo, fixedClock(october21)).List(ctx, service.ListEstimatesParams{})
	require.NoError(t, err)
}

func TestEstimateListDoesNotCallTheRepositoryForInvalidInput(t *testing.T) {
	// rejectsEstimate uses a mock with no expectations, so any repository call would fail the test.
	got := rejectsEstimate(t, service.ListEstimatesParams{Limit: "0"})
	require.Contains(t, got, "limit")
}

func TestNewEstimateServiceDefaultsTheClock(t *testing.T) {
	repo := servicemocks.NewMockEstimateRepository(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, nil).Once()
	repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.EstimateSummary{}, nil).Once()

	res, err := service.NewEstimateService(repo, nil).List(context.Background(), service.ListEstimatesParams{})

	require.NoError(t, err)
	require.False(t, res.From.IsZero(), "the default range comes from the real clock")
	require.Equal(t, 1, res.From.Day())
}
