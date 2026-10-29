//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

func TestCustomersDefaultToActive(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)

	var status string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT status::text FROM customers WHERE id = $1`, s.customerID).Scan(&status))
	require.Equal(t, "active", status)
}

func TestCustomerStatusMustBeKnown(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	for _, status := range []string{"active", "inactive", "lead", "archived"} {
		_, err := pool.Exec(ctx, `UPDATE customers SET status = $1::customer_status WHERE id = $2`, status, s.customerID)
		require.NoError(t, err, status)
	}
	_, err := pool.Exec(ctx, `UPDATE customers SET status = 'banned' WHERE id = $1`, s.customerID)
	require.Error(t, err)
}

func TestPaymentMethodsAreValidated(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO customer_payment_methods (customer_id, processor, brand, last4, exp_month, exp_year)
		VALUES ($1, 'stripe', 'Visa', '4242', 12, 2033), ($1, 'square', 'Visa', '0009', 12, 2032)`, s.customerID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO customer_payment_methods (customer_id, processor, last4, exp_month, exp_year)
		VALUES ($1, 'paypal', '4242', 12, 2033)`, s.customerID)
	requireViolation(t, err, "23514", "customer_payment_methods_processor_chk")

	for _, month := range []int{0, 13} {
		_, err = pool.Exec(ctx, `INSERT INTO customer_payment_methods (customer_id, processor, last4, exp_month, exp_year)
			VALUES ($1, 'stripe', '4242', $2, 2033)`, s.customerID, month)
		requireViolation(t, err, "23514", "customer_payment_methods_exp_month_chk")
	}
}

func TestACustomerHasOneDefaultCard(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO customer_payment_methods (customer_id, processor, last4, exp_month, exp_year, is_default)
		VALUES ($1, 'stripe', '4242', 12, 2033, true), ($1, 'stripe', '3222', 12, 2032, false), ($1, 'stripe', '0005', 12, 2032, false)`, s.customerID)
	require.NoError(t, err, "any number of non-default cards")

	_, err = pool.Exec(ctx, `INSERT INTO customer_payment_methods (customer_id, processor, last4, exp_month, exp_year, is_default)
		VALUES ($1, 'stripe', '5556', 2, 2034, true)`, s.customerID)
	requireViolation(t, err, "23505", "customer_payment_methods_default_idx")
}

func TestCardsAreRemovedWithTheirCustomer(t *testing.T) {
	pool := testdb.MigratedPool(t)
	ctx := context.Background()

	var customerID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO customers (account_number, display_name) VALUES ('7001', 'Temp') RETURNING id`).Scan(&customerID))
	_, err := pool.Exec(ctx, `INSERT INTO customer_payment_methods (customer_id, processor, last4, exp_month, exp_year)
		VALUES ($1, 'stripe', '4242', 12, 2033)`, customerID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, customerID)
	require.NoError(t, err)

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM customer_payment_methods WHERE customer_id = $1`, customerID).Scan(&n))
	require.Zero(t, n)
}

func TestCreditsAreAppliedToInvoicesOnly(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO credit_applications (credit_id, invoice_id, amount, applied_on)
		VALUES ($1, $2, 40, '2026-10-26')`, s.creditID, s.invoiceID)
	require.NoError(t, err, "a credit may pay an invoice, in parts")
	_, err = pool.Exec(ctx, `INSERT INTO credit_applications (credit_id, invoice_id, amount, applied_on)
		VALUES ($1, $2, 25, '2026-10-27')`, s.creditID, s.invoiceID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO credit_applications (credit_id, invoice_id, amount, applied_on)
		VALUES ($1, $2, 10, '2026-10-26')`, s.creditID, s.estimateID)
	requireViolation(t, err, "23503", "credit_applications_invoice_id_fkey")
}

func TestAppliedAmountMustBePositive(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)

	for _, amount := range []float64{0, -5} {
		_, err := pool.Exec(context.Background(), `INSERT INTO credit_applications (credit_id, invoice_id, amount, applied_on)
			VALUES ($1, $2, $3, '2026-10-26')`, s.creditID, s.invoiceID, amount)
		requireViolation(t, err, "23514", "credit_applications_amount_chk")
	}
}

func TestAvailableCreditIsTheCreditMinusItsApplications(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO credit_applications (credit_id, invoice_id, amount, applied_on)
		VALUES ($1, $2, 40, '2026-10-20'), ($1, $2, 25.50, '2026-10-28')`, s.creditID, s.invoiceID)
	require.NoError(t, err)

	// the credit is 100.00; as of 2026-10-25 only the first application counts
	var now, asOf string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT (c.amount - COALESCE(SUM(a.amount), 0))::text,
		       (c.amount - COALESCE(SUM(a.amount) FILTER (WHERE a.applied_on <= '2026-10-25'), 0))::text
		FROM credits c LEFT JOIN credit_applications a ON a.credit_id = c.id
		WHERE c.id = $1 GROUP BY c.id`, s.creditID).Scan(&now, &asOf))
	require.Equal(t, "34.50", now)
	require.Equal(t, "60.00", asOf)
}
