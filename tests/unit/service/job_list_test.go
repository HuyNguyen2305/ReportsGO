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

func jobRows(n int) []model.JobRow {
	out := make([]model.JobRow, n)
	for i := range out {
		out[i] = model.JobRow{
			ID: int64(i + 1), CustomerName: fmt.Sprint("Customer ", i),
			SortValue: fmt.Sprintf("2026-10-%02dT08:00:00.000000Z", i+1),
		}
	}
	return out
}

func TestJobListPaging(t *testing.T) {
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
			_, res := runJobs(t, service.ListJobsParams{Limit: tt.limit}, jobRows(tt.fromRepo))

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
			cur, err := service.DecodeJobCursor(res.NextCursor, model.JobSort{Key: model.JobSortCreatedAt, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.JobCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestJobListNilRowsBecomeAnEmptyList(t *testing.T) {
	_, res := runJobs(t, service.ListJobsParams{}, nil)
	require.NotNil(t, res.Items)
	require.Empty(t, res.Items)
}

func TestJobListPassesThePageSizeToTheRepository(t *testing.T) {
	q, _ := runJobs(t, service.ListJobsParams{Limit: "7"}, nil)
	require.Equal(t, 7, q.Limit, "the repository adds the extra row itself; the service passes the page size")
}

func TestJobListSummaryOnFirstPageOnly(t *testing.T) {
	t.Run("first page has it, computed from the same filter", func(t *testing.T) {
		repo := servicemocks.NewMockJobRepository(t)
		var asked model.JobQuery
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).
			Run(func(_ context.Context, q model.JobQuery, _ bool) { asked = q }).
			Return(model.JobPage{Rows: jobRows(2), Summary: &model.JobSummary{Count: 18, Total: 123456}}, nil).Once()

		res, err := service.NewJobService(repo, fixedClock(october21)).List(context.Background(),
			service.ListJobsParams{Status: []string{"completed"}, Search: "customer", ServiceTypeIDs: []string{"1"}, LeadSourceIDs: []string{"2"}})

		require.NoError(t, err)
		require.NotNil(t, res.Summary)
		require.EqualValues(t, 18, res.Summary.Count)
		require.Equal(t, model.Money(123456), res.Summary.Total)
		require.Equal(t, []model.JobStatus{model.JobCompleted}, asked.Filter.Statuses,
			"the repository is given the filter once, so the rows and the totals cover the same set")
	})

	t.Run("later pages do not run it", func(t *testing.T) {
		cursor := service.EncodeJobCursor(model.JobSort{Key: model.JobSortCreatedAt, Desc: true},
			model.JobRow{ID: 3, SortValue: "2026-10-03T08:00:00.000000Z"})
		repo := servicemocks.NewMockJobRepository(t) // only Page(..., false) is expected: asking for a summary fails the test
		repo.EXPECT().Page(mock.Anything, mock.Anything, false).Return(model.JobPage{Rows: jobRows(1)}, nil).Once()

		res, err := service.NewJobService(repo, fixedClock(october21)).List(context.Background(),
			service.ListJobsParams{Cursor: cursor})

		require.NoError(t, err)
		require.Nil(t, res.Summary)
	})
}

func TestJobListWrapsRepositoryErrors(t *testing.T) {
	t.Run("page", func(t *testing.T) {
		repo := servicemocks.NewMockJobRepository(t)
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.JobPage{}, errBoom).Once()

		res, err := service.NewJobService(repo, fixedClock(october21)).List(context.Background(), service.ListJobsParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "list jobs")
		require.NotErrorIs(t, err, model.ErrInvalidInput)
	})

	t.Run("a cancelled context stays recognisable", func(t *testing.T) {
		repo := servicemocks.NewMockJobRepository(t)
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.JobPage{}, context.Canceled).Once()

		_, err := service.NewJobService(repo, nil).List(context.Background(), service.ListJobsParams{})
		require.True(t, errors.Is(err, context.Canceled))
	})
}

func TestJobListPassesTheRequestContextToTheRepository(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	hasMarker := mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" })

	repo := servicemocks.NewMockJobRepository(t)
	repo.EXPECT().Page(hasMarker, mock.Anything, true).Return(model.JobPage{}, nil).Once()
	repo.EXPECT().ServiceTypes(hasMarker).Return(nil, nil).Once()
	repo.EXPECT().LeadSources(hasMarker).Return(nil, nil).Once()

	svc := service.NewJobService(repo, fixedClock(october21))
	_, err := svc.List(ctx, service.ListJobsParams{})
	require.NoError(t, err)
	_, err = svc.FilterOptions(ctx)
	require.NoError(t, err)
}

func TestNewJobServiceDefaultsTheClock(t *testing.T) {
	repo := servicemocks.NewMockJobRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.JobPage{}, nil).Once()

	res, err := service.NewJobService(repo, nil).List(context.Background(), service.ListJobsParams{})

	require.NoError(t, err)
	require.False(t, res.From.IsZero(), "the default range comes from the real clock")
	require.Equal(t, 1, res.From.Day())
	require.WithinDuration(t, time.Now(), res.From, 32*24*time.Hour)
}

func TestJobFilterOptions(t *testing.T) {
	t.Run("returns both lists in repository order", func(t *testing.T) {
		repo := servicemocks.NewMockJobRepository(t)
		repo.EXPECT().ServiceTypes(mock.Anything).Return([]model.ServiceType{{ID: 2, Name: "B"}, {ID: 1, Name: "A"}}, nil).Once()
		repo.EXPECT().LeadSources(mock.Anything).Return([]model.LeadSource{{ID: 9, Name: "Google"}}, nil).Once()

		opts, err := service.NewJobService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.Equal(t, []model.ServiceType{{ID: 2, Name: "B"}, {ID: 1, Name: "A"}}, opts.ServiceTypes)
		require.Equal(t, []model.LeadSource{{ID: 9, Name: "Google"}}, opts.LeadSources)
	})

	t.Run("no rows are empty lists, not nil", func(t *testing.T) {
		repo := servicemocks.NewMockJobRepository(t)
		repo.EXPECT().ServiceTypes(mock.Anything).Return(nil, nil).Once()
		repo.EXPECT().LeadSources(mock.Anything).Return(nil, nil).Once()

		opts, err := service.NewJobService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.NotNil(t, opts.ServiceTypes)
		require.Empty(t, opts.ServiceTypes)
		require.NotNil(t, opts.LeadSources)
		require.Empty(t, opts.LeadSources)
	})

	t.Run("a service type error is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockJobRepository(t)
		repo.EXPECT().ServiceTypes(mock.Anything).Return(nil, errBoom).Once()

		opts, err := service.NewJobService(repo, nil).FilterOptions(context.Background())

		require.Nil(t, opts)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "load filter options")
	})

	t.Run("a lead source error is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockJobRepository(t)
		repo.EXPECT().ServiceTypes(mock.Anything).Return([]model.ServiceType{{ID: 1, Name: "A"}}, nil).Once()
		repo.EXPECT().LeadSources(mock.Anything).Return(nil, errBoom).Once()

		opts, err := service.NewJobService(repo, nil).FilterOptions(context.Background())

		require.Nil(t, opts)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "load filter options")
	})
}
