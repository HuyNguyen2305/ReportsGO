package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func collectedFilter() model.SalesTaxFilter {
	return model.SalesTaxFilter{
		DateFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Basis:    model.SalesTaxCollected,
		Methods:  []model.PaymentMethod{model.MethodCard, model.MethodCheck},
	}
}

func invoicedFilter() model.SalesTaxFilter {
	return model.SalesTaxFilter{
		DateFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Basis:    model.SalesTaxInvoiced,
	}
}

var defaultSalesTaxSort = model.SalesTaxSort{Key: model.SalesTaxSortTaxableSales, Desc: true}

func TestBuildSalesTaxQueryCollectedBasis(t *testing.T) {
	stmt, err := repository.BuildSalesTaxQuery(collectedFilter(), defaultSalesTaxSort)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM tax_rates tr")
	require.Contains(t, stmt.SQL, "FROM payments p")
	require.Contains(t, stmt.SQL, "JOIN billing_documents i ON i.id = p.invoice_id")
	require.Contains(t, stmt.SQL, "JOIN invoice_tax_lines l ON l.invoice_id = i.id")
	require.Contains(t, stmt.SQL, "round(l.taxable_amount * p.amount / i.total, 2) AS taxable")
	require.Contains(t, stmt.SQL, "round(l.tax_amount * p.amount / i.total, 2) AS tax")
	require.Contains(t, stmt.SQL, "round(l.surcharge_amount * p.amount / i.total, 2) AS surcharge")
	require.Contains(t, stmt.SQL, "p.status = 'paid' AND p.deleted_at IS NULL AND i.total > 0")
	require.Contains(t, stmt.SQL, "p.payment_date >= $1::date")
	require.Contains(t, stmt.SQL, "p.payment_date <= $2::date")
	require.Contains(t, stmt.SQL, "p.method_type::text = ANY($3::text[])")
	require.NotContains(t, stmt.SQL, "document_date", "the collected basis follows the payment date")
	require.Equal(t, []any{
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		[]string{"card", "check"},
	}, stmt.Args)
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildSalesTaxQueryInvoicedBasis(t *testing.T) {
	stmt, err := repository.BuildSalesTaxQuery(invoicedFilter(), defaultSalesTaxSort)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM invoice_tax_lines l")
	require.Contains(t, stmt.SQL, "JOIN billing_documents i ON i.id = l.invoice_id")
	require.Contains(t, stmt.SQL, "l.taxable_amount AS taxable, l.tax_amount AS tax, l.surcharge_amount AS surcharge")
	require.Contains(t, stmt.SQL, "i.parent_type = 'invoice' AND i.status IN ('sent', 'paid') AND i.deleted_at IS NULL AND i.archived_at IS NULL")
	require.Contains(t, stmt.SQL, "i.document_date >= $1::date")
	require.Contains(t, stmt.SQL, "i.document_date <= $2::date")
	require.NotContains(t, stmt.SQL, "FROM payments", "the invoiced basis involves no payment")
	require.Len(t, stmt.Args, 2)
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildSalesTaxQueryDefaultBasisIsCollected(t *testing.T) {
	f := collectedFilter()
	f.Basis = ""

	stmt, err := repository.BuildSalesTaxQuery(f, defaultSalesTaxSort)

	require.NoError(t, err)
	require.Contains(t, stmt.SQL, "FROM payments p")
}

func TestBuildSalesTaxQueryOpenDateRangeAddsNoDateCondition(t *testing.T) {
	for _, basis := range []model.SalesTaxBasis{model.SalesTaxCollected, model.SalesTaxInvoiced} {
		t.Run(string(basis), func(t *testing.T) {
			stmt, err := repository.BuildSalesTaxQuery(model.SalesTaxFilter{Basis: basis}, defaultSalesTaxSort)
			require.NoError(t, err)
			require.NotContains(t, stmt.SQL, "::date")
			require.Empty(t, stmt.Args)
		})
	}
}

func TestBuildSalesTaxQueryListsEveryActiveTaxAndInactiveOnesWithActivity(t *testing.T) {
	stmt, err := repository.BuildSalesTaxQuery(collectedFilter(), defaultSalesTaxSort)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, ") x ON x.tax_rate_id = tr.id", "a tax with no activity still has a row")
	require.Contains(t, stmt.SQL, "GROUP BY tr.id")
	require.Contains(t, stmt.SQL, "HAVING tr.active OR count(x.tax_rate_id) > 0")
	require.Contains(t, stmt.SQL, "(COALESCE(SUM(x.taxable), 0) * 100)::bigint")
	require.Contains(t, stmt.SQL, "(COALESCE(SUM(x.tax), 0) * 100)::bigint")
	require.Contains(t, stmt.SQL, "(COALESCE(SUM(x.surcharge), 0) * 100)::bigint")
	require.Contains(t, stmt.SQL, "tr.rate::text")
}

func TestBuildSalesTaxQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.SalesTaxSortKey]string{
		model.SalesTaxSortName:         "lower(tr.name)",
		model.SalesTaxSortTaxableSales: "COALESCE(SUM(x.taxable), 0)",
		model.SalesTaxSortTaxRate:      "tr.rate",
		model.SalesTaxSortTaxCollected: "COALESCE(SUM(x.tax), 0)",
		model.SalesTaxSortSurchargeTax: "COALESCE(SUM(x.surcharge), 0)",
	}
	require.Len(t, wantExpr, len(model.SalesTaxSortKeys), "every key must be covered by this test")

	for _, key := range model.SalesTaxSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir := "ASC"
			if desc {
				dir = "DESC"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				stmt, err := repository.BuildSalesTaxQuery(collectedFilter(), model.SalesTaxSort{Key: key, Desc: desc})
				require.NoError(t, err)
				require.True(t, strings.HasSuffix(stmt.SQL, "ORDER BY "+want+" "+dir+", tr.id"), stmt.SQL)
			})
		}
	}
}

func TestBuildSalesTaxQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		filter model.SalesTaxFilter
		sort   model.SalesTaxSort
	}{
		{"unknown sort key", collectedFilter(), model.SalesTaxSort{Key: "name; DROP TABLE tax_rates"}},
		{"empty sort key", collectedFilter(), model.SalesTaxSort{}},
		{"another report's sort key", collectedFilter(), model.SalesTaxSort{Key: "amount"}},
		{"unknown basis", model.SalesTaxFilter{Basis: "everything"}, defaultSalesTaxSort},
		{"methods with the invoiced basis", model.SalesTaxFilter{Basis: model.SalesTaxInvoiced, Methods: []model.PaymentMethod{model.MethodCash}}, defaultSalesTaxSort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repository.BuildSalesTaxQuery(tt.filter, tt.sort)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildSalesTaxQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE tax_rates; --`
	f := collectedFilter()
	f.Methods = []model.PaymentMethod{model.PaymentMethod(hostile)}

	stmt, err := repository.BuildSalesTaxQuery(f, defaultSalesTaxSort)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(strings.TrimSpace(toString(a))) + "|"
	}
	require.Contains(t, flat, "drop table tax_rates", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}
