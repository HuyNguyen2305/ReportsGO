//go:build integration

package salestaxservice_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

func newService(t *testing.T) *service.SalesTaxService {
	t.Helper()
	pool := testdb.SeededPool(t)
	return service.NewSalesTaxService(repository.NewSalesTaxRepository(pool), func() time.Time { return october21 })
}

func mustList(t *testing.T, svc *service.SalesTaxService, p service.ListSalesTaxParams) *service.ListSalesTaxResult {
	t.Helper()
	res, err := svc.List(context.Background(), p)
	require.NoError(t, err)
	return res
}

func TestListDefaultsToThisMonthOnTheCollectedBasisBiggestFirst(t *testing.T) {
	svc := newService(t)

	res := mustList(t, svc, service.ListSalesTaxParams{})

	require.Equal(t, model.SalesTaxCollected, res.Basis)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), res.From)
	require.Equal(t, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), res.To)
	require.Len(t, res.Items, 10)
	require.Equal(t, []string{"VAT", "VN", "d12"}, []string{res.Items[0].Name, res.Items[1].Name, res.Items[2].Name})
	require.Equal(t, model.SalesTaxSummary{Count: 10, TaxableSales: 72150, TaxCollected: 11778, SurchargeTax: 292}, res.Summary)
}

func TestListSummaryAlwaysEqualsTheSumOfTheRows(t *testing.T) {
	svc := newService(t)

	for _, p := range []service.ListSalesTaxParams{
		{},
		{Basis: "invoiced"},
		{Method: []string{"card"}},
		{Method: []string{"cash", "check"}, Sort: "name"},
		{From: "2026-09-01", To: "2026-09-30"},
		{From: "2030-01-01", To: "2030-01-31", Basis: "invoiced"},
	} {
		res := mustList(t, svc, p)

		var taxable, tax, surcharge model.Money
		for _, r := range res.Items {
			taxable += r.TaxableSales
			tax += r.TaxCollected
			surcharge += r.SurchargeTax
		}
		require.Equal(t, model.SalesTaxSummary{Count: int64(len(res.Items)), TaxableSales: taxable, TaxCollected: tax, SurchargeTax: surcharge}, res.Summary, "%+v", p)
	}
}

func TestListInvoicedBasis(t *testing.T) {
	svc := newService(t)

	res := mustList(t, svc, service.ListSalesTaxParams{Basis: "invoiced"})

	require.Equal(t, model.SalesTaxInvoiced, res.Basis)
	require.Equal(t, model.SalesTaxSummary{Count: 10, TaxableSales: 97150, TaxCollected: 16370, SurchargeTax: 292}, res.Summary)
}

func TestListRejectsMethodsOnTheInvoicedBasis(t *testing.T) {
	svc := newService(t)

	_, err := svc.List(context.Background(), service.ListSalesTaxParams{Basis: "invoiced", Method: []string{"card"}})

	require.ErrorIs(t, err, model.ErrInvalidInput)
}

func TestListRejectsInvalidInputBeforeTouchingTheDatabase(t *testing.T) {
	svc := newService(t)

	_, err := svc.List(context.Background(), service.ListSalesTaxParams{Basis: "cash", Sort: "total"})

	require.ErrorIs(t, err, model.ErrInvalidInput)
}

func TestListAnEmptyRangeHasZeroRowsAndSummaryForEveryActiveTax(t *testing.T) {
	svc := newService(t)

	res := mustList(t, svc, service.ListSalesTaxParams{From: "2030-01-01", To: "2030-01-31"})

	require.Len(t, res.Items, 10)
	require.Equal(t, model.SalesTaxSummary{Count: 10}, res.Summary)
}

func TestRatesListsTheTaxRates(t *testing.T) {
	svc := newService(t)

	rates, err := svc.Rates(context.Background())

	require.NoError(t, err)
	require.Len(t, rates, 10)
	names := make([]string, len(rates))
	for i, r := range rates {
		names[i] = r.Name
	}
	require.Contains(t, names, "VAT")
	require.Contains(t, names, "US")
}
