# Total Sales report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`; money is a
decimal string with two places (`"148.56"`), never a JSON number.

It is a trimmed view of the [Invoices](invoices.md) report: the same invoices, with the columns Invoice, Customer,
Date, Status, Amount and W/O Tax, and a total at the foot.

**What counts as a sale.** Invoices that were **sent or paid** and are neither archived nor deleted. Drafts, voids,
archived and deleted invoices, and estimates, are not sales, so there is no `lifecycle` parameter.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/total-sales` | One page of the report, with filters, search, sorting, a count and totals |
| `GET /api/v1/reports/total-sales/filters` | The statuses, sort keys, default sort and limits (fixed; no database lookup) |

## `GET /api/v1/reports/total-sales`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes `lifecycle`, `signature_status`,
`service_id` and the parameters of the other reports.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the **invoice date**. If **both** are missing the range is the current calendar month (in the server's `APP_TIMEZONE`, UTC by default). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `status` | multi | `paid`, `partial`, `sent`; see [Status](#status) | all |
| `customer_id` | multi | Customer ids. There is no list of customers in `/filters`; find ids with `q` first. | all |
| `q` | text | Case-insensitive substring search over the invoice number, the account number and the customer name. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `invoice_number`, `customer`, `date`, `status`, `amount`, `without_tax` | `date` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first (`date` descending). With `sort` but no `order`: ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?status=sent&status=partial`, `?status=sent,partial`. Values are trimmed and case-insensitive; blanks and
duplicates are ignored.

`invoice_number` and `status` sort as text (so `100` comes before `9`, and `paid` before `partial` before `sent`);
`amount` and `without_tax` sort numerically.

### Status

The status is derived from the invoice and its payments, so it has a value (`partial`) that invoices do not:

| Status | When |
|---|---|
| `paid` | The invoice is paid, or its payments add up to at least its total. |
| `partial` | Some payments were made, but they do not cover the total. |
| `sent` | No payments were made. |

Only payments that are **paid** and not deleted count; pending, failed and refunded payments do not. Credits applied
to an invoice are not counted yet.

### Response

```json
{
  "data": [
    {
      "id": 85,
      "invoice_number": "85058",
      "customer": { "id": 6, "name": "AAA1 ccc1" },
      "date": "2026-10-26",
      "status": "paid",
      "amount": "13.64",
      "without_tax": "12.40"
    },
    {
      "id": 80,
      "invoice_number": "85056",
      "customer": { "id": 6, "name": "AAA1 ccc1" },
      "date": "2026-10-26",
      "status": "partial",
      "amount": "85.40",
      "without_tax": "70.00"
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoiZGF0ZSIsImQiOnRydWUsInYiOiIyMDI2LTEwLTI2IiwiaWQiOjgwfQ" },
  "summary": { "count": 28, "amount": "2006.35", "without_tax": "1794.80" },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

- `amount` is the invoice total. `without_tax` is the amount less the tax on the invoice's tax lines; an invoice with
  no tax lines has `without_tax` equal to `amount`. Surcharges are not tax and stay in both figures.
- `id` is the invoice id, the same as in the Invoices report.
- `summary` totals the **whole filtered set**, not just this page, and is only present on the **first page** (when no
  `cursor` is sent); it is `null` on later pages. The rows and the totals are read from one database snapshot, so
  the totals always describe exactly the set the rows were taken from, even while invoices are being added.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`.

An empty result keeps the same shape:

```json
{
  "data": [],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "summary": { "count": 0, "amount": "0.00", "without_tax": "0.00" },
  "range": { "from": "2025-01-01", "to": "2025-01-31" }
}
```

### Paging

1. Request the first page (no `cursor`). Keep `summary` from it.
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it stays fast on deep pages and never skips or repeats a row, even when rows are added
between requests. Ties are broken by the invoice id, so invoices on the same date keep a stable order. There is no
"jump to page N".

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
GET /api/v1/reports/total-sales?status=void
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "01a166d8-78c8-730e-8798-1958f1895adc",
    "details": [
      { "field": "status", "message": "must be one of: paid, partial, sent" }
    ]
  }
}
```

Invalid values are reported together, one detail per field, for example `customer_id=abc` gives a detail on
`customer_id` and `limit=0` gives one on `limit`.

#### Strictness

A typo such as `?statuss=paid` returns `422` with `{"field":"statuss","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/total-sales/filters`

Everything needed to build the filter controls that do not come from data. The static lists and limits mean the
frontend never has to hard-code them. The customer filter has no list here; use `q` to find customers.

```json
{
  "statuses": ["paid", "partial", "sent"],
  "sort_keys": ["invoice_number", "customer", "date", "status", "amount", "without_tax"],
  "default_sort": { "key": "date", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# This month's sales, newest first
curl.exe "http://localhost:8080/api/v1/reports/total-sales"

# Sales that still have a balance
curl.exe "http://localhost:8080/api/v1/reports/total-sales?status=sent,partial"

# The biggest sales of September, two per page
curl.exe "http://localhost:8080/api/v1/reports/total-sales?from=2026-09-01&to=2026-09-30&sort=amount&order=desc&limit=2"

# One customer's sales (ids come from searching with q)
curl.exe "http://localhost:8080/api/v1/reports/total-sales?customer_id=6"
```
