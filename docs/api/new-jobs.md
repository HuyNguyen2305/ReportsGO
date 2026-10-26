# New Jobs report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Timestamps are UTC with six fractional
digits (`"2026-10-22T08:15:30.123456Z"`); dates are `YYYY-MM-DD`; money is a decimal string with two places.

It works like the [Documents](documents.md) report: the date is a UTC creation timestamp and the filter choices
come from the database. Jobs have no Active / Deleted tab, so there is no `lifecycle` parameter.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/new-jobs` | One page of the report, with filters, search, sorting, a count and a total |
| `GET /api/v1/reports/new-jobs/filters` | The choices behind the filter controls (service types and lead sources), plus defaults and limits |

## `GET /api/v1/reports/new-jobs`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes `lifecycle` and the parameters
of the other reports, such as `method` and `document_type_id`.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the day the job was **created** (UTC). If **both** are missing the range is the current calendar month (in the server's `APP_TIMEZONE`, UTC by default). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `status` | multi | `unconfirmed`, `confirmed`, `completed`, `cancelled` | all |
| `service_type_id` | multi | Service type ids (from `/filters`). | all |
| `lead_source_id` | multi | Lead source ids (from `/filters`). Jobs without a source are only matched when no source filter is sent. | all |
| `q` | text | Case-insensitive substring search over customer name, account number and service type name. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `customer`, `account_number`, `service_type`, `source`, `invoice_value`, `created_at`, `status` | `created_at` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first (`created_at` descending). With `sort` but no `order`: ascending. A job without a source sorts as an empty name: first when ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?status=cancelled&status=completed`, `?status=cancelled,completed`. Values are trimmed and case-insensitive for
`status`; blanks and duplicates are ignored.

### Response

```json
{
  "data": [
    {
      "id": 32,
      "customer": { "id": 2, "name": "Customer 1" },
      "account_number": "63733",
      "service_type": { "id": 2, "name": "Initial Service" },
      "source": { "id": 2, "name": "Google" },
      "invoice_value": "95.00",
      "created_at": "2026-10-22T08:15:30.123456Z",
      "status": "cancelled"
    },
    {
      "id": 29,
      "customer": { "id": 11, "name": "William Saliba" },
      "account_number": "5001",
      "service_type": { "id": 1, "name": "Bi-Monthly Service" },
      "source": null,
      "invoice_value": "65.00",
      "created_at": "2026-10-15T08:00:00.000000Z",
      "status": "completed"
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoiY3JlYXRlZF9hdCIsImQiOnRydWUsInYiOiIyMDI2LTEwLTE1VDA4OjAwOjAwLjAwMDAwMFoiLCJpZCI6Mjl9" },
  "summary": { "count": 35, "total": "1861.26" },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

- `source` is `null` when the job has no lead source.
- `invoice_value` is the job's invoice value.
- `created_at` is always UTC with exactly six fractional digits, so every value has the same shape.
- `summary` counts and totals the **whole filtered set**, not just this page, whatever the status of the jobs, and
  is only present on the **first page** (when no `cursor` is sent). It is `null` on later pages.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`.

An empty result keeps the same shape:

```json
{
  "data": [],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "summary": { "count": 0, "total": "0.00" },
  "range": { "from": "2025-01-01", "to": "2025-01-31" }
}
```

### Paging

1. Request the first page (no `cursor`). Keep `summary` from it.
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it stays fast on deep pages and never skips or repeats a row, even when rows are added
between requests. Timestamps in cursors keep their microseconds, so jobs created within the same second are never
lost at a page boundary. There is no "jump to page N".

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
GET /api/v1/reports/new-jobs?lifecycle=active
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "01a166b4-aabc-706f-afb3-a7ee222b96e2",
    "details": [
      { "field": "lifecycle", "message": "is not a supported parameter" }
    ]
  }
}
```

Invalid values are reported together, one detail per field, for example `status=paid` gives
`{"field":"status","message":"must be one of: unconfirmed, confirmed, completed, cancelled"}`.

#### Strictness

A typo such as `?statuss=cancelled` returns `422` with `{"field":"statuss","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/new-jobs/filters`

Everything needed to build the filter controls. Use the returned ids for `service_type_id` and `lead_source_id`;
the static lists and limits mean the frontend never has to hard-code them.

```json
{
  "service_types": [
    { "id": 1, "name": "Bi-Monthly Service" },
    { "id": 3, "name": "Blank Service 123" },
    { "id": 5, "name": "Call Back Service" },
    { "id": 4, "name": "Full service" },
    { "id": 2, "name": "Initial Service" }
  ],
  "lead_sources": [
    { "id": 1, "name": "Facebook" },
    { "id": 2, "name": "Google" },
    { "id": 3, "name": "Referral" }
  ],
  "statuses": ["unconfirmed", "confirmed", "completed", "cancelled"],
  "sort_keys": ["customer", "account_number", "service_type", "source", "invoice_value", "created_at", "status"],
  "default_sort": { "key": "created_at", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Cancelled jobs, jobs without a source first
curl.exe "http://localhost:8080/api/v1/reports/new-jobs?status=cancelled&sort=source"

# One service type and one lead source (ids come from /filters), highest value first
curl.exe "http://localhost:8080/api/v1/reports/new-jobs?service_type_id=2&lead_source_id=2&sort=invoice_value&order=desc"

# Search by customer, account number or service type, two per page
curl.exe "http://localhost:8080/api/v1/reports/new-jobs?q=saliba&limit=2"

# All of September
curl.exe "http://localhost:8080/api/v1/reports/new-jobs?from=2026-09-01&to=2026-09-30"
```
