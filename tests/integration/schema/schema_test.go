//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

func TestMigrationsRoundTrip(t *testing.T) {
	dbURL := testdb.NewDatabase(t)
	m := testdb.NewMigrator(t, dbURL)

	require.NoError(t, m.Up())
	require.NoError(t, m.Down())
	require.NoError(t, m.Up())
}

// seed inserts one row in every table and returns the ids needed by other tests.
type seeded struct {
	customerID, invoiceID, estimateID, creditID int64
}

func seed(t *testing.T, pool *pgxpool.Pool) seeded {
	t.Helper()
	ctx := context.Background()

	var s seeded
	var userID, sourceID, serviceID, termsID, tagID, docTypeID, locationID, jobID, recurringID int64

	insert := func(dst *int64, sql string, args ...any) {
		t.Helper()
		require.NoError(t, pool.QueryRow(ctx, sql+" RETURNING id", args...).Scan(dst))
	}

	insert(&userID, `INSERT INTO users (full_name, email) VALUES ('Tho Nguyen', 'tho@example.com')`)
	insert(&sourceID, `INSERT INTO lead_sources (name) VALUES ('Facebook')`)
	insert(&serviceID, `INSERT INTO service_types (name) VALUES ('Bi-Monthly Service')`)
	insert(&termsID, `INSERT INTO payment_terms (name, description) VALUES ('Due on receipt', 'Payment is due on receipt.')`)
	insert(&tagID, `INSERT INTO invoice_tags (name) VALUES ('a')`)
	insert(&docTypeID, `INSERT INTO document_types (name) VALUES ('1-Time Treatment')`)

	insert(&s.customerID, `INSERT INTO customers (account_number, display_name, lead_source_id) VALUES ('5001', 'William Saliba', $1)`, sourceID)
	_, err := pool.Exec(ctx, `INSERT INTO customer_phones (customer_id, number) VALUES ($1, '012222222')`, s.customerID)
	require.NoError(t, err)
	insert(&locationID, `INSERT INTO service_locations (customer_id, address_line1) VALUES ($1, 'West Loop 1604 South')`, s.customerID)
	insert(&jobID, `INSERT INTO jobs (customer_id, location_id, service_type_id, invoice_value) VALUES ($1, $2, $3, 65)`, s.customerID, locationID, serviceID)

	insert(&recurringID, `INSERT INTO recurring_invoices (customer_id, job_id, frequency_unit, weekday, occurrences, total_value, started_on)
		VALUES ($1, $2, 'week', 4, 3, 65, '2026-09-14')`, s.customerID, jobID)
	insert(&s.invoiceID, `INSERT INTO billing_documents (document_number, customer_id, job_id, recurring_invoice_id, document_date, total, payment_terms_id, created_by)
		VALUES ('84936', $1, $2, $3, '2026-10-26', 65, $4, $5)`, s.customerID, jobID, recurringID, termsID, userID)
	_, err = pool.Exec(ctx, `INSERT INTO invoice_tag_links (invoice_id, tag_id) VALUES ($1, $2)`, s.invoiceID, tagID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE recurring_invoices SET next_invoice_id = $1, next_invoice_date = '2026-10-29' WHERE id = $2`, s.invoiceID, recurringID)
	require.NoError(t, err)

	insert(&s.estimateID, `INSERT INTO billing_documents (parent_type, document_number, customer_id, created_by, document_date, total)
		VALUES ('estimate', '123187', $1, $2, '2026-10-20', 24.40)`, s.customerID, userID)
	require.NotZero(t, s.estimateID)
	_, err = pool.Exec(ctx, `INSERT INTO documents (document_number, document_type_id, customer_id, created_by) VALUES ('5979', $1, $2, $3)`,
		docTypeID, s.customerID, userID)
	require.NoError(t, err)

	insert(&s.creditID, `INSERT INTO credits (customer_id, amount, method_type, credit_date) VALUES ($1, 100, 'check', '2026-10-15')`, s.customerID)
	return s
}

func TestSchemaAcceptsOneRowPerTable(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)

	_, err := pool.Exec(context.Background(),
		`INSERT INTO payments (customer_id, invoice_id, method_type, card_brand, card_last4, amount, payment_date)
		 VALUES ($1, $2, 'card', 'Credit', '4242', 65, '2026-10-16')`, s.customerID, s.invoiceID)
	require.NoError(t, err)
}

func TestPaymentsTargetConstraint(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)

	tests := []struct {
		name      string
		invoiceID *int64
		creditID  *int64
		wantErr   bool
	}{
		{name: "invoice only", invoiceID: &s.invoiceID},
		{name: "credit only", creditID: &s.creditID},
		{name: "neither", wantErr: true},
		{name: "both", invoiceID: &s.invoiceID, creditID: &s.creditID, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(context.Background(),
				`INSERT INTO payments (customer_id, invoice_id, credit_id, method_type, amount, payment_date)
				 VALUES ($1, $2, $3, 'cash', 10, '2026-10-16')`, s.customerID, tt.invoiceID, tt.creditID)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23514", pgErr.Code) // check_violation
			require.Equal(t, "payments_target_chk", pgErr.ConstraintName)
		})
	}
}

func TestUniqueConstraints(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)

	tests := []struct {
		name string
		sql  string
		args []any
	}{
		{"account number", `INSERT INTO customers (account_number, display_name) VALUES ('5001', 'Dup')`, nil},
		{"invoice number", `INSERT INTO billing_documents (document_number, customer_id, document_date) VALUES ('84936', $1, '2026-10-01')`, []any{s.customerID}},
		{"estimate number", `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date) VALUES ('estimate', '123187', $1, '2026-10-01')`, []any{s.customerID}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(context.Background(), tt.sql, tt.args...)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23505", pgErr.Code) // unique_violation
		})
	}
}
