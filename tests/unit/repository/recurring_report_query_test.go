package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultRecurringQuery() model.RecurringQuery {
	return model.RecurringQuery{
		Sort:  model.RecurringSort{Key: model.RecurringSortStartedOn, Desc: true},
		Limit: 50,
	}
}

func fullRecurringFilter() model.RecurringFilter {
	return model.RecurringFilter{
		DateFrom: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Statuses: []model.RecurringStatus{model.RecurringActive, model.RecurringPaused},
		Actions:  []model.RecurringAction{model.ActionSendByEmail},
		Search:   "85032",
	}
}

func TestBuildRecurringListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildRecurringListQuery(defaultRecurringQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM recurring_invoices r")
	require.Contains(t, stmt.SQL, "JOIN customers c ON c.id = r.customer_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN billing_documents i ON i.id = r.next_invoice_id")
	require.Contains(t, stmt.SQL, "ORDER BY r.started_on DESC, r.id DESC")
	for _, absent := range []string{"deleted_at", "archived_at", "WHERE"} {
		require.NotContains(t, stmt.SQL, absent, "no tabs, and an empty filter adds no condition")
	}
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildRecurringListQueryEverySortKeyAndDirection(t *testing.T) {
	const period = `lpad((r.frequency_interval * CASE r.frequency_unit
    WHEN 'day' THEN 1 WHEN 'week' THEN 7 WHEN 'month' THEN 30 ELSE 365 END)::text, 10, '0')`
	wantExpr := map[model.RecurringSortKey]struct{ expr, cast string }{
		model.RecurringSortCustomer:        {"lower(c.display_name)", "text"},
		model.RecurringSortAccountNumber:   {"c.account_number", "text"},
		model.RecurringSortRepeats:         {period, "text"},
		model.RecurringSortAction:          {"r.action::text", "text"},
		model.RecurringSortStatus:          {"r.status::text", "text"},
		model.RecurringSortTotalValue:      {"r.total_value", "numeric"},
		model.RecurringSortStartedOn:       {"r.started_on", "date"},
		model.RecurringSortEndsOn:          {"coalesce(r.ends_on, DATE '9999-12-31')", "date"},
		model.RecurringSortNextInvoiceDate: {"coalesce(r.next_invoice_date, DATE '9999-12-31')", "date"},
	}
	require.Len(t, wantExpr, len(model.RecurringSortKeys), "every key must be covered by this test")

	for _, key := range model.RecurringSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				q := defaultRecurringQuery()
				q.Sort = model.RecurringSort{Key: key, Desc: desc}
				q.After = &model.RecurringCursor{SortValue: "x", ID: 7}

				stmt, err := repository.BuildRecurringListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+sortKeySQL(want.expr, want.cast)+" "+dir+", r.id "+dir)
				require.Contains(t, stmt.SQL, "("+sortKeySQL(want.expr, want.cast)+", r.id) "+cmp+" (($")
				require.Contains(t, stmt.SQL, "::text)::"+want.cast+", $")
				require.Contains(t, stmt.SQL, "AS sort_value")
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildRecurringListQuerySortExpressionsAreNeverNull(t *testing.T) {
	// The nullable columns go through coalesce; otherwise a NULL cursor value would make the keyset condition
	// undefined and silently drop rows.
	for _, key := range []model.RecurringSortKey{model.RecurringSortEndsOn, model.RecurringSortNextInvoiceDate} {
		q := defaultRecurringQuery()
		q.Sort.Key = key
		stmt, err := repository.BuildRecurringListQuery(q)
		require.NoError(t, err)
		require.Contains(t, stmt.SQL, "coalesce(r.", string(key))
	}
}

func TestBuildRecurringListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultRecurringQuery()
	q.Filter = fullRecurringFilter()
	q.After = &model.RecurringCursor{SortValue: "2026-09-14", ID: 9}

	stmt, err := repository.BuildRecurringListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		[]string{"active", "paused"},
		[]string{"send_by_email"},
		"%85032%",
		"2026-09-14",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"r.started_on >= $1::date",
		"r.started_on <= $2::date",
		"r.status::text = ANY($3::text[])",
		"r.action::text = ANY($4::text[])",
		"c.display_name ILIKE $5",
		"c.account_number ILIKE $5",
		"i.document_number ILIKE $5",
		"(($6::text)::date, $7::bigint)",
		"LIMIT $8",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildRecurringListQueryEmptyFiltersAddNothing(t *testing.T) {
	stmt, err := repository.BuildRecurringListQuery(defaultRecurringQuery())
	require.NoError(t, err)

	for _, absent := range []string{"started_on >=", "started_on <=", "ANY(", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 1)
}

func TestBuildRecurringListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct{ search, want string }{
		{"85032", `%85032%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultRecurringQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildRecurringListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[0])
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildRecurringListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.RecurringQuery)
	}{
		{"unknown sort key", func(q *model.RecurringQuery) { q.Sort.Key = "id; DROP TABLE recurring_invoices" }},
		{"another report's sort key", func(q *model.RecurringQuery) { q.Sort.Key = "credit_date" }},
		{"empty sort key", func(q *model.RecurringQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.RecurringQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.RecurringQuery) { q.Limit = -5 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultRecurringQuery()
			tt.mutate(&q)
			_, err := repository.BuildRecurringListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildRecurringListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE recurring_invoices; --`
	q := defaultRecurringQuery()
	q.Filter.Search = hostile
	q.Filter.Statuses = []model.RecurringStatus{model.RecurringStatus(hostile)}
	q.Filter.Actions = []model.RecurringAction{model.RecurringAction(hostile)}
	q.After = &model.RecurringCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildRecurringListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(strings.TrimSpace(toString(a))) + "|"
	}
	require.Contains(t, flat, "drop table recurring_invoices", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildRecurringSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildRecurringSummaryQuery(fullRecurringFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "SELECT count(*), COALESCE(SUM(r.total_value * 100), 0)::bigint")
	require.NotContains(t, stmt.SQL, "ORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestRecurringListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullRecurringFilter()

	q := defaultRecurringQuery()
	q.Filter = f
	list, err := repository.BuildRecurringListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildRecurringSummaryQuery(f)
	require.NoError(t, err)

	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}
