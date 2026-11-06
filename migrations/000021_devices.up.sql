-- Device Tracking: the devices installed at a customer's service location (a bait box, a trap...), which stay the same, and
-- the checks made on them during a job visit, one row per device checked. This is not job_materials.device, which is the
-- free-text equipment lent to a customer. status, activity and notes are free text, as the screen shows values such as
-- Good, 40 and 10. A check is made by a staff member, who may not be recorded.

CREATE TABLE devices (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    location_id bigint NOT NULL REFERENCES service_locations (id) ON DELETE CASCADE,
    name        text NOT NULL DEFAULT '',
    barcode     text NOT NULL DEFAULT '',
    device_type text NOT NULL,
    area        text NOT NULL DEFAULT '',
    latitude    numeric(9, 6) CONSTRAINT devices_latitude_chk CHECK (latitude BETWEEN -90 AND 90),
    longitude   numeric(9, 6) CONSTRAINT devices_longitude_chk CHECK (longitude BETWEEN -180 AND 180),
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- A coordinate is both numbers or neither.
    CONSTRAINT devices_coordinate_chk CHECK ((latitude IS NULL) = (longitude IS NULL))
);
CREATE INDEX devices_location_id_idx ON devices (location_id);

CREATE TABLE job_device_checks (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id         bigint NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    device_id      bigint NOT NULL REFERENCES devices (id),
    checked_in_at  timestamptz,
    checked_out_at timestamptz,
    status         text NOT NULL DEFAULT '',
    activity       text NOT NULL DEFAULT '',
    notes          text NOT NULL DEFAULT '',
    assigned_to    bigint REFERENCES users (id),
    CONSTRAINT job_device_checks_times_chk CHECK (checked_out_at IS NULL OR checked_in_at IS NULL OR checked_out_at >= checked_in_at)
);
CREATE INDEX job_device_checks_job_id_idx ON job_device_checks (job_id);
CREATE INDEX job_device_checks_device_id_idx ON job_device_checks (device_id);
CREATE INDEX job_device_checks_assigned_to_idx ON job_device_checks (assigned_to);
