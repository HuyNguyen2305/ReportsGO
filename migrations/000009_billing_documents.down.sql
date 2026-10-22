-- Splits estimates back out of billing_documents (data-preserving) and restores the original names and types.

CREATE TYPE invoice_status AS ENUM ('draft', 'sent', 'paid', 'void');
CREATE TYPE estimate_status AS ENUM ('draft', 'sent', 'accepted', 'declined');

CREATE TABLE estimates (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    estimate_number  text NOT NULL UNIQUE,
    customer_id      bigint NOT NULL REFERENCES customers (id),
    billing_email    text,
    created_by       bigint REFERENCES users (id),
    estimate_date    date NOT NULL,
    total            numeric(12, 2) NOT NULL DEFAULT 0,
    status           estimate_status NOT NULL DEFAULT 'draft',
    signature_status signature_status NOT NULL DEFAULT 'draft',
    created_at       timestamptz NOT NULL DEFAULT now(),
    archived_at      timestamptz,
    deleted_at       timestamptz
);
CREATE INDEX estimates_customer_id_idx ON estimates (customer_id);
CREATE INDEX estimates_created_by_idx ON estimates (created_by);
CREATE INDEX estimates_date_idx ON estimates (estimate_date, id);
CREATE INDEX estimates_active_date_idx ON estimates (estimate_date, id)
    WHERE deleted_at IS NULL AND archived_at IS NULL;
CREATE INDEX estimates_status_idx ON estimates (status);

INSERT INTO estimates (estimate_number, customer_id, billing_email, created_by, estimate_date, total,
                       status, signature_status, created_at, archived_at, deleted_at)
SELECT document_number, customer_id, billing_email, created_by, document_date, total,
       status::text::estimate_status, signature_status, created_at, archived_at, deleted_at
FROM billing_documents
WHERE parent_type = 'estimate'
ORDER BY id;

DELETE FROM billing_documents WHERE parent_type = 'estimate';

DROP INDEX billing_documents_active_type_date_idx;
DROP INDEX billing_documents_type_date_idx;

-- Foreign keys back to plain references.
ALTER TABLE invoice_tag_links DROP CONSTRAINT invoice_tag_links_invoice_id_fkey;
ALTER TABLE invoice_tag_links DROP COLUMN invoice_parent_type;
ALTER TABLE invoice_tag_links
    ADD CONSTRAINT invoice_tag_links_invoice_id_fkey FOREIGN KEY (invoice_id)
    REFERENCES billing_documents (id) ON DELETE CASCADE;

ALTER TABLE recurring_invoices DROP CONSTRAINT recurring_invoices_next_invoice_id_fkey;
ALTER TABLE recurring_invoices DROP COLUMN next_invoice_parent_type;
ALTER TABLE recurring_invoices
    ADD CONSTRAINT recurring_invoices_next_invoice_id_fkey FOREIGN KEY (next_invoice_id)
    REFERENCES billing_documents (id);

ALTER TABLE payments DROP CONSTRAINT payments_invoice_id_fkey;
ALTER TABLE payments DROP COLUMN invoice_parent_type;
ALTER TABLE payments
    ADD CONSTRAINT payments_invoice_id_fkey FOREIGN KEY (invoice_id) REFERENCES billing_documents (id);

ALTER TABLE billing_documents DROP CONSTRAINT billing_documents_id_parent_type_key;
ALTER TABLE billing_documents DROP CONSTRAINT billing_documents_estimate_chk;
ALTER TABLE billing_documents DROP CONSTRAINT billing_documents_status_chk;
ALTER TABLE billing_documents DROP CONSTRAINT billing_documents_parent_number_key;

ALTER TABLE billing_documents ALTER COLUMN status DROP DEFAULT;
ALTER TABLE billing_documents
    ALTER COLUMN status TYPE invoice_status USING status::text::invoice_status;
ALTER TABLE billing_documents ALTER COLUMN status SET DEFAULT 'draft';
DROP TYPE billing_status;

ALTER TABLE billing_documents DROP COLUMN parent_type;
DROP TYPE document_parent_type;

ALTER TABLE billing_documents RENAME COLUMN document_date TO invoice_date;
ALTER TABLE billing_documents RENAME COLUMN document_number TO invoice_number;
ALTER TABLE billing_documents ADD CONSTRAINT invoices_invoice_number_key UNIQUE (invoice_number);
ALTER TABLE billing_documents RENAME TO invoices;

ALTER INDEX billing_documents_pkey RENAME TO invoices_pkey;
ALTER INDEX billing_documents_customer_id_idx RENAME TO invoices_customer_id_idx;
ALTER INDEX billing_documents_job_id_idx RENAME TO invoices_job_id_idx;
ALTER INDEX billing_documents_recurring_invoice_id_idx RENAME TO invoices_recurring_invoice_id_idx;
ALTER INDEX billing_documents_payment_terms_id_idx RENAME TO invoices_payment_terms_id_idx;
ALTER INDEX billing_documents_created_by_idx RENAME TO invoices_created_by_idx;
ALTER INDEX billing_documents_status_idx RENAME TO invoices_status_idx;
ALTER INDEX billing_documents_number_trgm_idx RENAME TO invoices_invoice_number_trgm_idx;
ALTER INDEX billing_documents_billing_email_trgm_idx RENAME TO invoices_billing_email_trgm_idx;
ALTER TABLE invoices
    RENAME CONSTRAINT billing_documents_recurring_invoice_id_fkey TO invoices_recurring_invoice_id_fkey;

CREATE INDEX invoices_active_date_idx ON invoices (invoice_date, id)
    WHERE deleted_at IS NULL AND archived_at IS NULL;
CREATE INDEX invoices_date_idx ON invoices (invoice_date, id);
