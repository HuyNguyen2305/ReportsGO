-- The New Jobs report leaves deleted jobs out; this keeps its default page (newest first) an index scan.
CREATE INDEX jobs_live_created_at_idx ON jobs (created_at, id) WHERE deleted_at IS NULL;
