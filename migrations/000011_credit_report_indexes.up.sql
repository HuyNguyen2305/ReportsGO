-- Keeps the Credits report's case-insensitive substring search (ILIKE '%term%') on the method detail fast.
-- pg_trgm is installed by migration 000008.
CREATE INDEX credits_method_detail_trgm_idx ON credits USING gin (method_detail gin_trgm_ops);
