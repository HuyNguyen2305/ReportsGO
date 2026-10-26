package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultCreditQuery() model.CreditQuery {
	return model.CreditQuery{
		Sort:  model.CreditSort{Key: model.CreditSortDate, Desc: true},
		Limit: 50,
	}
}

func fullCreditFilter() model.CreditFilter {
	return model.CreditFilter{
		DateFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Methods:  []model.PaymentMethod{model.MethodCard, model.MethodCheck},
		Search:   "stripe",
	}
}

func TestBuildCreditListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildCreditListQuery(defaultCreditQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM credits cr")
	require.Contains(t, stmt.SQL, "JOIN customers c ON c.id = cr.customer_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN users u ON u.id = cr.created_by")
	require.Contains(t, stmt.SQL, "ORDER BY cr.credit_date DESC, cr.id DESC")
	for _, absent := range []string{"deleted_at", "archived_at", "WHERE"} {
		require.NotContains(t, stmt.SQL, absent, "credits have no tabs and an empty filter adds no condition")
	}
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildCreditListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.CreditSortKey]struct{ expr, cast string }{
		model.CreditSortCustomer:      {"lower(c.display_name)", "text"},
		model.CreditSortAccountNumber: {"c.account_number", "text"},
		model.CreditSortMethod:        {"cr.method_type::text", "text"},
		model.CreditSortAmount:        {"cr.amount", "numeric"},
		model.CreditSortDate:          {"cr.credit_date", "date"},
		model.CreditSortCreatedBy:     {"lower(coalesce(u.full_name, ''))", "text"},
	}
	require.Len(t, wantExpr, len(model.CreditSortKeys), "every key must be covered by this test")

	for _, key := range model.CreditSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				q := defaultCreditQuery()
				q.Sort = model.CreditSort{Key: key, Desc: desc}
				q.After = &model.CreditCursor{SortValue: "x", ID: 7}

				stmt, err := repository.BuildCreditListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+sortKeySQL(want.expr, want.cast)+" "+dir+", cr.id "+dir)
				require.Contains(t, stmt.SQL, "("+sortKeySQL(want.expr, want.cast)+", cr.id) "+cmp+" (($")
				require.Contains(t, stmt.SQL, "::text)::"+want.cast+", $")
				require.Contains(t, stmt.SQL, "AS sort_value")
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildCreditListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultCreditQuery()
	q.Filter = fullCreditFilter()
	q.After = &model.CreditCursor{SortValue: "2026-10-12", ID: 9}

	stmt, err := repository.BuildCreditListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		[]string{"card", "check"},
		"%stripe%",
		"2026-10-12",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"cr.credit_date >= $1::date",
		"cr.credit_date <= $2::date",
		"cr.method_type::text = ANY($3::text[])",
		"c.display_name ILIKE $4",
		"c.account_number ILIKE $4",
		"cr.method_detail ILIKE $4",
		"(($5::text)::date, $6::bigint)",
		"LIMIT $7",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildCreditListQueryEmptyFiltersAddNothing(t *testing.T) {
	stmt, err := repository.BuildCreditListQuery(defaultCreditQuery())
	require.NoError(t, err)

	for _, absent := range []string{"credit_date >=", "credit_date <=", "ANY(", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 1)
}

func TestBuildCreditListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct{ search, want string }{
		{"stripe", `%stripe%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultCreditQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildCreditListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[0])
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildCreditListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.CreditQuery)
	}{
		{"unknown sort key", func(q *model.CreditQuery) { q.Sort.Key = "id; DROP TABLE credits" }},
		{"another report's sort key", func(q *model.CreditQuery) { q.Sort.Key = "payment_date" }},
		{"empty sort key", func(q *model.CreditQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.CreditQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.CreditQuery) { q.Limit = -5 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultCreditQuery()
			tt.mutate(&q)
			_, err := repository.BuildCreditListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildCreditListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE credits; --`
	q := defaultCreditQuery()
	q.Filter.Search = hostile
	q.Filter.Methods = []model.PaymentMethod{model.PaymentMethod(hostile)}
	q.After = &model.CreditCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildCreditListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(strings.TrimSpace(toString(a))) + "|"
	}
	require.Contains(t, flat, "drop table credits", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildCreditSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildCreditSummaryQuery(fullCreditFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "SELECT count(*), COALESCE(SUM(cr.amount * 100), 0)::bigint")
	require.NotContains(t, stmt.SQL, "ORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestCreditListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullCreditFilter()

	q := defaultCreditQuery()
	q.Filter = f
	list, err := repository.BuildCreditListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildCreditSummaryQuery(f)
	require.NoError(t, err)

	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}
