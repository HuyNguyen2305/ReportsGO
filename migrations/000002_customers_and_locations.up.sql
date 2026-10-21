CREATE TABLE customers (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_number text NOT NULL UNIQUE,
    first_name     text NOT NULL DEFAULT '',
    last_name      text NOT NULL DEFAULT '',
    display_name   text NOT NULL,
    email          text,
    billing_email  text,
    lead_source_id bigint REFERENCES lead_sources (id),
    top_note       text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX customers_lead_source_id_idx ON customers (lead_source_id);
CREATE INDEX customers_created_at_idx ON customers (created_at, id);

CREATE TABLE customer_phones (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id bigint NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    number      text NOT NULL,
    kind        text NOT NULL DEFAULT 'mobile'
);
CREATE INDEX customer_phones_customer_id_idx ON customer_phones (customer_id);

CREATE TABLE service_locations (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id   bigint NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    address_line1 text NOT NULL,
    address_line2 text NOT NULL DEFAULT '',
    city          text NOT NULL DEFAULT '',
    state         text NOT NULL DEFAULT '',
    postal_code   text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX service_locations_customer_id_idx ON service_locations (customer_id);
