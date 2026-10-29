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

## Tax, items, cards, credit use and job fields (migrations 000013 - 000016)

Added for Service Lookup, Customers / Locations w/o Active Jobs, Accounts Aging, Payments Collected, Total Sales,
Sales Tax Summary, Sales Forecast and Revenue. Like `invoice_tag_links`, the tables that hang off invoices use a
generated `invoice_parent_type` column and a composite foreign key, so only invoices (never estimates) can carry them.

| Migration | Adds |
|---|---|
| 000013 | `tax_rates` (name, rate 0 - 100, active); `invoice_tax_lines` (one row per tax per invoice: taxable, tax and surcharge amounts); `payments.received_by` (the staff member who took the payment) |
| 000014 | `items` (catalog); `invoice_items` (invoice line items: item, quantity, unit price, line total, taxable) |
| 000015 | `customers.status` (`active`, `inactive`, `lead`, `archived`); `customer_payment_methods` (Stripe / Square cards, at most one default per customer); `credit_applications` (credit used against an invoice, with the date) |
| 000016 | `jobs.assigned_to`, `jobs.deleted_at`, `jobs.in_work_pool`, `jobs.tax_amount`; `service_locations.county`; `job_tags` and `job_tag_links` |

**Derived, not stored**

- Invoice **W/O Tax** = `total - SUM(invoice_tax_lines.tax_amount)`. Job W/O Tax = `invoice_value - tax_amount`.
- Payment **W/O Tax** = `amount * (invoice.total - invoice tax) / invoice.total`.
- Sales Tax Summary, *Collected (cash based)*: tax apportioned by the payments received in the date range;
  *Accrual*: by the invoice date.
- **Available credit** = `credits.amount - SUM(credit_applications.amount)`; the Point in Time view of Accounts Aging
  only counts applications with `applied_on` up to the chosen date.
- **Service state** of a payment: the state of the service location of the invoice's job.
- Service Lookup tabs: *Active* = `deleted_at IS NULL AND NOT in_work_pool`; *Work Pool* = `in_work_pool`; *Deleted* = `deleted_at IS NOT NULL`.
- Sales Forecast comes from future-dated jobs and `recurring_invoices` (next invoice date, total value); Revenue from
  invoices and payments. Neither needs a table of its own.


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
| Service Lookup | Customer / Phone | `customers.display_name` / `customer_phones` |
| | Service Type / Scheduled Date | `service_types.name` / `jobs.scheduled_at` |
| | Address / City / Zip / County | `service_locations.address_line1`, `city`, `postal_code`, `county` |
| | Invoice Value / W/O Tax | `jobs.invoice_value` / `invoice_value - tax_amount` |
| | Job Status / Assigned To / Tags | `jobs.status` / `jobs.assigned_to` → `users.full_name` / `job_tag_links` → `job_tags.name` |
| | Active / Work Pool / Deleted tabs | `jobs.deleted_at`, `jobs.in_work_pool` |
| Customers w/o Active Jobs | Status / Account # / Customer | `customers.status` / `account_number` / `display_name` |
| | Last Service Type / Job Status / Last Service Date | latest `jobs` row: `service_types.name` / `status` / `scheduled_at` |
| Locations w/o Active Jobs | Status / Account # / Customer / Phone | as above, per `service_locations` row |
| | Service Address / City / Zip | `service_locations.*` |
| | Last Service Type / Job Status / Last Service Date | latest `jobs` row of that location |
| Accounts Aging | Customer - Location | `customers.display_name` + the invoice job's `service_locations` |
| | Billing Email / Phone | `billing_documents.billing_email` / `customer_phones` |
| | Credit Card | `customer_payment_methods` (`processor`, `last4`, `exp_month`, `exp_year`) |
| | 0-30 / 31-60 / 61-90 / 90+ Days, Total | unpaid balance of invoices (`total` - `payments`) by age of `document_date` |
| | Available Credit | `credits.amount` - `credit_applications.amount` |
| Payments Collected | Payment For / Customer / Method / Date / Amount | `payments.*`, `customers.display_name` |
| | Service State | `service_locations.state` of the invoice's job |
| | Staff | `payments.received_by` → `users.full_name` |
| | W/O Tax | payment amount less its share of the invoice tax |
| Total Sales | Invoice / Customer / Date / Status / Amount | `billing_documents.*` (`parent_type = 'invoice'`), `customers.display_name` |
| | W/O Tax | `total - SUM(invoice_tax_lines.tax_amount)` |
| Sales Tax Summary | Sales Tax / Tax Rate | `tax_rates.name` / `tax_rates.rate` |
| | Taxable Sales / Tax Collected / Surcharge Tax | `invoice_tax_lines.taxable_amount` / `tax_amount` / `surcharge_amount` |
| Sales Forecast, Revenue | By Client / Service / Staff / Source / Tags | `customers`, `service_types`, `users`, `lead_sources`, `job_tags` / `invoice_tags` |
| | By Item | `invoice_items` → `items.name` |

## Assumptions to confirm
- Enum values not visible in the screenshots (e.g. estimate statuses `accepted`/`declined`, `document_status`, `recurring_status.paused`).
- `billing_documents.frequency_label` is stored text, separate from `recurring_invoices`.
- Credits are inferred from "Credit" rows on the Payments report (no Credits screenshot).
- New Customers: Top Note, Service Address, Job Created Date and Completed Jobs are in the screenshot and mapped above, but the report does not return them. It covers the customer name, account number, email, phone, source and created date. Adding them would be a feature: a customer can have several service locations and several jobs, so each column needs a rule (first, latest or count).
- Customer statuses (`active`, `inactive`, `lead`, `archived`) are inferred from the "All Statuses" filter; confirm the real values.
- "Locations w/o Active Jobs" is the screen the request called "Address w/o Active Jobs".
- The seed figures for tax, items, cards and credit use are plausible October 2026 data, not the exact screenshot numbers.
- `jobs.tax_amount` is stored on the job because a job may have no invoice yet; invoice tax comes from `invoice_tax_lines`.
- By Tags in Sales Forecast / Revenue is assumed to use job tags for jobs and `invoice_tags` for invoices; decide per report when its API is designed.
