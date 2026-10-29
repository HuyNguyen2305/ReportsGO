package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func serviceLookupQuery(f model.ServiceLookupFilter, key model.ServiceLookupSortKey, desc bool) model.ServiceLookupQuery {
	return model.ServiceLookupQuery{Filter: f, Sort: model.ServiceLookupSort{Key: key, Desc: desc}, Limit: 50}
}

func fullServiceLookupFilter() model.ServiceLookupFilter {
	return model.ServiceLookupFilter{
		Tab:             model.ServiceLookupActive,
		ScheduledFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		ScheduledBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		Statuses:        []model.JobStatus{model.JobConfirmed, model.JobCompleted},
		ServiceTypeIDs:  []int64{3, 4},
		AssignedToIDs:   []int64{2},
		TagIDs:          []int64{1, 2},
		Search:          "elm",
	}
}

func TestBuildServiceLookupListQueryFilters(t *testing.T) {
	stmt, err := repository.BuildServiceLookupListQuery(serviceLookupQuery(fullServiceLookupFilter(), model.ServiceLookupSortScheduledAt, false))
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "(j.deleted_at IS NULL AND NOT j.in_work_pool)")
	require.Contains(t, stmt.SQL, "j.scheduled_at >= $1::timestamptz")
	require.Contains(t, stmt.SQL, "j.scheduled_at < $2::timestamptz")
	require.Contains(t, stmt.SQL, "j.status::text = ANY($3::text[])")
	require.Contains(t, stmt.SQL, "j.service_type_id = ANY($4::bigint[])")
	require.Contains(t, stmt.SQL, "j.assigned_to = ANY($5::bigint[])")
	require.Contains(t, stmt.SQL, "EXISTS (SELECT 1 FROM job_tag_links l WHERE l.job_id = j.id AND l.tag_id = ANY($6::bigint[]))",
		"a tag filter must not repeat a job that has several of the tags")
	require.Contains(t, stmt.SQL, "sl.address_line1 ILIKE $7 ESCAPE '\\'")
	require.Equal(t, []any{
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		[]string{"confirmed", "completed"}, []int64{3, 4}, []int64{2}, []int64{1, 2}, "%elm%", 51,
	}, stmt.Args)
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildServiceLookupListQueryJoinsAndSelectsTags(t *testing.T) {
	stmt, err := repository.BuildServiceLookupListQuery(serviceLookupQuery(model.ServiceLookupFilter{}, model.ServiceLookupSortCustomer, false))
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "JOIN service_locations sl ON sl.id = j.location_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN users u ON u.id = j.assigned_to")
	require.Contains(t, stmt.SQL, "array_agg(t.id ORDER BY lower(t.name), t.id) AS ids, array_agg(t.name ORDER BY lower(t.name), t.id) AS names")
	require.Contains(t, stmt.SQL, "(j.invoice_value - j.tax_amount) * 100", "the value without tax")
	require.NotContains(t, stmt.SQL, "::date", "no date condition without a range")
	require.NotContains(t, stmt.SQL, "ILIKE", "no search")
	require.Len(t, stmt.Args, 1, "only the limit")
}

func TestBuildServiceLookupListQueryTabs(t *testing.T) {
	tests := []struct {
		tab  model.ServiceLookupTab
		want string
	}{
		{model.ServiceLookupActive, "(j.deleted_at IS NULL AND NOT j.in_work_pool)"},
		{"", "(j.deleted_at IS NULL AND NOT j.in_work_pool)"},
		{model.ServiceLookupWorkPool, "(j.in_work_pool AND j.deleted_at IS NULL)"},
		{model.ServiceLookupDeleted, "(j.deleted_at IS NOT NULL)"},
	}
	for _, tt := range tests {
		t.Run(string(tt.tab), func(t *testing.T) {
			stmt, err := repository.BuildServiceLookupListQuery(serviceLookupQuery(model.ServiceLookupFilter{Tab: tt.tab}, model.ServiceLookupSortCustomer, false))
			require.NoError(t, err)
			require.Contains(t, stmt.SQL, "\nWHERE "+tt.want)
		})
	}
}

