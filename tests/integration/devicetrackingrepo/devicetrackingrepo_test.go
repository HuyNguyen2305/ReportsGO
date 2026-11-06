//go:build integration

package devicetrackingrepo_test

import (
	"context"
	"slices"
	"strconv"
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
	repo *repository.DeviceTrackingRepository
	f    testdb.DeviceTrackingFixture
}

// newEnv is a seeded database (six devices, eight checks) with the extra customers, devices, jobs and checks of
// testdb.InsertDeviceTracking on top.
func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewDeviceTrackingRepository(pool), f: testdb.InsertDeviceTracking(t, pool)}
}

func november() model.DeviceTrackingFilter {
	return model.DeviceTrackingFilter{ScheduledFrom: utc(2026, 11, 1, 0, 0, 0, 0), ScheduledBefore: utc(2026, 12, 1, 0, 0, 0, 0)}
}

func query(f model.DeviceTrackingFilter, key model.DeviceTrackingSortKey, desc bool, limit int) model.DeviceTrackingQuery {
	return model.DeviceTrackingQuery{Filter: f, Sort: model.DeviceTrackingSort{Key: key, Desc: desc}, Limit: limit}
}

func list(t *testing.T, e env, q model.DeviceTrackingQuery) []model.DeviceTrackingRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), q)
	require.NoError(t, err)
	return rows
}

func all(t *testing.T, e env, f model.DeviceTrackingFilter) []model.DeviceTrackingRow {
	t.Helper()
	return list(t, e, query(f, model.DeviceTrackingSortDate, true, 200))
}

