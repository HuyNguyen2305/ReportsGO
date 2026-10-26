# Database schema

PostgreSQL 18, managed by `golang-migrate` (`migrations/`). Single company, header-level data only.
Statuses are Postgres ENUMs; money is `numeric(12,2)`; Active/Archived/Deleted tabs map to `archived_at` / `deleted_at`.

## Relationships

```
lead_sources ─┐
              ├─< customers ─┬─< customer_phones
              │              ├─< service_locations ─┐
service_types ─┼──────────────┼─< jobs >─────────────┘
              │              ├─< billing_documents >─< invoice_tag_links >─ invoice_tags
payment_terms ─┼──────────────┤      ▲  │   (parent_type = invoice | estimate)
users ────────┤              │      │  └─ recurring_invoice_id ─> recurring_invoices (next_invoice_id ─> invoices)
document_types ┘              ├─< documents
                              ├─< credits
                              └─< payments ─> invoice_id XOR credit_id
```

`billing_documents` holds both invoices and estimates (migration 000009 merged the former `invoices` and `estimates` tables), told apart by `parent_type`:

- `UNIQUE (parent_type, document_number)`: an invoice and an estimate may share a number.
- Statuses depend on the type (`billing_documents_status_chk`): invoices `draft/sent/paid/void`, estimates `draft/sent/accepted/declined`.
- Estimates have no job, recurring invoice, payment terms or discount (`billing_documents_estimate_chk`).
- `payments.invoice_id`, `recurring_invoices.next_invoice_id` and `invoice_tag_links.invoice_id` can only reference invoices; a generated `*_parent_type` column plus a composite foreign key enforce it.

**Estimate ids change in migration 000009.** The migration re-inserts the former `estimates` rows into `billing_documents`, so each estimate gets a new id from the shared sequence (rolling back gives them new ids again). Numbers, dates, statuses, signature states, totals and archive/delete flags are preserved; ids are not, because invoice ids and estimate ids overlapped and cannot be kept. This only matters to an environment that already holds estimates which clients reference by id.

`billing_documents.recurring_invoice_id` and `recurring_invoices.next_invoice_id` reference each other; the first FK is added in migration 000007.

## Screenshot column → table.column

| Report | Column | Source |
|---|---|---|
| Invoices | Account # | `customers.account_number` |
| | Invoice # | `billing_documents.document_number` (`parent_type = 'invoice'`) |
| | Customer | `customers.display_name` |
| | Job | `jobs` → `service_types.name` + `service_locations` |
| | Frequency | `billing_documents.frequency_label` |
| | Billing Email | `billing_documents.billing_email` |
| | Date / Total / Discount | `billing_documents.document_date` / `total` / `discount` |
| | Invoice Tags | `invoice_tag_links` → `invoice_tags.name` |
| | Payment Terms | `payment_terms.name`, `description` |
| | Status / Signature | `billing_documents.status` / `signature_status` |
| Estimates | Estimate / Account # / Customer | `billing_documents.document_number` (`parent_type = 'estimate'`) / `customers.account_number` / `customers.display_name` |
| | Phone | `customer_phones` |
| | Billing Email / Created By / Date / Total / Status | `billing_documents.*`, `users.full_name` |
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
- Enum values not visible in the screenshots (e.g. estimate statuses `accepted`/`declined`, `document_status`, `recurring_status.paused`).
- `billing_documents.frequency_label` is stored text, separate from `recurring_invoices`.
- Credits are inferred from "Credit" rows on the Payments report (no Credits screenshot).
- New Customers: Top Note, Service Address, Job Created Date and Completed Jobs are in the screenshot and mapped above, but the report does not return them. It covers the customer name, account number, email, phone, source and created date. Adding them would be a feature: a customer can have several service locations and several jobs, so each column needs a rule (first, latest or count).
