-- recurring_invoice_id's foreign key is added in 000007, after recurring_invoices exists.
CREATE TABLE invoices (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    invoice_number       text NOT NULL UNIQUE,
    customer_id          bigint NOT NULL REFERENCES customers (id),
    job_id               bigint REFERENCES jobs (id),
    recurring_invoice_id bigint,
    billing_email        text,
    invoice_date         date NOT NULL,
    total                numeric(12, 2) NOT NULL DEFAULT 0,
    discount             numeric(12, 2) NOT NULL DEFAULT 0,
    status               invoice_status NOT NULL DEFAULT 'draft',
    signature_status     signature_status NOT NULL DEFAULT 'draft',
    payment_terms_id     bigint REFERENCES payment_terms (id),
    frequency_label      text NOT NULL DEFAULT '',
    created_by           bigint REFERENCES users (id),
    created_at           timestamptz NOT NULL DEFAULT now(),
    archived_at          timestamptz,
    deleted_at           timestamptz
);
CREATE INDEX invoices_customer_id_idx ON invoices (customer_id);
CREATE INDEX invoices_job_id_idx ON invoices (job_id);
CREATE INDEX invoices_recurring_invoice_id_idx ON invoices (recurring_invoice_id);
CREATE INDEX invoices_payment_terms_id_idx ON invoices (payment_terms_id);
CREATE INDEX invoices_created_by_idx ON invoices (created_by);
CREATE INDEX invoices_active_date_idx ON invoices (invoice_date, id)
    WHERE deleted_at IS NULL AND archived_at IS NULL;
CREATE INDEX invoices_date_idx ON invoices (invoice_date, id);
CREATE INDEX invoices_status_idx ON invoices (status);

CREATE TABLE invoice_tag_links (
    invoice_id bigint NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    tag_id     bigint NOT NULL REFERENCES invoice_tags (id),
    PRIMARY KEY (invoice_id, tag_id)
);
CREATE INDEX invoice_tag_links_tag_id_idx ON invoice_tag_links (tag_id);
