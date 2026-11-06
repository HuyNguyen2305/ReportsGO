//go:build integration

package testdb

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// DeviceTrackingFixture holds the ids InsertDeviceTracking adds on top of the seed, which already has six devices and
// eight checks (63706's October jobs and the November 2 jobs of 63716 and 63749).
type DeviceTrackingFixture struct {
	// Dana owns three devices and the jobs below; Other owns one device and one job.
	Dana, Other int64
	// Dana's devices: Gate has a name, a barcode and a coordinate; NoName has none of them and no coordinate; Beside has the
	// same latitude as Gate and a greater longitude.
	Gate, NoName, Beside int64

	// Dana's jobs. TieFirst and TieSecond are confirmed at the very same instant, November 10 at 09:00:00.123456 UTC.
	// Cancelled is listed. Deleted and Unscheduled never are. EdgeLast is the last microsecond of November and EdgeNext
	// the first instant of December. OtherJob is Other's job on November 9 at 08:00 UTC.
	TieFirst, TieSecond, Cancelled, Deleted, Unscheduled, EdgeLast, EdgeNext, OtherJob int64

	// Checks, by the job they were made on. Tie1Gate is the one with every column filled in; Tie1NoName has no times and no
	// assignee. Hidden are the two on the deleted and the unscheduled job.
	Tie1Gate, Tie1NoName, Tie2Beside, CancelledGate, EdgeLastGate, EdgeNextGate, OtherCheck int64
	HiddenDeleted, HiddenUnscheduled                                                        int64
}

// InsertDeviceTracking adds the customers, devices, jobs and checks the Device Tracking tests need to a seeded database,
// where the cases for ties, cancelled, deleted and unscheduled jobs, range edges, coordinates and assignees are built so
// they cannot change what other reports see.
func InsertDeviceTracking(t *testing.T, pool *pgxpool.Pool) DeviceTrackingFixture {
	t.Helper()
	ctx := context.Background()
	var f DeviceTrackingFixture

	addCustomer := func(dst *int64, acct, display string) {
		t.Helper()
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO customers (account_number, first_name, last_name, display_name) VALUES ($1, '', $2, $2) RETURNING id`,
			acct, display).Scan(dst))
	}
	addLocation := func(customerID int64, line1, line2, city, state, zip string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO service_locations (customer_id, address_line1, address_line2, city, state, postal_code, county)
			VALUES ($1, $2, $3, $4, $5, $6, 'Queens') RETURNING id`, customerID, line1, line2, city, state, zip).Scan(&id))
		return id
	}
	addDevice := func(locationID int64, name, barcode, deviceType, area string, lat, lng *float64) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO devices (location_id, name, barcode, device_type, area, latitude, longitude)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, locationID, name, barcode, deviceType, area, lat, lng).Scan(&id))
		return id
	}
	var serviceTypeID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM service_types WHERE name = 'Bi-Monthly Service'`).Scan(&serviceTypeID))
	str := func(s string) *string { return &s }
	flt := func(v float64) *float64 { return &v }
	addJob := func(customerID, locationID int64, status string, scheduledAt *string, deleted bool) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, deleted_at)
			VALUES ($1, $2, $3, $4::job_status, $5::timestamptz, CASE WHEN $6 THEN now() END)
			RETURNING id`, customerID, locationID, serviceTypeID, status, scheduledAt, deleted).Scan(&id))
		return id
	}
	addCheck := func(jobID, deviceID int64, in, out *string, status, activity, notes string, assignee *string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO job_device_checks (job_id, device_id, checked_in_at, checked_out_at, status, activity, notes, assigned_to)
			VALUES ($1, $2, $3::timestamptz, $4::timestamptz, $5, $6, $7, (SELECT id FROM users WHERE email = $8))
			RETURNING id`, jobID, deviceID, in, out, status, activity, notes, assignee).Scan(&id))
		return id
	}

	addCustomer(&f.Dana, "80031", "Dana Devices")
	addCustomer(&f.Other, "80032", "Other Owner")
	danaHome := addLocation(f.Dana, "Terminal 4 Arrivals", "Gate 7", "Queens", "NY", "11430")
	otherHome := addLocation(f.Other, "9 Other Road", "", "Miami Beach", "FL", "33139")

	f.Gate = addDevice(danaHome, "Gate Box", "BC-9001", "Bait Box", "Area 9", flt(37.5), flt(-122.1))
	f.NoName = addDevice(danaHome, "", "", "Laptop", "", nil, nil)
	f.Beside = addDevice(danaHome, "Beside Box", "BC-9003", "Trap", "Area 9", flt(37.5), flt(-122.0))
	otherDevice := addDevice(otherHome, "Other Box", "BC-9100", "Bait Box", "Area 1", flt(25.0), flt(-80.0))

	f.TieFirst = addJob(f.Dana, danaHome, "confirmed", str("2026-11-10 09:00:00.123456+00"), false)
	f.TieSecond = addJob(f.Dana, danaHome, "confirmed", str("2026-11-10 09:00:00.123456+00"), false)
	f.Cancelled = addJob(f.Dana, danaHome, "cancelled", str("2026-11-11 10:00:00+00"), false)
	f.Deleted = addJob(f.Dana, danaHome, "confirmed", str("2026-11-12 10:00:00+00"), true)
	f.Unscheduled = addJob(f.Dana, danaHome, "unconfirmed", nil, false)
	f.EdgeLast = addJob(f.Dana, danaHome, "unconfirmed", str("2026-11-30 23:59:59.999999+00"), false)
	f.EdgeNext = addJob(f.Dana, danaHome, "unconfirmed", str("2026-12-01 00:00:00+00"), false)
	f.OtherJob = addJob(f.Other, otherHome, "confirmed", str("2026-11-09 08:00:00+00"), false)

	tho, thon := str("tho.nguyen.quang@example.com"), str("thon.nguyen@example.com")
	f.Tie1Gate = addCheck(f.TieFirst, f.Gate, str("2026-11-10 09:10:00+00"), str("2026-11-10 09:25:00.5+00"), "Good", "Inspected", "Bait taken, refilled", tho)
	f.Tie1NoName = addCheck(f.TieFirst, f.NoName, nil, nil, "40", "", "", nil)
	f.Tie2Beside = addCheck(f.TieSecond, f.Beside, str("2026-11-10 09:05:00+00"), nil, "10", "Placed", "Near the door", thon)
	f.CancelledGate = addCheck(f.Cancelled, f.Gate, nil, nil, "Good", "", "", tho)
	f.HiddenDeleted = addCheck(f.Deleted, f.Gate, nil, nil, "Good", "", "", tho)
	f.HiddenUnscheduled = addCheck(f.Unscheduled, f.Gate, nil, nil, "Good", "", "", tho)
	f.EdgeLastGate = addCheck(f.EdgeLast, f.Gate, nil, nil, "Good", "", "", nil)
	f.EdgeNextGate = addCheck(f.EdgeNext, f.Gate, nil, nil, "Good", "", "", nil)
	f.OtherCheck = addCheck(f.OtherJob, otherDevice, nil, nil, "Good", "", "", thon)
	return f
}
