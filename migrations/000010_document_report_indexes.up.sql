-- Keeps the Documents report's case-insensitive substring search (ILIKE '%term%') on the document number fast.
-- pg_trgm is installed by migration 000008.
CREATE INDEX documents_document_number_trgm_idx ON documents USING gin (document_number gin_trgm_ops);
