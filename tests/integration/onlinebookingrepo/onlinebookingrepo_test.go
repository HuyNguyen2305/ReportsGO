//go:build integration

package onlinebookingrepo_test

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
	repo *repository.OnlineBookingRepository
	f    testdb.OnlineBookingFixture
}

// newEnv is a seeded database (four customers flagged online) with the extra customers and jobs of
// testdb.InsertOnlineBookings on top.
func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewOnlineBookingRepository(pool), f: testdb.InsertOnlineBookings(t, pool)}
}

func november() model.OnlineBookingFilter {
	return model.OnlineBookingFilter{ScheduledFrom: utc(2026, 11, 1, 0, 0, 0, 0), ScheduledBefore: utc(2026, 12, 1, 0, 0, 0, 0)}
}

func query(f model.OnlineBookingFilter, key model.OnlineBookingSortKey, desc bool, limit int) model.OnlineBookingQuery {
	return model.OnlineBookingQuery{Filter: f, Sort: model.OnlineBookingSort{Key: key, Desc: desc}, Limit: limit}
}

func list(t *testing.T, e env, q model.OnlineBookingQuery) []model.OnlineBookingRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), q)
	require.NoError(t, err)
	return rows
}

func all(t *testing.T, e env, f model.OnlineBookingFilter) []model.OnlineBookingRow {
	t.Helper()
	return list(t, e, query(f, model.OnlineBookingSortScheduledAt, true, 200))
}

func ids(rows []model.OnlineBookingRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func customers(rows []model.OnlineBookingRow) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, r := range rows {
		if !seen[r.CustomerID] {
			seen[r.CustomerID] = true
			out = append(out, r.CustomerID)
		}
	}
	slices.Sort(out)
	return out
}

