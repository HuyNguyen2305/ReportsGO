-- System Email History: one row per email the system sent to a customer. The status follows the other confirmed /
-- unconfirmed / cancelled states the screens use; the types are the general ones a field-service system sends.
-- email is the address it was sent to, kept on the row because the customer's own address can change later.

CREATE TYPE email_status AS ENUM ('unconfirmed', 'confirmed', 'cancelled');
CREATE TYPE email_type AS ENUM (
    'welcome', 'invoice', 'estimate', 'appointment_confirmation', 'appointment_reminder', 'payment_receipt',
    'review_request'
);

CREATE TABLE system_emails (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id bigint NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    email       text NOT NULL,
    email_type  email_type NOT NULL,
    status      email_status NOT NULL DEFAULT 'unconfirmed',
    sent_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX system_emails_customer_id_idx ON system_emails (customer_id);
CREATE INDEX system_emails_sent_at_idx ON system_emails (sent_at, id);
CREATE INDEX system_emails_status_idx ON system_emails (status);
