package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultDocumentQuery() model.DocumentQuery {
	return model.DocumentQuery{
		Filter: model.DocumentFilter{Lifecycle: model.LifecycleActive},
		Sort:   model.DocumentSort{Key: model.DocumentSortCreatedAt, Desc: true},
		Limit:  50,
	}
}

func fullDocumentFilter() model.DocumentFilter {
	return model.DocumentFilter{
		CreatedFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		CreatedBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		Lifecycle:     model.LifecycleActive,
		Statuses:      []model.DocumentStatus{model.DocumentSent, model.DocumentSigned},
		TypeIDs:       []int64{1, 2},
		Search:        "saliba",
	}
}

func TestBuildDocumentListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildDocumentListQuery(defaultDocumentQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM documents d")
	require.Contains(t, stmt.SQL, "JOIN document_types dt")
	require.Contains(t, stmt.SQL, "ORDER BY d.created_at DESC, d.id DESC")
	require.Contains(t, stmt.SQL, "d.deleted_at IS NULL")
	require.NotContains(t, stmt.SQL, "archived_at", "documents cannot be archived")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildDocumentListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.DocumentSortKey]struct{ expr, cast string }{
		model.DocumentSortNumber:        {"d.document_number", "text"},
		model.DocumentSortType:          {"lower(dt.name)", "text"},
		model.DocumentSortAccountNumber: {"c.account_number", "text"},
		model.DocumentSortCustomer:      {"lower(c.display_name)", "text"},
		model.DocumentSortCreatedBy:     {"lower(coalesce(u.full_name, ''))", "text"},
		model.DocumentSortCreatedAt:     {"d.created_at", "timestamptz"},
		model.DocumentSortStatus:        {"d.status::text", "text"},
	}
	require.Len(t, wantExpr, len(model.DocumentSortKeys), "every key must be covered by this test")

	for _, key := range model.DocumentSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				q := defaultDocumentQuery()
				q.Sort = model.DocumentSort{Key: key, Desc: desc}
				q.After = &model.DocumentCursor{SortValue: "x", ID: 7}

				stmt, err := repository.BuildDocumentListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+want.expr+" "+dir+", d.id "+dir)
				require.Contains(t, stmt.SQL, "("+want.expr+", d.id) "+cmp+" (($")
				require.Contains(t, stmt.SQL, "::text)::"+want.cast+", $")
				require.Contains(t, stmt.SQL, "AS sort_value")
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

// created_at is rendered as fixed-format UTC text for the cursor, so it never depends on the session time zone.
func TestBuildDocumentListQueryRendersCreatedAtAsUTCText(t *testing.T) {
	q := defaultDocumentQuery()
	stmt, err := repository.BuildDocumentListQuery(q)
	require.NoError(t, err)
	require.Contains(t, stmt.SQL, `to_char(d.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`)

	q.Sort.Key = model.DocumentSortNumber
	stmt, err = repository.BuildDocumentListQuery(q)
	require.NoError(t, err)
	require.NotContains(t, stmt.SQL, "to_char(", "other keys use their own expression")
	require.Contains(t, stmt.SQL, "(d.document_number)::text AS sort_value")
}

func TestBuildDocumentListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultDocumentQuery()
	q.Filter = fullDocumentFilter()
	q.After = &model.DocumentCursor{SortValue: "2026-10-12T00:00:00.000000Z", ID: 9}

	stmt, err := repository.BuildDocumentListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		[]string{"sent", "signed"},
		[]int64{1, 2},
		"%saliba%",
		"2026-10-12T00:00:00.000000Z",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"d.created_at >= $1::timestamptz",
		"d.created_at < $2::timestamptz",
		"d.status::text = ANY($3::text[])",
		"d.document_type_id = ANY($4::bigint[])",
		"d.document_number ILIKE $5",
		"c.account_number ILIKE $5",
		"c.display_name ILIKE $5",
		"(($6::text)::timestamptz, $7::bigint)",
		"LIMIT $8",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	require.NotContains(t, stmt.SQL, "billing_email", "documents have no billing email to search")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildDocumentListQueryTheUpperDateBoundIsExclusive(t *testing.T) {
	stmt, err := repository.BuildDocumentListQuery(model.DocumentQuery{
		Filter: model.DocumentFilter{CreatedBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		Sort:   model.DocumentSort{Key: model.DocumentSortNumber}, Limit: 5,
	})
	require.NoError(t, err)
	require.Contains(t, stmt.SQL, "d.created_at < $1")
	require.NotContains(t, stmt.SQL, "d.created_at <= ")
}

func TestBuildDocumentListQueryConvertsInstantsToUTC(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	stmt, err := repository.BuildDocumentListQuery(model.DocumentQuery{
		Filter: model.DocumentFilter{CreatedFrom: time.Date(2026, 10, 1, 7, 0, 0, 0, plus7)},
		Sort:   model.DocumentSort{Key: model.DocumentSortNumber}, Limit: 5,
	})
	require.NoError(t, err)
	got := stmt.Args[0].(time.Time)
	require.Equal(t, time.UTC, got.Location())
	require.True(t, got.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
}

func TestBuildDocumentListQueryEmptyFiltersAddNothing(t *testing.T) {
	stmt, err := repository.BuildDocumentListQuery(defaultDocumentQuery())
	require.NoError(t, err)

	for _, absent := range []string{"created_at >=", "created_at <", "ANY(", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 1)
}

func TestBuildDocumentListQueryLifecycleTabs(t *testing.T) {
	tests := []struct {
		lifecycle model.Lifecycle
		want      string
	}{
		{model.LifecycleActive, "d.deleted_at IS NULL"},
		{"", "d.deleted_at IS NULL"},
		{model.LifecycleDeleted, "d.deleted_at IS NOT NULL"},
	}
	for _, tt := range tests {
		t.Run(string(tt.lifecycle), func(t *testing.T) {
			q := defaultDocumentQuery()
			q.Filter.Lifecycle = tt.lifecycle
			stmt, err := repository.BuildDocumentListQuery(q)
			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
		})
	}
}

func TestBuildDocumentListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct{ search, want string }{
		{"saliba", `%saliba%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultDocumentQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildDocumentListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[0])
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildDocumentListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.DocumentQuery)
	}{
		{"unknown sort key", func(q *model.DocumentQuery) { q.Sort.Key = "id; DROP TABLE documents" }},
		{"an estimate-only sort key", func(q *model.DocumentQuery) { q.Sort.Key = "phone" }},
		{"empty sort key", func(q *model.DocumentQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.DocumentQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.DocumentQuery) { q.Limit = -5 }},
		{"unknown lifecycle", func(q *model.DocumentQuery) { q.Filter.Lifecycle = "everything" }},
		{"documents cannot be archived", func(q *model.DocumentQuery) { q.Filter.Lifecycle = model.LifecycleArchived }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultDocumentQuery()
			tt.mutate(&q)
			_, err := repository.BuildDocumentListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildDocumentListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE documents; --`
	q := defaultDocumentQuery()
	q.Filter.Search = hostile
	q.Filter.Statuses = []model.DocumentStatus{model.DocumentStatus(hostile)}
	q.After = &model.DocumentCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildDocumentListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(strings.TrimSpace(toString(a))) + "|"
	}
	require.Contains(t, flat, "drop table documents", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildDocumentSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildDocumentSummaryQuery(fullDocumentFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "SELECT count(*)")
	require.NotContains(t, stmt.SQL, "document_types", "the summary does not need the list-only joins")
	require.NotContains(t, stmt.SQL, "users")
	require.NotContains(t, stmt.SQL, "ORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestDocumentListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullDocumentFilter()

	q := defaultDocumentQuery()
	q.Filter = f
	list, err := repository.BuildDocumentListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildDocumentSummaryQuery(f)
	require.NoError(t, err)

	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}

func TestBuildDocumentSummaryQueryRejectsUnknownLifecycle(t *testing.T) {
	for _, l := range []model.Lifecycle{"bogus", model.LifecycleArchived} {
		_, err := repository.BuildDocumentSummaryQuery(model.DocumentFilter{Lifecycle: l})
		require.ErrorIs(t, err, repository.ErrInvalidQuery, string(l))
	}
}
