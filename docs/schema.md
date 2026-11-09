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
users ────────┤              │      │  └─ recurring_invoices (next_invoice_id ─> invoices)
document_types ┘              ├─< documents
                              ├─< credits
                              └─< payments ─> invoice_id XOR credit_id
```

`billing_documents` holds both invoices and estimates (migration 000009 merged the former `invoices` and `estimates` tables), told apart by `parent_type`:

- `UNIQUE (parent_type, document_number)`: an invoice and an estimate may share a number.
- Statuses depend on the type (`billing_documents_status_chk`): invoices `draft/sent/paid/void`, estimates `draft/sent/accepted/declined`.
- Estimates have no job, payment terms or discount (`billing_documents_estimate_chk`).
- `payments.invoice_id`, `recurring_invoices.next_invoice_id` and `invoice_tag_links.invoice_id` can only reference invoices; a generated `*_parent_type` column plus a composite foreign key enforce it.

**Estimate ids change in migration 000009.** The migration re-inserts the former `estimates` rows into `billing_documents`, so each estimate gets a new id from the shared sequence (rolling back gives them new ids again). Numbers, dates, statuses, signature states, totals and archive/delete flags are preserved; ids are not, because invoice ids and estimate ids overlapped and cannot be kept. This only matters to an environment that already holds estimates which clients reference by id.

Migration 000022 drops `billing_documents.recurring_invoice_id`; an invoice is linked to its recurring schedule only through `recurring_invoices.next_invoice_id`.

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
| 000018 | `materials` (catalog: name, EPA registration number, unit); `job_materials` (the materials added to a job, one row per addition: quantity and an optional device) |
| 000019 | `system_emails` (one row per email sent to a customer: the address, `email_type`, `email_status`, and when it was sent) |
| 000020 | `customers.booked_online` (whether the customer came from an online booking), with a partial index on the flagged customers |
| 000021 | `devices` (installed at a service location: name, barcode, type, area, optional coordinate); `job_device_checks` (one row per device checked during a job: check in and out, status, activity, notes, assignee) |
| 000022 | Drops columns no report reads: `customers.billing_email`, `customer_phones.kind`, `materials.unit`, `job_materials.created_at`, `customer_payment_methods.brand`, `billing_documents.recurring_invoice_id`, `items.unit_price` / `active`, and `invoice_items.description` / `quantity` / `unit_price` / `taxable` (`invoice_items` keeps the item and the line total). The down migration restores the columns, not their data |

**Derived, not stored**

- Invoice **W/O Tax** = `total - SUM(invoice_tax_lines.tax_amount)`. Job W/O Tax = `invoice_value - tax_amount`.
- Payment **W/O Tax** = `amount * (invoice.total - invoice tax) / invoice.total`.
- Sales Tax Summary, *Collected (cash based)*: tax apportioned by the payments received in the date range;
  *Accrual*: by the invoice date.
- **Available credit** = `credits.amount - SUM(credit_applications.amount)`; the Point in Time view of Accounts Aging
  only counts applications with `applied_on` up to the chosen date.
- **Service state** of a payment: the state of the service location of the invoice's job.
- Service Lookup tabs: *Active* = `deleted_at IS NULL AND NOT in_work_pool`; *Work Pool* = `in_work_pool`; *Deleted* = `deleted_at IS NOT NULL`.
- Sales Forecast and Revenue are one report (`type=forecast|revenue`, see `docs/api/sales-breakdown.md`). The forecast
  adds up scheduled jobs (`invoice_value`, by scheduled month) and invoices without a job (by invoice month); revenue adds
  up paid invoice payments (by payment month). Recurring invoices are not projected. Neither needs a table of its own.


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
| | Phone / Service Address | first `customer_phones.number` (lowest id) / `jobs.location_id` → `service_locations` |
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
| Material Use | Material / EPA | `materials.name` / `materials.epa_reg_no` |
| | Qty / # Of Applications | `SUM(job_materials.qty)` / `COUNT(job_materials)` per material, over the jobs in the date range |
| | Date range | `jobs.scheduled_at` (the date the job occurs) |
| | All / Jobs / Devices tabs | `job_materials.device` is NULL (Jobs) or set (Devices) |
| | Clients / Materials filters | `jobs.customer_id` / `job_materials.material_id` |
| System Email History | Customer | `customers.first_name` / `last_name` / `display_name`, by the "First name" dropdown |
| | Email / Email Type / Status | `system_emails.email` / `email_type` / `status` |
| | Time Stamp / date range | `system_emails.sent_at` |
| | All Statuses / All customers filters | `system_emails.status` / `system_emails.customer_id` |
| Inbound Leads | Customer / Email / Notes | `customers` with `status = 'lead'`: `first_name` / `last_name` / `display_name` (by the "First name" dropdown), `email`, `top_note` |
| | Source / Phone | `lead_sources.name` (NA when none) / first `customer_phones.number` (lowest id) |
| | Date Added / Time Added | `customers.created_at` (one column) |
| | Active Jobs Schedule | earliest `jobs.scheduled_at` of the customer's open (`unconfirmed` / `confirmed`), not deleted jobs, with `service_types.name` |
| Online Bookings | Customer / Email / Notes | `customers` with `booked_online`: `first_name` / `last_name` / `display_name` (by the "First name" dropdown), `email`, `top_note` |
| | Date / Time / Service / Status | `jobs.scheduled_at` (one column) / `service_types.name` / `jobs.status` |
| | Phone / Address | first `customer_phones.number` (lowest id) / the job's `service_locations` |
| | All Sources filter | the customer's `lead_sources.name` (`customers.lead_source_id`), not the job's |
| Device Tracking | Customer Name / Service Type / Date | `customers.display_name` / `service_types.name` / `jobs.scheduled_at`, of the job a check belongs to |
| | Address Line 1 / 2, City, State, Zip | the job's own `service_locations` |
| | Device Name / Barcode / Coordinate / Area / Device Type | `devices.name` / `barcode` / `latitude`, `longitude` / `area` / `device_type` |
| | Check In / Check Out / Status / Activity / Finding/Notes | `job_device_checks.checked_in_at` / `checked_out_at` / `status` / `activity` / `notes` |
| | Assigned To / Clients filter | `job_device_checks.assigned_to` → `users.full_name` / `jobs.customer_id` |

## Assumptions to confirm
- Enum values not visible in the screenshots (e.g. estimate statuses `accepted`/`declined`, `document_status`, `recurring_status.paused`).
- `billing_documents.frequency_label` is stored text, separate from `recurring_invoices`.
- Credits are inferred from "Credit" rows on the Payments report (no Credits screenshot).
- New Customers: a customer can have several service locations and several jobs, so Service Address is the first location (lowest id), Job Created Date is the first job (earliest created, not deleted, any status) and Completed Jobs counts the completed jobs that are not deleted. Confirm these rules against the real screen.
- Documents: the avatar shown next to Created By has no source (`users` stores only `full_name` and `email`), so the report returns the creator's name only. Adding it would be a feature: a `users` picture column (or URL) and a `created_by` object instead of a string.
- Customer statuses (`active`, `inactive`, `lead`, `archived`) are inferred from the "All Statuses" filter; confirm the real values.
- "Locations w/o Active Jobs" is the screen the request called "Address w/o Active Jobs".
- The seed figures for tax, items, cards and credit use are plausible October 2026 data, not the exact screenshot numbers.
- `jobs.tax_amount` is stored on the job because a job may have no invoice yet; invoice tax comes from `invoice_tax_lines`.
- By Tags in Sales Forecast / Revenue counts a fact under the tags of its job (`job_tags`) and of its invoice (`invoice_tags`), matched by name; an invoice tag with no same-named job tag is identified by its negated id. By Staff in Revenue is the staff member who took the payment (`payments.received_by`); in the Forecast it is the job's assignee.
- Material Use: every job adds its own `job_materials` rows, so Qty is summed and # Of Applications counts those rows per material. The date is the job's scheduled date (`jobs.scheduled_at`); a job that was never scheduled is left out as soon as a date range is applied, and a deleted job is always left out. Cancelled jobs still count. `job_materials.device` is free text for the equipment lent to the customer on request (there is no devices table); the Jobs tab is the rows without a device and the Devices tab the rows with one. Confirm these rules, and the EPA registration numbers and quantities in the seed (plausible, not from a screenshot), against the real screen.
- System Email History: the statuses (`unconfirmed`, `confirmed`, `cancelled`) come from the user description, like the confirmed / unconfirmed / cancelled states elsewhere, and the email types (`welcome`, `invoice`, `estimate`, `appointment_confirmation`, `appointment_reminder`, `payment_receipt`, `review_request`) are the general ones a field-service system sends; the screen was empty, so confirm both. The date range is the time the email was sent. The "First name" dropdown only changes how the Customer column is shown (`name_format`): a customer with no first (or last) name shows the full display name. The `customer` sort always uses the full display name. `system_emails.email` is the address the email was sent to, which can differ from the customer address. The seed adds four November emails so the report is not empty here, although the real screen showed none.
- Inbound Leads: a lead is a customer whose `status` is `lead`, so there is no leads table. The screen's year chart and source pie are not built yet (no endpoint). Date Added and Time Added are one column, `date_added`, the creation instant (UTC); the screen splits it into a date and a time. The date range is the day the lead was added. "NA" in the source filter is `source_id=none`. Active Jobs Schedule is the earliest scheduled job that is not deleted and still open (unconfirmed or confirmed), not the next one after today; cancelled, completed and never-scheduled jobs do not count. The "Columns (8)" picker is screen-side, so the API always returns every column. The seed has one lead (customer 63730, added on October 8 with no source, as in the screenshot); the tests add the others themselves so the other reports' totals do not move.
- Online Bookings: a booking is a job of a customer whose `customers.booked_online` is true. The flag is set per customer and nothing derives it from the lead source yet, so setting it when a customer is created online is a later feature; the seed flags 63714 (source Facebook), 63689, 63716 and 63749. The source shown and filtered is the customer's own (`customers.lead_source_id`), not the job's (`jobs.lead_source_id`), and `source_id=none` is the customers without one. Date and Time are one column, the appointment (`jobs.scheduled_at`); a job that was never scheduled is not a booking row, and a deleted job never is. Cancelled jobs are listed with their status. Notes are the customer's top note, since jobs have none. The screen's bookings-per-month chart is not built.
- Device Tracking: a device is installed once at a service location and checked on many jobs, so what stays the same is in `devices` and each visit is a `job_device_checks` row; the report has one row per check. This is not `job_materials.device`, the free-text equipment lent to a customer. The address shown is the job's own location, not the device's, so a device checked on a job at another location still shows the job's address. `status`, `activity` and `notes` are free text (the screen shows values such as Good, 40 and 10). Check In and Check Out are timestamps, null until the device is checked. The Date is the job's appointment, so a job with none is not listed, and neither is a deleted job; cancelled jobs are. The screen's Columns picker and its "Export 21 records" label are screen-side, so the API has neither. The seed adds six devices and eight checks on existing jobs of customers 63706, 63716 and 63749 (no customer, location or job is added); the tests add the other cases themselves. The screen's coordinates and names (QA device ...) are sample values, not the screenshot's exact rows.
