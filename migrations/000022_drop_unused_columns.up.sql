-- Drop columns that no report reads. Dropping a column also drops the indexes and check constraints that name it,
-- so the estimate rule (which mentions recurring_invoice_id) is rebuilt without it.
ALTER TABLE customers DROP COLUMN billing_email;
ALTER TABLE customer_phones DROP COLUMN kind;
ALTER TABLE materials DROP COLUMN unit;
ALTER TABLE job_materials DROP COLUMN created_at;
ALTER TABLE customer_payment_methods DROP COLUMN brand;

ALTER TABLE billing_documents DROP CONSTRAINT billing_documents_estimate_chk;
ALTER TABLE billing_documents DROP CONSTRAINT billing_documents_recurring_invoice_id_fkey;
DROP INDEX billing_documents_recurring_invoice_id_idx;
ALTER TABLE billing_documents DROP COLUMN recurring_invoice_id;
ALTER TABLE billing_documents
    ADD CONSTRAINT billing_documents_estimate_chk CHECK (
        parent_type = 'invoice'
        OR (job_id IS NULL AND payment_terms_id IS NULL AND discount = 0));

ALTER TABLE items DROP COLUMN unit_price;
ALTER TABLE items DROP COLUMN active;

ALTER TABLE invoice_items DROP CONSTRAINT invoice_items_quantity_chk;
ALTER TABLE invoice_items
    DROP COLUMN description,
    DROP COLUMN quantity,
    DROP COLUMN unit_price,
    DROP COLUMN taxable;
