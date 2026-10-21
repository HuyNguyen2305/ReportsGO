CREATE TABLE jobs (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id     bigint NOT NULL REFERENCES customers (id),
    location_id     bigint NOT NULL REFERENCES service_locations (id),
    service_type_id bigint NOT NULL REFERENCES service_types (id),
    lead_source_id  bigint REFERENCES lead_sources (id),
    status          job_status NOT NULL DEFAULT 'unconfirmed',
    scheduled_at    timestamptz,
    invoice_value   numeric(12, 2) NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX jobs_customer_id_idx ON jobs (customer_id);
CREATE INDEX jobs_location_id_idx ON jobs (location_id);
CREATE INDEX jobs_service_type_id_idx ON jobs (service_type_id);
CREATE INDEX jobs_lead_source_id_idx ON jobs (lead_source_id);
CREATE INDEX jobs_created_at_idx ON jobs (created_at, id);
CREATE INDEX jobs_status_idx ON jobs (status);
