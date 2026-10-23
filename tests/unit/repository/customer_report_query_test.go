package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultCustomerQuery() model.CustomerQuery {
	return model.CustomerQuery{
		Sort:  model.CustomerSort{Key: model.CustomerSortCreatedAt, Desc: true},
		Limit: 50,
	}
}

func fullCustomerFilter() model.CustomerFilter {
	return model.CustomerFilter{
		CreatedFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		CreatedBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		LeadSourceIDs: []int64{2, 3},
		Search:        "ada",
	}
}

func TestBuildCustomerListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildCustomerListQuery(defaultCustomerQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM customers c")
	require.Contains(t, stmt.SQL, "LEFT JOIN lead_sources ls ON ls.id = c.lead_source_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN LATERAL (")
	require.Contains(t, stmt.SQL, "SELECT p.number FROM customer_phones p WHERE p.customer_id = c.id ORDER BY p.id LIMIT 1")
	require.Contains(t, stmt.SQL, "ORDER BY c.created_at DESC, c.id DESC")
	for _, absent := range []string{"deleted_at", "archived_at", "\nWHERE "} {
		require.NotContains(t, stmt.SQL, absent, "customers have no tabs and an empty filter adds no condition")
	}
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildCustomerListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.CustomerSortKey]struct{ expr, cast string }{
		model.CustomerSortCustomer:      {"lower(c.display_name)", "text"},
		model.CustomerSortAccountNumber: {"c.account_number", "text"},
		model.CustomerSortEmail:         {"lower(coalesce(c.email, ''))", "text"},
		model.CustomerSortPhone:         {"coalesce(ph.number, '')", "text"},
		model.CustomerSortSource:        {"lower(coalesce(ls.name, ''))", "text"},
		model.CustomerSortCreatedAt:     {"c.created_at", "timestamptz"},
	}
	require.Len(t, wantExpr, len(model.CustomerSortKeys), "every key must be covered by this test")

	for _, key := range model.CustomerSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				q := defaultCustomerQuery()
				q.Sort = model.CustomerSort{Key: key, Desc: desc}
				q.After = &model.CustomerCursor{SortValue: "x", ID: 7}

				stmt, err := repository.BuildCustomerListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+want.expr+" "+dir+", c.id "+dir)
				require.Contains(t, stmt.SQL, "("+want.expr+", c.id) "+cmp+" (($")
				require.Contains(t, stmt.SQL, "::text)::"+want.cast+", $")
				require.Contains(t, stmt.SQL, "AS sort_value")
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

// created_at is rendered as fixed-format UTC text for the cursor, so it never depends on the session time zone.
func TestBuildCustomerListQueryRendersCreatedAtAsUTCText(t *testing.T) {
	stmt, err := repository.BuildCustomerListQuery(defaultCustomerQuery())
	require.NoError(t, err)
	require.Contains(t, stmt.SQL, `to_char(c.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`)

	q := defaultCustomerQuery()
	q.Sort.Key = model.CustomerSortAccountNumber
	stmt, err = repository.BuildCustomerListQuery(q)
	require.NoError(t, err)
	require.NotContains(t, stmt.SQL, "to_char(", "other keys use their own expression")
	require.Contains(t, stmt.SQL, "(c.account_number)::text AS sort_value")
}

func TestBuildCustomerListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultCustomerQuery()
	q.Filter = fullCustomerFilter()
	q.After = &model.CustomerCursor{SortValue: "2026-10-12T08:00:00.000000Z", ID: 9}

	stmt, err := repository.BuildCustomerListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		[]int64{2, 3},
		"%ada%",
		"2026-10-12T08:00:00.000000Z",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"c.created_at >= $1::timestamptz",
		"c.created_at < $2::timestamptz",
		"c.lead_source_id = ANY($3::bigint[])",
		"c.display_name ILIKE $4",
		"c.account_number ILIKE $4",
		"c.email ILIKE $4",
		"ph.number ILIKE $4",
		"(($5::text)::timestamptz, $6::bigint)",
		"LIMIT $7",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildCustomerListQueryTheUpperBoundIsExclusive(t *testing.T) {
	stmt, err := repository.BuildCustomerListQuery(model.CustomerQuery{
		Filter: model.CustomerFilter{CreatedBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		Sort:   model.CustomerSort{Key: model.CustomerSortCustomer}, Limit: 5,
	})
	require.NoError(t, err)
	require.Contains(t, stmt.SQL, "c.created_at < $1")
	require.NotContains(t, stmt.SQL, "c.created_at <= ")
}

func TestBuildCustomerListQueryConvertsInstantsToUTC(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	stmt, err := repository.BuildCustomerListQuery(model.CustomerQuery{
		Filter: model.CustomerFilter{CreatedFrom: time.Date(2026, 10, 1, 7, 0, 0, 0, plus7)},
		Sort:   model.CustomerSort{Key: model.CustomerSortCustomer}, Limit: 5,
	})
	require.NoError(t, err)
	got := stmt.Args[0].(time.Time)
	require.Equal(t, time.UTC, got.Location())
	require.True(t, got.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
}

func TestBuildCustomerListQueryEmptyFiltersAddNothing(t *testing.T) {
	stmt, err := repository.BuildCustomerListQuery(defaultCustomerQuery())
	require.NoError(t, err)

	for _, absent := range []string{"created_at >=", "created_at <", "ANY(", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 1)
}

func TestBuildCustomerListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct{ search, want string }{
		{"ada", `%ada%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultCustomerQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildCustomerListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[0])
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildCustomerListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.CustomerQuery)
	}{
		{"unknown sort key", func(q *model.CustomerQuery) { q.Sort.Key = "id; DROP TABLE customers" }},
		{"another report's sort key", func(q *model.CustomerQuery) { q.Sort.Key = "invoice_value" }},
		{"empty sort key", func(q *model.CustomerQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.CustomerQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.CustomerQuery) { q.Limit = -5 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultCustomerQuery()
			tt.mutate(&q)
			_, err := repository.BuildCustomerListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildCustomerListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE customers; --`
	q := defaultCustomerQuery()
	q.Filter.Search = hostile
	q.After = &model.CustomerCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildCustomerListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(strings.TrimSpace(toString(a))) + "|"
	}
	require.Contains(t, flat, "drop table customers", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildCustomerSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildCustomerSummaryQuery(fullCustomerFilter())
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(stmt.SQL, "SELECT count(*)\nFROM customers c"))
	require.NotContains(t, stmt.SQL, "\nORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT $")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestCustomerListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullCustomerFilter()

	q := defaultCustomerQuery()
	q.Filter = f
	list, err := repository.BuildCustomerListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildCustomerSummaryQuery(f)
	require.NoError(t, err)

	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}
