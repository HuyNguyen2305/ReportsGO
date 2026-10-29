package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultPaymentCollectedQuery() model.PaymentCollectedQuery {
	return model.PaymentCollectedQuery{
		Filter: model.PaymentCollectedFilter{PaymentFor: model.PaymentForInvoices},
		Sort:   model.PaymentCollectedSort{Key: model.PaymentCollectedSortDate, Desc: true},
		Limit:  50,
	}
}

func fullPaymentCollectedFilter() model.PaymentCollectedFilter {
	return model.PaymentCollectedFilter{
		DateFrom:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		DateTo:      time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		PaymentFor:  model.PaymentForInvoices,
		Methods:     []model.PaymentMethod{model.MethodCard, model.MethodCheck},
		CustomerIDs: []int64{3, 5},
		StaffIDs:    []int64{1, 2},
		Search:      "85004",
	}
}

const (
	withoutTaxSQL = "(CASE WHEN i.id IS NULL OR i.total = 0 THEN p.amount\n    ELSE round(p.amount * (i.total - tx.tax) / i.total, 2) END)"
	stateSQL      = "NULLIF(sl.state, '')"
)

func TestBuildPaymentsCollectedListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildPaymentsCollectedListQuery(defaultPaymentCollectedQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM payments p")
	require.Contains(t, stmt.SQL, "JOIN customers c ON c.id = p.customer_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN billing_documents i ON i.id = p.invoice_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN jobs j ON j.id = i.job_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN service_locations sl ON sl.id = j.location_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN users u ON u.id = p.received_by")
	require.Contains(t, stmt.SQL, "FROM invoice_tax_lines l WHERE l.invoice_id = p.invoice_id")
	require.Contains(t, stmt.SQL, "p.status = 'paid' AND p.deleted_at IS NULL", "only collected payments")
	require.Contains(t, stmt.SQL, "p.invoice_id IS NOT NULL")
	require.NotContains(t, stmt.SQL, "credit_id IS NOT NULL")
	require.Contains(t, stmt.SQL, "ORDER BY p.payment_date DESC, p.id DESC")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildPaymentsCollectedListQuerySelectsTheDerivedColumns(t *testing.T) {
	stmt, err := repository.BuildPaymentsCollectedListQuery(defaultPaymentCollectedQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, stateSQL)
	require.Contains(t, stmt.SQL, "u.id, u.full_name")
	require.Contains(t, stmt.SQL, "(p.amount * 100)::bigint")
	require.Contains(t, stmt.SQL, "("+withoutTaxSQL+" * 100)::bigint")
}

func TestBuildPaymentsCollectedListQueryPaymentFor(t *testing.T) {
	tests := []struct {
		paymentFor model.PaymentFor
		want       string
		absent     string
	}{
		{model.PaymentForInvoices, "p.invoice_id IS NOT NULL", "p.credit_id IS NOT NULL"},
		{"", "p.invoice_id IS NOT NULL", "p.credit_id IS NOT NULL"},
		{model.PaymentForCredits, "p.credit_id IS NOT NULL", "p.invoice_id IS NOT NULL"},
	}
	for _, tt := range tests {
		t.Run(string(tt.paymentFor), func(t *testing.T) {
			q := defaultPaymentCollectedQuery()
			q.Filter.PaymentFor = tt.paymentFor
			stmt, err := repository.BuildPaymentsCollectedListQuery(q)
			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
			require.NotContains(t, stmt.SQL, tt.absent)
		})
	}
}

func TestBuildPaymentsCollectedListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.PaymentCollectedSortKey]struct{ expr, cast string }{
		model.PaymentCollectedSortPaymentFor:   {"lower(coalesce(i.document_number, 'credit'))", "text"},
		model.PaymentCollectedSortCustomer:     {"lower(c.display_name)", "text"},
		model.PaymentCollectedSortMethod:       {"p.method_type::text", "text"},
		model.PaymentCollectedSortServiceState: {"lower(coalesce(" + stateSQL + ", ''))", "text"},
		model.PaymentCollectedSortStaff:        {"lower(coalesce(u.full_name, ''))", "text"},
		model.PaymentCollectedSortDate:         {"p.payment_date", "date"},
		model.PaymentCollectedSortAmount:       {"p.amount", "numeric"},
		model.PaymentCollectedSortWithoutTax:   {withoutTaxSQL, "numeric"},
	}
	require.Len(t, wantExpr, len(model.PaymentCollectedSortKeys), "every key must be covered by this test")

	for _, key := range model.PaymentCollectedSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				q := defaultPaymentCollectedQuery()
				q.Sort = model.PaymentCollectedSort{Key: key, Desc: desc}
				q.After = &model.PaymentCollectedCursor{SortValue: "x", ID: 7}

				stmt, err := repository.BuildPaymentsCollectedListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+sortKeySQL(want.expr, want.cast)+" "+dir+", p.id "+dir)
				require.Contains(t, stmt.SQL, "("+sortKeySQL(want.expr, want.cast)+", p.id) "+cmp+" (($")
				require.Contains(t, stmt.SQL, "::text)::"+want.cast+", $")
				require.Contains(t, stmt.SQL, "AS sort_value")
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildPaymentsCollectedListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultPaymentCollectedQuery()
	q.Filter = fullPaymentCollectedFilter()
	q.After = &model.PaymentCollectedCursor{SortValue: "2026-10-12", ID: 9}

	stmt, err := repository.BuildPaymentsCollectedListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		[]string{"card", "check"},
		[]int64{3, 5},
		[]int64{1, 2},
		"%85004%",
		"2026-10-12",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"p.payment_date >= $1::date",
		"p.payment_date <= $2::date",
		"p.method_type::text = ANY($3::text[])",
		"p.customer_id = ANY($4::bigint[])",
		"p.received_by = ANY($5::bigint[])",
		"i.document_number ILIKE $6",
		"c.account_number ILIKE $6",
		"c.display_name ILIKE $6",
		"(($7::text)::date, $8::bigint)",
		"LIMIT $9",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildPaymentsCollectedListQueryEmptyFiltersAddNothingBeyondTheCollectedSet(t *testing.T) {
	stmt, err := repository.BuildPaymentsCollectedListQuery(defaultPaymentCollectedQuery())
	require.NoError(t, err)

	for _, absent := range []string{"payment_date >=", "payment_date <=", "method_type::text = ANY", "customer_id = ANY", "received_by = ANY", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 1)
}

func TestBuildPaymentsCollectedListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct{ search, want string }{
		{"85004", `%85004%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`%_\`, `%\%\_\\%`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultPaymentCollectedQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildPaymentsCollectedListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[0])
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildPaymentsCollectedListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.PaymentCollectedQuery)
	}{
		{"unknown sort key", func(q *model.PaymentCollectedQuery) { q.Sort.Key = "id; DROP TABLE payments" }},
		{"another report's sort key", func(q *model.PaymentCollectedQuery) { q.Sort.Key = "payment_date" }},
		{"empty sort key", func(q *model.PaymentCollectedQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.PaymentCollectedQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.PaymentCollectedQuery) { q.Limit = -5 }},
		{"unknown payment_for", func(q *model.PaymentCollectedQuery) { q.Filter.PaymentFor = "everything" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultPaymentCollectedQuery()
			tt.mutate(&q)
			_, err := repository.BuildPaymentsCollectedListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildPaymentsCollectedListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE payments; --`
	q := defaultPaymentCollectedQuery()
	q.Filter.Search = hostile
	q.Filter.Methods = []model.PaymentMethod{model.PaymentMethod(hostile)}
	q.After = &model.PaymentCollectedCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildPaymentsCollectedListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(strings.TrimSpace(toString(a))) + "|"
	}
	require.Contains(t, flat, "drop table payments", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildPaymentsCollectedSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildPaymentsCollectedSummaryQuery(fullPaymentCollectedFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "SELECT count(*)")
	require.Contains(t, stmt.SQL, "COALESCE(SUM(p.amount * 100), 0)::bigint")
	require.Contains(t, stmt.SQL, "COALESCE(SUM("+withoutTaxSQL+" * 100), 0)::bigint",
		"the footer adds up the same rounded expression the rows show")
	require.NotContains(t, stmt.SQL, "ORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildPaymentsCollectedSummaryQueryRejectsUnknownPaymentFor(t *testing.T) {
	_, err := repository.BuildPaymentsCollectedSummaryQuery(model.PaymentCollectedFilter{PaymentFor: "bogus"})
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

func TestPaymentsCollectedListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullPaymentCollectedFilter()

	q := defaultPaymentCollectedQuery()
	q.Filter = f
	list, err := repository.BuildPaymentsCollectedListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildPaymentsCollectedSummaryQuery(f)
	require.NoError(t, err)

	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}
