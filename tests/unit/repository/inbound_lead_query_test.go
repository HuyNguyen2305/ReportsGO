package repository_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultInboundLeadQuery() model.InboundLeadQuery {
	return model.InboundLeadQuery{
		Sort:  model.InboundLeadSort{Key: model.InboundLeadSortDateAdded, Desc: true},
		Limit: 50,
	}
}

func TestBuildInboundLeadListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildInboundLeadListQuery(defaultInboundLeadQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM customers c")
	require.Contains(t, stmt.SQL, "LEFT JOIN lead_sources ls ON ls.id = c.lead_source_id")
	require.Contains(t, stmt.SQL, "SELECT p.number FROM customer_phones p WHERE p.customer_id = c.id ORDER BY p.id LIMIT 1",
		"the phone is the customer's first one")
	require.Contains(t, stmt.SQL, "j.deleted_at IS NULL AND j.status IN ('unconfirmed', 'confirmed')",
		"the active job is open and not deleted")
	require.Contains(t, stmt.SQL, "AND j.scheduled_at IS NOT NULL")
	require.Contains(t, stmt.SQL, "ORDER BY j.scheduled_at, j.id LIMIT 1", "the earliest one")
	require.Contains(t, stmt.SQL, "COALESCE(NULLIF(c.first_name, ''), c.display_name)", "an unset format shows the first name")
	require.Contains(t, stmt.SQL, "WHERE c.status = 'lead'\nORDER BY c.created_at DESC, c.id DESC", "only leads, always")
	require.NotContains(t, stmt.SQL, "AND c.lead_source_id", "an empty filter adds no source condition")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildInboundLeadListQueryNameFormats(t *testing.T) {
	tests := []struct {
		format model.NameFormat
		want   string
	}{
		{model.NameFirst, "COALESCE(NULLIF(c.first_name, ''), c.display_name)"},
		{model.NameLast, "COALESCE(NULLIF(c.last_name, ''), c.display_name)"},
		{model.NameFull, "SELECT c.id, c.display_name, ls.id"},
		{"", "COALESCE(NULLIF(c.first_name, ''), c.display_name)"},
	}
	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			q := defaultInboundLeadQuery()
			q.NameFormat = tt.format

			stmt, err := repository.BuildInboundLeadListQuery(q)

			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
		})
	}
}

func TestBuildInboundLeadListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.InboundLeadSortKey]struct{ order, cast string }{
		model.InboundLeadSortCustomer:  {"left(lower(c.display_name), 200)", "text"},
		model.InboundLeadSortSource:    {"left(lower(coalesce(ls.name, '')), 200)", "text"},
		model.InboundLeadSortEmail:     {"left(lower(coalesce(c.email, '')), 200)", "text"},
		model.InboundLeadSortPhone:     {"left(coalesce(ph.number, ''), 200)", "text"},
		model.InboundLeadSortNotes:     {"left(lower(c.top_note), 200)", "text"},
		model.InboundLeadSortDateAdded: {"c.created_at", "timestamptz"},
	}
	require.Len(t, wantExpr, len(model.InboundLeadSortKeys), "every key must be covered by this test")

	for _, key := range model.InboundLeadSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)
		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+"/"+dir, func(t *testing.T) {
				q := defaultInboundLeadQuery()
				q.Sort = model.InboundLeadSort{Key: key, Desc: desc}
				q.After = &model.InboundLeadCursor{SortValue: "x", ID: 9}

				stmt, err := repository.BuildInboundLeadListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+want.order+" "+dir+", c.id "+dir)
				require.Contains(t, stmt.SQL, "AND ("+want.order+", c.id) "+cmp+" (($1::text)::"+want.cast+", $2::bigint)")
				require.Equal(t, []any{"x", int64(9), 51}, stmt.Args)
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildInboundLeadListQueryDateAddedCursorIsFixedFormatUTC(t *testing.T) {
	stmt, err := repository.BuildInboundLeadListQuery(defaultInboundLeadQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, `to_char(c.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`)
}

func TestBuildInboundLeadListQueryFilters(t *testing.T) {
	f := model.InboundLeadFilter{
		CreatedFrom:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		CreatedBefore: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		LeadSourceIDs: []int64{2, 3},
		Search:        "100%_",
	}
	q := defaultInboundLeadQuery()
	q.Filter = f
	q.After = &model.InboundLeadCursor{SortValue: "2026-11-03T12:00:00.000000Z", ID: 1}

	stmt, err := repository.BuildInboundLeadListQuery(q)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "c.created_at >= $1::timestamptz")
	require.Contains(t, stmt.SQL, "c.created_at < $2::timestamptz")
	require.Contains(t, stmt.SQL, "c.lead_source_id = ANY($3::bigint[])")
	require.NotContains(t, stmt.SQL, "lead_source_id IS NULL")
	require.Contains(t, stmt.SQL, `(c.display_name ILIKE $4 ESCAPE '\' OR c.email ILIKE $4 ESCAPE '\' OR ph.number ILIKE $4 ESCAPE '\' OR c.top_note ILIKE $4 ESCAPE '\')`)
	require.Equal(t, []any{
		f.CreatedFrom, f.CreatedBefore, []int64{2, 3}, `%100\%\_%`,
		"2026-11-03T12:00:00.000000Z", int64(1), 51,
	}, stmt.Args, "the keyset arguments continue the numbering after the filter's")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildInboundLeadListQuerySourceFilterForms(t *testing.T) {
	tests := []struct {
		name     string
		ids      []int64
		none     bool
		want     string
		wantArgs []any
	}{
		{"ids only", []int64{4}, false, "c.lead_source_id = ANY($1::bigint[])", []any{[]int64{4}, 51}},
		{"none only", nil, true, "c.lead_source_id IS NULL", []any{51}},
		{"ids and none", []int64{4}, true, "(c.lead_source_id = ANY($1::bigint[]) OR c.lead_source_id IS NULL)", []any{[]int64{4}, 51}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultInboundLeadQuery()
			q.Filter = model.InboundLeadFilter{LeadSourceIDs: tt.ids, NoSource: tt.none}

			stmt, err := repository.BuildInboundLeadListQuery(q)

			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
			require.Equal(t, tt.wantArgs, stmt.Args)
			requirePlaceholdersMatchArgs(t, stmt)
		})
	}
}

func TestBuildInboundLeadListQueryRejectsBadInput(t *testing.T) {
	t.Run("limit below one", func(t *testing.T) {
		q := defaultInboundLeadQuery()
		q.Limit = 0
		_, err := repository.BuildInboundLeadListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
	t.Run("unknown sort key", func(t *testing.T) {
		q := defaultInboundLeadQuery()
		q.Sort.Key = "email; DROP TABLE customers"
		_, err := repository.BuildInboundLeadListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
	t.Run("unknown name format", func(t *testing.T) {
		q := defaultInboundLeadQuery()
		q.NameFormat = "first_name; DROP TABLE customers"
		_, err := repository.BuildInboundLeadListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestBuildInboundLeadListQueryNeverEmbedsCallerText(t *testing.T) {
	q := defaultInboundLeadQuery()
	q.Filter.Search = "'; DROP TABLE customers; --"
	q.After = &model.InboundLeadCursor{SortValue: "x'; DROP TABLE customers; --", ID: 1}

	stmt, err := repository.BuildInboundLeadListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP TABLE")
}
