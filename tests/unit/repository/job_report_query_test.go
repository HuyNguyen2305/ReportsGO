package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultJobQuery() model.JobQuery {
	return model.JobQuery{
		Sort:  model.JobSort{Key: model.JobSortCreatedAt, Desc: true},
		Limit: 50,
	}
}

func fullJobFilter() model.JobFilter {
	return model.JobFilter{
		CreatedFrom:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		CreatedBefore:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		Statuses:       []model.JobStatus{model.JobCompleted, model.JobCancelled},
		ServiceTypeIDs: []int64{1, 2},
		LeadSourceIDs:  []int64{3},
		Search:         "saliba",
	}
}

func TestBuildJobListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildJobListQuery(defaultJobQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM jobs j")
	require.Contains(t, stmt.SQL, "JOIN customers c ON c.id = j.customer_id")
	require.Contains(t, stmt.SQL, "JOIN service_types st ON st.id = j.service_type_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN lead_sources ls ON ls.id = j.lead_source_id")
	require.Contains(t, stmt.SQL, "ORDER BY j.created_at DESC, j.id DESC")
	for _, absent := range []string{"deleted_at", "archived_at", "WHERE"} {
		require.NotContains(t, stmt.SQL, absent, "jobs have no tabs and an empty filter adds no condition")
	}
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildJobListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.JobSortKey]struct{ expr, cast string }{
		model.JobSortCustomer:      {"lower(c.display_name)", "text"},
		model.JobSortAccountNumber: {"c.account_number", "text"},
		model.JobSortServiceType:   {"lower(st.name)", "text"},
		model.JobSortSource:        {"lower(coalesce(ls.name, ''))", "text"},
		model.JobSortInvoiceValue:  {"j.invoice_value", "numeric"},
		model.JobSortCreatedAt:     {"j.created_at", "timestamptz"},
		model.JobSortStatus:        {"j.status::text", "text"},
	}
	require.Len(t, wantExpr, len(model.JobSortKeys), "every key must be covered by this test")

	for _, key := range model.JobSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				q := defaultJobQuery()
				q.Sort = model.JobSort{Key: key, Desc: desc}
				q.After = &model.JobCursor{SortValue: "x", ID: 7}

				stmt, err := repository.BuildJobListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+want.expr+" "+dir+", j.id "+dir)
				require.Contains(t, stmt.SQL, "("+want.expr+", j.id) "+cmp+" (($")
				require.Contains(t, stmt.SQL, "::text)::"+want.cast+", $")
				require.Contains(t, stmt.SQL, "AS sort_value")
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

// created_at is rendered as fixed-format UTC text for the cursor, so it never depends on the session time zone.
func TestBuildJobListQueryRendersCreatedAtAsUTCText(t *testing.T) {
	stmt, err := repository.BuildJobListQuery(defaultJobQuery())
	require.NoError(t, err)
	require.Contains(t, stmt.SQL, `to_char(j.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`)

	q := defaultJobQuery()
	q.Sort.Key = model.JobSortInvoiceValue
	stmt, err = repository.BuildJobListQuery(q)
	require.NoError(t, err)
	require.NotContains(t, stmt.SQL, "to_char(", "other keys use their own expression")
	require.Contains(t, stmt.SQL, "(j.invoice_value)::text AS sort_value")
}

func TestBuildJobListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultJobQuery()
	q.Filter = fullJobFilter()
	q.After = &model.JobCursor{SortValue: "2026-10-12T08:00:00.000000Z", ID: 9}

	stmt, err := repository.BuildJobListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		[]string{"completed", "cancelled"},
		[]int64{1, 2},
		[]int64{3},
		"%saliba%",
		"2026-10-12T08:00:00.000000Z",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"j.created_at >= $1::timestamptz",
		"j.created_at < $2::timestamptz",
		"j.status::text = ANY($3::text[])",
		"j.service_type_id = ANY($4::bigint[])",
		"j.lead_source_id = ANY($5::bigint[])",
		"c.display_name ILIKE $6",
		"c.account_number ILIKE $6",
		"st.name ILIKE $6",
		"(($7::text)::timestamptz, $8::bigint)",
		"LIMIT $9",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildJobListQueryTheUpperBoundIsExclusive(t *testing.T) {
	stmt, err := repository.BuildJobListQuery(model.JobQuery{
		Filter: model.JobFilter{CreatedBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		Sort:   model.JobSort{Key: model.JobSortCustomer}, Limit: 5,
	})
	require.NoError(t, err)
	require.Contains(t, stmt.SQL, "j.created_at < $1")
	require.NotContains(t, stmt.SQL, "j.created_at <= ")
}

func TestBuildJobListQueryConvertsInstantsToUTC(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	stmt, err := repository.BuildJobListQuery(model.JobQuery{
		Filter: model.JobFilter{CreatedFrom: time.Date(2026, 10, 1, 7, 0, 0, 0, plus7)},
		Sort:   model.JobSort{Key: model.JobSortCustomer}, Limit: 5,
	})
	require.NoError(t, err)
	got := stmt.Args[0].(time.Time)
	require.Equal(t, time.UTC, got.Location())
	require.True(t, got.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
}

func TestBuildJobListQueryEmptyFiltersAddNothing(t *testing.T) {
	stmt, err := repository.BuildJobListQuery(defaultJobQuery())
	require.NoError(t, err)

	for _, absent := range []string{"created_at >=", "created_at <", "ANY(", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 1)
}

func TestBuildJobListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct{ search, want string }{
		{"saliba", `%saliba%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultJobQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildJobListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[0])
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildJobListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.JobQuery)
	}{
		{"unknown sort key", func(q *model.JobQuery) { q.Sort.Key = "id; DROP TABLE jobs" }},
		{"another report's sort key", func(q *model.JobQuery) { q.Sort.Key = "credit_date" }},
		{"empty sort key", func(q *model.JobQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.JobQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.JobQuery) { q.Limit = -5 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultJobQuery()
			tt.mutate(&q)
			_, err := repository.BuildJobListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildJobListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE jobs; --`
	q := defaultJobQuery()
	q.Filter.Search = hostile
	q.Filter.Statuses = []model.JobStatus{model.JobStatus(hostile)}
	q.After = &model.JobCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildJobListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(strings.TrimSpace(toString(a))) + "|"
	}
	require.Contains(t, flat, "drop table jobs", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildJobSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildJobSummaryQuery(fullJobFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "SELECT count(*), COALESCE(SUM(j.invoice_value * 100), 0)::bigint")
	require.NotContains(t, stmt.SQL, "ORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestJobListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullJobFilter()

	q := defaultJobQuery()
	q.Filter = f
	list, err := repository.BuildJobListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildJobSummaryQuery(f)
	require.NoError(t, err)

	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}
