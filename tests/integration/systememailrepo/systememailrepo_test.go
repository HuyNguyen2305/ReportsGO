//go:build integration

package systememailrepo_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

func utc(y int, m time.Month, d, h, min, s, ns int) time.Time {
	return time.Date(y, m, d, h, min, s, ns, time.UTC)
}

type env struct {
	pool *pgxpool.Pool
	repo *repository.SystemEmailRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewSystemEmailRepository(pool)}
}

// november is the whole month in UTC, the range of the screen.
func november() model.SystemEmailFilter {
	return model.SystemEmailFilter{SentFrom: utc(2026, 11, 1, 0, 0, 0, 0), SentBefore: utc(2026, 12, 1, 0, 0, 0, 0)}
}

func october() model.SystemEmailFilter {
	return model.SystemEmailFilter{SentFrom: utc(2026, 10, 1, 0, 0, 0, 0), SentBefore: utc(2026, 11, 1, 0, 0, 0, 0)}
}

func query(f model.SystemEmailFilter, key model.SystemEmailSortKey, desc bool, limit int) model.SystemEmailQuery {
	return model.SystemEmailQuery{Filter: f, Sort: model.SystemEmailSort{Key: key, Desc: desc}, Limit: limit}
}

func list(t *testing.T, e env, q model.SystemEmailQuery) []model.SystemEmailRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), q)
	require.NoError(t, err)
	return rows
}

func all(t *testing.T, e env, f model.SystemEmailFilter) []model.SystemEmailRow {
	t.Helper()
	return list(t, e, query(f, model.SystemEmailSortTimeStamp, true, 100))
}

func ids(rows []model.SystemEmailRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// The seeded emails, by id: 1 Sep 30 (end of day), 2 Oct 3, 3 and 4 Oct 15 at the same instant, 5 Oct 20, 6 Oct 31
// (end of day), 7 Nov 2 01:30, 8 Nov 2 03:42, 9 Nov 3, 10 Nov 4.

func TestSeededNovemberHasFourEmailsNewestFirst(t *testing.T) {
	rows := all(t, newEnv(t), november())

	require.Equal(t, []int64{10, 9, 8, 7}, ids(rows))
	require.Equal(t, utc(2026, 11, 4, 8, 0, 0, 0), rows[0].SentAt)
	require.Equal(t, model.EmailEstimate, rows[0].Type)
	require.Equal(t, model.EmailConfirmed, rows[0].Status)
	require.Equal(t, "billing.alt@example.com", rows[1].Email, "an email keeps the address it was sent to")
}

func TestSeededOctoberAndTheEdges(t *testing.T) {
	e := newEnv(t)

	require.Equal(t, []int64{6, 5, 4, 3, 2}, ids(all(t, e, october())),
		"the one just before October and the first of November are outside, the last microsecond of October is inside")
	require.Equal(t, []int64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}, ids(all(t, e, model.SystemEmailFilter{})), "an open range lists everything")
}

func TestTheRangeIsHalfOpenOnTheSentInstant(t *testing.T) {
	e := newEnv(t)
	for _, tt := range []struct {
		name    string
		at      string
		inRange bool
	}{
		{"the first instant of the range", "2026-11-01 00:00:00+00", true},
		{"the last microsecond of the range", "2026-11-30 23:59:59.999999+00", true},
		{"the first instant after the range", "2026-12-01 00:00:00+00", false},
		{"the last instant before the range", "2026-10-31 23:59:59.999999+00", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.pool.Exec(context.Background(), `UPDATE system_emails SET sent_at = $1::timestamptz WHERE id = 10`, tt.at)
			require.NoError(t, err)

			require.Equal(t, tt.inRange, slices.Contains(ids(all(t, e, november())), 10))
		})
	}
}

func TestNameFormatsShowTheNamePartsAndFallBackToTheDisplayName(t *testing.T) {
	e := newEnv(t)
	namesBy := func(format model.NameFormat) map[int64]string {
		q := query(november(), model.SystemEmailSortTimeStamp, true, 100)
		q.NameFormat = format
		out := map[int64]string{}
		for _, r := range list(t, e, q) {
			out[r.ID] = r.CustomerName
		}
		return out
	}

	// 10: no first name (PerfTest), 9: Customer / 1, 8: # / 1, 7: # / D
	require.Equal(t, map[int64]string{10: "PerfTest", 9: "Customer", 8: "#", 7: "#"}, namesBy(model.NameFirst),
		"the first name, or the display name when there is none")
	require.Equal(t, namesBy(""), namesBy(model.NameFirst), "an unset format is the first name")
	require.Equal(t, map[int64]string{10: "PerfTest", 9: "1", 8: "1", 7: "D"}, namesBy(model.NameLast))
	require.Equal(t, map[int64]string{10: "PerfTest", 9: "Customer 1", 8: "#1", 7: "# D"}, namesBy(model.NameFull))
}

func TestFilterByStatus(t *testing.T) {
	e := newEnv(t)
	with := func(f model.SystemEmailFilter, s ...model.EmailStatus) model.SystemEmailFilter {
		f.Statuses = s
		return f
	}

	require.Equal(t, []int64{10, 9, 7}, ids(all(t, e, with(november(), model.EmailConfirmed))))
	require.Equal(t, []int64{8}, ids(all(t, e, with(november(), model.EmailUnconfirmed))))
	require.Empty(t, all(t, e, with(november(), model.EmailCancelled)), "nothing was cancelled in November")
	require.Equal(t, []int64{10, 9, 8, 7}, ids(all(t, e, with(november(), model.EmailConfirmed, model.EmailUnconfirmed))))
	require.Equal(t, []int64{6, 1}, ids(all(t, e, with(model.SystemEmailFilter{}, model.EmailCancelled))))
}

