package repository_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultMaterialUseQuery() model.MaterialUseQuery {
	return model.MaterialUseQuery{Sort: model.MaterialUseSort{Key: model.MaterialUseSortMaterial}, Limit: 50}
}

func TestBuildMaterialUseListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildMaterialUseListQuery(defaultMaterialUseQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM job_materials jm")
	require.Contains(t, stmt.SQL, "JOIN jobs j ON j.id = jm.job_id")
	require.Contains(t, stmt.SQL, "JOIN materials m ON m.id = jm.material_id")
	require.Contains(t, stmt.SQL, "SUM(jm.qty) AS qty, COUNT(*) AS applications")
	require.Contains(t, stmt.SQL, "GROUP BY m.id, m.name, m.epa_reg_no")
	require.Contains(t, stmt.SQL, "WHERE j.deleted_at IS NULL\n    GROUP BY", "a deleted job's materials are always left out")
	require.NotContains(t, stmt.SQL, "scheduled_at", "no range, no date condition")
	require.NotContains(t, stmt.SQL, "device", "the All tab has no device condition")
	require.Contains(t, stmt.SQL, "ORDER BY left(lower(a.name), 200) ASC, a.id ASC")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildMaterialUseListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.MaterialUseSortKey]struct{ order, cast string }{
		model.MaterialUseSortMaterial:     {"left(lower(a.name), 200)", "text"},
		model.MaterialUseSortEPA:          {"left(lower(coalesce(a.epa_reg_no, '')), 200)", "text"},
		model.MaterialUseSortQty:          {"a.qty", "numeric"},
		model.MaterialUseSortApplications: {"a.applications", "bigint"},
	}
	require.Len(t, wantExpr, len(model.MaterialUseSortKeys), "every key must be covered by this test")

	for _, key := range model.MaterialUseSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)
		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+"/"+dir, func(t *testing.T) {
				q := defaultMaterialUseQuery()
				q.Sort = model.MaterialUseSort{Key: key, Desc: desc}
				q.After = &model.MaterialUseCursor{SortValue: "5", ID: 9}

				stmt, err := repository.BuildMaterialUseListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+want.order+" "+dir+", a.id "+dir)
				require.Contains(t, stmt.SQL, "WHERE ("+want.order+", a.id) "+cmp+" (($1::text)::"+want.cast+", $2::bigint)",
					"the keyset condition runs on the grouped rows, outside the inner query")
				require.Equal(t, []any{"5", int64(9), 51}, stmt.Args)
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildMaterialUseListQueryFilters(t *testing.T) {
	f := model.MaterialUseFilter{
		ScheduledFrom:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		ScheduledBefore: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		Tab:             model.MaterialUseDevices,
		CustomerIDs:     []int64{3, 5},
		MaterialIDs:     []int64{2},
		Search:          "100%_",
	}
	q := defaultMaterialUseQuery()
	q.Filter = f
	q.After = &model.MaterialUseCursor{SortValue: "demand", ID: 1}

	stmt, err := repository.BuildMaterialUseListQuery(q)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "j.scheduled_at >= $1::timestamptz")
	require.Contains(t, stmt.SQL, "j.scheduled_at < $2::timestamptz")
	require.Contains(t, stmt.SQL, "jm.device IS NOT NULL")
	require.Contains(t, stmt.SQL, "j.customer_id = ANY($3::bigint[])")
	require.Contains(t, stmt.SQL, "jm.material_id = ANY($4::bigint[])")
	require.Contains(t, stmt.SQL, `(m.name ILIKE $5 ESCAPE '\' OR m.epa_reg_no ILIKE $5 ESCAPE '\')`)
	require.Equal(t, []any{
		f.ScheduledFrom, f.ScheduledBefore, []int64{3, 5}, []int64{2}, `%100\%\_%`,
		"demand", int64(1), 51,
	}, stmt.Args, "the keyset arguments continue the numbering after the filter's")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildMaterialUseListQueryTabs(t *testing.T) {
	tests := []struct {
		tab         model.MaterialUseTab
		want        string
		wantAbsent  string
		description string
	}{
		{model.MaterialUseAll, "", "jm.device", "no device condition"},
		{"", "", "jm.device", "an unset tab is All"},
		{model.MaterialUseJobs, "jm.device IS NULL", "jm.device IS NOT NULL", "rows without a device"},
		{model.MaterialUseDevices, "jm.device IS NOT NULL", "jm.device IS NULL", "rows with a device"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			q := defaultMaterialUseQuery()
			q.Filter.Tab = tt.tab

			stmt, err := repository.BuildMaterialUseListQuery(q)
			require.NoError(t, err)

			if tt.want != "" {
				require.Contains(t, stmt.SQL, tt.want)
			}
			require.NotContains(t, stmt.SQL, tt.wantAbsent)
		})
	}
}

func TestBuildMaterialUseListQueryRejectsBadInput(t *testing.T) {
	t.Run("limit below one", func(t *testing.T) {
		q := defaultMaterialUseQuery()
		q.Limit = 0
		_, err := repository.BuildMaterialUseListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
	t.Run("unknown sort key", func(t *testing.T) {
		q := defaultMaterialUseQuery()
		q.Sort.Key = "unit; DROP TABLE jobs"
		_, err := repository.BuildMaterialUseListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
	t.Run("unknown tab", func(t *testing.T) {
		q := defaultMaterialUseQuery()
		q.Filter.Tab = "all; DROP TABLE jobs"
		_, err := repository.BuildMaterialUseListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestBuildMaterialUseListQueryNeverEmbedsCallerText(t *testing.T) {
	q := defaultMaterialUseQuery()
	q.Filter.Search = "'; DROP TABLE materials; --"
	q.After = &model.MaterialUseCursor{SortValue: "x'; DROP TABLE materials; --", ID: 1}

	stmt, err := repository.BuildMaterialUseListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP TABLE")
}
