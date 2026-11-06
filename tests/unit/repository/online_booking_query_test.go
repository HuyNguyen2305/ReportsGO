package repository_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultOnlineBookingQuery() model.OnlineBookingQuery {
	return model.OnlineBookingQuery{
		Sort:  model.OnlineBookingSort{Key: model.OnlineBookingSortScheduledAt, Desc: true},
		Limit: 50,
	}
}

func TestBuildOnlineBookingListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildOnlineBookingListQuery(defaultOnlineBookingQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM jobs j")
	require.Contains(t, stmt.SQL, "JOIN customers c ON c.id = j.customer_id")
	require.Contains(t, stmt.SQL, "JOIN service_types st ON st.id = j.service_type_id")
	require.Contains(t, stmt.SQL, "JOIN service_locations sl ON sl.id = j.location_id", "the address is the job's own location")
	require.Contains(t, stmt.SQL, "LEFT JOIN lead_sources ls ON ls.id = c.lead_source_id", "the source is the customer's")
	require.NotContains(t, stmt.SQL, "j.lead_source_id", "not the job's own source")
	require.Contains(t, stmt.SQL, "SELECT p.number FROM customer_phones p WHERE p.customer_id = c.id ORDER BY p.id LIMIT 1",
		"the phone is the customer's first one")
	require.Contains(t, stmt.SQL, "COALESCE(NULLIF(c.first_name, ''), c.display_name)", "an unset format shows the first name")
	require.Contains(t, stmt.SQL, "WHERE c.booked_online\n  AND j.deleted_at IS NULL\n  AND j.scheduled_at IS NOT NULL\nORDER BY",
		"only flagged customers, never a deleted or unscheduled job, whatever the filter")
	require.Contains(t, stmt.SQL, "ORDER BY j.scheduled_at DESC, j.id DESC")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildOnlineBookingListQueryNameFormats(t *testing.T) {
	tests := []struct {
		format model.NameFormat
		want   string
	}{
		{model.NameFirst, "COALESCE(NULLIF(c.first_name, ''), c.display_name)"},
		{model.NameLast, "COALESCE(NULLIF(c.last_name, ''), c.display_name)"},
		{model.NameFull, "SELECT j.id, c.id, c.display_name, j.scheduled_at"},
		{"", "COALESCE(NULLIF(c.first_name, ''), c.display_name)"},
	}
	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			q := defaultOnlineBookingQuery()
			q.NameFormat = tt.format

			stmt, err := repository.BuildOnlineBookingListQuery(q)

			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
		})
	}
}

func TestBuildOnlineBookingListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.OnlineBookingSortKey]struct{ order, cast string }{
		model.OnlineBookingSortCustomer:    {"left(lower(c.display_name), 200)", "text"},
		model.OnlineBookingSortScheduledAt: {"j.scheduled_at", "timestamptz"},
		model.OnlineBookingSortService:     {"left(lower(st.name), 200)", "text"},
		model.OnlineBookingSortEmail:       {"left(lower(coalesce(c.email, '')), 200)", "text"},
		model.OnlineBookingSortPhone:       {"left(coalesce(ph.number, ''), 200)", "text"},
		model.OnlineBookingSortAddress:     {"left(lower(sl.address_line1), 200)", "text"},
		model.OnlineBookingSortNotes:       {"left(lower(c.top_note), 200)", "text"},
		model.OnlineBookingSortStatus:      {"left(j.status::text, 200)", "text"},
	}
	require.Len(t, wantExpr, len(model.OnlineBookingSortKeys), "every key must be covered by this test")

	for _, key := range model.OnlineBookingSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)
		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+"/"+dir, func(t *testing.T) {
				q := defaultOnlineBookingQuery()
				q.Sort = model.OnlineBookingSort{Key: key, Desc: desc}
				q.After = &model.OnlineBookingCursor{SortValue: "x", ID: 9}

				stmt, err := repository.BuildOnlineBookingListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+want.order+" "+dir+", j.id "+dir)
				require.Contains(t, stmt.SQL, "AND ("+want.order+", j.id) "+cmp+" (($1::text)::"+want.cast+", $2::bigint)")
				require.Equal(t, []any{"x", int64(9), 51}, stmt.Args)
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildOnlineBookingListQueryScheduledCursorIsFixedFormatUTC(t *testing.T) {
	stmt, err := repository.BuildOnlineBookingListQuery(defaultOnlineBookingQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, `to_char(j.scheduled_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`)
}

func TestBuildOnlineBookingListQueryFilters(t *testing.T) {
	f := model.OnlineBookingFilter{
		ScheduledFrom:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		ScheduledBefore: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		LeadSourceIDs:   []int64{2, 3},
		Search:          "100%_",
	}
	q := defaultOnlineBookingQuery()
	q.Filter = f
	q.After = &model.OnlineBookingCursor{SortValue: "2026-11-03T12:00:00.000000Z", ID: 1}

	stmt, err := repository.BuildOnlineBookingListQuery(q)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "j.scheduled_at >= $1::timestamptz")
	require.Contains(t, stmt.SQL, "j.scheduled_at < $2::timestamptz")
	require.Contains(t, stmt.SQL, "c.lead_source_id = ANY($3::bigint[])")
	require.NotContains(t, stmt.SQL, "lead_source_id IS NULL")
	require.Contains(t, stmt.SQL, `(c.display_name ILIKE $4 ESCAPE '\' OR c.email ILIKE $4 ESCAPE '\' OR ph.number ILIKE $4 ESCAPE '\' OR sl.address_line1 ILIKE $4 ESCAPE '\' OR sl.city ILIKE $4 ESCAPE '\' OR st.name ILIKE $4 ESCAPE '\' OR c.top_note ILIKE $4 ESCAPE '\')`)
	require.Equal(t, []any{
		f.ScheduledFrom, f.ScheduledBefore, []int64{2, 3}, `%100\%\_%`,
		"2026-11-03T12:00:00.000000Z", int64(1), 51,
	}, stmt.Args, "the keyset arguments continue the numbering after the filter's")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildOnlineBookingListQuerySourceFilterForms(t *testing.T) {
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
			q := defaultOnlineBookingQuery()
			q.Filter = model.OnlineBookingFilter{LeadSourceIDs: tt.ids, NoSource: tt.none}

			stmt, err := repository.BuildOnlineBookingListQuery(q)

			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
			require.Equal(t, tt.wantArgs, stmt.Args)
			requirePlaceholdersMatchArgs(t, stmt)
		})
	}
}

func TestBuildOnlineBookingListQueryRejectsBadInput(t *testing.T) {
	t.Run("limit below one", func(t *testing.T) {
		q := defaultOnlineBookingQuery()
		q.Limit = 0
		_, err := repository.BuildOnlineBookingListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
	t.Run("unknown sort key", func(t *testing.T) {
		q := defaultOnlineBookingQuery()
		q.Sort.Key = "email; DROP TABLE jobs"
		_, err := repository.BuildOnlineBookingListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
	t.Run("unknown name format", func(t *testing.T) {
		q := defaultOnlineBookingQuery()
		q.NameFormat = "first_name; DROP TABLE customers"
		_, err := repository.BuildOnlineBookingListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestBuildOnlineBookingListQueryNeverEmbedsCallerText(t *testing.T) {
	q := defaultOnlineBookingQuery()
	q.Filter.Search = "'; DROP TABLE jobs; --"
	q.After = &model.OnlineBookingCursor{SortValue: "x'; DROP TABLE jobs; --", ID: 1}

	stmt, err := repository.BuildOnlineBookingListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP TABLE")
}
