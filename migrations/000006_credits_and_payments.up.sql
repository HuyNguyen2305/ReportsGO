CREATE TABLE credits (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id   bigint NOT NULL REFERENCES customers (id),
    amount        numeric(12, 2) NOT NULL,
    method_type   payment_method_type NOT NULL,
    method_detail text NOT NULL DEFAULT '',
    credit_date   date NOT NULL,
    created_by    bigint REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credits_customer_id_idx ON credits (customer_id);
CREATE INDEX credits_created_by_idx ON credits (created_by);
CREATE INDEX credits_date_idx ON credits (credit_date, id);

CREATE TABLE payments (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id  bigint NOT NULL REFERENCES customers (id),
    invoice_id   bigint REFERENCES invoices (id),
    credit_id    bigint REFERENCES credits (id),
    method_type  payment_method_type NOT NULL,
    card_brand   text,
    card_last4   char(4),
    amount       numeric(12, 2) NOT NULL,
    payment_date date NOT NULL,
    status       payment_status NOT NULL DEFAULT 'paid',
    created_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz,
    -- "Payment For" is either an invoice or a credit, never both.
    CONSTRAINT payments_target_chk CHECK (num_nonnulls(invoice_id, credit_id) = 1)
);
CREATE INDEX payments_customer_id_idx ON payments (customer_id);
CREATE INDEX payments_invoice_id_idx ON payments (invoice_id);
CREATE INDEX payments_credit_id_idx ON payments (credit_id);
CREATE INDEX payments_date_idx ON payments (payment_date, id);
CREATE INDEX payments_active_date_idx ON payments (payment_date, id)
    WHERE deleted_at IS NULL;
CREATE INDEX payments_method_type_idx ON payments (method_type);