func byID(rows []model.OnlineBookingRow) map[int64]model.OnlineBookingRow {
	out := map[int64]model.OnlineBookingRow{}
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

// seededJobs counts, by an independent query, the not deleted scheduled jobs of the customers the seed flags online.
func seededJobs(t *testing.T, e env, from, before string) int {
	t.Helper()
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM jobs j JOIN customers c ON c.id = j.customer_id
		WHERE c.account_number IN ('63714', '63689', '63716', '63749') AND j.deleted_at IS NULL AND j.scheduled_at IS NOT NULL
		  AND j.scheduled_at >= $1::timestamptz AND j.scheduled_at < $2::timestamptz`, from, before).Scan(&n))
	return n
}

func TestOnlyJobsOfOnlineCustomersThatAreScheduledAndNotDeletedAreListed(t *testing.T) {
	e := newEnv(t)

	rows := all(t, e, model.OnlineBookingFilter{})

	got := ids(rows)
	for _, want := range []int64{e.f.TieFirst, e.f.TieSecond, e.f.Cancelled, e.f.EdgeLast, e.f.EdgeNext, e.f.NinaJob} {
		require.Contains(t, got, want)
	}
	require.NotContains(t, got, e.f.Deleted, "a deleted job is never a booking")
	require.NotContains(t, got, e.f.Unscheduled, "a job with no appointment has no Date and Time")
	require.NotContains(t, got, e.f.OfflineJob, "a customer who did not come from an online booking")

	flagged := append([]int64{e.f.Olive, e.f.Nina}, e.f.SeededFlagged...)
	slices.Sort(flagged)
	require.Equal(t, flagged, customers(rows), "only online customers")
	require.Len(t, rows, 6+seededJobs(t, e, "0001-01-01+00", "9999-01-01+00"),
		"Olive's five listed jobs and Nina's one, plus the seeded online ones")
}

func TestTheDefaultNovemberViewAndNewestFirst(t *testing.T) {
	e := newEnv(t)

	rows := all(t, e, november())

	// Olive's tie at 09:00:00.123456 comes after (is older than) the November 11 cancelled job, and the later id comes first.
	require.Equal(t, e.f.EdgeLast, rows[0].ID, "the last microsecond of November is the latest appointment")
	require.Equal(t, e.f.Cancelled, rows[1].ID)
	require.Equal(t, []int64{e.f.TieSecond, e.f.TieFirst}, ids(rows[2:4]), "equal instants: the later id first")
	require.Equal(t, e.f.NinaJob, rows[4].ID)
	require.NotContains(t, ids(rows), e.f.EdgeNext, "December 1 is outside")
	require.Len(t, rows, 5+seededJobs(t, e, "2026-11-01+00", "2026-12-01+00"), "the seeded November bookings are 63716's and 63749's")
}

func TestTheRangeIsHalfOpenOnTheAppointment(t *testing.T) {
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
			_, err := e.pool.Exec(context.Background(), `UPDATE jobs SET scheduled_at = $2::timestamptz WHERE id = $1`, e.f.NinaJob, tt.at)
			require.NoError(t, err)

			require.Equal(t, tt.inRange, slices.Contains(ids(all(t, e, november())), e.f.NinaJob))
		})
	}
}

func TestRowsCarryEveryColumnOfTheScreen(t *testing.T) {
	e := newEnv(t)
	rows := byID(all(t, e, model.OnlineBookingFilter{}))

	got := rows[e.f.TieFirst]
	require.Equal(t, e.f.Olive, got.CustomerID)
	require.Equal(t, "Olive", got.CustomerName)
	require.Equal(t, utc(2026, 11, 10, 9, 0, 0, 123456000), got.ScheduledAt, "the appointment, with microseconds")
	require.Equal(t, "Bi-Monthly Service", got.ServiceType)
	require.Equal(t, "olive.ng@example.com", *got.Email)
	require.Equal(t, "(555) 010-0011", *got.Phone, "the first phone, whatever its number says")
	require.Equal(t, model.LookupAddress{Line1: "11 Olive Way", City: "Round Rock", State: "TX", Zip: "73301", County: "Travis"}, got.Address,
		"the job's own location")
	require.Equal(t, "Gate code 1234", got.Notes)
	require.Equal(t, model.JobConfirmed, got.Status)
	require.NotNil(t, got.Source)
	require.Equal(t, "Google", *got.Source, "the customer's source")
	require.Equal(t, sourceID(t, e, "Google"), *got.SourceID)

	require.Equal(t, model.JobCancelled, rows[e.f.Cancelled].Status, "a cancelled booking is listed, with its status")

	nina := rows[e.f.NinaJob]
	require.Nil(t, nina.Email)
	require.Nil(t, nina.Phone)
	require.Nil(t, nina.Source)
	require.Equal(t, "", nina.Notes)
}

func TestTheSourceIsTheCustomersNotTheJobs(t *testing.T) {
	e := newEnv(t)
	// Give Olive's job a different source of its own: the report still shows the customer's.
	_, err := e.pool.Exec(context.Background(),
		`UPDATE jobs SET lead_source_id = (SELECT id FROM lead_sources WHERE name = 'Referral') WHERE id = $1`, e.f.TieFirst)
	require.NoError(t, err)

	got := byID(all(t, e, model.OnlineBookingFilter{}))[e.f.TieFirst]

	require.Equal(t, "Google", *got.Source)
}

func TestNameFormatsShowTheNamePartsAndFallBackToTheDisplayName(t *testing.T) {
	e := newEnv(t)
	namesBy := func(format model.NameFormat) map[int64]string {
		q := query(model.OnlineBookingFilter{}, model.OnlineBookingSortScheduledAt, true, 200)
		q.NameFormat = format
		out := map[int64]string{}
		for _, r := range list(t, e, q) {
			out[r.CustomerID] = r.CustomerName
		}
		return out
	}

	first, last, full := namesBy(model.NameFirst), namesBy(model.NameLast), namesBy(model.NameFull)
	require.Equal(t, "Olive", first[e.f.Olive])
	require.Equal(t, "Nina Nosource", first[e.f.Nina], "no first name: the display name")
	require.Equal(t, "Ng", last[e.f.Olive])
	require.Equal(t, "Nosource", last[e.f.Nina])
	require.Equal(t, "Olive Ng", full[e.f.Olive])
	require.Equal(t, "Nina Nosource", full[e.f.Nina])
	require.Equal(t, first, namesBy(""), "an unset format is the first name")
}

func TestFilterBySource(t *testing.T) {
	e := newEnv(t)
	google, facebook := sourceID(t, e, "Google"), sourceID(t, e, "Facebook")
	custs := func(ids []int64, none bool) []int64 {
		return customers(all(t, e, model.OnlineBookingFilter{LeadSourceIDs: ids, NoSource: none}))
	}
	var facebookCustomer int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT id FROM customers WHERE account_number = '63714'`).Scan(&facebookCustomer))

	require.Equal(t, []int64{e.f.Olive}, custs([]int64{google}, false), "Google: Olive; the offline Google customer is not online")
	require.Equal(t, []int64{facebookCustomer}, custs([]int64{facebook}, false))

	none := custs(nil, true)
	require.Contains(t, none, e.f.Nina, "none: the customers without a source, the screen's NA")
	require.NotContains(t, none, e.f.Olive)
	require.NotContains(t, none, facebookCustomer)

	both := custs([]int64{google}, true)
	require.Contains(t, both, e.f.Olive)
	require.Contains(t, both, e.f.Nina)
	require.NotContains(t, both, facebookCustomer)
	require.Empty(t, all(t, e, model.OnlineBookingFilter{LeadSourceIDs: []int64{999999}}), "an unknown source has no bookings")
}

func TestSearchMatchesNameEmailPhoneAddressServiceAndNotesAndTreatsWildcardsLiterally(t *testing.T) {
	e := newEnv(t)
	search := func(s string) []int64 {
		return customers(all(t, e, model.OnlineBookingFilter{Search: s}))
	}

	require.Equal(t, []int64{e.f.Olive}, search("OLIVE NG"), "case-insensitive, by name")
	require.Equal(t, []int64{e.f.Olive}, search("olive.ng@"), "by email")
	require.Equal(t, []int64{e.f.Olive}, search("010-0011"), "by phone")
	require.Equal(t, []int64{e.f.Olive}, search("round rock"), "by city")
	require.Equal(t, []int64{e.f.Olive}, search("11 Olive Way"), "by address line")
	require.Equal(t, []int64{e.f.Olive}, search("gate code"), "by notes")
	require.Contains(t, search("Initial Service"), e.f.Nina, "by service type")
	require.NotContains(t, search("walk-in"), e.f.Offline, "the offline customer is never listed, whatever matches")
	require.Empty(t, search("%"), "a percent sign is not a wildcard")
	require.Empty(t, search("_"), "an underscore is not a wildcard")
	require.Empty(t, search("'; DROP TABLE jobs; --"))
}

