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
