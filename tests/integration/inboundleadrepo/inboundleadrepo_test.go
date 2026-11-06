//go:build integration

package inboundleadrepo_test

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
	repo *repository.InboundLeadRepository
	f    testdb.InboundLeadFixture
}

// newEnv is a seeded database with the extra leads of testdb.InsertInboundLeads on top of the seeded lead 63730.
func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewInboundLeadRepository(pool), f: testdb.InsertInboundLeads(t, pool)}
}

func november() model.InboundLeadFilter {
	return model.InboundLeadFilter{CreatedFrom: utc(2026, 11, 1, 0, 0, 0, 0), CreatedBefore: utc(2026, 12, 1, 0, 0, 0, 0)}
}

func october() model.InboundLeadFilter {
	return model.InboundLeadFilter{CreatedFrom: utc(2026, 10, 1, 0, 0, 0, 0), CreatedBefore: utc(2026, 11, 1, 0, 0, 0, 0)}
}

func query(f model.InboundLeadFilter, key model.InboundLeadSortKey, desc bool, limit int) model.InboundLeadQuery {
	return model.InboundLeadQuery{Filter: f, Sort: model.InboundLeadSort{Key: key, Desc: desc}, Limit: limit}
}

func list(t *testing.T, e env, q model.InboundLeadQuery) []model.InboundLeadRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), q)
	require.NoError(t, err)
	return rows
}

func all(t *testing.T, e env, f model.InboundLeadFilter) []model.InboundLeadRow {
	t.Helper()
	return list(t, e, query(f, model.InboundLeadSortDateAdded, true, 100))
}

