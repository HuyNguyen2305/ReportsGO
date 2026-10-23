DROP INDEX IF EXISTS customer_phones_number_trgm_idx;
DROP INDEX IF EXISTS customers_email_trgm_idx;
-- The pg_trgm extension is intentionally left installed: other objects may depend on it.
