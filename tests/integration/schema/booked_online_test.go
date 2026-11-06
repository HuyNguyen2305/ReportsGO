//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

func TestCustomerBookedOnlineDefaultsToFalse(t *testing.T) {
	pool := testdb.MigratedPool(t)
	ctx := context.Background()

	var online bool
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO customers (account_number, display_name) VALUES ('90001', 'Plain Customer') RETURNING booked_online`).Scan(&online))
	require.False(t, online, "a customer is not an online booking until it is flagged")

	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO customers (account_number, display_name, booked_online) VALUES ('90002', 'Online Customer', true) RETURNING booked_online`).Scan(&online))
	require.True(t, online)
}

func TestCustomerBookedOnlineCannotBeNull(t *testing.T) {
	pool := testdb.MigratedPool(t)

	_, err := pool.Exec(context.Background(),
		`INSERT INTO customers (account_number, display_name, booked_online) VALUES ('90003', 'Nobody Knows', NULL)`)

	require.Error(t, err)
}

func TestCustomerBookedOnlineIndexOnlyCoversFlaggedCustomers(t *testing.T) {
	pool := testdb.MigratedPool(t)

	var def string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE indexname = 'customers_booked_online_idx'`).Scan(&def))

	require.Contains(t, def, "WHERE booked_online", "a partial index: the report only looks at flagged customers")
}