func TestFilterByClient(t *testing.T) {
	e := newEnv(t)
	var customerID int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT id FROM customers WHERE account_number = '63733'`).Scan(&customerID))

	f := model.SystemEmailFilter{CustomerIDs: []int64{customerID}}
	require.Equal(t, []int64{9, 5}, ids(all(t, e, f)))

	f.CustomerIDs = []int64{customerID, -1}
	require.Equal(t, []int64{9, 5}, ids(all(t, e, f)), "an unknown client adds nothing")

	f.CustomerIDs = []int64{-1}
	require.Empty(t, all(t, e, f))
}

func TestSearchMatchesNameAccountNumberAndAddressAndTreatsWildcardsLiterally(t *testing.T) {
	e := newEnv(t)
	search := func(s string) []int64 {
		return ids(all(t, e, model.SystemEmailFilter{Search: s}))
	}

	require.Equal(t, []int64{10}, search("PERFtest"), "case-insensitive, by customer name")
	require.Equal(t, []int64{4, 3, 2}, search("63714"), "by account number")
	require.Equal(t, []int64{9}, search("billing.alt"), "by the address the email was sent to")
	require.Equal(t, []int64{8, 4, 3, 2}, search("d@cm.co"), "the customer own address, on every email sent to it")
	require.Empty(t, search("%"), "a percent sign is not a wildcard")
	require.Empty(t, search("_"), "an underscore is not a wildcard")
	require.Empty(t, search("'; DROP TABLE system_emails; --"))
}

func TestEverySortKeyOrdersInBothDirections(t *testing.T) {
	e := newEnv(t)

	for _, key := range model.SystemEmailSortKeys {
		asc := list(t, e, query(model.SystemEmailFilter{}, key, false, 100))
		desc := list(t, e, query(model.SystemEmailFilter{}, key, true, 100))
		require.Len(t, asc, 10, key)

		reversed := ids(desc)
		slices.Reverse(reversed)
		require.Equal(t, ids(asc), reversed, "%s: descending is exactly the reverse of ascending, ties included", key)

		for i := 1; i < len(asc); i++ {
			a, b := asc[i-1], asc[i]
			switch key {
			case model.SystemEmailSortTimeStamp:
				require.False(t, a.SentAt.After(b.SentAt), "%s: %v before %v", key, a, b)
				if a.SentAt.Equal(b.SentAt) {
					require.Less(t, a.ID, b.ID, "ties break by id")
				}
			case model.SystemEmailSortEmail:
				require.LessOrEqual(t, strings.ToLower(a.Email), strings.ToLower(b.Email), key)
			case model.SystemEmailSortType:
				require.LessOrEqual(t, string(a.Type), string(b.Type), key)
			case model.SystemEmailSortStatus:
				require.LessOrEqual(t, string(a.Status), string(b.Status), key)
			}
		}
	}
}

func TestKeysetPagingWalksEveryRowOnceForEveryKey(t *testing.T) {
	e := newEnv(t)
	// emails 3 and 4 share an instant, an email address and a type, so the id tie-break matters
	for _, key := range model.SystemEmailSortKeys {
		for _, desc := range []bool{false, true} {
			full := ids(list(t, e, query(model.SystemEmailFilter{}, key, desc, 100)))

			var walked []int64
			var after *model.SystemEmailCursor
			for pages := 0; pages < 20; pages++ {
				q := query(model.SystemEmailFilter{}, key, desc, 3)
				q.After = after
				rows := list(t, e, q)
				require.LessOrEqual(t, len(rows), 4, "limit+1 rows at most")
				page := rows
				if len(page) > 3 {
					page = page[:3]
				}
				for _, r := range page {
					walked = append(walked, r.ID)
				}
				if len(rows) <= 3 {
					break
				}
				last := page[len(page)-1]
				after = &model.SystemEmailCursor{SortValue: last.SortValue, ID: last.ID}
			}

			require.Equal(t, full, walked, "%s desc=%v", key, desc)
		}
	}
}

func TestTheTimeStampCursorKeepsMicroseconds(t *testing.T) {
	e := newEnv(t)

	rows := list(t, e, query(october(), model.SystemEmailSortTimeStamp, true, 100))

	var got string
	for _, r := range rows {
		if r.ID == 3 {
			got = r.SortValue
		}
	}
	require.Equal(t, "2026-10-15T14:30:15.123456Z", got)
}

func TestNothingToShowIsAnEmptyList(t *testing.T) {
	e := newEnv(t)
	f := model.SystemEmailFilter{SentFrom: utc(2030, 1, 1, 0, 0, 0, 0), SentBefore: utc(2030, 2, 1, 0, 0, 0, 0)}

	rows := list(t, e, query(f, model.SystemEmailSortTimeStamp, true, 10))

	require.NotNil(t, rows)
	require.Empty(t, rows)
}

func TestListFollowsTheContextAndRejectsBadQueries(t *testing.T) {
	e := newEnv(t)

	t.Run("a cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := e.repo.List(ctx, query(november(), model.SystemEmailSortTimeStamp, true, 10))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("an invalid query", func(t *testing.T) {
		_, err := e.repo.List(context.Background(), query(november(), model.SystemEmailSortTimeStamp, true, 0))
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestClientsLookup(t *testing.T) {
	e := newEnv(t)

	clients, err := e.repo.Clients(context.Background())

	require.NoError(t, err)
	require.Len(t, clients, 20, "every seeded customer")
	for _, c := range clients {
		require.NotEmpty(t, c.Name)
	}
}
