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

func documentRows(n int) []model.DocumentRow {
	out := make([]model.DocumentRow, n)
	for i := range out {
		out[i] = model.DocumentRow{
			ID: int64(i + 1), DocumentNumber: fmt.Sprint(5000 + i),
			SortValue: fmt.Sprintf("2026-10-%02dT08:00:00.000000Z", i+1),
		}
	}
	return out
}

func TestDocumentListPaging(t *testing.T) {
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
			_, res := runDocuments(t, service.ListDocumentsParams{Limit: tt.limit}, documentRows(tt.fromRepo))

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
			cur, err := service.DecodeDocumentCursor(res.NextCursor, model.DocumentSort{Key: model.DocumentSortCreatedAt, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.DocumentCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestDocumentListPassesThePageSizeToTheRepository(t *testing.T) {
	q, _ := runDocuments(t, service.ListDocumentsParams{Limit: "7"}, nil)
	require.Equal(t, 7, q.Limit, "the repository adds the extra row itself; the service passes the page size")
}

func TestDocumentListSummaryOnFirstPageOnly(t *testing.T) {
	t.Run("first page asks for it together with the rows, from one filtered query", func(t *testing.T) {
		repo := servicemocks.NewMockDocumentRepository(t)
		var asked model.DocumentQuery
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).
			RunAndReturn(func(_ context.Context, q model.DocumentQuery, _ bool) (model.DocumentPage, error) {
				asked = q
				return model.DocumentPage{Rows: documentRows(2), Summary: &model.DocumentSummary{Count: 18}}, nil
			}).Once()

		res, err := service.NewDocumentService(repo, fixedClock(october21)).List(context.Background(),
			service.ListDocumentsParams{Status: []string{"signed"}, Search: "customer", DocumentTypeIDs: []string{"1"}})

		require.NoError(t, err)
		require.NotNil(t, res.Summary)
		require.EqualValues(t, 18, res.Summary.Count)
		require.Equal(t, "customer", asked.Filter.Search, "rows and count are read from the one filtered query")
		require.Equal(t, []model.DocumentStatus{model.DocumentSigned}, asked.Filter.Statuses)
	})

	t.Run("later pages do not run it", func(t *testing.T) {
		cursor := service.EncodeDocumentCursor(model.DocumentSort{Key: model.DocumentSortCreatedAt, Desc: true},
			model.DocumentRow{ID: 3, SortValue: "2026-10-03T08:00:00.000000Z"})
		repo := servicemocks.NewMockDocumentRepository(t) // an expectation on false: asking for a summary fails the test
		repo.EXPECT().Page(mock.Anything, mock.Anything, false).Return(model.DocumentPage{Rows: documentRows(1)}, nil).Once()

		res, err := service.NewDocumentService(repo, fixedClock(october21)).List(context.Background(),
			service.ListDocumentsParams{Cursor: cursor})

		require.NoError(t, err)
		require.Nil(t, res.Summary)
	})
}

func TestDocumentListWrapsRepositoryErrors(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		repo := servicemocks.NewMockDocumentRepository(t)
		repo.EXPECT().Page(mock.Anything, mock.Anything, mock.Anything).Return(model.DocumentPage{}, errBoom).Once()

		res, err := service.NewDocumentService(repo, fixedClock(october21)).List(context.Background(), service.ListDocumentsParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "list documents")
		require.NotErrorIs(t, err, model.ErrInvalidInput)
	})

	t.Run("a failure inside the snapshot is wrapped the same way, with no partial result", func(t *testing.T) {
		repo := servicemocks.NewMockDocumentRepository(t)
		summaryErr := fmt.Errorf("query document summary: %w", errBoom)
		repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(model.DocumentPage{}, summaryErr).Once()

		res, err := service.NewDocumentService(repo, fixedClock(october21)).List(context.Background(), service.ListDocumentsParams{})

		require.Nil(t, res)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "list documents: query document summary")
	})

	t.Run("a cancelled context stays recognisable", func(t *testing.T) {
		repo := servicemocks.NewMockDocumentRepository(t)
		repo.EXPECT().Page(mock.Anything, mock.Anything, mock.Anything).Return(model.DocumentPage{}, context.Canceled).Once()

		_, err := service.NewDocumentService(repo, nil).List(context.Background(), service.ListDocumentsParams{})
		require.True(t, errors.Is(err, context.Canceled))
	})
}

func TestDocumentListPassesTheRequestContextToTheRepository(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	hasMarker := mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" })

	repo := servicemocks.NewMockDocumentRepository(t)
	repo.EXPECT().Page(hasMarker, mock.Anything, mock.Anything).Return(documentPage(nil, true), nil).Once()
	repo.EXPECT().DocumentTypes(hasMarker).Return(nil, nil).Once()

	svc := service.NewDocumentService(repo, fixedClock(october21))
	_, err := svc.List(ctx, service.ListDocumentsParams{})
	require.NoError(t, err)
	_, err = svc.FilterOptions(ctx)
	require.NoError(t, err)
}

func TestDocumentListDoesNotCallTheRepositoryForInvalidInput(t *testing.T) {
	// rejectsDocument uses a mock with no expectations, so any repository call would fail the test.
	got := rejectsDocument(t, service.ListDocumentsParams{Limit: "0"})
	require.Contains(t, got, "limit")
}

func TestNewDocumentServiceDefaultsTheClock(t *testing.T) {
	repo := servicemocks.NewMockDocumentRepository(t)
	repo.EXPECT().Page(mock.Anything, mock.Anything, true).Return(documentPage(nil, true), nil).Once()

	res, err := service.NewDocumentService(repo, nil).List(context.Background(), service.ListDocumentsParams{})

	require.NoError(t, err)
	require.False(t, res.From.IsZero(), "the default range comes from the real clock")
	require.Equal(t, 1, res.From.Day())
	require.WithinDuration(t, time.Now(), res.From, 32*24*time.Hour)
}

func TestDocumentFilterOptions(t *testing.T) {
	t.Run("returns the types in repository order", func(t *testing.T) {
		repo := servicemocks.NewMockDocumentRepository(t)
		repo.EXPECT().DocumentTypes(mock.Anything).Return([]model.DocumentType{{ID: 2, Name: "B"}, {ID: 1, Name: "A"}}, nil).Once()

		opts, err := service.NewDocumentService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.Equal(t, []model.DocumentType{{ID: 2, Name: "B"}, {ID: 1, Name: "A"}}, opts.Types)
	})

	t.Run("no types is an empty list, not nil", func(t *testing.T) {
		repo := servicemocks.NewMockDocumentRepository(t)
		repo.EXPECT().DocumentTypes(mock.Anything).Return(nil, nil).Once()

		opts, err := service.NewDocumentService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.NotNil(t, opts.Types)
		require.Empty(t, opts.Types)
	})

	t.Run("a storage error is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockDocumentRepository(t)
		repo.EXPECT().DocumentTypes(mock.Anything).Return(nil, errBoom).Once()

		opts, err := service.NewDocumentService(repo, nil).FilterOptions(context.Background())

		require.Nil(t, opts)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "load filter options")
	})
}
