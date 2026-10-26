package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultPaymentQuery() model.PaymentQuery {
	return model.PaymentQuery{
		Filter: model.PaymentFilter{Lifecycle: model.LifecycleActive},
		Sort:   model.PaymentSort{Key: model.PaymentSortDate, Desc: true},
		Limit:  50,
	}
}

func fullPaymentFilter() model.PaymentFilter {
	return model.PaymentFilter{
		DateFrom:  time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		DateTo:    time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Lifecycle: model.LifecycleActive,
		Statuses:  []model.PaymentStatus{model.PaymentPaid, model.PaymentPending},
		Methods:   []model.PaymentMethod{model.MethodCard, model.MethodCheck},
		Search:    "85004",
	}
}

func TestBuildPaymentListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildPaymentListQuery(defaultPaymentQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM payments p")
	require.Contains(t, stmt.SQL, "LEFT JOIN billing_documents i ON i.id = p.invoice_id")
	require.Contains(t, stmt.SQL, "ORDER BY p.payment_date DESC, p.id DESC")
	require.Contains(t, stmt.SQL, "p.deleted_at IS NULL")
	require.NotContains(t, stmt.SQL, "archived_at", "payments cannot be archived")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildPaymentListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.PaymentSortKey]struct{ expr, cast string }{
		model.PaymentSortPaymentFor: {"lower(coalesce(i.document_number, 'credit'))", "text"},
		model.PaymentSortCustomer:   {"lower(c.display_name)", "text"},
		model.PaymentSortMethod:     {"p.method_type::text", "text"},
		model.PaymentSortDate:       {"p.payment_date", "date"},
		model.PaymentSortAmount:     {"p.amount", "numeric"},
		model.PaymentSortStatus:     {"p.status::text", "text"},
	}
	require.Len(t, wantExpr, len(model.PaymentSortKeys), "every key must be covered by this test")

	for _, key := range model.PaymentSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				q := defaultPaymentQuery()
				q.Sort = model.PaymentSort{Key: key, Desc: desc}
				q.After = &model.PaymentCursor{SortValue: "x", ID: 7}

				stmt, err := repository.BuildPaymentListQuery(q)
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

func TestBuildPaymentListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultPaymentQuery()
	q.Filter = fullPaymentFilter()
	q.After = &model.PaymentCursor{SortValue: "2026-10-12", ID: 9}

	stmt, err := repository.BuildPaymentListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		[]string{"paid", "pending"},
		[]string{"card", "check"},
		"%85004%",
		"2026-10-12",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"p.payment_date >= $1::date",
		"p.payment_date <= $2::date",
		"p.status::text = ANY($3::text[])",
		"p.method_type::text = ANY($4::text[])",
		"i.document_number ILIKE $5",
		"c.account_number ILIKE $5",
		"c.display_name ILIKE $5",
		"(($6::text)::date, $7::bigint)",
		"LIMIT $8",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildPaymentListQueryEmptyFiltersAddNothing(t *testing.T) {
	stmt, err := repository.BuildPaymentListQuery(defaultPaymentQuery())
	require.NoError(t, err)

	for _, absent := range []string{"payment_date >=", "payment_date <=", "ANY(", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 1)
}

func TestBuildPaymentListQueryLifecycleTabs(t *testing.T) {
	tests := []struct {
		lifecycle model.Lifecycle
		want      string
	}{
		{model.LifecycleActive, "p.deleted_at IS NULL"},
		{"", "p.deleted_at IS NULL"},
		{model.LifecycleDeleted, "p.deleted_at IS NOT NULL"},
	}
	for _, tt := range tests {
		t.Run(string(tt.lifecycle), func(t *testing.T) {
			q := defaultPaymentQuery()
			q.Filter.Lifecycle = tt.lifecycle
			stmt, err := repository.BuildPaymentListQuery(q)
			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
		})
	}
}

func TestBuildPaymentListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct{ search, want string }{
		{"85004", `%85004%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultPaymentQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildPaymentListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[0])
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildPaymentListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.PaymentQuery)
	}{
		{"unknown sort key", func(q *model.PaymentQuery) { q.Sort.Key = "id; DROP TABLE payments" }},
		{"another report's sort key", func(q *model.PaymentQuery) { q.Sort.Key = "document_number" }},
		{"empty sort key", func(q *model.PaymentQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.PaymentQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.PaymentQuery) { q.Limit = -5 }},
		{"unknown lifecycle", func(q *model.PaymentQuery) { q.Filter.Lifecycle = "everything" }},
		{"payments cannot be archived", func(q *model.PaymentQuery) { q.Filter.Lifecycle = model.LifecycleArchived }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultPaymentQuery()
			tt.mutate(&q)
			_, err := repository.BuildPaymentListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildPaymentListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE payments; --`
	q := defaultPaymentQuery()
	q.Filter.Search = hostile
	q.Filter.Statuses = []model.PaymentStatus{model.PaymentStatus(hostile)}
	q.Filter.Methods = []model.PaymentMethod{model.PaymentMethod(hostile)}
	q.After = &model.PaymentCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildPaymentListQuery(q)
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

func TestBuildPaymentSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildPaymentSummaryQuery(fullPaymentFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "SELECT count(*), COALESCE(SUM(p.amount * 100), 0)::bigint")
	require.NotContains(t, stmt.SQL, "ORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestPaymentListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullPaymentFilter()

	q := defaultPaymentQuery()
	q.Filter = f
	list, err := repository.BuildPaymentListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildPaymentSummaryQuery(f)
	require.NoError(t, err)

	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}

func TestBuildPaymentSummaryQueryRejectsUnknownLifecycle(t *testing.T) {
	for _, l := range []model.Lifecycle{"bogus", model.LifecycleArchived} {
		_, err := repository.BuildPaymentSummaryQuery(model.PaymentFilter{Lifecycle: l})
		require.ErrorIs(t, err, repository.ErrInvalidQuery, string(l))
	}
}
