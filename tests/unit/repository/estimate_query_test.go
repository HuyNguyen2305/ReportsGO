package repository_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultEstimateQuery() model.EstimateQuery {
	return model.EstimateQuery{
		Filter: model.EstimateFilter{Lifecycle: model.LifecycleActive},
		Sort:   model.EstimateSort{Key: model.EstimateSortDate, Desc: true},
		Limit:  50,
	}
}

func fullEstimateFilter() model.EstimateFilter {
	return model.EstimateFilter{
		DateFrom:        day(2026, 10, 1),
		DateTo:          day(2026, 10, 31),
		Lifecycle:       model.LifecycleActive,
		Statuses:        []model.EstimateStatus{model.EstimateAccepted, model.EstimateSent},
		SignatureStatus: []model.SignatureStatus{model.SignatureSigned},
		Search:          "saliba",
	}
}

func TestBuildEstimateListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildEstimateListQuery(defaultEstimateQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM billing_documents e")
	require.Contains(t, stmt.SQL, "e.parent_type = 'estimate'")
	require.NotContains(t, stmt.SQL, "'invoice'")
	require.Contains(t, stmt.SQL, "ORDER BY e.document_date DESC, e.id DESC")
	require.Contains(t, stmt.SQL, "e.deleted_at IS NULL AND e.archived_at IS NULL")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildEstimateListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.EstimateSortKey]struct{ expr, cast string }{
		model.EstimateSortAccountNumber:   {"c.account_number", "text"},
		model.EstimateSortNumber:          {"e.document_number", "text"},
		model.EstimateSortCustomer:        {"lower(c.display_name)", "text"},
		model.EstimateSortPhone:           {"coalesce(ph.number, '')", "text"},
		model.EstimateSortBillingEmail:    {"lower(coalesce(e.billing_email, ''))", "text"},
		model.EstimateSortCreatedBy:       {"lower(coalesce(u.full_name, ''))", "text"},
		model.EstimateSortDate:            {"e.document_date", "date"},
		model.EstimateSortTotal:           {"e.total", "numeric"},
		model.EstimateSortStatus:          {"e.status::text", "text"},
		model.EstimateSortSignatureStatus: {"e.signature_status::text", "text"},
	}
	require.Len(t, wantExpr, len(model.EstimateSortKeys), "every key must be covered by this test")

	for _, key := range model.EstimateSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				q := defaultEstimateQuery()
				q.Sort = model.EstimateSort{Key: key, Desc: desc}
				q.After = &model.EstimateCursor{SortValue: "x", ID: 7}

				stmt, err := repository.BuildEstimateListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+sortKeySQL(want.expr, want.cast)+" "+dir+", e.id "+dir)
				require.Contains(t, stmt.SQL, "("+sortKeySQL(want.expr, want.cast)+", e.id) "+cmp+" (($")
				require.Contains(t, stmt.SQL, "::text)::"+want.cast+", $")
				require.Contains(t, stmt.SQL, "AS sort_value")
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildEstimateListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultEstimateQuery()
	q.Filter = fullEstimateFilter()
	q.After = &model.EstimateCursor{SortValue: "2026-10-12", ID: 9}

	stmt, err := repository.BuildEstimateListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		day(2026, 10, 1),
		day(2026, 10, 31),
		[]string{"accepted", "sent"},
		[]string{"signed"},
		"%saliba%",
		"2026-10-12",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"e.document_date >= $1::date",
		"e.document_date <= $2::date",
		"e.status::text = ANY($3::text[])",
		"e.signature_status::text = ANY($4::text[])",
		"e.document_number ILIKE $5",
		"c.account_number ILIKE $5",
		"c.display_name ILIKE $5",
		"e.billing_email ILIKE $5",
		"(($6::text)::date, $7::bigint)",
		"LIMIT $8",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildEstimateListQueryEmptyFiltersAddNothing(t *testing.T) {
	stmt, err := repository.BuildEstimateListQuery(defaultEstimateQuery())
	require.NoError(t, err)

	for _, absent := range []string{"document_date >=", "document_date <=", "ANY(", "EXISTS", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 1)
}

func TestBuildEstimateListQueryLifecycleTabs(t *testing.T) {
	tests := []struct {
		lifecycle model.Lifecycle
		want      string
	}{
		{model.LifecycleActive, "e.deleted_at IS NULL AND e.archived_at IS NULL"},
		{"", "e.deleted_at IS NULL AND e.archived_at IS NULL"},
		{model.LifecycleArchived, "e.deleted_at IS NULL AND e.archived_at IS NOT NULL"},
		{model.LifecycleDeleted, "e.deleted_at IS NOT NULL"},
	}
	for _, tt := range tests {
		t.Run(string(tt.lifecycle), func(t *testing.T) {
			q := defaultEstimateQuery()
			q.Filter.Lifecycle = tt.lifecycle
			stmt, err := repository.BuildEstimateListQuery(q)
			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
		})
	}
}

func TestBuildEstimateListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct{ search, want string }{
		{"saliba", `%saliba%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultEstimateQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildEstimateListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[0])
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildEstimateListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.EstimateQuery)
	}{
		{"unknown sort key", func(q *model.EstimateQuery) { q.Sort.Key = "id; DROP TABLE billing_documents" }},
		{"an invoice-only sort key", func(q *model.EstimateQuery) { q.Sort.Key = "discount" }},
		{"empty sort key", func(q *model.EstimateQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.EstimateQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.EstimateQuery) { q.Limit = -5 }},
		{"unknown lifecycle", func(q *model.EstimateQuery) { q.Filter.Lifecycle = "everything" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultEstimateQuery()
			tt.mutate(&q)
			_, err := repository.BuildEstimateListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildEstimateListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE billing_documents; --`
	q := defaultEstimateQuery()
	q.Filter.Search = hostile
	q.Filter.Statuses = []model.EstimateStatus{model.EstimateStatus(hostile)}
	q.Filter.SignatureStatus = []model.SignatureStatus{model.SignatureStatus(hostile)}
	q.After = &model.EstimateCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildEstimateListQuery(q)
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

func TestBuildEstimateSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildEstimateSummaryQuery(fullEstimateFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "count(*)")
	require.Contains(t, stmt.SQL, "SUM(e.total * 100)")
	require.Contains(t, stmt.SQL, "e.parent_type = 'estimate'")
	require.NotContains(t, stmt.SQL, "customer_phones", "the summary does not need the list-only joins")
	require.NotContains(t, stmt.SQL, "ORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestEstimateListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullEstimateFilter()

	q := defaultEstimateQuery()
	q.Filter = f
	list, err := repository.BuildEstimateListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildEstimateSummaryQuery(f)
	require.NoError(t, err)

	// Same arguments for the filter part (the list adds only its trailing LIMIT).
	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}

func TestBuildEstimateSummaryQueryRejectsUnknownLifecycle(t *testing.T) {
	_, err := repository.BuildEstimateSummaryQuery(model.EstimateFilter{Lifecycle: "bogus"})
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

// The phone is a lateral "first phone" join, so an estimate stays one row however many phones the customer has.
func TestBuildEstimateListQueryTakesTheFirstPhoneOnly(t *testing.T) {
	stmt, err := repository.BuildEstimateListQuery(defaultEstimateQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "LEFT JOIN LATERAL")
	require.Contains(t, stmt.SQL, "ORDER BY p.id LIMIT 1")
}
