ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_recurring_invoice_id_fkey;
DROP TABLE IF EXISTS recurring_invoices;
