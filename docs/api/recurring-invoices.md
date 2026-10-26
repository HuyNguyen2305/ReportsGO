# Recurring Invoices report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`; money is a
decimal string with two places (`"65.00"`), never a JSON number.

It works like the [Invoices](invoices.md), [Estimates](estimates.md), [Documents](documents.md),
[Payments](payments.md) and [Credits](credits.md) reports, with three differences:

- **There is no default date range.** Recurring invoices are long-lived, so without `from` and `to` the report
  covers all of them (the other reports default to the current month).
- **There is no `lifecycle` (Active / Deleted) tab**: a recurring invoice cannot be deleted or archived.
- The `status` filter has this report's own values (`active`, `paused`, `failed`, `ended`), and there is an
  `action` filter.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/recurring-invoices` | One page of the report, with filters, search, sorting, a count and a total |
| `GET /api/v1/reports/recurring-invoices/filters` | The choices behind the filter controls, plus defaults and limits (fixed; no database lookup) |

## `GET /api/v1/reports/recurring-invoices`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes `lifecycle` and `method`, and
the parameters of the other reports: `signature_status`, `service_id`, `tag_id`, `payment_term_id` and
`document_type_id`.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the **start date** (`started_on`). If only one is sent, the other end is open. `to` must not be before `from`. | none (all) |
| `status` | multi | `active`, `paused`, `failed`, `ended` | all |
| `action` | multi | `send_by_email`, `create_only` | all |
| `q` | text | Case-insensitive substring search over the customer name, the account number and the **next invoice's number**. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `customer`, `account_number`, `repeats`, `action`, `status`, `total_value`, `started_on`, `ends_on`, `next_invoice_date` | `started_on` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest start first (`started_on` descending). With `sort` but no `order`: ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?status=paused&status=ended`, `?status=paused,ended`, `?status=paused,ended&status=paused`. Values are trimmed
and case-insensitive; blanks and duplicates are ignored.

**Sorting notes.** `repeats` orders by how often the invoice repeats, shortest period first (every day, every
week, every 2 weeks, every month, every 2 months, every year; a month counts as 30 days and a year as 365).
`ends_on` and `next_invoice_date` sort recurring invoices **without** that date after all the others when
ascending (and first when descending). Every sort is stable: ties are broken by id in the same direction.

Number columns are identifiers, not quantities: `account_number` sort as text, character by character, not numerically, so `100` comes before `9`.

### Response

```json
{
  "data": [
    {
      "id": 7,
      "customer": { "id": 6, "name": "AAA1 ccc1" },
      "account_number": "63706",
      "repeats": { "unit": "day", "interval": 3, "weekday": null, "occurrences": 10, "label": "Every 3 days, 10 times" },
      "action": "create_only",
      "status": "active",
      "total_value": "15.00",
      "started_on": "2026-10-15",
      "ends_on": null,
      "next_invoice": { "invoice_id": null, "invoice_number": null, "date": "2026-10-18" }
    },
    {
      "id": 1,
      "customer": { "id": 9, "name": "Bukayo Saka" },
      "account_number": "63689",
      "repeats": { "unit": "week", "interval": 1, "weekday": 4, "occurrences": 3, "label": "Every week on Thursday, 3 times" },
      "action": "send_by_email",
      "status": "active",
      "total_value": "65.00",
      "started_on": "2026-09-14",
      "ends_on": null,
      "next_invoice": { "invoice_id": 4, "invoice_number": "84890", "date": "2026-09-24" }
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoicmVwZWF0cyIsImQiOmZhbHNlLCJ2IjoiMDAwMDAwMDAwNyIsImlkIjo2fQ" },
  "summary": { "count": 7, "total": "713.49" },
  "range": { "from": null, "to": null }
}
```

(abridged from `?limit=3&sort=repeats&order=asc`)

- **`repeats`** is the repeat rule. `unit` is `day`, `week`, `month` or `year`; `interval` is how many units
  between invoices; `weekday` is `0` (Sunday) to `6` (Saturday) or `null`; `occurrences` is how many invoices to
  create in total, or `null` for no limit. `label` is a ready-made description. The structured fields are the
  contract; use `label` when you only need text.
