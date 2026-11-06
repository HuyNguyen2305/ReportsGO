//go:build integration

package testdb

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// OnlineBookingFixture holds the ids InsertOnlineBookings adds on top of the seed, whose only change for this report is
// the booked_online flag on customers 63714, 63689, 63716 and 63749.
type OnlineBookingFixture struct {
	// Olive is an online customer from Google with an email, two phones and a note.
	Olive int64
	// Nina is an online customer with no first name, source, email or phone.
	Nina int64
	// Offline is a customer who did not come from an online booking, from Google as well.
	Offline int64

	// Olive's jobs. TieFirst and TieSecond are confirmed at the very same instant, November 10 at 09:00:00.123456 UTC.
	// Cancelled is listed. Deleted and Unscheduled never are. EdgeLast is the last microsecond of November and EdgeNext
	// the first instant of December.
	TieFirst, TieSecond, Cancelled, Deleted, Unscheduled, EdgeLast, EdgeNext int64
	// NinaJob is Nina's job on November 9 at 08:00 UTC and OfflineJob the offline customer's job on November 10.
	NinaJob, OfflineJob int64

	// SeededFlagged are the customers the seed flags: 63714 (Facebook), 63689, 63716 and 63749 (no source).
	SeededFlagged []int64
}

// InsertOnlineBookings adds the customers and jobs the Online Bookings tests need to a seeded database, where the cases
// for ties, cancelled, deleted and unscheduled jobs, range edges, sources and names are built so they cannot change what
// other reports see.
func InsertOnlineBookings(t *testing.T, pool *pgxpool.Pool) OnlineBookingFixture {
	t.Helper()
	ctx := context.Background()
	var f OnlineBookingFixture

	rows, err := pool.Query(ctx, `SELECT id FROM customers WHERE booked_online ORDER BY id`)
	require.NoError(t, err)
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		f.SeededFlagged = append(f.SeededFlagged, id)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Len(t, f.SeededFlagged, 4, "the seed flags four customers")

	str := func(s string) *string { return &s }
	addCustomer := func(dst *int64, acct, first, last, display string, email, source *string, note string, online bool) {
		t.Helper()
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO customers (account_number, first_name, last_name, display_name, email, lead_source_id, top_note, booked_online)
			VALUES ($1, $2, $3, $4, $5, (SELECT id FROM lead_sources WHERE name = $6), $7, $8)
			RETURNING id`, acct, first, last, display, email, source, note, online).Scan(dst))
	}
	addLocation := func(customerID int64, line1, city string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO service_locations (customer_id, address_line1, city, state, postal_code, county)
			VALUES ($1, $2, $3, 'TX', '73301', 'Travis') RETURNING id`, customerID, line1, city).Scan(&id))
		return id
	}
	serviceType := func(name string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM service_types WHERE name = $1`, name).Scan(&id))
		return id
	}
	biMonthly, initial := serviceType("Bi-Monthly Service"), serviceType("Initial Service")
	addJob := func(customerID, locationID, serviceTypeID int64, status string, scheduledAt *string, deleted bool) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, deleted_at)
			VALUES ($1, $2, $3, $4::job_status, $5::timestamptz, CASE WHEN $6 THEN now() END)
			RETURNING id`, customerID, locationID, serviceTypeID, status, scheduledAt, deleted).Scan(&id))
		return id
	}

	addCustomer(&f.Olive, "80011", "Olive", "Ng", "Olive Ng", str("olive.ng@example.com"), str("Google"), "Gate code 1234", true)
	addCustomer(&f.Nina, "80012", "", "Nosource", "Nina Nosource", nil, nil, "", true)
	addCustomer(&f.Offline, "80013", "Ollie", "Offline", "Ollie Offline", str("ollie@example.com"), str("Google"), "Walk-in", false)

	// Olive's first phone is the one added first, whatever its number says.
	_, err = pool.Exec(ctx, `INSERT INTO customer_phones (customer_id, number, kind) VALUES ($1, '(555) 010-0011', 'mobile'), ($1, '(555) 010-9911', 'home')`, f.Olive)
	require.NoError(t, err)

	oliveHome := addLocation(f.Olive, "11 Olive Way", "Round Rock")
	ninaHome := addLocation(f.Nina, "12 Nina Road", "Cedar Park")
	offlineHome := addLocation(f.Offline, "13 Offline Ave", "Pflugerville")

	f.TieFirst = addJob(f.Olive, oliveHome, biMonthly, "confirmed", str("2026-11-10 09:00:00.123456+00"), false)
	f.TieSecond = addJob(f.Olive, oliveHome, biMonthly, "confirmed", str("2026-11-10 09:00:00.123456+00"), false)
	f.Cancelled = addJob(f.Olive, oliveHome, initial, "cancelled", str("2026-11-11 10:00:00+00"), false)
	f.Deleted = addJob(f.Olive, oliveHome, initial, "confirmed", str("2026-11-12 10:00:00+00"), true)
	f.Unscheduled = addJob(f.Olive, oliveHome, initial, "unconfirmed", nil, false)
	f.EdgeLast = addJob(f.Olive, oliveHome, biMonthly, "unconfirmed", str("2026-11-30 23:59:59.999999+00"), false)
	f.EdgeNext = addJob(f.Olive, oliveHome, biMonthly, "unconfirmed", str("2026-12-01 00:00:00+00"), false)
	f.NinaJob = addJob(f.Nina, ninaHome, initial, "unconfirmed", str("2026-11-09 08:00:00+00"), false)
	f.OfflineJob = addJob(f.Offline, offlineHome, biMonthly, "confirmed", str("2026-11-10 09:00:00.123456+00"), false)
	return f
}
