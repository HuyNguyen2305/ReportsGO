-- Material Use: a catalog of materials (with the EPA registration number) and the materials added to each job.
-- Every job adds its own rows independently; the report adds them up per material. The report's date is the date the
-- job occurs (jobs.scheduled_at), so job_materials carries no date of its own. device is the equipment lent to the
-- customer on request, just another column of the row; NULL means no device was lent.

CREATE TABLE materials (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text NOT NULL UNIQUE,
    epa_reg_no text,
    unit       text NOT NULL DEFAULT ''
);

CREATE TABLE job_materials (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id      bigint NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    material_id bigint NOT NULL REFERENCES materials (id),
    qty         numeric(12, 3) NOT NULL CONSTRAINT job_materials_qty_chk CHECK (qty > 0),
    device      text,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX job_materials_job_id_idx ON job_materials (job_id);
CREATE INDEX job_materials_material_id_idx ON job_materials (material_id);
