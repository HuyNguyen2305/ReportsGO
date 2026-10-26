# Payments report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`; money is a
decimal string with two places (`"148.56"`), never a JSON number.

It works like the [Invoices](invoices.md), [Estimates](estimates.md) and [Documents](documents.md) reports; only
the columns, the statuses, the tabs and the filters differ.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/payments` | One page of the report, with filters, search, sorting, a count and a total |
| `GET /api/v1/reports/payments/filters` | The choices behind the filter controls, plus defaults and limits (fixed; no database lookup) |

## `GET /api/v1/reports/payments`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes the parameters of the other
reports: `signature_status`, `service_id`, `tag_id`, `payment_term_id` and `document_type_id`.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the **payment date**. If **both** are missing the range is the current calendar month (in the server's `APP_TIMEZONE`, UTC by default). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `lifecycle` | `active` \| `deleted` | The Active / Deleted tab. Payments cannot be archived, so there is no `archived` tab. | `active` |
| `status` | multi | `paid`, `pending`, `failed`, `refunded` | all |
| `method` | multi | `cash`, `check`, `card`, `other` | all |
| `q` | text | Case-insensitive substring search over the invoice number, the account number and the customer name. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `payment_for`, `customer`, `method`, `payment_date`, `amount`, `status` | `payment_date` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first (`payment_date` descending). With `sort` but no `order`: ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?status=failed&status=refunded`, `?status=failed,refunded`, `?status=failed,refunded&status=failed`. Values are
trimmed and case-insensitive; blanks and duplicates are ignored.

`payment_for` sorts by the invoice number; a payment made for a **credit** has no invoice number and sorts as the
word `credit`, so in ascending order the invoice payments come first and the credits last. Every sort is stable:
ties are broken by payment id in the same direction.

### Response

```json
{
  "data": [
    {
      "id": 11,
      "customer": { "id": 6, "name": "AAA1 ccc1" },
      "account_number": "63706",
      "payment_for": { "type": "credit", "invoice_id": null, "label": "Credit" },
      "method": { "type": "card", "card_brand": "Credit", "card_last4": "3222" },
      "amount": "200.00",
      "payment_date": "2026-10-15",
      "status": "paid"
    },
    {
      "id": 2,
      "customer": { "id": 1, "name": "#F Last Name" },
      "account_number": "63714",
      "payment_for": { "type": "invoice", "invoice_id": 1, "label": "85005" },
      "method": { "type": "card", "card_brand": "Credit", "card_last4": "4242" },
      "amount": "152.26",
      "payment_date": "2026-10-11",
      "status": "paid"
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoiYW1vdW50IiwiZCI6dHJ1ZSwidiI6IjE1Mi4yNiIsImlkIjoyfQ" },
  "summary": { "count": 15, "total": "1158.56" },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

(from `?limit=2&sort=amount&order=desc`)

- `payment_for.type` is `invoice` or `credit`. For an invoice, `label` is the invoice number and `invoice_id` its
  id. For a credit, `label` is `"Credit"` and `invoice_id` is `null`.
- `method.type` is `cash`, `check`, `card` or `other`. `card_brand` and `card_last4` are `null` unless the
  payment was made by card (and can be `null` for a card payment whose details are unknown).
- `summary` counts and totals the **whole filtered set**, not just this page, and is only present on the
  **first page** (when no `cursor` is sent). It is `null` on later pages. The total adds up **every** payment in
  scope whatever its status, so `failed` and `refunded` payments are included; send `status=paid` to total only
  the money that was collected.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`.

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
GET /api/v1/reports/payments?limit=0&lifecycle=archived&status=signed&method=wire
X-Request-ID: demo-123

HTTP/1.1 422 Unprocessable Entity
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "demo-123",
    "details": [
      { "field": "lifecycle", "message": "must be one of: active, deleted" },
      { "field": "status", "message": "must be one of: paid, pending, failed, refunded" },
      { "field": "method", "message": "must be one of: cash, check, card, other" },
      { "field": "limit", "message": "must be a whole number between 1 and 200" }
    ]
  }
}
```

#### Strictness

A typo such as `?statuss=paid` returns `422` with `{"field":"statuss","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/payments/filters`

Everything needed to build the filter controls. The lists and limits are fixed, so the frontend never has to
hard-code them.

```json
{
  "statuses": ["paid", "pending", "failed", "refunded"],
  "methods": ["cash", "check", "card", "other"],
  "lifecycles": ["active", "deleted"],
  "sort_keys": ["payment_for", "customer", "method", "payment_date", "amount", "status"],
  "default_sort": { "key": "payment_date", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Everything that went wrong this month: failed or refunded payments, largest first
curl.exe "http://localhost:8080/api/v1/reports/payments?status=failed,refunded&sort=amount&order=desc"

# Card and check payments in the Deleted tab
curl.exe "http://localhost:8080/api/v1/reports/payments?method=card,check&lifecycle=deleted"

# All payments for invoice 85004 in any month, two per page
curl.exe "http://localhost:8080/api/v1/reports/payments?q=85004&from=2000-01-01&to=2099-12-31&limit=2"
```
