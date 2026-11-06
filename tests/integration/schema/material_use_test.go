//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

func TestMaterialNamesAreUnique(t *testing.T) {
	pool := testdb.MigratedPool(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO materials (name, epa_reg_no) VALUES ('Demand CS', '100-1066')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO materials (name) VALUES ('Demand CS')`)
	requireViolation(t, err, "23505", "materials_name_key")
}

func TestJobMaterialRules(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	var materialID, jobID, locationID, serviceTypeID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT min(id) FROM service_locations`).Scan(&locationID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT min(id) FROM service_types`).Scan(&serviceTypeID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO materials (name) VALUES ('Glue Boards') RETURNING id`).Scan(&materialID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO jobs (customer_id, location_id, service_type_id, status) VALUES ($1, $2, $3, 'confirmed') RETURNING id`,
		s.customerID, locationID, serviceTypeID).Scan(&jobID))

	t.Run("a quantity must be positive", func(t *testing.T) {
		for _, qty := range []string{"0", "-1.5"} {
			_, err := pool.Exec(ctx, `INSERT INTO job_materials (job_id, material_id, qty) VALUES ($1, $2, $3::numeric)`, jobID, materialID, qty)
			requireViolation(t, err, "23514", "job_materials_qty_chk")
		}
	})

	t.Run("a job and a material must exist", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO job_materials (job_id, material_id, qty) VALUES (-1, $1, 1)`, materialID)
		requireViolation(t, err, "23503", "job_materials_job_id_fkey")
		_, err = pool.Exec(ctx, `INSERT INTO job_materials (job_id, material_id, qty) VALUES ($1, -1, 1)`, jobID)
		requireViolation(t, err, "23503", "job_materials_material_id_fkey")
	})

	t.Run("a job adds the same material more than once, each row on its own", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO job_materials (job_id, material_id, qty, device) VALUES ($1, $2, 1.5, NULL), ($1, $2, 2, 'Fogger')`, jobID, materialID)
		require.NoError(t, err)
	})

	t.Run("deleting a job removes its materials, not the catalog", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, jobID)
		require.NoError(t, err)

		var rows, materials int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM job_materials WHERE job_id = $1`, jobID).Scan(&rows))
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM materials WHERE id = $1`, materialID).Scan(&materials))
		require.Zero(t, rows)
		require.Equal(t, 1, materials)
	})

	t.Run("a material in use cannot be deleted", func(t *testing.T) {
		var other int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO jobs (customer_id, location_id, service_type_id, status) VALUES ($1, $2, $3, 'confirmed') RETURNING id`,
			s.customerID, locationID, serviceTypeID).Scan(&other))
		_, err := pool.Exec(ctx, `INSERT INTO job_materials (job_id, material_id, qty) VALUES ($1, $2, 1)`, other, materialID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `DELETE FROM materials WHERE id = $1`, materialID)
		requireViolation(t, err, "23503", "job_materials_material_id_fkey")
	})
}

func TestJobMaterialIndexesExist(t *testing.T) {
	pool := testdb.MigratedPool(t)

	for _, idx := range []string{"job_materials_job_id_idx", "job_materials_material_id_idx"} {
		var n int
		require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_indexes WHERE indexname = $1`, idx).Scan(&n))
		require.Equal(t, 1, n, idx)
	}
}
