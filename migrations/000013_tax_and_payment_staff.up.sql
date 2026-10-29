-- Tax for the Total Sales, Payments Collected and Sales Tax Summary reports.
-- "W/O Tax" is not stored: an invoice's is total - SUM(tax_amount) of its tax lines, a payment's is its amount scaled
-- by the same ratio. Surcharge is kept apart from tax because the Sales Tax Summary reports it in its own column.

CREATE TABLE tax_rates (
    id     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name   text NOT NULL UNIQUE,
    rate   numeric(5, 2) NOT NULL CONSTRAINT tax_rates_rate_chk CHECK (rate >= 0 AND rate <= 100),
    active boolean NOT NULL DEFAULT true
);

-- One row per tax per invoice. The generated column pins the referenced document type to 'invoice' and the composite
-- foreign key enforces it, as for invoice_tag_links, so an estimate cannot carry tax lines.
CREATE TABLE invoice_tax_lines (
    invoice_id          bigint NOT NULL,
    invoice_parent_type document_parent_type NOT NULL GENERATED ALWAYS AS ('invoice'::document_parent_type) STORED,
    tax_rate_id         bigint NOT NULL REFERENCES tax_rates (id),
    taxable_amount      numeric(12, 2) NOT NULL DEFAULT 0,
    tax_amount          numeric(12, 2) NOT NULL DEFAULT 0,
    surcharge_amount    numeric(12, 2) NOT NULL DEFAULT 0,
    PRIMARY KEY (invoice_id, tax_rate_id),
    CONSTRAINT invoice_tax_lines_invoice_id_fkey FOREIGN KEY (invoice_id, invoice_parent_type)
        REFERENCES billing_documents (id, parent_type) ON DELETE CASCADE,
    CONSTRAINT invoice_tax_lines_amounts_chk CHECK (taxable_amount >= 0 AND tax_amount >= 0 AND surcharge_amount >= 0)
);
CREATE INDEX invoice_tax_lines_tax_rate_id_idx ON invoice_tax_lines (tax_rate_id);

-- The staff member who took the payment (the Payments Collected "Staff" column); online card payments have none.
ALTER TABLE payments ADD COLUMN received_by bigint REFERENCES users (id);
CREATE INDEX payments_received_by_idx ON payments (received_by);
