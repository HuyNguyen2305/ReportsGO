-- Trigram indexes keep the report's case-insensitive substring search (ILIKE '%term%') fast.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX invoices_invoice_number_trgm_idx ON invoices USING gin (invoice_number gin_trgm_ops);
CREATE INDEX invoices_billing_email_trgm_idx ON invoices USING gin (billing_email gin_trgm_ops);
CREATE INDEX customers_display_name_trgm_idx ON customers USING gin (display_name gin_trgm_ops);
CREATE INDEX customers_account_number_trgm_idx ON customers USING gin (account_number gin_trgm_ops);
