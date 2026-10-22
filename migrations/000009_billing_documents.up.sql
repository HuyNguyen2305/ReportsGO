-- Invoices and estimates become one table, told apart by parent_type.
-- A new status enum is used (rather than ALTER TYPE ... ADD VALUE) because a value added to an enum cannot be
-- used in the same transaction, and this migration both adds values and uses them.

CREATE TYPE document_parent_type AS ENUM ('invoice', 'estimate');
CREATE TYPE billing_status AS ENUM ('draft', 'sent', 'paid', 'void', 'accepted', 'declined');

ALTER TABLE invoices RENAME TO billing_documents;
ALTER TABLE billing_documents RENAME COLUMN invoice_number TO document_number;
ALTER TABLE billing_documents RENAME COLUMN invoice_date TO document_date;

ALTER INDEX invoices_pkey RENAME TO billing_documents_pkey;
ALTER INDEX invoices_customer_id_idx RENAME TO billing_documents_customer_id_idx;
ALTER INDEX invoices_job_id_idx RENAME TO billing_documents_job_id_idx;
ALTER INDEX invoices_recurring_invoice_id_idx RENAME TO billing_documents_recurring_invoice_id_idx;
ALTER INDEX invoices_payment_terms_id_idx RENAME TO billing_documents_payment_terms_id_idx;
ALTER INDEX invoices_created_by_idx RENAME TO billing_documents_created_by_idx;
ALTER INDEX invoices_status_idx RENAME TO billing_documents_status_idx;
ALTER INDEX invoices_invoice_number_trgm_idx RENAME TO billing_documents_number_trgm_idx;
ALTER INDEX invoices_billing_email_trgm_idx RENAME TO billing_documents_billing_email_trgm_idx;
ALTER TABLE billing_documents
    RENAME CONSTRAINT invoices_recurring_invoice_id_fkey TO billing_documents_recurring_invoice_id_fkey;

-- Existing rows are invoices.
ALTER TABLE billing_documents ADD COLUMN parent_type document_parent_type NOT NULL DEFAULT 'invoice';

ALTER TABLE billing_documents ALTER COLUMN status DROP DEFAULT;
ALTER TABLE billing_documents
    ALTER COLUMN status TYPE billing_status USING status::text::billing_status;
ALTER TABLE billing_documents ALTER COLUMN status SET DEFAULT 'draft';

-- An estimate and an invoice may share a number; two documents of one type may not.
ALTER TABLE billing_documents DROP CONSTRAINT invoices_invoice_number_key;
ALTER TABLE billing_documents
    ADD CONSTRAINT billing_documents_parent_number_key UNIQUE (parent_type, document_number);

-- Move the estimates in, oldest first so their relative id order is kept.
INSERT INTO billing_documents (parent_type, document_number, customer_id, billing_email, created_by,
                               document_date, total, status, signature_status, created_at, archived_at, deleted_at)
SELECT 'estimate', estimate_number, customer_id, billing_email, created_by,
       estimate_date, total, status::text::billing_status, signature_status, created_at, archived_at, deleted_at
FROM estimates
ORDER BY id;

DROP TABLE estimates;
DROP TYPE estimate_status;
DROP TYPE invoice_status;

-- Each type keeps its own statuses, and estimates have no invoice-only data.
ALTER TABLE billing_documents
    ADD CONSTRAINT billing_documents_status_chk CHECK (
        (parent_type = 'invoice' AND status IN ('draft', 'sent', 'paid', 'void'))
        OR (parent_type = 'estimate' AND status IN ('draft', 'sent', 'accepted', 'declined'))),
    ADD CONSTRAINT billing_documents_estimate_chk CHECK (
        parent_type = 'invoice'
        OR (job_id IS NULL AND recurring_invoice_id IS NULL AND payment_terms_id IS NULL AND discount = 0));

-- Payments, recurring invoices and tags can only point at invoices. The generated column pins the referenced
-- type to 'invoice' and the composite foreign key enforces it.
ALTER TABLE billing_documents
    ADD CONSTRAINT billing_documents_id_parent_type_key UNIQUE (id, parent_type);

ALTER TABLE payments
    ADD COLUMN invoice_parent_type document_parent_type
        GENERATED ALWAYS AS (CASE WHEN invoice_id IS NULL THEN NULL ELSE 'invoice'::document_parent_type END) STORED;
ALTER TABLE payments DROP CONSTRAINT payments_invoice_id_fkey;
ALTER TABLE payments
    ADD CONSTRAINT payments_invoice_id_fkey FOREIGN KEY (invoice_id, invoice_parent_type)
    REFERENCES billing_documents (id, parent_type);

ALTER TABLE recurring_invoices
    ADD COLUMN next_invoice_parent_type document_parent_type
        GENERATED ALWAYS AS (CASE WHEN next_invoice_id IS NULL THEN NULL ELSE 'invoice'::document_parent_type END) STORED;
ALTER TABLE recurring_invoices DROP CONSTRAINT recurring_invoices_next_invoice_id_fkey;
ALTER TABLE recurring_invoices
    ADD CONSTRAINT recurring_invoices_next_invoice_id_fkey FOREIGN KEY (next_invoice_id, next_invoice_parent_type)
    REFERENCES billing_documents (id, parent_type);

ALTER TABLE invoice_tag_links
    ADD COLUMN invoice_parent_type document_parent_type NOT NULL GENERATED ALWAYS AS ('invoice'::document_parent_type) STORED;
ALTER TABLE invoice_tag_links DROP CONSTRAINT invoice_tag_links_invoice_id_fkey;
ALTER TABLE invoice_tag_links
    ADD CONSTRAINT invoice_tag_links_invoice_id_fkey FOREIGN KEY (invoice_id, invoice_parent_type)
    REFERENCES billing_documents (id, parent_type) ON DELETE CASCADE;

-- The reports always filter on the type first, so the date indexes lead with it.
DROP INDEX invoices_active_date_idx;
DROP INDEX invoices_date_idx;
CREATE INDEX billing_documents_type_date_idx ON billing_documents (parent_type, document_date, id);
CREATE INDEX billing_documents_active_type_date_idx ON billing_documents (parent_type, document_date, id)
    WHERE deleted_at IS NULL AND archived_at IS NULL;
