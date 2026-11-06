//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

func TestSystemEmailRules(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	t.Run("status defaults to unconfirmed and the time stamp to now", func(t *testing.T) {
		var status string
		var recent bool
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO system_emails (customer_id, email, email_type) VALUES ($1, 'a@example.com', 'welcome')
			 RETURNING status::text, sent_at > now() - interval '1 minute'`, s.customerID).Scan(&status, &recent))
		require.Equal(t, "unconfirmed", status)
		require.True(t, recent)
	})

	t.Run("status and type are closed sets", func(t *testing.T) {
		for _, tt := range []struct{ typ, status string }{{"newsletter", "confirmed"}, {"welcome", "sent"}} {
			_, err := pool.Exec(ctx,
				`INSERT INTO system_emails (customer_id, email, email_type, status) VALUES ($1, 'a@example.com', $2, $3)`,
				s.customerID, tt.typ, tt.status)
			require.Error(t, err, "%s / %s", tt.typ, tt.status)
		}
	})

	t.Run("every status and type is accepted", func(t *testing.T) {
		for _, status := range []string{"unconfirmed", "confirmed", "cancelled"} {
			for _, typ := range []string{
				"welcome", "invoice", "estimate", "appointment_confirmation", "appointment_reminder", "payment_receipt",
				"review_request",
			} {
				_, err := pool.Exec(ctx,
					`INSERT INTO system_emails (customer_id, email, email_type, status) VALUES ($1, 'a@example.com', $2, $3)`,
					s.customerID, typ, status)
				require.NoError(t, err, "%s / %s", typ, status)
			}
		}
	})

	t.Run("an email needs a customer and an address", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO system_emails (customer_id, email, email_type) VALUES (-1, 'a@example.com', 'welcome')`)
		requireViolation(t, err, "23503", "system_emails_customer_id_fkey")
		_, err = pool.Exec(ctx, `INSERT INTO system_emails (customer_id, email_type) VALUES ($1, 'welcome')`, s.customerID)
		require.Error(t, err)
	})

	t.Run("deleting a customer removes its emails", func(t *testing.T) {
		var customerID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO customers (account_number, display_name) VALUES ('99999', 'Gone Soon') RETURNING id`).Scan(&customerID))
		_, err := pool.Exec(ctx, `INSERT INTO system_emails (customer_id, email, email_type) VALUES ($1, 'g@example.com', 'welcome')`, customerID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, customerID)
		require.NoError(t, err)

		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM system_emails WHERE customer_id = $1`, customerID).Scan(&n))
		require.Zero(t, n)
	})
}

func TestSystemEmailIndexesExist(t *testing.T) {
	pool := testdb.MigratedPool(t)

	for _, idx := range []string{"system_emails_customer_id_idx", "system_emails_sent_at_idx", "system_emails_status_idx"} {
		var n int
		require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_indexes WHERE indexname = $1`, idx).Scan(&n))
		require.Equal(t, 1, n, idx)
	}
}
