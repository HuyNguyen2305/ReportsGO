-- Reads ClientPortalGO data in ReportsGO's shape, for a deployment where the portal lives in schema `portal`
-- of the same database. Point ReportsGO's DATABASE_URL at search_path=reports_live,public and every report
-- query resolves to these views; tables with no view here (users, tax_rates, lead_sources, ...) still come from
-- public. See docs/portal-link.md. Safe to run again.
--
-- shortcut: payment dates use America/Chicago, the portal's default BUSINESS_TZ; change the literal below if the
-- portal runs in another zone.

CREATE SCHEMA IF NOT EXISTS reports_live;

CREATE OR REPLACE VIEW reports_live.customers AS
SELECT c.id, c.account_number::text AS account_number, ''::text AS first_name, ''::text AS last_name,
       c.name AS display_name, c.email, NULL::bigint AS lead_source_id, ''::text AS top_note, c.created_at,
       'active'::public.customer_status AS status, false AS booked_online
FROM portal.customers c;

CREATE OR REPLACE VIEW reports_live.customer_phones AS
SELECT c.id, c.id AS customer_id, c.phone AS number
FROM portal.customers c
WHERE c.phone IS NOT NULL AND c.phone <> '';

CREATE OR REPLACE VIEW reports_live.service_locations AS
SELECT l.id, l.customer_id, l.street AS address_line1, ''::text AS address_line2, l.city, l.state, l.postal_code,
       l.created_at, ''::text AS county
FROM portal.locations l;

CREATE OR REPLACE VIEW reports_live.service_types AS
SELECT s.id, s.name
FROM portal.services s;

-- One job per work order. Cancelling the booking cancels the job.
CREATE OR REPLACE VIEW reports_live.jobs AS
SELECT w.id, w.customer_id, b.location_id, w.service_id AS service_type_id, NULL::bigint AS lead_source_id,
       (CASE WHEN b.cancelled_at IS NOT NULL THEN 'cancelled'
             WHEN w.status = 'completed' THEN 'completed'
             WHEN w.status = 'in_progress' THEN 'confirmed'
             ELSE 'unconfirmed' END)::public.job_status AS status,
       b.starts_at AS scheduled_at, (e.total_cents / 100.0)::numeric(12, 2) AS invoice_value, w.created_at,
       NULL::bigint AS assigned_to, NULL::timestamptz AS deleted_at, false AS in_work_pool,
       0::numeric(12, 2) AS tax_amount
FROM portal.work_orders w
JOIN portal.bookings b ON b.id = w.booking_id
JOIN portal.billing_documents e ON e.id = b.estimate_id;

-- Invoices and estimates. An invoice's job is the work order of the estimate it was converted from.
CREATE OR REPLACE VIEW reports_live.billing_documents AS
SELECT d.id, d.number::text AS document_number, d.customer_id, w.id AS job_id, c.email AS billing_email,
       d.document_date, (d.total_cents / 100.0)::numeric(12, 2) AS total, 0::numeric(12, 2) AS discount,
       (CASE WHEN d.status IN ('open', 'partially_paid') THEN 'sent' ELSE d.status END)::public.billing_status AS status,
       'draft'::public.signature_status AS signature_status, NULL::bigint AS payment_terms_id,
       ''::text AS frequency_label, NULL::bigint AS created_by, d.created_at, NULL::timestamptz AS archived_at,
       NULL::timestamptz AS deleted_at, d.parent_type::public.document_parent_type AS parent_type
FROM portal.billing_documents d
JOIN portal.customers c ON c.id = d.customer_id
LEFT JOIN portal.bookings b ON d.parent_type = 'invoice' AND b.estimate_id = d.converted_from_id
LEFT JOIN portal.work_orders w ON w.booking_id = b.id;

-- The method and card summary come from the portal's payments (000017); older payments are "other".
CREATE OR REPLACE VIEW reports_live.payments AS
SELECT p.id, d.customer_id, p.billing_document_id AS invoice_id, NULL::bigint AS credit_id,
       p.method::public.payment_method_type AS method_type, p.card_brand, p.card_last4::char(4) AS card_last4,
       (p.amount_cents / 100.0)::numeric(12, 2) AS amount,
       (p.paid_at AT TIME ZONE 'America/Chicago')::date AS payment_date, 'paid'::public.payment_status AS status,
       p.created_at, NULL::timestamptz AS deleted_at, 'invoice'::public.document_parent_type AS invoice_parent_type,
       NULL::bigint AS received_by
FROM portal.payments p
JOIN portal.billing_documents d ON d.id = p.billing_document_id;

-- The portal has no tax, credits, line-item catalog or tags. Empty views keep a portal id from joining a row of
-- the same number in public.
CREATE OR REPLACE VIEW reports_live.invoice_tax_lines AS SELECT * FROM public.invoice_tax_lines WHERE false;
CREATE OR REPLACE VIEW reports_live.credits AS SELECT * FROM public.credits WHERE false;
CREATE OR REPLACE VIEW reports_live.credit_applications AS SELECT * FROM public.credit_applications WHERE false;
CREATE OR REPLACE VIEW reports_live.invoice_items AS SELECT * FROM public.invoice_items WHERE false;
CREATE OR REPLACE VIEW reports_live.invoice_tag_links AS SELECT * FROM public.invoice_tag_links WHERE false;
CREATE OR REPLACE VIEW reports_live.job_tag_links AS SELECT * FROM public.job_tag_links WHERE false;
