-- Trigram indexes keep the new customers report's search over email and phone (ILIKE '%term%') fast.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX customers_email_trgm_idx ON customers USING gin (email gin_trgm_ops);
CREATE INDEX customer_phones_number_trgm_idx ON customer_phones USING gin (number gin_trgm_ops);
