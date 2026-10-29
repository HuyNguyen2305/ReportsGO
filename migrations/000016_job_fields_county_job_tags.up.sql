-- Service Lookup: the Active / Work Pool / Deleted tabs, "Assigned To", County, W/O Tax and Tags, plus the
-- state / city / zip filters. Job W/O Tax is invoice_value - tax_amount; it is stored on the job because a job
-- may have no invoice yet.

ALTER TABLE jobs ADD COLUMN assigned_to  bigint REFERENCES users (id);
ALTER TABLE jobs ADD COLUMN deleted_at   timestamptz;
ALTER TABLE jobs ADD COLUMN in_work_pool boolean NOT NULL DEFAULT false;
ALTER TABLE jobs ADD COLUMN tax_amount   numeric(12, 2) NOT NULL DEFAULT 0
    CONSTRAINT jobs_tax_amount_chk CHECK (tax_amount >= 0);

CREATE INDEX jobs_assigned_to_idx ON jobs (assigned_to);
-- The Active tab: scheduled, not deleted, not in the work pool.
CREATE INDEX jobs_active_scheduled_idx ON jobs (scheduled_at, id)
    WHERE deleted_at IS NULL AND NOT in_work_pool;

ALTER TABLE service_locations ADD COLUMN county text NOT NULL DEFAULT '';
CREATE INDEX service_locations_state_city_postal_idx ON service_locations (state, city, postal_code);

CREATE TABLE job_tags (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE job_tag_links (
    job_id bigint NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    tag_id bigint NOT NULL REFERENCES job_tags (id),
    PRIMARY KEY (job_id, tag_id)
);
CREATE INDEX job_tag_links_tag_id_idx ON job_tag_links (tag_id);
