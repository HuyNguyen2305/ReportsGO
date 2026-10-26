# Invoices report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Money is always a **string** with two
decimals (`"148.56"`), never a JSON number; dates are `YYYY-MM-DD`.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/invoices` | One page of the report, with filters, search, sorting and a summary |
| `GET /api/v1/reports/invoices/filters` | The choices behind the filter controls, plus defaults and limits |

## `GET /api/v1/reports/invoices`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)).

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive invoice date range. If **both** are missing the range is the current calendar month (UTC). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `lifecycle` | `active` \| `archived` \| `deleted` | The Active / Archived / Deleted tab. | `active` |
| `status` | multi | `draft`, `sent`, `paid`, `void` | all |
| `signature_status` | multi | `draft`, `pending`, `signed`, `declined` | all |
| `service_id` | multi | Service type ids (from `/filters`). Matches the service of the invoice's job. | all |
| `tag_id` | multi | Tag ids. An invoice matches if it has **any** of them. | all |
| `payment_term_id` | multi | Payment terms ids. | all |
| `q` | text | Case-insensitive substring search over invoice number, account number, customer name and billing email. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `account_number`, `invoice_number`, `customer`, `job`, `billing_email`, `date`, `total`, `discount`, `status`, `signature_status`, `payment_terms` | `date` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first (`date` descending). With `sort` but no `order`: ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?status=paid&status=sent`, `?status=paid,sent`, `?status=paid,sent&status=paid`. Values are trimmed and
case-insensitive; blanks and duplicates are ignored.

### Response

```json
{
  "data": [
    {
      "id": 19,
      "invoice_number": "84936",
      "account_number": "5001",
      "customer": { "id": 11, "name": "William Saliba" },
      "job": "Bi-Monthly Service (West Loop 1604 South)",
      "frequency": "Weekly",
      "billing_email": "williamsaliba@yopmail.com",
      "date": "2026-10-26",
      "total": "65.00",
      "discount": "0.00",
      "tags": [],
      "payment_terms": { "name": "Due on receipt", "description": "Payment is due on receipt." },
      "status": "draft",
      "signature_status": "draft"
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoiZGF0ZSIsImQiOnRydWUsInYiOiIyMDI2LTEwLTIyIiwiaWQiOjI0fQ" },
  "summary": { "count": 21, "total": "1741.06", "discount": "1.23", "outstanding": "264.00" },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

- `job`, `billing_email` and `payment_terms` are `null` when the invoice has none; `tags` is always an array.
- `summary` totals the **whole filtered set**, not just this page, and is only present on the **first page**
  (when no `cursor` is sent). It is `null` on later pages.
  `total` and `discount` include every invoice in the filtered set, void and draft invoices too; add
  `status=sent,paid` (or another `status` filter) to leave them out.
  `outstanding` is the unpaid balance (`total` minus paid payments, never below zero) of the filtered invoices
  whose status is `sent`.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page.

### Paging

1. Request the first page (no `cursor`). Keep `summary` from it.
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it stays fast on deep pages and never skips or repeats a row, even when rows are added
between requests. There is no "jump to page N".

### Errors

All errors use one envelope. `request_id` equals the `X-Request-ID` response header (a well-formed
`X-Request-ID` sent by the client is reused).

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid, a parameter is unknown, or a single-value parameter is repeated. `details` names each field. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

```http
GET /api/v1/reports/invoices?limit=0&sort=bogus&status=x
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
      { "field": "status", "message": "must be one of: draft, sent, paid, void" },
      { "field": "sort", "message": "must be one of: account_number, invoice_number, customer, job, billing_email, date, total, discount, status, signature_status, payment_terms" },
      { "field": "limit", "message": "must be a whole number between 1 and 200" }
    ]
  }
}
```

#### Strictness

A typo such as `?statuss=paid` returns `422` with `{"field":"statuss","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/invoices/filters`

Everything needed to build the filter controls. Use the returned ids for `service_id`, `tag_id` and
`payment_term_id`; the static lists and limits mean the frontend never has to hard-code them.

```json
{
  "service_types": [ { "id": 1, "name": "Bi-Monthly Service" } ],
  "tags": [ { "id": 1, "name": "a" } ],
  "payment_terms": [ { "id": 1, "name": "Due on receipt", "description": "Payment is due on receipt." } ],
  "statuses": ["draft", "sent", "paid", "void"],
  "signature_statuses": ["draft", "pending", "signed", "declined"],
  "lifecycles": ["active", "archived", "deleted"],
  "sort_keys": ["account_number", "invoice_number", "customer", "job", "billing_email", "date", "total", "discount", "status", "signature_status", "payment_terms"],
  "default_sort": { "key": "date", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# October, sent invoices, largest first, two per page
curl.exe "http://localhost:8080/api/v1/reports/invoices?status=sent&sort=total&order=desc&limit=2"

# Tag filter and search; note the URL-encoded @
curl.exe "http://localhost:8080/api/v1/reports/invoices?tag_id=1,2&q=d%40cm.co"

# Whole of September
curl.exe "http://localhost:8080/api/v1/reports/invoices?from=2026-09-01&to=2026-09-30"
```
