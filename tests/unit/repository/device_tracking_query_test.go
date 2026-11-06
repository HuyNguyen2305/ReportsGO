package repository_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func defaultDeviceTrackingQuery() model.DeviceTrackingQuery {
	return model.DeviceTrackingQuery{
		Sort:  model.DeviceTrackingSort{Key: model.DeviceTrackingSortDate, Desc: true},
		Limit: 50,
	}
}

func TestBuildDeviceTrackingListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildDeviceTrackingListQuery(defaultDeviceTrackingQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM job_device_checks chk")
	require.Contains(t, stmt.SQL, "JOIN jobs j ON j.id = chk.job_id")
	require.Contains(t, stmt.SQL, "JOIN customers c ON c.id = j.customer_id")
	require.Contains(t, stmt.SQL, "JOIN service_types st ON st.id = j.service_type_id")
	require.Contains(t, stmt.SQL, "JOIN service_locations sl ON sl.id = j.location_id", "the address is the job's own location")
	require.Contains(t, stmt.SQL, "JOIN devices d ON d.id = chk.device_id")
	require.Contains(t, stmt.SQL, "LEFT JOIN users u ON u.id = chk.assigned_to", "the staff member is optional")
	require.Contains(t, stmt.SQL, "d.latitude::float8, d.longitude::float8")
	require.Contains(t, stmt.SQL, "WHERE j.deleted_at IS NULL\n  AND j.scheduled_at IS NOT NULL\nORDER BY",
		"never a deleted job or one without an appointment, whatever the filter")
	require.Contains(t, stmt.SQL, "ORDER BY j.scheduled_at DESC, chk.id DESC")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildDeviceTrackingListQueryEverySortKeyAndDirection(t *testing.T) {
	const coordinate = `coalesce(lpad(round((d.latitude + 90) * 1000000)::bigint::text, 12, '0'), '') || ',' || ` +
		`coalesce(lpad(round((d.longitude + 180) * 1000000)::bigint::text, 12, '0'), '')`
	wantExpr := map[model.DeviceTrackingSortKey]struct{ order, cast string }{
		model.DeviceTrackingSortCustomer:   {"left(lower(c.display_name), 200)", "text"},
		model.DeviceTrackingSortService:    {"left(lower(st.name), 200)", "text"},
		model.DeviceTrackingSortDate:       {"j.scheduled_at", "timestamptz"},
		model.DeviceTrackingSortAddress1:   {"left(lower(sl.address_line1), 200)", "text"},
		model.DeviceTrackingSortAddress2:   {"left(lower(sl.address_line2), 200)", "text"},
		model.DeviceTrackingSortCity:       {"left(lower(sl.city), 200)", "text"},
		model.DeviceTrackingSortState:      {"left(lower(sl.state), 200)", "text"},
		model.DeviceTrackingSortZip:        {"left(lower(sl.postal_code), 200)", "text"},
		model.DeviceTrackingSortDeviceName: {"left(lower(d.name), 200)", "text"},
		model.DeviceTrackingSortBarcode:    {"left(lower(d.barcode), 200)", "text"},
		model.DeviceTrackingSortCoordinate: {"left(" + coordinate + ", 200)", "text"},
		model.DeviceTrackingSortArea:       {"left(lower(d.area), 200)", "text"},
		model.DeviceTrackingSortDeviceType: {"left(lower(d.device_type), 200)", "text"},
		model.DeviceTrackingSortCheckIn:    {"coalesce((extract(epoch from chk.checked_in_at) * 1000)::bigint, 0)", "bigint"},
		model.DeviceTrackingSortStatus:     {"left(lower(chk.status), 200)", "text"},
		model.DeviceTrackingSortActivity:   {"left(lower(chk.activity), 200)", "text"},
		model.DeviceTrackingSortCheckOut:   {"coalesce((extract(epoch from chk.checked_out_at) * 1000)::bigint, 0)", "bigint"},
		model.DeviceTrackingSortNotes:      {"left(lower(chk.notes), 200)", "text"},
		model.DeviceTrackingSortAssignedTo: {"left(lower(coalesce(u.full_name, '')), 200)", "text"},
	}
	require.Len(t, wantExpr, len(model.DeviceTrackingSortKeys), "every key must be covered by this test")

	for _, key := range model.DeviceTrackingSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)
		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+"/"+dir, func(t *testing.T) {
				q := defaultDeviceTrackingQuery()
				q.Sort = model.DeviceTrackingSort{Key: key, Desc: desc}
				q.After = &model.DeviceTrackingCursor{SortValue: "x", ID: 9}

				stmt, err := repository.BuildDeviceTrackingListQuery(q)
				require.NoError(t, err)

				require.Contains(t, stmt.SQL, "ORDER BY "+want.order+" "+dir+", chk.id "+dir)
				require.Contains(t, stmt.SQL, "AND ("+want.order+", chk.id) "+cmp+" (($1::text)::"+want.cast+", $2::bigint)")
				require.Equal(t, []any{"x", int64(9), 51}, stmt.Args)
				requirePlaceholdersMatchArgs(t, stmt)
			})
		}
	}
}

