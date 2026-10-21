CREATE TABLE recurring_invoices (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id        bigint NOT NULL REFERENCES customers (id),
    job_id             bigint REFERENCES jobs (id),
    frequency_unit     frequency_unit NOT NULL,
    frequency_interval integer NOT NULL DEFAULT 1 CHECK (frequency_interval > 0),
    weekday            smallint CHECK (weekday BETWEEN 0 AND 6),
    occurrences        integer CHECK (occurrences > 0),
    action             recurring_action NOT NULL DEFAULT 'send_by_email',
    status             recurring_status NOT NULL DEFAULT 'active',
    total_value        numeric(12, 2) NOT NULL DEFAULT 0,
    started_on         date NOT NULL,
    ends_on            date,
    next_invoice_id    bigint REFERENCES invoices (id),
    next_invoice_date  date,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recurring_invoices_customer_id_idx ON recurring_invoices (customer_id);
CREATE INDEX recurring_invoices_job_id_idx ON recurring_invoices (job_id);
CREATE INDEX recurring_invoices_next_invoice_id_idx ON recurring_invoices (next_invoice_id);
CREATE INDEX recurring_invoices_status_idx ON recurring_invoices (status);
CREATE INDEX recurring_invoices_started_on_idx ON recurring_invoices (started_on, id);

ALTER TABLE invoices
    ADD CONSTRAINT invoices_recurring_invoice_id_fkey
    FOREIGN KEY (recurring_invoice_id) REFERENCES recurring_invoices (id);
