//go:build integration

package schema_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/tests/integration/testdb"
)

// seededJobID returns the job created by seed.
func seededJobID(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT id FROM jobs LIMIT 1`).Scan(&id))
	return id
}

func TestJobsDefaultToTheActiveTab(t *testing.T) {
	pool := testdb.MigratedPool(t)
	seed(t, pool)

	var inPool bool
	var deletedAt, assignedTo *string
	var tax string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT in_work_pool, deleted_at::text, assigned_to::text, tax_amount::text FROM jobs WHERE id = $1`,
		seededJobID(t, pool)).Scan(&inPool, &deletedAt, &assignedTo, &tax))

	require.False(t, inPool)
	require.Nil(t, deletedAt)
	require.Nil(t, assignedTo)
	require.Equal(t, "0.00", tax)
}

func TestJobsCanBeAssignedToStaff(t *testing.T) {
	pool := testdb.MigratedPool(t)
	seed(t, pool)
	ctx := context.Background()
	jobID := seededJobID(t, pool)

	var userID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM users LIMIT 1`).Scan(&userID))
	_, err := pool.Exec(ctx, `UPDATE jobs SET assigned_to = $1 WHERE id = $2`, userID, jobID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE jobs SET assigned_to = 999999 WHERE id = $1`, jobID)
	requireViolation(t, err, "23503", "jobs_assigned_to_fkey")
}

func TestJobTaxCannotBeNegative(t *testing.T) {
	pool := testdb.MigratedPool(t)
	seed(t, pool)

	_, err := pool.Exec(context.Background(), `UPDATE jobs SET tax_amount = -0.01 WHERE id = $1`, seededJobID(t, pool))
	requireViolation(t, err, "23514", "jobs_tax_amount_chk")
}

func TestLocationsHaveACounty(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()

	var county string
	require.NoError(t, pool.QueryRow(ctx, `SELECT county FROM service_locations WHERE customer_id = $1`, s.customerID).Scan(&county))
	require.Empty(t, county, "a county is optional")

	_, err := pool.Exec(ctx, `UPDATE service_locations SET county = 'Bexar County' WHERE customer_id = $1`, s.customerID)
	require.NoError(t, err)
}

func TestJobTagNamesAreUnique(t *testing.T) {
	pool := testdb.MigratedPool(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO job_tags (name) VALUES ('VIP')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO job_tags (name) VALUES ('VIP')`)
	requireViolation(t, err, "23505", "job_tags_name_key")
}

func TestJobTagLinksAreUniqueAndFollowTheJob(t *testing.T) {
	pool := testdb.MigratedPool(t)
	s := seed(t, pool)
	ctx := context.Background()
	jobID := seededJobID(t, pool)

	var tagID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO job_tags (name) VALUES ('VIP') RETURNING id`).Scan(&tagID))
	_, err := pool.Exec(ctx, `INSERT INTO job_tag_links (job_id, tag_id) VALUES ($1, $2)`, jobID, tagID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO job_tag_links (job_id, tag_id) VALUES ($1, $2)`, jobID, tagID)
	requireViolation(t, err, "23505", "job_tag_links_pkey")

	// a job with no invoice can be removed together with its links
	var loneJob, locationID, serviceID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM service_locations WHERE customer_id = $1`, s.customerID).Scan(&locationID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM service_types LIMIT 1`).Scan(&serviceID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO jobs (customer_id, location_id, service_type_id) VALUES ($1, $2, $3) RETURNING id`,
		s.customerID, locationID, serviceID).Scan(&loneJob))
	_, err = pool.Exec(ctx, `INSERT INTO job_tag_links (job_id, tag_id) VALUES ($1, $2)`, loneJob, tagID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, loneJob)
	require.NoError(t, err)

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM job_tag_links WHERE job_id = $1`, loneJob).Scan(&n))
	require.Zero(t, n)
}

func TestUsedJobTagsCannotBeDeleted(t *testing.T) {
	pool := testdb.MigratedPool(t)
	seed(t, pool)
	ctx := context.Background()

	var tagID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO job_tags (name) VALUES ('VIP') RETURNING id`).Scan(&tagID))
	_, err := pool.Exec(ctx, `INSERT INTO job_tag_links (job_id, tag_id) VALUES ($1, $2)`, seededJobID(t, pool), tagID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM job_tags WHERE id = $1`, tagID)
	requireViolation(t, err, "23503", "job_tag_links_tag_id_fkey")
}

func TestServiceLookupFiltersHaveIndexes(t *testing.T) {
	pool := testdb.MigratedPool(t)

	for _, name := range []string{
		"jobs_assigned_to_idx", "jobs_active_scheduled_idx", "service_locations_state_city_postal_idx", "job_tag_links_tag_id_idx",
	} {
		var n int
		require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_indexes WHERE indexname = $1`, name).Scan(&n))
		require.Equal(t, 1, n, name)
	}
}