func TestBuildDeviceTrackingListQueryDateCursorIsFixedFormatUTC(t *testing.T) {
	stmt, err := repository.BuildDeviceTrackingListQuery(defaultDeviceTrackingQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, `to_char(j.scheduled_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`)
}

func TestBuildDeviceTrackingListQueryFilters(t *testing.T) {
	f := model.DeviceTrackingFilter{
		ScheduledFrom:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		ScheduledBefore: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		CustomerIDs:     []int64{3, 5},
		Search:          "100%_",
	}
	q := defaultDeviceTrackingQuery()
	q.Filter = f
	q.After = &model.DeviceTrackingCursor{SortValue: "2026-11-03T12:00:00.000000Z", ID: 1}

	stmt, err := repository.BuildDeviceTrackingListQuery(q)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "j.scheduled_at >= $1::timestamptz")
	require.Contains(t, stmt.SQL, "j.scheduled_at < $2::timestamptz")
	require.Contains(t, stmt.SQL, "j.customer_id = ANY($3::bigint[])")
	for _, col := range []string{
		"c.display_name", "st.name", "sl.address_line1", "sl.address_line2", "sl.city", "sl.state", "sl.postal_code",
		"d.name", "d.barcode", "d.area", "d.device_type", "chk.status", "chk.activity", "chk.notes", "u.full_name",
	} {
		require.Contains(t, stmt.SQL, col+` ILIKE $4 ESCAPE '\'`, "the search covers %s", col)
	}
	require.Equal(t, []any{
		f.ScheduledFrom, f.ScheduledBefore, []int64{3, 5}, `%100\%\_%`,
		"2026-11-03T12:00:00.000000Z", int64(1), 51,
	}, stmt.Args, "the keyset arguments continue the numbering after the filter's")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildDeviceTrackingListQueryRejectsBadInput(t *testing.T) {
	t.Run("limit below one", func(t *testing.T) {
		q := defaultDeviceTrackingQuery()
		q.Limit = 0
		_, err := repository.BuildDeviceTrackingListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
	t.Run("unknown sort key", func(t *testing.T) {
		q := defaultDeviceTrackingQuery()
		q.Sort.Key = "barcode; DROP TABLE devices"
		_, err := repository.BuildDeviceTrackingListQuery(q)
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestBuildDeviceTrackingListQueryNeverEmbedsCallerText(t *testing.T) {
	q := defaultDeviceTrackingQuery()
	q.Filter.Search = "'; DROP TABLE devices; --"
	q.After = &model.DeviceTrackingCursor{SortValue: "x'; DROP TABLE devices; --", ID: 1}

	stmt, err := repository.BuildDeviceTrackingListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP TABLE")
}
