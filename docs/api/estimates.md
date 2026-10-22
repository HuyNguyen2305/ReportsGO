# Estimates report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Money is always a **string** with two
decimals (`"24.40"`), never a JSON number; dates are `YYYY-MM-DD`.

It works exactly like the [Invoices report](invoices.md); only the columns, the statuses and the filters differ.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/estimates` | One page of the report, with filters, search, sorting and a summary |
| `GET /api/v1/reports/estimates/filters` | The choices behind the filter controls, plus defaults and limits |

## `GET /api/v1/reports/estimates`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes the invoice-only filters
`service_id`, `tag_id` and `payment_term_id`.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive estimate date range. If **both** are missing the range is the current calendar month (UTC). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `lifecycle` | `active` \| `archived` \| `deleted` | The Active / Archived / Deleted tab. | `active` |
| `status` | multi | `draft`, `sent`, `accepted`, `declined` | all |
| `signature_status` | multi | `draft`, `pending`, `signed`, `declined` | all |
| `q` | text | Case-insensitive substring search over estimate number, account number, customer name and billing email. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `account_number`, `estimate_number`, `customer`, `phone`, `billing_email`, `created_by`, `date`, `total`, `status`, `signature_status` | `date` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first (`date` descending). With `sort` but no `order`: ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?status=accepted&status=declined`, `?status=accepted,declined`, `?status=accepted,declined&status=accepted`.
Values are trimmed and case-insensitive; blanks and duplicates are ignored.

Estimates and invoices have different statuses: `paid` and `void` are invoice statuses and are rejected here,
and `accepted` and `declined` are rejected by the invoice report.

### Response

```json
{
  "data": [
    {
      "id": 44,
      "estimate_number": "123189",
      "account_number": "63717",
      "customer": { "id": 4, "name": "# Â" },
      "phone": null,
      "billing_email": null,
      "created_by": "Thọ Nguyễn Quang",
      "date": "2026-10-21",
      "total": "75.50",
      "status": "declined",
      "signature_status": "declined"
    },
    {
      "id": 43,
      "estimate_number": "123188",
      "account_number": "63733",
      "customer": { "id": 2, "name": "Customer 1" },
      "phone": null,
      "billing_email": null,
      "created_by": "Thọ Nguyễn Quang",
      "date": "2026-10-21",
      "total": "150.00",
      "status": "accepted",
      "signature_status": "signed"
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoiZGF0ZSIsImQiOnRydWUsInYiOiIyMDI2LTEwLTIxIiwiaWQiOjQzfQ" },
  "summary": { "count": 19, "total": "698.12" },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

An estimate whose customer has a phone and an email, from `?q=123183`:

```json
{
  "id": 34,
  "estimate_number": "123183",
  "account_number": "63714",
  "customer": { "id": 1, "name": "#F Last Name" },
  "phone": "012222222",
  "billing_email": "d@cm.co",
  "created_by": "Thọ Nguyễn Quang",
  "date": "2026-10-19",
  "total": "24.40",
  "status": "draft",
  "signature_status": "draft"
}
```

- `phone` is the customer's first phone number. It is `null` when the customer has none. `billing_email` and
  `created_by` are `null` when unknown.
- `summary` totals the **whole filtered set**, not just this page, and is only present on the **first page**
  (when no `cursor` is sent). It is `null` on later pages.
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

All errors use the same envelope as the Invoices report. `request_id` equals the `X-Request-ID` response header (a
well-formed `X-Request-ID` sent by the client is reused).

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid, a parameter is unknown, or a single-value parameter is repeated. `details` names each field. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

```http
GET /api/v1/reports/estimates?limit=0&sort=discount&status=paid
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
      { "field": "status", "message": "must be one of: draft, sent, accepted, declined" },
      { "field": "sort", "message": "must be one of: account_number, estimate_number, customer, phone, billing_email, created_by, date, total, status, signature_status" },
      { "field": "limit", "message": "must be a whole number between 1 and 200" }
    ]
  }
}
```

#### Strictness

A typo such as `?statuss=draft` returns `422` with `{"field":"statuss","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/estimates/filters`

Everything needed to build the filter controls. The choices are fixed, so this endpoint does not touch the
database; the lists and limits mean the frontend never has to hard-code them.

```json
{
  "statuses": ["draft", "sent", "accepted", "declined"],
  "signature_statuses": ["draft", "pending", "signed", "declined"],
  "lifecycles": ["active", "archived", "deleted"],
  "sort_keys": ["account_number", "estimate_number", "customer", "phone", "billing_email", "created_by", "date", "total", "status", "signature_status"],
  "default_sort": { "key": "date", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET`, `HEAD` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Accepted or declined estimates, largest first
curl.exe "http://localhost:8080/api/v1/reports/estimates?status=accepted,declined&sort=total&order=desc"

# Search by customer, in the archived tab
curl.exe "http://localhost:8080/api/v1/reports/estimates?lifecycle=archived&q=alaba"

# Whole of September, two per page
curl.exe "http://localhost:8080/api/v1/reports/estimates?from=2026-09-01&to=2026-09-30&limit=2"
```
