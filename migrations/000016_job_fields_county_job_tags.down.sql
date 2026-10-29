DROP TABLE job_tag_links;
DROP TABLE job_tags;

DROP INDEX service_locations_state_city_postal_idx;
ALTER TABLE service_locations DROP COLUMN county;

DROP INDEX jobs_active_scheduled_idx;
DROP INDEX jobs_assigned_to_idx;
ALTER TABLE jobs DROP COLUMN tax_amount;
ALTER TABLE jobs DROP COLUMN in_work_pool;
ALTER TABLE jobs DROP COLUMN deleted_at;
ALTER TABLE jobs DROP COLUMN assigned_to;
