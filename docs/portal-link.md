# Reading ClientPortalGO data

ReportsGO can report on the invoices, estimates, payments and visits of ClientPortalGO (the sibling project) with no
change to either service's code. The two live in **one Postgres database**, in separate schemas, and a set of views
shows the portal's tables in ReportsGO's shape.

| Schema | Holds | Used by |
|---|---|---|
| `portal` | ClientPortalGO's tables | portal `DATABASE_URL=...?search_path=portal` (its migrations follow it) |
| `public` | ReportsGO's tables and reference data (`users`, `lead_sources`, `tax_rates`, ...) | ReportsGO migrations |
| `reports_live` | the views in `db/portal/reports_live.sql` | ReportsGO `DATABASE_URL=...?search_path=reports_live,public` |

With that `search_path`, each report query finds the view of the same name first and falls back to `public` for
everything that has no view, so no report query changed.

## Setup
1. Put the portal in schema `portal`. A portal that already lives in `public` must move first
   (`ALTER TABLE ... SET SCHEMA portal` for each of its tables and sequences, then `schema_migrations`), because the
   two projects both have `customers`, `billing_documents` and `payments`.
2. Run ReportsGO's migrations against `public` (`task migrate:up` with a URL without the `search_path` above).
3. `task portal:views` creates or refreshes the views (needs `psql`; run it again after either schema changes).
4. Start ReportsGO with `search_path=reports_live,public`.

While `search_path` points at `reports_live`, ReportsGO's own `public` rows for the shadowed tables are hidden. This is
for a portal-backed deployment, not a mixed one.

## Mapping
| View | From | Notes |
|---|---|---|
| `customers`, `customer_phones`, `service_locations`, `service_types` | `customers`, `locations`, `services` | `name` is the display name (first and last name stay empty); status `active`; the portal's one phone is the only phone |
| `billing_documents` | `billing_documents` | cents become dollars; `number` is the document number; `open` and `partially_paid` show as `sent`; an invoice's job is the work order of the estimate it came from |
| `payments` | `payments` | method and card brand/last 4 as the portal stored them (older payments are `other`); no staff; the date is the payment's day in `America/Chicago` |
| `jobs` | `work_orders` + `bookings` | one job per work order; cancelled booking = `cancelled`, `completed` = `completed`, `in_progress` = `confirmed`, `scheduled` = `unconfirmed`; value = the estimate total |
| `invoice_tax_lines`, `credits`, `credit_applications`, `invoice_items`, `invoice_tag_links`, `job_tag_links` | none | empty, so a portal id never joins a same-numbered row in `public` |

## Limits
- **No tax.** The portal has none: W/O Tax equals the total and the Sales Tax Summary is empty for portal data.
- **Staff is unknown**: the portal records who paid, not which staff member took it, so Staff is empty. Payments made before migration 000017 show as `other`.
- **Void invoices** pass through as `void`; ReportsGO's own void rules apply unchanged.
- The payment timezone is a literal in `reports_live.sql`; change it if the portal's `BUSINESS_TZ` is not `America/Chicago`.
- `tests/integration/portal` builds both schemas from `../ClientPortalGO/migrations` and skips when that folder is absent.
