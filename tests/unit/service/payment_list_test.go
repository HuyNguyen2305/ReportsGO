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

func paymentRows(n int) []model.PaymentRow {
	out := make([]model.PaymentRow, n)
	for i := range out {
		out[i] = model.PaymentRow{ID: int64(i + 1), SortValue: fmt.Sprintf("2026-10-%02d", i+1)}
	}
	return out
}

func TestPaymentListPaging(t *testing.T) {
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
			_, res := runPayments(t, service.ListPaymentsParams{Limit: tt.limit}, paymentRows(tt.fromRepo))

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
			cur, err := service.DecodePaymentCursor(res.NextCursor, model.PaymentSort{Key: model.PaymentSortDate, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.PaymentCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestPaymentListPassesThePageSizeToTheRepository(t *testing.T) {
	q, _ := runPayments(t, service.ListPaymentsParams{Limit: "7"}, nil)
	require.Equal(t, 7, q.Limit, "the repository adds the extra row itself; the service passes the page size")
}

func TestPaymentListSummaryOnFirstPageOnly(t *testing.T) {
	t.Run("first page has it, computed from the same filter", func(t *testing.T) {
		repo := servicemocks.NewMockPaymentRepository(t)
		var listFilter, summaryFilter model.PaymentFilter
		repo.EXPECT().List(mock.Anything, mock.Anything).
			Run(func(_ context.Context, q model.PaymentQuery) { listFilter = q.Filter }).
			Return(paymentRows(2), nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).
			Run(func(_ context.Context, f model.PaymentFilter) { summaryFilter = f }).
			Return(model.PaymentSummary{Count: 15, Total: 123450}, nil).Once()

		res, err := service.NewPaymentService(repo, fixedClock(october21)).List(context.Background(),
			service.ListPaymentsParams{Status: []string{"paid"}, Method: []string{"card"}, Search: "850"})

		require.NoError(t, err)
		require.NotNil(t, res.Summary)
		require.Equal(t, model.PaymentSummary{Count: 15, Total: 123450}, *res.Summary)
		require.Equal(t, listFilter, summaryFilter, "the totals cover exactly what the list filters on")
	})

	t.Run("later pages do not run it", func(t *testing.T) {
		cursor := service.EncodePaymentCursor(model.PaymentSort{Key: model.PaymentSortDate, Desc: true},
			model.PaymentRow{ID: 3, SortValue: "2026-10-03"})
		repo := servicemocks.NewMockPaymentRepository(t) // Summary has no expectation: calling it fails the test
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(paymentRows(1), nil).Once()

		res, err := service.NewPaymentService(repo, fixedClock(october21)).List(context.Background(),
			service.ListPaymentsParams{Cursor: cursor})

		require.NoError(t, err)
		require.Nil(t, res.Summary)
	})
}

func TestPaymentListWrapsRepositoryErrors(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		repo := servicemocks.NewMockPaymentRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

		res, err := service.NewPaymentService(repo, fixedClock(october21)).List(context.Background(), service.ListPaymentsParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "list payments")
		require.NotErrorIs(t, err, model.ErrInvalidInput)
	})

	t.Run("summary", func(t *testing.T) {
		repo := servicemocks.NewMockPaymentRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(paymentRows(1), nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.PaymentSummary{}, errBoom).Once()

		res, err := service.NewPaymentService(repo, fixedClock(october21)).List(context.Background(), service.ListPaymentsParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "summarize payments")
	})

	t.Run("a cancelled context stays recognisable", func(t *testing.T) {
		repo := servicemocks.NewMockPaymentRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, context.Canceled).Once()

		_, err := service.NewPaymentService(repo, nil).List(context.Background(), service.ListPaymentsParams{})
		require.True(t, errors.Is(err, context.Canceled))
	})
}

func TestPaymentListPassesTheRequestContextToTheRepository(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	hasMarker := mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" })

	repo := servicemocks.NewMockPaymentRepository(t)
	repo.EXPECT().List(hasMarker, mock.Anything).Return(nil, nil).Once()
	repo.EXPECT().Summary(hasMarker, mock.Anything).Return(model.PaymentSummary{}, nil).Once()

	_, err := service.NewPaymentService(repo, fixedClock(october21)).List(ctx, service.ListPaymentsParams{})
	require.NoError(t, err)
}

func TestPaymentListDoesNotCallTheRepositoryForInvalidInput(t *testing.T) {
	got := rejectsPayment(t, service.ListPaymentsParams{Limit: "0"})
	require.Contains(t, got, "limit")
}

func TestNewPaymentServiceDefaultsTheClock(t *testing.T) {
	repo := servicemocks.NewMockPaymentRepository(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, nil).Once()
	repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.PaymentSummary{}, nil).Once()

	res, err := service.NewPaymentService(repo, nil).List(context.Background(), service.ListPaymentsParams{})

	require.NoError(t, err)
	require.False(t, res.From.IsZero(), "the default range comes from the real clock")
	require.Equal(t, 1, res.From.Day())
	require.WithinDuration(t, time.Now(), res.From, 32*24*time.Hour)
}

func TestPaymentListTurnsANilResultIntoAnEmptyList(t *testing.T) {
	_, res := runPayments(t, service.ListPaymentsParams{}, nil)

	require.NotNil(t, res.Items)
	require.Empty(t, res.Items)
}
