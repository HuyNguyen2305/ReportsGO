package repository_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultSystemEmailQuery() model.SystemEmailQuery {
	return model.SystemEmailQuery{
		Sort:  model.SystemEmailSort{Key: model.SystemEmailSortTimeStamp, Desc: true},
		Limit: 50,
	}
}

func TestBuildSystemEmailListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildSystemEmailListQuery(defaultSystemEmailQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM system_emails e")
	require.Contains(t, stmt.SQL, "JOIN customers c ON c.id = e.customer_id")
	require.Contains(t, stmt.SQL, "COALESCE(NULLIF(c.first_name, ''), c.display_name)", "an unset format shows the first name")
	require.Contains(t, stmt.SQL, "e.email_type::text, e.status::text, e.sent_at")
	require.Contains(t, stmt.SQL, "ORDER BY e.sent_at DESC, e.id DESC")
	require.NotContains(t, stmt.SQL, "WHERE", "an empty filter adds no condition")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildSystemEmailListQueryNameFormats(t *testing.T) {
	tests := []struct {
		format model.NameFormat
		want   string
	}{
		{model.NameFirst, "COALESCE(NULLIF(c.first_name, ''), c.display_name)"},
		{model.NameLast, "COALESCE(NULLIF(c.last_name, ''), c.display_name)"},
		{model.NameFull, "c.display_name, e.email"},
		{"", "COALESCE(NULLIF(c.first_name, ''), c.display_name)"},
	}
	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			q := defaultSystemEmailQuery()
			q.NameFormat = tt.format

			stmt, err := repository.BuildSystemEmailListQuery(q)

			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
		})
	}
}

func TestBuildSystemEmailListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.SystemEmailSortKey]struct{ order, cast string }{
		model.SystemEmailSortTimeStamp: {"e.sent_at", "timestamptz"},
		model.SystemEmailSortCustomer:  {"left(lower(c.display_name), 200)", "text"},
		model.SystemEmailSortEmail:     {"left(lower(e.email), 200)", "text"},
		model.SystemEmailSortType:      {"left(e.email_type::text, 200)", "text"},
		model.SystemEmailSortStatus:    {"left(e.status::text, 200)", "text"},
	}
	require.Len(t, wantExpr, len(model.SystemEmailSortKeys), "every key must be covered by this test")

	for _, key := range model.SystemEmailSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)
		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+"/"+dir, func(t *testing.T) {
				q := defaultSystemEmailQuery()
				q.Sort = model.SystemEmailSort{Key: key, Desc: desc}
				q.After = &model.SystemEmailCursor{SortValue: "x", ID: 9}

				stmt, err := repository.BuildSystemEmailListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+want.order+" "+dir+", e.id "+dir)
				require.Contains(t, stmt.SQL, "WHERE ("+want.order+", e.id) "+cmp+" (($1::text)::"+want.cast+", $2::bigint)")
				require.Equal(t, []any{"x", int64(9), 51}, stmt.Args)
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildSystemEmailListQueryTimeStampCursorIsFixedFormatUTC(t *testing.T) {
	stmt, err := repository.BuildSystemEmailListQuery(defaultSystemEmailQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, `to_char(e.sent_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`)
}

func TestBuildSystemEmailListQueryFilters(t *testing.T) {
	f := model.SystemEmailFilter{
		SentFrom:    time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		SentBefore:  time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		Statuses:    []model.EmailStatus{model.EmailConfirmed, model.EmailCancelled},
		CustomerIDs: []int64{3, 5},
		Search:      "100%_",
	}
	q := defaultSystemEmailQuery()
	q.Filter = f
	q.After = &model.SystemEmailCursor{SortValue: "2026-11-03T12:00:00.000000Z", ID: 1}

	stmt, err := repository.BuildSystemEmailListQuery(q)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "e.sent_at >= $1::timestamptz")
	require.Contains(t, stmt.SQL, "e.sent_at < $2::timestamptz")
	require.Contains(t, stmt.SQL, "e.status::text = ANY($3::text[])")
	require.Contains(t, stmt.SQL, "e.customer_id = ANY($4::bigint[])")
	require.Contains(t, stmt.SQL, `(c.display_name ILIKE $5 ESCAPE '\' OR c.account_number ILIKE $5 ESCAPE '\' OR e.email ILIKE $5 ESCAPE '\')`)
	require.Equal(t, []any{
		f.SentFrom, f.SentBefore, []string{"confirmed", "cancelled"}, []int64{3, 5}, `%100\%\_%`,
		"2026-11-03T12:00:00.000000Z", int64(1), 51,
	}, stmt.Args, "the keyset arguments continue the numbering after the filter's")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildSystemEmailListQueryRejectsBadInput(t *testing.T) {
	t.Run("limit below one", func(t *testing.T) {
		q := defaultSystemEmailQuery()
		q.Limit = 0
		_, err := repository.BuildSystemEmailListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
	t.Run("unknown sort key", func(t *testing.T) {
		q := defaultSystemEmailQuery()
		q.Sort.Key = "email; DROP TABLE system_emails"
		_, err := repository.BuildSystemEmailListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
	t.Run("unknown name format", func(t *testing.T) {
		q := defaultSystemEmailQuery()
		q.NameFormat = "first_name; DROP TABLE customers"
		_, err := repository.BuildSystemEmailListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestBuildSystemEmailListQueryNeverEmbedsCallerText(t *testing.T) {
	q := defaultSystemEmailQuery()
	q.Filter.Search = "'; DROP TABLE system_emails; --"
	q.After = &model.SystemEmailCursor{SortValue: "x'; DROP TABLE system_emails; --", ID: 1}

	stmt, err := repository.BuildSystemEmailListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP TABLE")
}
