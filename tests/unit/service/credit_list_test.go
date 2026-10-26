package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
	servicemocks "reportsgo/tests/mocks/service"
)

func creditRows(n int) []model.CreditRow {
	out := make([]model.CreditRow, n)
	for i := range out {
		out[i] = model.CreditRow{ID: int64(i + 1), SortValue: fmt.Sprintf("2026-10-%02d", i+1)}
	}
	return out
}

func TestCreditListPaging(t *testing.T) {
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
			_, res := runCredits(t, service.ListCreditsParams{Limit: tt.limit}, creditRows(tt.fromRepo))

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
			cur, err := service.DecodeCreditCursor(res.NextCursor, model.CreditSort{Key: model.CreditSortDate, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.CreditCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestCreditListPassesThePageSizeToTheRepository(t *testing.T) {
	q, _ := runCredits(t, service.ListCreditsParams{Limit: "7"}, nil)
	require.Equal(t, 7, q.Limit, "the repository adds the extra row itself; the service passes the page size")
}

func TestCreditListTurnsANilResultIntoAnEmptyList(t *testing.T) {
	_, res := runCredits(t, service.ListCreditsParams{}, nil)

	require.NotNil(t, res.Items)
	require.Empty(t, res.Items)
}

func TestCreditListSummaryOnFirstPageOnly(t *testing.T) {
	t.Run("first page has it, computed from the same filter", func(t *testing.T) {
		repo := servicemocks.NewMockCreditRepository(t)
		var asked model.CreditQuery
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).
			Run(func(_ context.Context, q model.CreditQuery, _ bool) { asked = q }).
			Return(model.CreditPage{Rows: creditRows(2), Summary: &model.CreditSummary{Count: 7, Total: 57525}}, nil).Once()

		res, err := service.NewCreditService(repo, fixedClock(october21)).List(context.Background(),
			service.ListCreditsParams{Method: []string{"card"}, Search: "stripe"})

		require.NoError(t, err)
		require.NotNil(t, res.Summary)
		require.Equal(t, model.CreditSummary{Count: 7, Total: 57525}, *res.Summary)
		require.Equal(t, []model.PaymentMethod{model.MethodCard}, asked.Filter.Methods,
			"the repository is given the filter once, so the rows and the totals cover the same set")
	})

	t.Run("later pages do not run it", func(t *testing.T) {
		cursor := service.EncodeCreditCursor(model.CreditSort{Key: model.CreditSortDate, Desc: true},
			model.CreditRow{ID: 3, SortValue: "2026-10-03"})
		repo := servicemocks.NewMockCreditRepository(t) // only Page(..., false) is expected: asking for a summary fails the test
		repo.EXPECT().Page(mock.Anything, mock.Anything, false).Return(model.CreditPage{Rows: creditRows(1)}, nil).Once()

		res, err := service.NewCreditService(repo, fixedClock(october21)).List(context.Background(),
			service.ListCreditsParams{Cursor: cursor})

		require.NoError(t, err)
		require.Nil(t, res.Summary)
	})
}

func TestCreditListWrapsRepositoryErrors(t *testing.T) {
	t.Run("page", func(t *testing.T) {
		repo := servicemocks.NewMockCreditRepository(t)
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.CreditPage{}, errBoom).Once()

		res, err := service.NewCreditService(repo, fixedClock(october21)).List(context.Background(), service.ListCreditsParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "list credits")
		require.NotErrorIs(t, err, model.ErrInvalidInput)
	})

	t.Run("a cancelled context stays recognisable", func(t *testing.T) {
		repo := servicemocks.NewMockCreditRepository(t)
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.CreditPage{}, context.Canceled).Once()

		_, err := service.NewCreditService(repo, nil).List(context.Background(), service.ListCreditsParams{})
		require.True(t, errors.Is(err, context.Canceled))
	})
}

func TestCreditListPassesTheRequestContextToTheRepository(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	hasMarker := mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" })

	repo := servicemocks.NewMockCreditRepository(t)
	repo.EXPECT().Page(hasMarker, mock.Anything, true).Return(model.CreditPage{}, nil).Once()

	_, err := service.NewCreditService(repo, fixedClock(october21)).List(ctx, service.ListCreditsParams{})
	require.NoError(t, err)
}

func TestCreditListDoesNotCallTheRepositoryForInvalidInput(t *testing.T) {
	got := rejectsCredit(t, service.ListCreditsParams{Limit: "0"})
	require.Contains(t, got, "limit")
}

func TestNewCreditServiceDefaultsTheClock(t *testing.T) {
	repo := servicemocks.NewMockCreditRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.CreditPage{}, nil).Once()

	res, err := service.NewCreditService(repo, nil).List(context.Background(), service.ListCreditsParams{})

	require.NoError(t, err)
	require.False(t, res.From.IsZero(), "the default range comes from the real clock")
	require.Equal(t, 1, res.From.Day())
	require.WithinDuration(t, time.Now(), res.From, 32*24*time.Hour)
}