func ids(rows []model.DeviceTrackingRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func byID(rows []model.DeviceTrackingRow) map[int64]model.DeviceTrackingRow {
	out := map[int64]model.DeviceTrackingRow{}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

// seededChecks counts, by an independent query, the checks the seed adds, all on scheduled jobs that are not deleted.
func seededChecks(t *testing.T, e env, from, before string) int {
	t.Helper()
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM job_device_checks chk JOIN jobs j ON j.id = chk.job_id JOIN customers c ON c.id = j.customer_id
		WHERE c.account_number IN ('63706', '63716', '63749') AND j.deleted_at IS NULL AND j.scheduled_at IS NOT NULL
		  AND j.scheduled_at >= $1::timestamptz AND j.scheduled_at < $2::timestamptz`, from, before).Scan(&n))
	return n
}

func TestOnlyChecksOfScheduledJobsThatAreNotDeletedAreListed(t *testing.T) {
	e := newEnv(t)

	rows := all(t, e, model.DeviceTrackingFilter{})

	got := ids(rows)
	for _, want := range []int64{e.f.Tie1Gate, e.f.Tie1NoName, e.f.Tie2Beside, e.f.CancelledGate, e.f.EdgeLastGate, e.f.EdgeNextGate, e.f.OtherCheck} {
		require.Contains(t, got, want)
	}
	require.NotContains(t, got, e.f.HiddenDeleted, "a deleted job's checks are never listed")
	require.NotContains(t, got, e.f.HiddenUnscheduled, "a job with no appointment has no Date")
	require.Len(t, rows, 7+seededChecks(t, e, "0001-01-01+00", "9999-01-01+00"), "the fixture's seven visible checks plus the seeded ones")
	require.Equal(t, 8, seededChecks(t, e, "0001-01-01+00", "9999-01-01+00"), "the seed adds eight checks")
}

func TestTheDefaultNovemberViewIsNewestFirst(t *testing.T) {
	e := newEnv(t)

	rows := all(t, e, november())

	require.Equal(t, e.f.EdgeLastGate, rows[0].ID, "the last microsecond of November is the latest appointment")
	require.Equal(t, e.f.CancelledGate, rows[1].ID)
	// The three checks of the two tied jobs share one instant: the later check id comes first.
	require.Equal(t, []int64{e.f.Tie2Beside, e.f.Tie1NoName, e.f.Tie1Gate}, ids(rows[2:5]))
	require.Equal(t, e.f.OtherCheck, rows[5].ID)
	require.NotContains(t, ids(rows), e.f.EdgeNextGate, "December 1 is outside")
	require.Len(t, rows, 6+seededChecks(t, e, "2026-11-01+00", "2026-12-01+00"), "the seeded November checks are on the November 2 jobs")
	require.Equal(t, 4, seededChecks(t, e, "2026-11-01+00", "2026-12-01+00"))
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
			_, err := e.pool.Exec(context.Background(), `UPDATE jobs SET scheduled_at = $2::timestamptz WHERE id = $1`, e.f.OtherJob, tt.at)
			require.NoError(t, err)

			require.Equal(t, tt.inRange, slices.Contains(ids(all(t, e, november())), e.f.OtherCheck))
		})
	}
}

func TestRowsCarryEveryColumnOfTheScreen(t *testing.T) {
	e := newEnv(t)
	rows := byID(all(t, e, model.DeviceTrackingFilter{}))

	got := rows[e.f.Tie1Gate]
	require.Equal(t, e.f.Dana, got.CustomerID)
	require.Equal(t, "Dana Devices", got.CustomerName)
	require.Equal(t, "Bi-Monthly Service", got.ServiceType)
	require.Equal(t, utc(2026, 11, 10, 9, 0, 0, 123456000), got.Date, "the job's appointment, with microseconds")
	require.Equal(t, model.LookupAddress{Line1: "Terminal 4 Arrivals", Line2: "Gate 7", City: "Queens", State: "NY", Zip: "11430", County: "Queens"}, got.Address,
		"the job's own location")
	require.Equal(t, e.f.Gate, got.DeviceID)
	require.Equal(t, "Gate Box", got.DeviceName)
	require.Equal(t, "BC-9001", got.Barcode)
	require.Equal(t, 37.5, *got.Latitude)
	require.Equal(t, -122.1, *got.Longitude)
	require.Equal(t, "Area 9", got.Area)
	require.Equal(t, "Bait Box", got.DeviceType)
	require.Equal(t, utc(2026, 11, 10, 9, 10, 0, 0), *got.CheckedInAt)
	require.Equal(t, utc(2026, 11, 10, 9, 25, 0, 500000000), *got.CheckedOutAt)
	require.Equal(t, "Good", got.Status)
	require.Equal(t, "Inspected", got.Activity)
	require.Equal(t, "Bait taken, refilled", got.Notes)
	require.Equal(t, "Thọ Nguyễn Quang", *got.AssignedTo)

	bare := rows[e.f.Tie1NoName]
	require.Equal(t, "", bare.DeviceName)
	require.Equal(t, "", bare.Barcode)
	require.Nil(t, bare.Latitude, "a device without a coordinate")
	require.Nil(t, bare.Longitude)
	require.Nil(t, bare.CheckedInAt)
	require.Nil(t, bare.CheckedOutAt)
	require.Nil(t, bare.AssignedTo)
	require.Nil(t, bare.AssignedToID)
	require.Equal(t, "40", bare.Status, "status is free text")

	require.Nil(t, rows[e.f.Tie2Beside].CheckedOutAt, "checked in but not out yet")
	require.NotNil(t, rows[e.f.Tie2Beside].CheckedInAt)
}

func TestTheAddressIsTheJobsNotTheDevices(t *testing.T) {
	e := newEnv(t)
	// Move the job to another location of the same customer: the report follows the job.
	var other int64
	require.NoError(t, e.pool.QueryRow(context.Background(),
		`INSERT INTO service_locations (customer_id, address_line1, city, state, postal_code) VALUES ($1, 'Second Home', 'Dallas', 'TX', '75001') RETURNING id`,
		e.f.Dana).Scan(&other))
	_, err := e.pool.Exec(context.Background(), `UPDATE jobs SET location_id = $2 WHERE id = $1`, e.f.TieFirst, other)
	require.NoError(t, err)

	got := byID(all(t, e, model.DeviceTrackingFilter{}))[e.f.Tie1Gate]

	require.Equal(t, "Second Home", got.Address.Line1)
	require.Equal(t, "Dallas", got.Address.City)
}

func TestFilterByClient(t *testing.T) {
	e := newEnv(t)
	with := func(ids ...int64) model.DeviceTrackingFilter { return model.DeviceTrackingFilter{CustomerIDs: ids} }

	require.Equal(t, []int64{e.f.OtherCheck}, ids(all(t, e, with(e.f.Other))))
	require.Len(t, all(t, e, with(e.f.Dana)), 6, "Dana's six visible checks")
	require.Len(t, all(t, e, with(e.f.Dana, e.f.Other)), 7)
	require.Empty(t, all(t, e, with(999999)), "a client with no devices")
}

func TestSearchMatchesEveryColumnAndTheAssigneeAndTreatsWildcardsLiterally(t *testing.T) {
	e := newEnv(t)
	search := func(s string) []int64 {
		return ids(all(t, e, model.DeviceTrackingFilter{Search: s, CustomerIDs: []int64{e.f.Dana, e.f.Other}}))
	}

	require.Equal(t, []int64{e.f.EdgeNextGate, e.f.EdgeLastGate, e.f.CancelledGate, e.f.Tie1Gate}, search("GATE BOX"), "by device name, case-insensitive")
	require.Equal(t, []int64{e.f.Tie2Beside}, search("bc-9003"), "by barcode")
	require.Equal(t, []int64{e.f.OtherCheck}, search("other owner"), "by customer")
	require.Equal(t, []int64{e.f.OtherCheck}, search("miami beach"), "by city")
	require.Equal(t, []int64{e.f.OtherCheck}, search("33139"), "by zip")
	require.Equal(t, []int64{e.f.Tie1NoName}, search("laptop"), "by device type")
	require.Equal(t, []int64{e.f.Tie2Beside}, search("placed"), "by activity")
	require.Equal(t, []int64{e.f.Tie2Beside}, search("near the door"), "by notes")
	require.Equal(t, []int64{e.f.Tie1NoName}, search("40"), "by status")
	require.Equal(t, []int64{e.f.Tie2Beside, e.f.OtherCheck}, search("thon nguyen"), "by the assignee's name, newest first")
	require.Contains(t, search("gate 7"), e.f.Tie1Gate, "by the second address line")
	require.Empty(t, search("%"), "a percent sign is not a wildcard")
	require.Empty(t, search("_"), "an underscore is not a wildcard")
	require.Empty(t, search("'; DROP TABLE devices; --"))
}

func TestEverySortKeyOrdersInBothDirections(t *testing.T) {
	e := newEnv(t)
	n := len(all(t, e, model.DeviceTrackingFilter{}))

	for _, key := range model.DeviceTrackingSortKeys {
		asc := list(t, e, query(model.DeviceTrackingFilter{}, key, false, 200))
		desc := list(t, e, query(model.DeviceTrackingFilter{}, key, true, 200))
		require.Len(t, asc, n, key)

		reversed := ids(desc)
		slices.Reverse(reversed)
		require.Equal(t, ids(asc), reversed, "%s: descending is exactly the reverse of ascending, ties included", key)

		for i := 1; i < len(asc); i++ {
			a, b := asc[i-1], asc[i]
			switch key {
			case model.DeviceTrackingSortDate:
				require.False(t, a.Date.After(b.Date), "%s: %v before %v", key, a.ID, b.ID)
				if a.Date.Equal(b.Date) {
					require.Less(t, a.ID, b.ID, "ties break by id")
				}
			case model.DeviceTrackingSortCheckIn:
				require.LessOrEqual(t, millis(a.CheckedInAt), millis(b.CheckedInAt), key)
			case model.DeviceTrackingSortCheckOut:
				require.LessOrEqual(t, millis(a.CheckedOutAt), millis(b.CheckedOutAt), key)
			case model.DeviceTrackingSortStatus:
				require.LessOrEqual(t, strings.ToLower(a.Status), strings.ToLower(b.Status), key)
			case model.DeviceTrackingSortDeviceType:
				require.LessOrEqual(t, strings.ToLower(a.DeviceType), strings.ToLower(b.DeviceType), key)
			}
		}
	}
}

func millis(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.UnixMilli()
}

func TestNeverCheckedAndNoCoordinateSortFirstWhenAscending(t *testing.T) {
	e := newEnv(t)
	dana := model.DeviceTrackingFilter{CustomerIDs: []int64{e.f.Dana}}

	t.Run("check in", func(t *testing.T) {
		rows := list(t, e, query(dana, model.DeviceTrackingSortCheckIn, false, 200))

		require.Nil(t, rows[0].CheckedInAt, "never checked in is first")
		last := rows[len(rows)-1]
		require.Equal(t, e.f.Tie1Gate, last.ID, "the latest check in is last: 09:10 against 09:05")
	})

	t.Run("coordinate: latitude, then longitude", func(t *testing.T) {
		rows := list(t, e, query(dana, model.DeviceTrackingSortCoordinate, false, 200))

		require.Equal(t, e.f.NoName, rows[0].DeviceID, "a device without a coordinate is first")
		var seen []int64
		for _, r := range rows[1:] {
			if len(seen) == 0 || seen[len(seen)-1] != r.DeviceID {
				seen = append(seen, r.DeviceID)
			}
		}
		require.Equal(t, []int64{e.f.Gate, e.f.Beside}, seen, "same latitude: the smaller longitude (-122.1) before -122.0")
	})
}

func TestKeysetPagingWalksEveryRowOnceForEveryKey(t *testing.T) {
	e := newEnv(t)
	// Many rows share a device, a status, an address or an instant, and many are never checked, so the id tie-break matters.
	for _, key := range model.DeviceTrackingSortKeys {
		for _, desc := range []bool{false, true} {
			full := ids(list(t, e, query(model.DeviceTrackingFilter{}, key, desc, 200)))

			var walked []int64
			var after *model.DeviceTrackingCursor
			for pages := 0; pages < 30; pages++ {
				q := query(model.DeviceTrackingFilter{}, key, desc, 4)
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
				after = &model.DeviceTrackingCursor{SortValue: last.SortValue, ID: last.ID}
			}

			require.Equal(t, full, walked, "%s desc=%v", key, desc)
		}
	}
}

func TestTheDateCursorKeepsMicrosecondsAndTheCheckCursorsAreMilliseconds(t *testing.T) {
	e := newEnv(t)

	rowsByDate := byID(list(t, e, query(november(), model.DeviceTrackingSortDate, true, 200)))
	require.Equal(t, "2026-11-10T09:00:00.123456Z", rowsByDate[e.f.Tie1Gate].SortValue)

	rowsByIn := byID(list(t, e, query(november(), model.DeviceTrackingSortCheckIn, true, 200)))
	require.Equal(t, strconv.FormatInt(utc(2026, 11, 10, 9, 10, 0, 0).UnixMilli(), 10), rowsByIn[e.f.Tie1Gate].SortValue,
		"2026-11-10 09:10:00 UTC in epoch milliseconds")
	require.Equal(t, "0", rowsByIn[e.f.Tie1NoName].SortValue, "never checked in")

	rowsByOut := byID(list(t, e, query(november(), model.DeviceTrackingSortCheckOut, true, 200)))
	require.Equal(t, strconv.FormatInt(utc(2026, 11, 10, 9, 25, 0, 500000000).UnixMilli(), 10), rowsByOut[e.f.Tie1Gate].SortValue,
		"milliseconds are kept")
}

func TestNothingToShowIsAnEmptyList(t *testing.T) {
	e := newEnv(t)
	f := model.DeviceTrackingFilter{ScheduledFrom: utc(2030, 1, 1, 0, 0, 0, 0), ScheduledBefore: utc(2030, 2, 1, 0, 0, 0, 0)}

	rows := list(t, e, query(f, model.DeviceTrackingSortDate, true, 10))

	require.NotNil(t, rows)
	require.Empty(t, rows)
}

func TestListFollowsTheContextAndRejectsBadQueries(t *testing.T) {
	e := newEnv(t)

	t.Run("a cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := e.repo.List(ctx, query(november(), model.DeviceTrackingSortDate, true, 10))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("an invalid query", func(t *testing.T) {
		_, err := e.repo.List(context.Background(), query(november(), model.DeviceTrackingSortDate, true, 0))
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestClientsLookup(t *testing.T) {
	e := newEnv(t)

	clients, err := e.repo.Clients(context.Background())

	require.NoError(t, err)
	require.Len(t, clients, 22, "every seeded customer and the two the fixture adds")
}