func ids(rows []model.InboundLeadRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func byID(rows []model.InboundLeadRow) map[int64]model.InboundLeadRow {
	out := map[int64]model.InboundLeadRow{}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

func sourceID(t *testing.T, e env, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT id FROM lead_sources WHERE name = $1`, name).Scan(&id))
	return id
}

func TestOnlyCustomersWithStatusLeadAreListedNewestFirst(t *testing.T) {
	e := newEnv(t)

	rows := all(t, e, model.InboundLeadFilter{})

	// Noah and Cold were added at one instant, so the later id comes first; Active Person is not a lead.
	require.Equal(t, []int64{e.f.Noah, e.f.Cold, e.f.Mia, e.f.QA}, ids(rows))
	require.NotContains(t, ids(rows), e.f.NotALead)
}

func TestTheSeededLeadMatchesTheScreenshot(t *testing.T) {
	e := newEnv(t)

	rows := all(t, e, october())

	got := byID(rows)[e.f.QA]
	require.Equal(t, "QA", got.CustomerName)
	require.Nil(t, got.Source, "one lead in October with no source: the screen's NA 100%")
	require.Nil(t, got.Email)
	require.Nil(t, got.Phone)
	require.Equal(t, "", got.Notes)
	require.Nil(t, got.ActiveJob)
	require.Equal(t, utc(2026, 10, 8, 0, 0, 0, 0), got.DateAdded)
}

func TestTheRangeSplitsOctoberAndNovember(t *testing.T) {
	e := newEnv(t)

	require.Equal(t, []int64{e.f.Mia, e.f.QA}, ids(all(t, e, october())))
	require.Equal(t, []int64{e.f.Noah, e.f.Cold}, ids(all(t, e, november())))
	require.Empty(t, all(t, e, model.InboundLeadFilter{CreatedFrom: utc(2030, 1, 1, 0, 0, 0, 0), CreatedBefore: utc(2030, 2, 1, 0, 0, 0, 0)}))
}

func TestTheRangeIsHalfOpenOnTheAddedInstant(t *testing.T) {
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
			_, err := e.pool.Exec(context.Background(), `UPDATE customers SET created_at = $2::timestamptz WHERE id = $1`, e.f.Cold, tt.at)
			require.NoError(t, err)

			require.Equal(t, tt.inRange, slices.Contains(ids(all(t, e, november())), e.f.Cold))
		})
	}
}

func TestRowsCarryEveryColumnOfTheScreen(t *testing.T) {
	e := newEnv(t)

	rows := byID(all(t, e, model.InboundLeadFilter{}))

	mia := rows[e.f.Mia]
	require.Equal(t, "Mia", mia.CustomerName)
	require.NotNil(t, mia.Source)
	require.Equal(t, "Google", *mia.Source)
	require.NotNil(t, mia.SourceID)
	require.Equal(t, sourceID(t, e, "Google"), *mia.SourceID)
	require.Equal(t, "mia.torres@example.com", *mia.Email)
	require.Equal(t, "(555) 010-0001", *mia.Phone, "the first phone, whatever its number says")
	require.Equal(t, "Asked for a quote", mia.Notes)
	require.Equal(t, utc(2026, 10, 20, 10, 0, 0, 0), mia.DateAdded)
	require.Nil(t, mia.ActiveJob)

	cold := rows[e.f.Cold]
	require.Equal(t, utc(2026, 11, 3, 9, 15, 30, 123456000), cold.DateAdded, "microseconds are kept")
	require.Nil(t, cold.Source)
	require.Nil(t, cold.Email)
	require.Nil(t, cold.Phone)
}

func TestNameFormatsShowTheNamePartsAndFallBackToTheDisplayName(t *testing.T) {
	e := newEnv(t)
	namesBy := func(format model.NameFormat) map[int64]string {
		q := query(model.InboundLeadFilter{}, model.InboundLeadSortDateAdded, true, 100)
		q.NameFormat = format
		out := map[int64]string{}
		for _, r := range list(t, e, q) {
			out[r.ID] = r.CustomerName
		}
		return out
	}

	require.Equal(t, map[int64]string{e.f.Noah: "Noah", e.f.Cold: "Cold Call Co", e.f.Mia: "Mia", e.f.QA: "QA"}, namesBy(model.NameFirst),
		"the first name, or the display name when there is none")
	require.Equal(t, namesBy(""), namesBy(model.NameFirst), "an unset format is the first name")
	require.Equal(t, map[int64]string{e.f.Noah: "Berg", e.f.Cold: "Cold Call Co", e.f.Mia: "Torres", e.f.QA: "customer"}, namesBy(model.NameLast))
	require.Equal(t, map[int64]string{e.f.Noah: "Noah Berg", e.f.Cold: "Cold Call Co", e.f.Mia: "Mia Torres", e.f.QA: "QA customer edited ad35e0"},
		namesBy(model.NameFull))
}

func TestFilterBySource(t *testing.T) {
	e := newEnv(t)
	google, facebook := sourceID(t, e, "Google"), sourceID(t, e, "Facebook")
	with := func(ids []int64, none bool) model.InboundLeadFilter {
		return model.InboundLeadFilter{LeadSourceIDs: ids, NoSource: none}
	}

	require.Equal(t, []int64{e.f.Mia}, ids(all(t, e, with([]int64{google}, false))))
	require.Equal(t, []int64{e.f.Noah}, ids(all(t, e, with([]int64{facebook}, false))))
	require.Equal(t, []int64{e.f.Noah, e.f.Mia}, ids(all(t, e, with([]int64{google, facebook}, false))))
	require.Equal(t, []int64{e.f.Cold, e.f.QA}, ids(all(t, e, with(nil, true))), "none: the leads without a source, the screen's NA")
	require.Equal(t, []int64{e.f.Cold, e.f.Mia, e.f.QA}, ids(all(t, e, with([]int64{google}, true))), "a source and none together")
	require.Empty(t, all(t, e, with([]int64{999999}, false)), "an unknown source has no leads")
}

func TestSearchMatchesNameEmailPhoneAndNotesAndTreatsWildcardsLiterally(t *testing.T) {
	e := newEnv(t)
	search := func(s string) []int64 {
		return ids(all(t, e, model.InboundLeadFilter{Search: s}))
	}

	require.Equal(t, []int64{e.f.Noah}, search("NOAH"), "case-insensitive, by name")
	require.Equal(t, []int64{e.f.Mia}, search("mia.torres@"), "by email")
	require.Equal(t, []int64{e.f.Noah}, search("010-0003"), "by phone")
	require.Equal(t, []int64{e.f.Mia}, search("quote"), "by notes")
	require.Equal(t, []int64{e.f.Cold}, search("cold call"), "by the display name, even when the first name is empty")
	require.Empty(t, search("%"), "a percent sign is not a wildcard")
	require.Empty(t, search("_"), "an underscore is not a wildcard")
	require.Empty(t, search("'; DROP TABLE customers; --"))
}

func TestTheActiveJobIsTheEarliestOpenScheduledOne(t *testing.T) {
	e := newEnv(t)
	noah := func() model.InboundLeadRow {
		return byID(all(t, e, model.InboundLeadFilter{}))[e.f.Noah]
	}
	var serviceType string
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT name FROM service_types ORDER BY id LIMIT 1`).Scan(&serviceType))

	t.Run("the earliest of the open jobs, ignoring cancelled, deleted, completed and unscheduled ones", func(t *testing.T) {
		got := noah().ActiveJob

		require.NotNil(t, got)
		require.Equal(t, utc(2026, 11, 12, 9, 0, 0, 0), got.ScheduledAt)
		require.Equal(t, serviceType, got.ServiceType)
	})

	t.Run("one lead is still one row however many jobs it has", func(t *testing.T) {
		require.Equal(t, 1, countOf(ids(all(t, e, model.InboundLeadFilter{})), e.f.Noah))
	})

	t.Run("when that job is deleted the next open one takes over", func(t *testing.T) {
		_, err := e.pool.Exec(context.Background(), `UPDATE jobs SET deleted_at = now() WHERE id = $1`, e.f.NoahOpenJob)
		require.NoError(t, err)

		require.Equal(t, utc(2026, 11, 20, 9, 0, 0, 0), noah().ActiveJob.ScheduledAt)
	})

	t.Run("when it is cancelled there is none", func(t *testing.T) {
		_, err := e.pool.Exec(context.Background(), `UPDATE jobs SET status = 'cancelled' WHERE id = $1`, e.f.NoahLaterJob)
		require.NoError(t, err)

		require.Nil(t, noah().ActiveJob)
	})

	t.Run("a customer who is not a lead stays out whatever their jobs", func(t *testing.T) {
		require.NotContains(t, ids(all(t, e, model.InboundLeadFilter{})), e.f.NotALead)
	})
}