- **`next_invoice`** is always an object, but each part is `null` when unknown: a recurring invoice can have a
  due `date` before its invoice has been created (`invoice_id` and `invoice_number` are `null`), and one that has
  ended has none of the three.
- `ends_on` is `null` when there is no end date.
- `summary` counts and totals the **whole filtered set**, not just this page, and is only present on the
  **first page** (when no `cursor` is sent). It is `null` on later pages. `total` adds up `total_value` of every
  recurring invoice in scope whatever its status; send `status=active` to total only the active ones.
- `range` is the start-date range actually applied; an end that was not sent is `null`, so with no dates it is
  `{ "from": null, "to": null }`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`.

#### How `repeats.label` is built

| Rule | Label |
|---|---|
| interval 1 | `Every day`, `Every week`, `Every month`, `Every year` |
| interval n | `Every 3 days`, `Every 2 weeks`, `Every 2 months`, `Every 10 years` |
| weekly rule with a `weekday` | adds ` on <Weekday>`: `Every week on Thursday`, `Every 2 weeks on Tuesday` |
| `occurrences` set | adds `, 3 times` (`, 1 time` for one) |

The weekday is only mentioned for weekly rules; on a daily, monthly or yearly rule it is still returned in
`repeats.weekday` but not put in the label, because "Every month on Thursday" would read ambiguously.

### Paging

1. Request the first page (no `cursor`). Keep `summary` from it.
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it stays fast on deep pages and never skips or repeats a row, even when rows are added
between requests. There is no "jump to page N".

### Errors

All errors use the same envelope as the other reports. `request_id` equals the `X-Request-ID` response header (a
well-formed `X-Request-ID` sent by the client is reused).

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid, a parameter is unknown, or a single-value parameter is repeated. `details` names each field. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

```http
GET /api/v1/reports/recurring-invoices?status=paid&action=card&sort=credit_date&limit=0
X-Request-ID: demo-124

HTTP/1.1 422 Unprocessable Entity
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "demo-124",
    "details": [
      { "field": "status", "message": "must be one of: active, paused, failed, ended" },
      { "field": "action", "message": "must be one of: send_by_email, create_only" },
      { "field": "sort", "message": "must be one of: customer, account_number, repeats, action, status, total_value, started_on, ends_on, next_invoice_date" },
      { "field": "limit", "message": "must be a whole number between 1 and 200" }
    ]
  }
}
```

#### Strictness

A typo such as `?statuss=active`, or a parameter this report does not have, returns `422` with
`{"field":"statuss","message":"is not a supported parameter"}` instead of silently returning unfiltered data.
This applies to `lifecycle` too, since there is no Active / Deleted tab:

```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "demo-123",
    "details": [ { "field": "lifecycle", "message": "is not a supported parameter" } ]
  }
}
```

Unsupported parameters are reported on their own, before any value is validated; fix them, and any remaining
value errors are reported next.

## `GET /api/v1/reports/recurring-invoices/filters`

Everything needed to build the filter controls. The lists and limits are fixed, so the frontend never has to
hard-code them. There are no tabs to list.

```json
{
  "statuses": ["active", "paused", "failed", "ended"],
  "actions": ["send_by_email", "create_only"],
  "sort_keys": ["customer", "account_number", "repeats", "action", "status", "total_value", "started_on", "ends_on", "next_invoice_date"],
  "default_sort": { "key": "started_on", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Everything that needs attention: paused or failed recurring invoices, biggest first
curl.exe "http://localhost:8080/api/v1/reports/recurring-invoices?status=paused,failed&sort=total_value&order=desc"

# Recurring invoices that started this autumn and only create the invoice (no email)
curl.exe "http://localhost:8080/api/v1/reports/recurring-invoices?action=create_only&from=2026-09-01&to=2026-11-30"

# What is coming up next: soonest next invoice date first, three per page
curl.exe "http://localhost:8080/api/v1/reports/recurring-invoices?sort=next_invoice_date&order=asc&limit=3"
```
