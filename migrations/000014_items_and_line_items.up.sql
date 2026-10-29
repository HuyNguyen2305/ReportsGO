-- Items and invoice line items, for the "By Item" breakdowns of Sales Forecast and Revenue.

CREATE TABLE items (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text NOT NULL UNIQUE,
    unit_price numeric(12, 2) NOT NULL DEFAULT 0,
    active     boolean NOT NULL DEFAULT true
);

-- Only invoices have line items; the generated column and composite foreign key keep estimates out, as for
-- invoice_tax_lines. An item may be removed from the catalog only when no invoice line uses it.
CREATE TABLE invoice_items (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    invoice_id          bigint NOT NULL,
    invoice_parent_type document_parent_type NOT NULL GENERATED ALWAYS AS ('invoice'::document_parent_type) STORED,
    item_id             bigint NOT NULL REFERENCES items (id),
    description         text NOT NULL DEFAULT '',
    quantity            numeric(12, 3) NOT NULL DEFAULT 1,
    unit_price          numeric(12, 2) NOT NULL DEFAULT 0,
    line_total          numeric(12, 2) NOT NULL DEFAULT 0,
    taxable             boolean NOT NULL DEFAULT true,
    CONSTRAINT invoice_items_invoice_id_fkey FOREIGN KEY (invoice_id, invoice_parent_type)
        REFERENCES billing_documents (id, parent_type) ON DELETE CASCADE,
    CONSTRAINT invoice_items_quantity_chk CHECK (quantity > 0)
);
CREATE INDEX invoice_items_invoice_id_idx ON invoice_items (invoice_id);
CREATE INDEX invoice_items_item_id_idx ON invoice_items (item_id);