func TestBuildServiceLookupListQueryKeyset(t *testing.T) {
	q := serviceLookupQuery(model.ServiceLookupFilter{}, model.ServiceLookupSortScheduledAt, false)
	q.After = &model.ServiceLookupCursor{SortValue: "2026-10-15T08:00:00.000000Z", ID: 11}

	stmt, err := repository.BuildServiceLookupListQuery(q)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "(coalesce(j.scheduled_at, '0001-01-01 00:00:00+00'::timestamptz), j.id) > (($1::text)::timestamptz, $2::bigint)")
	require.Contains(t, stmt.SQL, "THEN '0001-01-01T00:00:00.000000Z'", "an unscheduled job has a fixed sort value")
	require.Equal(t, []any{"2026-10-15T08:00:00.000000Z", int64(11), 51}, stmt.Args)
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildServiceLookupListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.ServiceLookupSortKey]string{
		model.ServiceLookupSortCustomer:     "left(lower(c.display_name), 200)",
		model.ServiceLookupSortServiceType:  "left(lower(st.name), 200)",
		model.ServiceLookupSortScheduledAt:  "coalesce(j.scheduled_at, '0001-01-01 00:00:00+00'::timestamptz)",
		model.ServiceLookupSortAddress:      "left(lower(sl.address_line1), 200)",
		model.ServiceLookupSortCity:         "left(lower(sl.city), 200)",
		model.ServiceLookupSortZip:          "left(lower(sl.postal_code), 200)",
		model.ServiceLookupSortCounty:       "left(lower(sl.county), 200)",
		model.ServiceLookupSortInvoiceValue: "j.invoice_value",
		model.ServiceLookupSortWithoutTax:   "(j.invoice_value - j.tax_amount)",
		model.ServiceLookupSortStatus:       "left(j.status::text, 200)",
		model.ServiceLookupSortAssignedTo:   "left(lower(coalesce(u.full_name, '')), 200)",
	}
	require.Len(t, wantExpr, len(model.ServiceLookupSortKeys), "every key must be covered by this test")

	for _, key := range model.ServiceLookupSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir := "ASC"
			if desc {
				dir = "DESC"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				stmt, err := repository.BuildServiceLookupListQuery(serviceLookupQuery(model.ServiceLookupFilter{}, key, desc))
				require.NoError(t, err)
				require.True(t, strings.HasSuffix(stmt.SQL, "ORDER BY "+want+" "+dir+", j.id "+dir+"\nLIMIT $1"), stmt.SQL)
			})
		}
	}
}

func TestBuildServiceLookupSummaryQuery(t *testing.T) {
	f := fullServiceLookupFilter()
	f.Tab = model.ServiceLookupWorkPool

	stmt, err := repository.BuildServiceLookupSummaryQuery(f)
	require.NoError(t, err)

	selected := "FILTER (WHERE (j.in_work_pool AND j.deleted_at IS NULL))"
	require.True(t, strings.HasPrefix(stmt.SQL, "SELECT count(*) "+selected), stmt.SQL)
	require.Contains(t, stmt.SQL, "SUM(j.invoice_value * 100) "+selected)
	require.Contains(t, stmt.SQL, "SUM((j.invoice_value - j.tax_amount) * 100) "+selected)
	for _, cond := range []string{
		"(j.deleted_at IS NULL AND NOT j.in_work_pool)", "(j.in_work_pool AND j.deleted_at IS NULL)", "(j.deleted_at IS NOT NULL)",
	} {
		require.Contains(t, stmt.SQL, "count(*) FILTER (WHERE "+cond+")", "a count for every tab")
	}
	require.NotContains(t, stmt.SQL, "\nWHERE (j.", "the tab is a selector of the aggregates, not a condition")
	require.Contains(t, stmt.SQL, "j.scheduled_at >= $1::timestamptz", "the other filters apply to every tab")
	require.Contains(t, stmt.SQL, "EXISTS (SELECT 1 FROM job_tag_links")
	require.NotContains(t, stmt.SQL, "LEFT JOIN users", "the summary needs no assignee")
	require.NotContains(t, stmt.SQL, "array_agg", "the summary needs no tag list")
	require.NotContains(t, stmt.SQL, "LIMIT $")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildServiceLookupQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		q    model.ServiceLookupQuery
	}{
		{"unknown sort key", serviceLookupQuery(model.ServiceLookupFilter{}, "name; DROP TABLE jobs", false)},
		{"empty sort key", model.ServiceLookupQuery{Limit: 5}},
		{"another report's sort key", serviceLookupQuery(model.ServiceLookupFilter{}, "amount", false)},
		{"unknown tab", serviceLookupQuery(model.ServiceLookupFilter{Tab: "archived"}, model.ServiceLookupSortCustomer, false)},
		{"no limit", model.ServiceLookupQuery{Sort: model.ServiceLookupSort{Key: model.ServiceLookupSortCustomer}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repository.BuildServiceLookupListQuery(tt.q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}

	t.Run("the summary rejects an unknown tab", func(t *testing.T) {
		_, err := repository.BuildServiceLookupSummaryQuery(model.ServiceLookupFilter{Tab: "archived"})
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestBuildServiceLookupQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE jobs; --`
	f := model.ServiceLookupFilter{Search: hostile, Statuses: []model.JobStatus{model.JobStatus(hostile)}}

	stmt, err := repository.BuildServiceLookupListQuery(serviceLookupQuery(f, model.ServiceLookupSortCustomer, false))
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(toString(a)) + "|"
	}
	require.Contains(t, flat, "drop table jobs", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}
