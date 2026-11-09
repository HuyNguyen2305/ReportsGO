//go:build integration

package testdb

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// InboundLeadFixture holds the ids of the leads InsertInboundLeads adds on top of the seeded lead.
type InboundLeadFixture struct {
	// QA is the seeded lead 63730 (QA customer): added on October 8, no source, email, phone or note.
	QA int64
	// Mia has a source (Google), an email, two phones and a note; added on October 20.
	Mia int64
	// Cold has nothing but a name and no first name; added on November 3 at 09:15:30.123456 UTC.
	Cold int64
	// Noah has a source (Facebook), an email, a note and several jobs; added at the very same instant as Cold.
	Noah int64
	// NoahOpenJob is Noah's earliest open scheduled job (confirmed, November 12 at 09:00 UTC) and NoahLaterJob the
	// next one (unconfirmed, November 20), which only counts once the first is gone.
	NoahOpenJob, NoahLaterJob int64
	// NotALead is a customer who is not a lead and must never be listed.
	NotALead int64
}

// InsertInboundLeads adds the leads the Inbound Leads tests need to a seeded database. The seed itself only has one
// lead, so the cases for sources, notes, phones, name formats, ties and active jobs are built here, where they cannot
// change what other reports see.
func InsertInboundLeads(t *testing.T, pool *pgxpool.Pool) InboundLeadFixture {
	t.Helper()
	ctx := context.Background()

	var f InboundLeadFixture
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM customers WHERE account_number = '63730'`).Scan(&f.QA))

	addCustomer := func(dst *int64, acct, first, last, display string, email *string, source *string, note, status, createdAt string) {
		t.Helper()
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO customers (account_number, first_name, last_name, display_name, email, lead_source_id, top_note, status, created_at)
			VALUES ($1, $2, $3, $4, $5, (SELECT id FROM lead_sources WHERE name = $6), $7, $8::customer_status, $9::timestamptz)
			RETURNING id`, acct, first, last, display, email, source, note, status, createdAt).Scan(dst))
	}
	str := func(s string) *string { return &s }

	addCustomer(&f.Mia, "80001", "Mia", "Torres", "Mia Torres", str("mia.torres@example.com"), str("Google"),
		"Asked for a quote", "lead", "2026-10-20 10:00:00+00")
	addCustomer(&f.Cold, "80002", "", "Cold Call Co", "Cold Call Co", nil, nil, "", "lead", "2026-11-03 09:15:30.123456+00")
	addCustomer(&f.Noah, "80003", "Noah", "Berg", "Noah Berg", str("noah.berg@example.com"), str("Facebook"),
		"Wants a Bi-Monthly Service", "lead", "2026-11-03 09:15:30.123456+00")
	addCustomer(&f.NotALead, "80004", "Active", "Person", "Active Person", str("active@example.com"), str("Google"),
		"", "active", "2026-11-03 10:00:00+00")

	// Mia's first phone is the one added first, whatever its number says.
	_, err := pool.Exec(ctx, `
		INSERT INTO customer_phones (customer_id, number) VALUES
		($1, '(555) 010-0001'), ($1, '(555) 010-9999'), ($2, '(555) 010-0003')`, f.Mia, f.Noah)
	require.NoError(t, err)

	var locationID, serviceTypeID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO service_locations (customer_id, address_line1, city, state, postal_code) VALUES ($1, '3 Lead Lane', 'Austin', 'TX', '73301') RETURNING id`,
		f.Noah).Scan(&locationID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM service_types ORDER BY id LIMIT 1`).Scan(&serviceTypeID))

	addJob := func(status string, scheduledAt *string, deleted bool) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, deleted_at)
			VALUES ($1, $2, $3, $4::job_status, $5::timestamptz, CASE WHEN $6 THEN now() END)
			RETURNING id`, f.Noah, locationID, serviceTypeID, status, scheduledAt, deleted).Scan(&id))
		return id
	}
	// Jobs that must never be the active job: cancelled, deleted, completed and never scheduled.
	addJob("cancelled", str("2026-11-05 10:00:00+00"), false)
	addJob("unconfirmed", str("2026-11-04 10:00:00+00"), true)
	addJob("completed", str("2026-11-01 08:00:00+00"), false)
	addJob("confirmed", nil, false)
	f.NoahLaterJob = addJob("unconfirmed", str("2026-11-20 09:00:00+00"), false)
	f.NoahOpenJob = addJob("confirmed", str("2026-11-12 09:00:00+00"), false)

	// The customer who is not a lead has an open job too, which must not make them one.
	_, err = pool.Exec(ctx, `
		INSERT INTO service_locations (customer_id, address_line1, city, state, postal_code) VALUES ($1, '4 Active Ave', 'Austin', 'TX', '73301')`,
		f.NotALead)
	require.NoError(t, err)
	return f
}
