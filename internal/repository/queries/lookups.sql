-- name: ListServiceTypes :many
SELECT id, name
FROM service_types
ORDER BY name, id;

-- name: ListInvoiceTags :many
SELECT id, name
FROM invoice_tags
ORDER BY name, id;

-- name: ListPaymentTerms :many
SELECT id, name, description
FROM payment_terms
ORDER BY name, id;

-- name: ListDocumentTypes :many
SELECT id, name
FROM document_types
ORDER BY name, id;

-- name: ListLeadSources :many
SELECT id, name
FROM lead_sources
ORDER BY name, id;

-- name: ListStaff :many
SELECT id, full_name
FROM users
ORDER BY full_name, id;

-- name: ListTaxRates :many
SELECT id, name, rate::text AS rate, active
FROM tax_rates
ORDER BY name, id;

-- name: ListJobTags :many
SELECT id, name
FROM job_tags
ORDER BY name, id;

-- name: ListItems :many
SELECT id, name
FROM items
ORDER BY name, id;
