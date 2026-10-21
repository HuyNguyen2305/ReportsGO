//go:build integration

package invoiceservice_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newService builds the real service on the real repository over a seeded database. With withNulls it adds
// October invoices that have no job, email or payment terms, so NULL sort keys travel through the cursors.
func newService(t *testing.T, withNulls bool) *service.InvoiceService {
	t.Helper()

	pool := testdb.SeededPool(t)
	if withNulls {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO invoices (invoice_number, customer_id, invoice_date, total, discount, status)
			SELECT 'N' || g, c.id, '2026-10-10', 10 * g, g, 'draft'
			FROM generate_series(1, 3) g
			JOIN customers c ON c.account_number = '63716'`)
		require.NoError(t, err)
	}
	return service.NewInvoiceService(repository.NewInvoiceRepository(pool), func() time.Time { return october21 })
}

func ids(rows []model.InvoiceRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func numbers(rows []model.InvoiceRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.InvoiceNumber
	}
	return out
}

func TestDefaultRequestReturnsTheCurrentMonthWithASummary(t *testing.T) {
	svc := newService(t, false)

	res, err := svc.List(context.Background(), service.ListInvoicesParams{})

	require.NoError(t, err)
	require.Len(t, res.Items, 21, "the active October invoices")
	require.False(t, res.HasMore)
	require.Empty(t, res.NextCursor)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)

	require.NotNil(t, res.Summary)
	require.EqualValues(t, 21, res.Summary.Count)
	require.Equal(t, "264.00", res.Summary.Outstanding.String())

	for i := 1; i < len(res.Items); i++ {
		require.False(t, res.Items[i].Date.After(res.Items[i-1].Date), "newest first by default")
	}
	require.Equal(t, "84936", res.Items[0].InvoiceNumber)
}

func TestPagingThroughTheServicesOwnCursorsReturnsEveryRowExactlyOnce(t *testing.T) {
	svc := newService(t, true)
	ctx := context.Background()

	for _, key := range model.InvoiceSortKeys {
		for _, order := range []string{"asc", "desc"} {
			t.Run(fmt.Sprintf("%s/%s", key, order), func(t *testing.T) {
				all, err := svc.List(ctx, service.ListInvoicesParams{Sort: string(key), Order: order, Limit: "200"})
				require.NoError(t, err)
				require.Len(t, all.Items, 24)
				want := ids(all.Items)

				var got []int64
				cursor := ""
				pages := 0
				for {
					pages++
					require.LessOrEqual(t, pages, 10, "paging must terminate")

					res, err := svc.List(ctx, service.ListInvoicesParams{
						Sort: string(key), Order: order, Limit: "5", Cursor: cursor,
					})
					require.NoError(t, err, "the service must accept the cursor it produced (page %d)", pages)

					if cursor == "" {
						require.NotNil(t, res.Summary, "the first page carries the summary")
						require.EqualValues(t, 24, res.Summary.Count)
					} else {
						require.Nil(t, res.Summary, "later pages skip the aggregate")
					}

					got = append(got, ids(res.Items)...)
					if !res.HasMore {
						require.Empty(t, res.NextCursor)
						break
					}
					require.Len(t, res.Items, 5)
					require.NotEmpty(t, res.NextCursor)
					cursor = res.NextCursor
				}

				require.Equal(t, want, got, "no row may be skipped or repeated")
				require.Equal(t, 5, pages, "24 rows at 5 per page")
			})
		}
	}
}

func TestRejectsBadCursorsAsClientErrors(t *testing.T) {
	svc := newService(t, false)
	ctx := context.Background()

	first, err := svc.List(ctx, service.ListInvoicesParams{Limit: "2"})
	require.NoError(t, err)
	require.True(t, first.HasMore)

	forged := func(payload string) string { return base64.RawURLEncoding.EncodeToString([]byte(payload)) }

	tests := []struct {
		name   string
		params service.ListInvoicesParams
	}{
		{"garbage", service.ListInvoicesParams{Limit: "2", Cursor: "garbage!"}},
		{"valid cursor, different sort", service.ListInvoicesParams{Limit: "2", Sort: "total", Cursor: first.NextCursor}},
		{"valid cursor, different direction", service.ListInvoicesParams{Limit: "2", Order: "asc", Cursor: first.NextCursor}},
		{"forged id", service.ListInvoicesParams{Limit: "2", Cursor: forged(`{"k":"date","d":true,"v":"2026-10-10","id":0}`)}},
		{"forged value that would break the database cast", service.ListInvoicesParams{Limit: "2", Cursor: forged(`{"k":"date","d":true,"v":"x'; DROP TABLE invoices;--","id":5}`)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.List(ctx, tt.params)

			require.ErrorIs(t, err, model.ErrInvalidInput, "must be a 422, never a database error")
			var in *model.InvalidInputError
			require.ErrorAs(t, err, &in)
			require.Equal(t, "cursor", in.Fields[0].Field)
		})
	}
}

func TestFilteredRequestsSummarizeExactlyTheirRows(t *testing.T) {
	svc := newService(t, false)
	ctx := context.Background()

	tests := []struct {
		name            string
		params          service.ListInvoicesParams
		wantNumbers     []string
		wantOutstanding string
	}{
		{"sent invoices", service.ListInvoicesParams{Status: []string{"sent"}, Sort: "invoice_number"},
			[]string{"85031", "85032", "85033"}, "264.00"},
		{"paid invoices owe nothing", service.ListInvoicesParams{Status: []string{"paid"}, Search: "saliba", Sort: "invoice_number"},
			[]string{"84975", "84979", "84985", "85000"}, "0.00"},
		{"archived tab", service.ListInvoicesParams{Lifecycle: "archived"}, []string{"85034"}, "0.00"},
		{"deleted tab", service.ListInvoicesParams{Lifecycle: "deleted"}, []string{"85035"}, "0.00"},
		{"search is literal", service.ListInvoicesParams{Search: "%"}, nil, "0.00"},
		{"sql text is just text", service.ListInvoicesParams{Search: "'; DROP TABLE invoices; --"}, nil, "0.00"},
		{"september only", service.ListInvoicesParams{From: "2026-09-01", To: "2026-09-30"}, []string{"84890"}, "0.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := svc.List(ctx, tt.params)

			require.NoError(t, err)
			require.ElementsMatch(t, tt.wantNumbers, numbers(res.Items))
			require.NotNil(t, res.Summary)
			require.EqualValues(t, len(tt.wantNumbers), res.Summary.Count)
			require.Equal(t, tt.wantOutstanding, res.Summary.Outstanding.String())
		})
	}
}

func TestPageSizeAndHasMore(t *testing.T) {
	svc := newService(t, false)
	ctx := context.Background()

	res, err := svc.List(ctx, service.ListInvoicesParams{Limit: "21"})
	require.NoError(t, err)
	require.Len(t, res.Items, 21)
	require.False(t, res.HasMore, "exactly a full page is not followed by another")

	res, err = svc.List(ctx, service.ListInvoicesParams{Limit: "20"})
	require.NoError(t, err)
	require.Len(t, res.Items, 20)
	require.True(t, res.HasMore)
	require.EqualValues(t, 21, res.Summary.Count, "the summary covers the whole set, not the page")
}

func TestFilterOptionsComeFromTheDatabase(t *testing.T) {
	svc := newService(t, false)

	opts, err := svc.FilterOptions(context.Background())

	require.NoError(t, err)
	require.Len(t, opts.ServiceTypes, 5)
	require.Len(t, opts.Tags, 3)
	require.Len(t, opts.PaymentTerms, 2)
	require.Equal(t, "Due on receipt", opts.PaymentTerms[0].Name)

	// Those ids are usable as filters.
	res, err := svc.List(context.Background(), service.ListInvoicesParams{
		TagIDs: []string{fmt.Sprint(opts.Tags[0].ID)}, Sort: "invoice_number",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"85005", "85028", "85030"}, numbers(res.Items), "tag a")
}

func TestStorageErrorsAreNotValidationErrors(t *testing.T) {
	svc := newService(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.List(ctx, service.ListInvoicesParams{})

	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, model.ErrInvalidInput)
}
