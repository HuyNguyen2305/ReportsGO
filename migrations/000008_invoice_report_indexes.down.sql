DROP INDEX IF EXISTS customers_account_number_trgm_idx;
DROP INDEX IF EXISTS customers_display_name_trgm_idx;
DROP INDEX IF EXISTS invoices_billing_email_trgm_idx;
DROP INDEX IF EXISTS invoices_invoice_number_trgm_idx;
-- The pg_trgm extension is intentionally left installed: other objects may depend on it.