func TestEverySortKeyOrdersInBothDirections(t *testing.T) {
	e := newEnv(t)
	n := len(all(t, e, model.OnlineBookingFilter{}))

	for _, key := range model.OnlineBookingSortKeys {
		asc := list(t, e, query(model.OnlineBookingFilter{}, key, false, 200))
		desc := list(t, e, query(model.OnlineBookingFilter{}, key, true, 200))
		require.Len(t, asc, n, key)

		reversed := ids(desc)
		slices.Reverse(reversed)
		require.Equal(t, ids(asc), reversed, "%s: descending is exactly the reverse of ascending, ties included", key)

		for i := 1; i < len(asc); i++ {
			a, b := asc[i-1], asc[i]
			switch key {
			case model.OnlineBookingSortScheduledAt:
				require.False(t, a.ScheduledAt.After(b.ScheduledAt), "%s: %v before %v", key, a.ID, b.ID)
				if a.ScheduledAt.Equal(b.ScheduledAt) {
					require.Less(t, a.ID, b.ID, "ties break by id")
				}
			case model.OnlineBookingSortStatus:
				require.LessOrEqual(t, string(a.Status), string(b.Status), key)
			case model.OnlineBookingSortEmail:
				require.LessOrEqual(t, strings.ToLower(deref(a.Email)), strings.ToLower(deref(b.Email)), key)
			case model.OnlineBookingSortPhone:
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
	// The tied jobs share an instant, a customer, a service, an address and a status, and many rows share an empty phone
	// or email, so the id tie-break matters.
	for _, key := range model.OnlineBookingSortKeys {
		for _, desc := range []bool{false, true} {
			full := ids(list(t, e, query(model.OnlineBookingFilter{}, key, desc, 200)))

			var walked []int64
			var after *model.OnlineBookingCursor
			for pages := 0; pages < 30; pages++ {
				q := query(model.OnlineBookingFilter{}, key, desc, 4)
				q.After = after
				rows := list(t, e, q)
				require.LessOrEqual(t, len(rows), 5, "limit+1 rows at most")
				page := rows
				if len(page) > 4 {
					page = page[:4]
				}
				walked = append(walked, ids(page)...)
				if len(rows) <= 4 {
					break
				}
				last := page[len(page)-1]
				after = &model.OnlineBookingCursor{SortValue: last.SortValue, ID: last.ID}
			}

			require.Equal(t, full, walked, "%s desc=%v", key, desc)
		}
	}
}

func TestTheScheduledCursorKeepsMicroseconds(t *testing.T) {
	e := newEnv(t)

	rows := all(t, e, november())

	require.Equal(t, "2026-11-10T09:00:00.123456Z", byID(rows)[e.f.TieFirst].SortValue)
}

func TestNothingToShowIsAnEmptyList(t *testing.T) {
	e := newEnv(t)
	f := model.OnlineBookingFilter{ScheduledFrom: utc(2030, 1, 1, 0, 0, 0, 0), ScheduledBefore: utc(2030, 2, 1, 0, 0, 0, 0)}

	rows := list(t, e, query(f, model.OnlineBookingSortScheduledAt, true, 10))

	require.NotNil(t, rows)
	require.Empty(t, rows)
}

func TestTheFlagIsWhatMakesABookingNotTheSource(t *testing.T) {
	e := newEnv(t)

	t.Run("flagging the offline customer lists its job", func(t *testing.T) {
		_, err := e.pool.Exec(context.Background(), `UPDATE customers SET booked_online = true WHERE id = $1`, e.f.Offline)
		require.NoError(t, err)

		require.Contains(t, ids(all(t, e, model.OnlineBookingFilter{})), e.f.OfflineJob)
	})

	t.Run("clearing it removes the customer's jobs", func(t *testing.T) {
		_, err := e.pool.Exec(context.Background(), `UPDATE customers SET booked_online = false WHERE id = $1`, e.f.Olive)
		require.NoError(t, err)

		require.NotContains(t, customers(all(t, e, model.OnlineBookingFilter{})), e.f.Olive)
	})
}

func TestListFollowsTheContextAndRejectsBadQueries(t *testing.T) {
	e := newEnv(t)

	t.Run("a cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := e.repo.List(ctx, query(november(), model.OnlineBookingSortScheduledAt, true, 10))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("an invalid query", func(t *testing.T) {
		_, err := e.repo.List(context.Background(), query(november(), model.OnlineBookingSortScheduledAt, true, 0))
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
