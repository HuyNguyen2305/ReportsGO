# Database schema

PostgreSQL 18, managed by `golang-migrate` (`migrations/`). Single company, header-level data only.
Statuses are Postgres ENUMs; money is `numeric(12,2)`; Active/Archived/Deleted tabs map to `archived_at` / `deleted_at`.

## Relationships

```
lead_sources ─┐
              ├─< customers ─┬─< customer_phones
              │              ├─< service_locations ─┐
service_types ─┼──────────────┼─< jobs >─────────────┘
              │              ├─< invoices >─< invoice_tag_links >─ invoice_tags
payment_terms ─┼──────────────┤      ▲  │
users ────────┤              │      │  └─ recurring_invoice_id ─> recurring_invoices (next_invoice_id ─> invoices)
document_types ┘              ├─< estimates
                              ├─< documents
                              ├─< credits
                              └─< payments ─> invoice_id XOR credit_id
```

`invoices.recurring_invoice_id` and `recurring_invoices.next_invoice_id` reference each other; the first FK is added in migration 000007.

## Screenshot column → table.column

| Report | Column | Source |
|---|---|---|
| Invoices | Account # | `customers.account_number` |
| | Invoice # | `invoices.invoice_number` |
| | Customer | `customers.display_name` |
| | Job | `jobs` → `service_types.name` + `service_locations` |
| | Frequency | `invoices.frequency_label` |
| | Billing Email | `invoices.billing_email` |
| | Date / Total / Discount | `invoices.invoice_date` / `total` / `discount` |
| | Invoice Tags | `invoice_tag_links` → `invoice_tags.name` |
| | Payment Terms | `payment_terms.name`, `description` |
| | Status / Signature | `invoices.status` / `signature_status` |
| Estimates | Estimate / Account # / Customer | `estimates.estimate_number` / `customers.account_number` / `customers.display_name` |
| | Phone | `customer_phones` |
| | Billing Email / Created By / Date / Total / Status | `estimates.*`, `users.full_name` |
| Documents | Document # / Type | `documents.document_number` / `document_types.name` |
| | Created By / Date Created / Status | `users.full_name` / `documents.created_at` / `documents.status` |
| Payments | Payment For | `payments.invoice_id` or `payments.credit_id` |
| | Method | `payments.method_type`, `card_brand`, `card_last4` |
| | Date / Amount / Status | `payments.payment_date` / `amount` / `status` |
| Recurring Invoices | Repeats | `frequency_unit`, `frequency_interval`, `weekday`, `occurrences` |
| | Action / Status / Total Value | `action` / `status` / `total_value` |
| | Started / End Date / Next Invoice | `started_on` / `ends_on` / `next_invoice_id`, `next_invoice_date` |
| New Jobs | Service Type / Source / Invoice Value | `service_types.name` / `lead_sources.name` / `jobs.invoice_value` |
| | Job Created Date / Status | `jobs.created_at` / `jobs.status` |
| New Customers | Date / Source / Email / Top Note | `customers.created_at` / `lead_sources.name` / `email` / `top_note` |
| | Service Address | `service_locations` |
| | Job Created Date / Completed Jobs | derived from `jobs` |

## Assumptions to confirm
- Enum values not visible in the screenshots (e.g. `estimate_status`, `document_status`, `recurring_status.paused`).
- `invoices.frequency_label` is stored text, separate from `recurring_invoices`.
- Credits are inferred from "Credit" rows on the Payments report (no Credits screenshot).
