//go:build integration

package paymentscollectedservice_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

func newService(t *testing.T) *service.PaymentsCollectedService {
	t.Helper()
	pool := testdb.SeededPool(t)
	return service.NewPaymentsCollectedService(repository.NewPaymentsCollectedRepository(pool), func() time.Time { return october21 })
}

func labels(rows []model.PaymentCollectedRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		if r.InvoiceNumber != nil {
			out[i] = *r.InvoiceNumber
		} else {
			out[i] = "credit"
		}
	}
	return out
}

func mustList(t *testing.T, svc *service.PaymentsCollectedService, p service.ListPaymentsCollectedParams) *service.ListPaymentsCollectedResult {
	t.Helper()
	res, err := svc.List(context.Background(), p)
	require.NoError(t, err)
	return res
}

func TestListDefaultsToThisMonthsInvoicePaymentsNewestFirstWithASummary(t *testing.T) {
	svc := newService(t)

	res := mustList(t, svc, service.ListPaymentsCollectedParams{})

	require.Len(t, res.Items, 10)
	require.False(t, res.HasMore)
	require.Empty(t, res.NextCursor)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.Equal(t, &model.PaymentCollectedSummary{Count: 10, Amount: 81106, WithoutTax: 69328}, res.Summary)
	require.Equal(t, "85032", *res.Items[0].InvoiceNumber, "the newest payment first")
}

func TestListCreditsView(t *testing.T) {
	svc := newService(t)

	res := mustList(t, svc, service.ListPaymentsCollectedParams{PaymentFor: "credits"})

	require.Equal(t, []string{"credit", "credit"}, labels(res.Items))
	require.Equal(t, &model.PaymentCollectedSummary{Count: 2, Amount: 30000, WithoutTax: 30000}, res.Summary)
}

func TestListWalksEveryPageThroughTheCursor(t *testing.T) {
	svc := newService(t)

	var got []string
	cursor := ""
	var summaries int
	for pages := 0; pages < 10; pages++ {
		res := mustList(t, svc, service.ListPaymentsCollectedParams{Limit: "4", Sort: "without_tax", Order: "desc", Cursor: cursor})
		if res.Summary != nil {
			summaries++
			require.Equal(t, int64(10), res.Summary.Count, "the summary covers the whole set, not the page")
		}
		got = append(got, labels(res.Items)...)
		if !res.HasMore {
			require.Empty(t, res.NextCursor)
			break
		}
		cursor = res.NextCursor
	}

	require.Len(t, got, 10)
	require.ElementsMatch(t, got, labels(mustList(t, svc, service.ListPaymentsCollectedParams{Limit: "200"}).Items), "every payment exactly once")
	require.Equal(t, 1, summaries, "only the first page carries the summary")
}

func TestListFiltersByMethodAndStaff(t *testing.T) {
	svc := newService(t)

	cards := mustList(t, svc, service.ListPaymentsCollectedParams{Method: []string{"card"}})
	require.ElementsMatch(t, []string{"85004", "85010", "85011", "85005"}, labels(cards.Items))

	opts, err := svc.FilterOptions(context.Background())
	require.NoError(t, err)
	require.Len(t, opts.Staff, 2)

	var total int
	for _, s := range opts.Staff {
		res := mustList(t, svc, service.ListPaymentsCollectedParams{StaffIDs: []string{strconv.FormatInt(s.ID, 10)}})
		for _, r := range res.Items {
			require.Equal(t, s.ID, *r.StaffID)
		}
		total += len(res.Items)
	}
	require.Equal(t, 6, total, "every cash and check payment belongs to one of the two staff members")
}

func TestListRespectsTheDateRangeAndAnEmptyRangeHasAZeroSummary(t *testing.T) {
	svc := newService(t)

	res := mustList(t, svc, service.ListPaymentsCollectedParams{From: "2026-08-01", To: "2026-08-31"})

	require.Empty(t, res.Items)
	require.NotNil(t, res.Items, "an empty page is an empty list")
	require.Equal(t, &model.PaymentCollectedSummary{}, res.Summary)
}

func TestListRejectsInvalidInputBeforeTouchingTheDatabase(t *testing.T) {
	svc := newService(t)

	_, err := svc.List(context.Background(), service.ListPaymentsCollectedParams{PaymentFor: "all", Method: []string{"wire"}})

	require.ErrorIs(t, err, model.ErrInvalidInput)
}

func TestListRejectsACursorFromAnotherSort(t *testing.T) {
	svc := newService(t)
	res := mustList(t, svc, service.ListPaymentsCollectedParams{Limit: "2"})
	require.True(t, res.HasMore)

	_, err := svc.List(context.Background(), service.ListPaymentsCollectedParams{Limit: "2", Sort: "amount", Cursor: res.NextCursor})

	require.ErrorIs(t, err, model.ErrInvalidInput)
}

func TestFilterOptionsListsTheStaff(t *testing.T) {
	svc := newService(t)

	opts, err := svc.FilterOptions(context.Background())

	require.NoError(t, err)
	names := []string{opts.Staff[0].Name, opts.Staff[1].Name}
	require.ElementsMatch(t, []string{"Thọ Nguyễn Quang", "thon nguyen"}, names)
}
