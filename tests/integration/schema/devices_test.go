//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

func TestDeviceRules(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	var locationID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO service_locations (customer_id, address_line1) VALUES ($1, '1 Device Way') RETURNING id`, s.customerID).Scan(&locationID))

	add := func(lat, lng any) error {
		_, err := pool.Exec(ctx, `INSERT INTO devices (location_id, device_type, latitude, longitude) VALUES ($1, 'Bait Box', $2, $3)`, locationID, lat, lng)
		return err
	}

	t.Run("name, barcode and area default to empty and a coordinate is optional", func(t *testing.T) {
		var name, barcode, area string
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO devices (location_id, device_type) VALUES ($1, 'Laptop') RETURNING name, barcode, area`, locationID).Scan(&name, &barcode, &area))
		require.Equal(t, "", name)
		require.Equal(t, "", barcode)
		require.Equal(t, "", area)
	})

	t.Run("a device needs a type and an existing location", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO devices (location_id) VALUES ($1)`, locationID)
		require.Error(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO devices (location_id, device_type) VALUES (-1, 'Bait Box')`)
		requireViolation(t, err, "23503", "devices_location_id_fkey")
	})

	t.Run("a coordinate is both numbers or neither, within range", func(t *testing.T) {
		require.NoError(t, add(37.5, -122.1))
		require.NoError(t, add(nil, nil))
		requireViolation(t, add(37.5, nil), "23514", "devices_coordinate_chk")
		requireViolation(t, add(nil, -122.1), "23514", "devices_coordinate_chk")
		requireViolation(t, add(90.5, 0), "23514", "devices_latitude_chk")
		requireViolation(t, add(-90.5, 0), "23514", "devices_latitude_chk")
		requireViolation(t, add(0, 180.5), "23514", "devices_longitude_chk")
		requireViolation(t, add(0, -180.5), "23514", "devices_longitude_chk")
		require.NoError(t, add(90, 180), "the limits themselves are valid")
		require.NoError(t, add(-90, -180))
	})

	t.Run("a location removes its devices with it", func(t *testing.T) {
		var other, device int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO service_locations (customer_id, address_line1) VALUES ($1, '2 Gone Way') RETURNING id`, s.customerID).Scan(&other))
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO devices (location_id, device_type) VALUES ($1, 'Trap') RETURNING id`, other).Scan(&device))

		_, err := pool.Exec(ctx, `DELETE FROM service_locations WHERE id = $1`, other)
		require.NoError(t, err)

		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE id = $1`, device).Scan(&n))
		require.Zero(t, n)
	})
}

func TestJobDeviceCheckRules(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	var locationID, serviceTypeID, jobID, deviceID, userID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO service_locations (customer_id, address_line1) VALUES ($1, '1 Check Way') RETURNING id`, s.customerID).Scan(&locationID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT min(id) FROM service_types`).Scan(&serviceTypeID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO jobs (customer_id, location_id, service_type_id, status) VALUES ($1, $2, $3, 'confirmed') RETURNING id`,
		s.customerID, locationID, serviceTypeID).Scan(&jobID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO devices (location_id, device_type) VALUES ($1, 'Bait Box') RETURNING id`, locationID).Scan(&deviceID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT min(id) FROM users`).Scan(&userID))

	t.Run("status, activity and notes default to empty and nothing else is required", func(t *testing.T) {
		var status, activity, notes string
		var in, out, assignee *string
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO job_device_checks (job_id, device_id) VALUES ($1, $2)
			 RETURNING status, activity, notes, checked_in_at::text, checked_out_at::text, assigned_to::text`, jobID, deviceID).
			Scan(&status, &activity, &notes, &in, &out, &assignee))
		require.Equal(t, "", status)
		require.Equal(t, "", activity)
		require.Equal(t, "", notes)
		require.Nil(t, in)
		require.Nil(t, out)
		require.Nil(t, assignee)
	})

	t.Run("a check out cannot come before the check in", func(t *testing.T) {
		insert := func(in, out any) error {
			_, err := pool.Exec(ctx, `INSERT INTO job_device_checks (job_id, device_id, checked_in_at, checked_out_at) VALUES ($1, $2, $3, $4)`,
				jobID, deviceID, in, out)
			return err
		}
		require.NoError(t, insert("2026-11-10 09:00:00+00", "2026-11-10 09:00:00+00"), "the same instant is fine")
		require.NoError(t, insert("2026-11-10 09:00:00+00", nil), "checked in, not out yet")
		require.NoError(t, insert(nil, "2026-11-10 09:00:00+00"), "an out with no in is not contradicted")
		requireViolation(t, insert("2026-11-10 09:00:00+00", "2026-11-10 08:59:59+00"), "23514", "job_device_checks_times_chk")
	})

	t.Run("a check needs an existing job, device and assignee", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO job_device_checks (job_id, device_id) VALUES (-1, $1)`, deviceID)
		requireViolation(t, err, "23503", "job_device_checks_job_id_fkey")
		_, err = pool.Exec(ctx, `INSERT INTO job_device_checks (job_id, device_id) VALUES ($1, -1)`, jobID)
		requireViolation(t, err, "23503", "job_device_checks_device_id_fkey")
		_, err = pool.Exec(ctx, `INSERT INTO job_device_checks (job_id, device_id, assigned_to) VALUES ($1, $2, -1)`, jobID, deviceID)
		requireViolation(t, err, "23503", "job_device_checks_assigned_to_fkey")
		_, err = pool.Exec(ctx, `INSERT INTO job_device_checks (job_id, device_id, assigned_to) VALUES ($1, $2, $3)`, jobID, deviceID, userID)
		require.NoError(t, err)
	})

	t.Run("a device in use cannot be deleted", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM devices WHERE id = $1`, deviceID)
		requireViolation(t, err, "23503", "job_device_checks_device_id_fkey")
	})

	t.Run("deleting a job removes its checks, not the device", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, jobID)
		require.NoError(t, err)

		var checks, devices int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM job_device_checks WHERE job_id = $1`, jobID).Scan(&checks))
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE id = $1`, deviceID).Scan(&devices))
		require.Zero(t, checks)
		require.Equal(t, 1, devices)
	})
}

func TestDeviceIndexesExist(t *testing.T) {
	pool := testdb.MigratedPool(t)

	for _, idx := range []string{
		"devices_location_id_idx", "job_device_checks_job_id_idx", "job_device_checks_device_id_idx", "job_device_checks_assigned_to_idx",
	} {
		var n int
		require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_indexes WHERE indexname = $1`, idx).Scan(&n))
		require.Equal(t, 1, n, idx)
	}
}