func countOf(list []int64, want int64) int {
	n := 0
	for _, v := range list {
		if v == want {
			n++
		}
	}
	return n
}

func TestEverySortKeyOrdersInBothDirections(t *testing.T) {
	e := newEnv(t)

	for _, key := range model.InboundLeadSortKeys {
		asc := list(t, e, query(model.InboundLeadFilter{}, key, false, 100))
		desc := list(t, e, query(model.InboundLeadFilter{}, key, true, 100))
		require.Len(t, asc, 4, key)

		reversed := ids(desc)
		slices.Reverse(reversed)
		require.Equal(t, ids(asc), reversed, "%s: descending is exactly the reverse of ascending, ties included", key)

		for i := 1; i < len(asc); i++ {
			a, b := asc[i-1], asc[i]
			switch key {
			case model.InboundLeadSortDateAdded:
				require.False(t, a.DateAdded.After(b.DateAdded), "%s: %v before %v", key, a.ID, b.ID)
				if a.DateAdded.Equal(b.DateAdded) {
					require.Less(t, a.ID, b.ID, "ties break by id")
				}
			case model.InboundLeadSortEmail:
				require.LessOrEqual(t, strings.ToLower(deref(a.Email)), strings.ToLower(deref(b.Email)), key)
			case model.InboundLeadSortSource:
				require.LessOrEqual(t, strings.ToLower(deref(a.Source)), strings.ToLower(deref(b.Source)), key)
			case model.InboundLeadSortNotes:
				require.LessOrEqual(t, strings.ToLower(a.Notes), strings.ToLower(b.Notes), key)
			case model.InboundLeadSortPhone:
				require.LessOrEqual(t, deref(a.Phone), deref(b.Phone), key)
			}
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestKeysetPagingWalksEveryRowOnceForEveryKey(t *testing.T) {
	e := newEnv(t)
	// Noah and Cold share an instant, and Cold and QA share an empty email, phone and note, so the id tie-break matters.
	for _, key := range model.InboundLeadSortKeys {
		for _, desc := range []bool{false, true} {
			full := ids(list(t, e, query(model.InboundLeadFilter{}, key, desc, 100)))

			var walked []int64
			var after *model.InboundLeadCursor
			for pages := 0; pages < 10; pages++ {
				q := query(model.InboundLeadFilter{}, key, desc, 1)
				q.After = after
				rows := list(t, e, q)
				require.LessOrEqual(t, len(rows), 2, "limit+1 rows at most")
				if len(rows) == 0 {
					break
				}
				walked = append(walked, rows[0].ID)
				after = &model.InboundLeadCursor{SortValue: rows[0].SortValue, ID: rows[0].ID}
				if len(rows) == 1 {
					break
				}
			}

			require.Equal(t, full, walked, "%s desc=%v", key, desc)
		}
	}
}

func TestTheDateAddedCursorKeepsMicroseconds(t *testing.T) {
	e := newEnv(t)

	rows := all(t, e, november())

	require.Equal(t, "2026-11-03T09:15:30.123456Z", byID(rows)[e.f.Cold].SortValue)
}

func TestListFollowsTheContextAndRejectsBadQueries(t *testing.T) {
	e := newEnv(t)

	t.Run("a cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := e.repo.List(ctx, query(november(), model.InboundLeadSortDateAdded, true, 10))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("an invalid query", func(t *testing.T) {
		_, err := e.repo.List(context.Background(), query(november(), model.InboundLeadSortDateAdded, true, 0))
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestLeadSourcesLookup(t *testing.T) {
	e := newEnv(t)

	sources, err := e.repo.LeadSources(context.Background())

	require.NoError(t, err)
	var names []string
	for _, s := range sources {
		names = append(names, s.Name)
	}
	require.Equal(t, []string{"Facebook", "Google", "Referral"}, names, "every source, by name")
}
