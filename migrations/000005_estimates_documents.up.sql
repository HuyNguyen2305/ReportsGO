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

CREATE TABLE documents (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_number  text NOT NULL UNIQUE,
    document_type_id bigint NOT NULL REFERENCES document_types (id),
    customer_id      bigint NOT NULL REFERENCES customers (id),
    created_by       bigint REFERENCES users (id),
    status           document_status NOT NULL DEFAULT 'draft',
    created_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz
);
CREATE INDEX documents_document_type_id_idx ON documents (document_type_id);
CREATE INDEX documents_customer_id_idx ON documents (customer_id);
CREATE INDEX documents_created_by_idx ON documents (created_by);
CREATE INDEX documents_created_at_idx ON documents (created_at, id);
CREATE INDEX documents_active_created_at_idx ON documents (created_at, id)
    WHERE deleted_at IS NULL;
CREATE INDEX documents_status_idx ON documents (status);
