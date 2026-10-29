-- Customer status (the "All Statuses" filter of the w/o Active Jobs reports), saved cards (Accounts Aging "Credit Card")
-- and the use of credits against invoices (Accounts Aging "Available Credit").

CREATE TYPE customer_status AS ENUM ('active', 'inactive', 'lead', 'archived');
ALTER TABLE customers ADD COLUMN status customer_status NOT NULL DEFAULT 'active';
CREATE INDEX customers_status_idx ON customers (status);

CREATE TABLE customer_payment_methods (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id bigint NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    processor   text NOT NULL CONSTRAINT customer_payment_methods_processor_chk CHECK (processor IN ('stripe', 'square')),
    brand       text NOT NULL DEFAULT '',
    last4       char(4) NOT NULL,
    exp_month   smallint NOT NULL CONSTRAINT customer_payment_methods_exp_month_chk CHECK (exp_month BETWEEN 1 AND 12),
    exp_year    smallint NOT NULL,
    is_default  boolean NOT NULL DEFAULT false
);
CREATE INDEX customer_payment_methods_customer_id_idx ON customer_payment_methods (customer_id);
-- A customer has at most one default card.
CREATE UNIQUE INDEX customer_payment_methods_default_idx ON customer_payment_methods (customer_id) WHERE is_default;

-- Available credit is credits.amount minus its applications; replaying applied_on gives the Point in Time view.
-- Only invoices can be paid with a credit; the generated column and composite foreign key keep estimates out.
CREATE TABLE credit_applications (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    credit_id           bigint NOT NULL REFERENCES credits (id),
    invoice_id          bigint NOT NULL,
    invoice_parent_type document_parent_type NOT NULL GENERATED ALWAYS AS ('invoice'::document_parent_type) STORED,
    amount              numeric(12, 2) NOT NULL CONSTRAINT credit_applications_amount_chk CHECK (amount > 0),
    applied_on          date NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT credit_applications_invoice_id_fkey FOREIGN KEY (invoice_id, invoice_parent_type)
        REFERENCES billing_documents (id, parent_type)
);
CREATE INDEX credit_applications_credit_id_idx ON credit_applications (credit_id);
CREATE INDEX credit_applications_invoice_id_idx ON credit_applications (invoice_id);
CREATE INDEX credit_applications_applied_on_idx ON credit_applications (applied_on, id);
