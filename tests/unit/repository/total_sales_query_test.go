package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultTotalSaleQuery() model.TotalSaleQuery {
	return model.TotalSaleQuery{
		Sort:  model.TotalSaleSort{Key: model.TotalSaleSortDate, Desc: true},
		Limit: 50,
	}
}

func fullTotalSaleFilter() model.TotalSaleFilter {
	return model.TotalSaleFilter{
		DateFrom:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		DateTo:      time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Statuses:    []model.TotalSaleStatus{model.TotalSalePaid, model.TotalSalePartial},
		CustomerIDs: []int64{3, 5},
		Search:      "85004",
	}
}

// derivedStatus is the SQL that decides paid / partial / sent; the filter and the sort must use the same text.
const derivedStatus = "CASE\n    WHEN i.status = 'paid' THEN 'paid'"

func TestBuildTotalSalesListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildTotalSalesListQuery(defaultTotalSaleQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM billing_documents i")
	require.Contains(t, stmt.SQL, "JOIN customers c ON c.id = i.customer_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN LATERAL")
	require.Contains(t, stmt.SQL, "FROM invoice_tax_lines l WHERE l.invoice_id = i.id")
	require.Contains(t, stmt.SQL, "p.invoice_id = i.id AND p.deleted_at IS NULL AND p.status = 'paid'")
	require.Contains(t, stmt.SQL, "i.parent_type = 'invoice'")
	require.Contains(t, stmt.SQL, "i.deleted_at IS NULL AND i.archived_at IS NULL", "only active invoices are sales")
	require.Contains(t, stmt.SQL, "i.status::text = ANY($1::text[])", "only sent and paid invoices are sales")
	require.Contains(t, stmt.SQL, "ORDER BY i.document_date DESC, i.id DESC")
	require.Contains(t, stmt.SQL, "LIMIT $2")
	require.Equal(t, []any{[]string{"sent", "paid"}, 51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildTotalSalesListQuerySelectsAmountAndAmountWithoutTax(t *testing.T) {
	stmt, err := repository.BuildTotalSalesListQuery(defaultTotalSaleQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "(i.total * 100)::bigint")
	require.Contains(t, stmt.SQL, "((i.total - tx.tax) * 100)::bigint")
	require.Contains(t, stmt.SQL, derivedStatus)
	require.Contains(t, stmt.SQL, "WHEN pay.paid > 0 AND pay.paid >= i.total THEN 'paid'")
	require.Contains(t, stmt.SQL, "WHEN pay.paid > 0 THEN 'partial'")
	require.Contains(t, stmt.SQL, "ELSE 'sent' END")
}

func TestBuildTotalSalesListQueryEverySortKeyAndDirection(t *testing.T) {
	statusExpr := "(CASE\n    WHEN i.status = 'paid' THEN 'paid'\n    WHEN pay.paid > 0 AND pay.paid >= i.total THEN 'paid'\n    WHEN pay.paid > 0 THEN 'partial'\n    ELSE 'sent' END)"
	wantExpr := map[model.TotalSaleSortKey]struct{ expr, cast string }{
		model.TotalSaleSortInvoiceNumber: {"i.document_number", "text"},
		model.TotalSaleSortCustomer:      {"lower(c.display_name)", "text"},
		model.TotalSaleSortDate:          {"i.document_date", "date"},
		model.TotalSaleSortStatus:        {statusExpr, "text"},
		model.TotalSaleSortAmount:        {"i.total", "numeric"},
		model.TotalSaleSortWithoutTax:    {"(i.total - tx.tax)", "numeric"},
	}
	require.Len(t, wantExpr, len(model.TotalSaleSortKeys), "every key must be covered by this test")

	for _, key := range model.TotalSaleSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				q := defaultTotalSaleQuery()
				q.Sort = model.TotalSaleSort{Key: key, Desc: desc}
				q.After = &model.TotalSaleCursor{SortValue: "x", ID: 7}

				stmt, err := repository.BuildTotalSalesListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+sortKeySQL(want.expr, want.cast)+" "+dir+", i.id "+dir)
				require.Contains(t, stmt.SQL, "("+sortKeySQL(want.expr, want.cast)+", i.id) "+cmp+" (($")
				require.Contains(t, stmt.SQL, "::text)::"+want.cast+", $")
				require.Contains(t, stmt.SQL, "AS sort_value")
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildTotalSalesListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultTotalSaleQuery()
	q.Filter = fullTotalSaleFilter()
	q.After = &model.TotalSaleCursor{SortValue: "2026-10-12", ID: 9}

	stmt, err := repository.BuildTotalSalesListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		[]string{"sent", "paid"},
		[]string{"paid", "partial"},
		[]int64{3, 5},
		"%85004%",
		"2026-10-12",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"i.document_date >= $1::date",
		"i.document_date <= $2::date",
		"i.status::text = ANY($3::text[])",
		"= ANY($4::text[])",
		"i.customer_id = ANY($5::bigint[])",
		"i.document_number ILIKE $6",
		"c.account_number ILIKE $6",
		"c.display_name ILIKE $6",
		"(($7::text)::date, $8::bigint)",
		"LIMIT $9",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	require.Contains(t, stmt.SQL, "("+derivedStatus, "the status filter uses the derived status")
	require.NotContains(t, stmt.SQL, "billing_email", "the report does not search the billing email")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildTotalSalesListQueryEmptyFiltersAddNothingBeyondTheSalesSet(t *testing.T) {
	stmt, err := repository.BuildTotalSalesListQuery(defaultTotalSaleQuery())
	require.NoError(t, err)

	for _, absent := range []string{"document_date >=", "document_date <=", "customer_id = ANY", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 2)
}

func TestBuildTotalSalesListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct{ search, want string }{
		{"85004", `%85004%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultTotalSaleQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildTotalSalesListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[1], "args: the sales statuses, then the search term")
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildTotalSalesListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.TotalSaleQuery)
	}{
		{"unknown sort key", func(q *model.TotalSaleQuery) { q.Sort.Key = "id; DROP TABLE billing_documents" }},
		{"another report's sort key", func(q *model.TotalSaleQuery) { q.Sort.Key = "payment_date" }},
		{"empty sort key", func(q *model.TotalSaleQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.TotalSaleQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.TotalSaleQuery) { q.Limit = -5 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultTotalSaleQuery()
			tt.mutate(&q)
			_, err := repository.BuildTotalSalesListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildTotalSalesListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE billing_documents; --`
	q := defaultTotalSaleQuery()
	q.Filter.Search = hostile
	q.Filter.Statuses = []model.TotalSaleStatus{model.TotalSaleStatus(hostile)}
	q.After = &model.TotalSaleCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildTotalSalesListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(strings.TrimSpace(toString(a))) + "|"
	}
	require.Contains(t, flat, "drop table billing_documents", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildTotalSalesSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildTotalSalesSummaryQuery(fullTotalSaleFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "SELECT count(*)")
	require.Contains(t, stmt.SQL, "COALESCE(SUM(i.total * 100), 0)::bigint")
	require.Contains(t, stmt.SQL, "COALESCE(SUM((i.total - tx.tax) * 100), 0)::bigint")
	require.NotContains(t, stmt.SQL, "ORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestTotalSalesListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullTotalSaleFilter()

	q := defaultTotalSaleQuery()
	q.Filter = f
	list, err := repository.BuildTotalSalesListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildTotalSalesSummaryQuery(f)
	require.NoError(t, err)

	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}
