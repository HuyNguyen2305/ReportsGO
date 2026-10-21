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

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// fixedClock returns a service.Clock frozen at t.
func fixedClock(t time.Time) service.Clock { return func() time.Time { return t } }

// october21 is "now" in most tests: the default range is then 2026-10-01 to 2026-10-31.
var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// fields extracts the per-field messages of a validation error.
func fields(t *testing.T, err error) map[string]string {
	t.Helper()
	require.ErrorIs(t, err, model.ErrInvalidInput)
	var in *model.InvalidInputError
	require.ErrorAs(t, err, &in)
	out := make(map[string]string, len(in.Fields))
	for _, f := range in.Fields {
		_, dup := out[f.Field]
		require.False(t, dup, "field %q reported twice", f.Field)
		out[f.Field] = f.Message
	}
	return out
}

func row(id int64, sortValue string) model.InvoiceRow {
	return model.InvoiceRow{ID: id, InvoiceNumber: "INV" + sortValue, SortValue: sortValue}
}

// run calls List on a service whose repository returns rows and summary, and returns the query the
// repository received along with the result.
func run(t *testing.T, p service.ListInvoicesParams, rows []model.InvoiceRow) (model.InvoiceQuery, *service.ListInvoicesResult) {
	t.Helper()

	repo := servicemocks.NewMockInvoiceRepository(t)
	var got model.InvoiceQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.InvoiceQuery) { got = q }).
		Return(rows, nil).Once()
	if p.Cursor == "" {
		repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.InvoiceSummary{}, nil).Once()
	}

	res, err := service.NewInvoiceService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// newRepoReturning is a repository whose List returns rows and whose Summary returns an empty summary,
// each expected exactly once.
func newRepoReturning(t *testing.T, rows []model.InvoiceRow) *servicemocks.MockInvoiceRepository {
	t.Helper()
	repo := servicemocks.NewMockInvoiceRepository(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(rows, nil).Once()
	repo.EXPECT().Summary(mock.Anything, mock.Anything).Return(model.InvoiceSummary{}, nil).Once()
	return repo
}

// rejects asserts that p is invalid and that the repository is never touched.
func rejects(t *testing.T, p service.ListInvoicesParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockInvoiceRepository(t) // no expectations: any call fails the test
	_, err := service.NewInvoiceService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

var errBoom = errors.New("boom")
