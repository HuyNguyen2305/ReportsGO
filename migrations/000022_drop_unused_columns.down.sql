-- Restores the shape only; the dropped values are not recoverable.
ALTER TABLE invoice_items
    ADD COLUMN description text NOT NULL DEFAULT '',
    ADD COLUMN quantity numeric(12, 3) NOT NULL DEFAULT 1,
    ADD COLUMN unit_price numeric(12, 2) NOT NULL DEFAULT 0,
    ADD COLUMN taxable boolean NOT NULL DEFAULT true;
ALTER TABLE invoice_items ADD CONSTRAINT invoice_items_quantity_chk CHECK (quantity > 0);

ALTER TABLE items ADD COLUMN unit_price numeric(12, 2) NOT NULL DEFAULT 0;
ALTER TABLE items ADD COLUMN active boolean NOT NULL DEFAULT true;

ALTER TABLE billing_documents DROP CONSTRAINT billing_documents_estimate_chk;
ALTER TABLE billing_documents ADD COLUMN recurring_invoice_id bigint;
ALTER TABLE billing_documents
    ADD CONSTRAINT billing_documents_recurring_invoice_id_fkey
    FOREIGN KEY (recurring_invoice_id) REFERENCES recurring_invoices (id);
CREATE INDEX billing_documents_recurring_invoice_id_idx ON billing_documents (recurring_invoice_id);
ALTER TABLE billing_documents
    ADD CONSTRAINT billing_documents_estimate_chk CHECK (
        parent_type = 'invoice'
        OR (job_id IS NULL AND recurring_invoice_id IS NULL AND payment_terms_id IS NULL AND discount = 0));

ALTER TABLE customer_payment_methods ADD COLUMN brand text NOT NULL DEFAULT '';
ALTER TABLE job_materials ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE materials ADD COLUMN unit text NOT NULL DEFAULT '';
ALTER TABLE customer_phones ADD COLUMN kind text NOT NULL DEFAULT 'mobile';
ALTER TABLE customers ADD COLUMN billing_email text;
