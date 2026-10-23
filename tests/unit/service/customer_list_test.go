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

func customerRows(n int) []model.CustomerRow {
	out := make([]model.CustomerRow, n)
	for i := range out {
		out[i] = model.CustomerRow{
			ID: int64(i + 1), Name: fmt.Sprint("Customer ", i), AccountNumber: fmt.Sprint(7000 + i),
			SortValue: fmt.Sprintf("2026-10-%02dT08:00:00.000000Z", i+1),
		}
	}
	return out
}

func TestCustomerListPaging(t *testing.T) {
	tests := []struct {
		name        string
		limit       string
		fromRepo    int
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
			_, res := runCustomers(t, service.ListCustomersParams{Limit: tt.limit}, customerRows(tt.fromRepo))

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
			cur, err := service.DecodeCustomerCursor(res.NextCursor, model.CustomerSort{Key: model.CustomerSortCreatedAt, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.CustomerCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestCustomerListNilRowsBecomeAnEmptyList(t *testing.T) {
	_, res := runCustomers(t, service.ListCustomersParams{}, nil)
	require.NotNil(t, res.Items)
	require.Empty(t, res.Items)
}

func TestCustomerListPassesThePageSizeToTheRepository(t *testing.T) {
	q, _ := runCustomers(t, service.ListCustomersParams{Limit: "7"}, nil)
	require.Equal(t, 7, q.Limit, "the repository adds the extra row itself; the service passes the page size")
}

func TestCustomerListSummaryOnFirstPageOnly(t *testing.T) {
	t.Run("first page has it, computed from the same filter", func(t *testing.T) {
		repo := servicemocks.NewMockCustomerRepository(t)
		var listFilter, summaryFilter model.CustomerFilter
		repo.EXPECT().List(mock.Anything, mock.Anything).
			Run(func(_ context.Context, q model.CustomerQuery) { listFilter = q.Filter }).
			Return(customerRows(2), nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).
			Run(func(_ context.Context, f model.CustomerFilter) { summaryFilter = f }).
			Return(model.CustomerSummary{Count: 14}, nil).Once()

		res, err := service.NewCustomerService(repo, fixedClock(october21)).List(context.Background(),
			service.ListCustomersParams{Search: "ada", LeadSourceIDs: []string{"2"}})

		require.NoError(t, err)
		require.NotNil(t, res.Summary)
		require.EqualValues(t, 14, res.Summary.Count)
		require.Equal(t, listFilter, summaryFilter, "the count covers exactly what the list filters on")
	})

	t.Run("later pages do not run it", func(t *testing.T) {
		cursor := service.EncodeCustomerCursor(model.CustomerSort{Key: model.CustomerSortCreatedAt, Desc: true},
			model.CustomerRow{ID: 3, SortValue: "2026-10-03T08:00:00.000000Z"})
		repo := servicemocks.NewMockCustomerRepository(t) // Summary has no expectation: calling it fails the test
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(customerRows(1), nil).Once()

		res, err := service.NewCustomerService(repo, fixedClock(october21)).List(context.Background(),
			service.ListCustomersParams{Cursor: cursor})

		require.NoError(t, err)
		require.Nil(t, res.Summary)
	})
}

func TestCustomerListWrapsRepositoryErrors(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		repo := servicemocks.NewMockCustomerRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

		res, err := service.NewCustomerService(repo, fixedClock(october21)).List(context.Background(), service.ListCustomersParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "list customers")
		require.NotErrorIs(t, err, model.ErrInvalidInput)
	})

	t.Run("summary", func(t *testing.T) {
		repo := servicemocks.NewMockCustomerRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(customerRows(1), nil).Once()
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.CustomerSummary{}, errBoom).Once()

		res, err := service.NewCustomerService(repo, fixedClock(october21)).List(context.Background(), service.ListCustomersParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "summarize customers")
	})

	t.Run("a cancelled context stays recognisable", func(t *testing.T) {
		repo := servicemocks.NewMockCustomerRepository(t)
		repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, context.Canceled).Once()

		_, err := service.NewCustomerService(repo, nil).List(context.Background(), service.ListCustomersParams{})
		require.True(t, errors.Is(err, context.Canceled))
	})
}

func TestCustomerListPassesTheRequestContextToTheRepository(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	hasMarker := mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" })

	repo := servicemocks.NewMockCustomerRepository(t)
	repo.EXPECT().List(hasMarker, mock.Anything).Return(nil, nil).Once()
	repo.EXPECT().Summary(hasMarker, mock.Anything).Return(model.CustomerSummary{}, nil).Once()
	repo.EXPECT().LeadSources(hasMarker).Return(nil, nil).Once()

	svc := service.NewCustomerService(repo, fixedClock(october21))
	_, err := svc.List(ctx, service.ListCustomersParams{})
	require.NoError(t, err)
	_, err = svc.FilterOptions(ctx)
	require.NoError(t, err)
}

func TestNewCustomerServiceDefaultsTheClock(t *testing.T) {
	repo := servicemocks.NewMockCustomerRepository(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, nil).Once()
	repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.CustomerSummary{}, nil).Once()

	res, err := service.NewCustomerService(repo, nil).List(context.Background(), service.ListCustomersParams{})

	require.NoError(t, err)
	require.False(t, res.From.IsZero(), "the default range comes from the real clock")
	require.Equal(t, 1, res.From.Day())
	require.WithinDuration(t, time.Now(), res.From, 32*24*time.Hour)
}

func TestCustomerFilterOptions(t *testing.T) {
	t.Run("returns the lead sources in repository order", func(t *testing.T) {
		repo := servicemocks.NewMockCustomerRepository(t)
		repo.EXPECT().LeadSources(mock.Anything).Return([]model.LeadSource{{ID: 9, Name: "Google"}, {ID: 1, Name: "Facebook"}}, nil).Once()

		opts, err := service.NewCustomerService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.Equal(t, []model.LeadSource{{ID: 9, Name: "Google"}, {ID: 1, Name: "Facebook"}}, opts.LeadSources)
	})

	t.Run("no rows is an empty list, not nil", func(t *testing.T) {
		repo := servicemocks.NewMockCustomerRepository(t)
		repo.EXPECT().LeadSources(mock.Anything).Return(nil, nil).Once()

		opts, err := service.NewCustomerService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.NotNil(t, opts.LeadSources)
		require.Empty(t, opts.LeadSources)
	})

	t.Run("a storage error is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockCustomerRepository(t)
		repo.EXPECT().LeadSources(mock.Anything).Return(nil, errBoom).Once()

		opts, err := service.NewCustomerService(repo, nil).FilterOptions(context.Background())

		require.Nil(t, opts)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "load filter options")
	})
}
