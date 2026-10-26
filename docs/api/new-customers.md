# New Customers report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Timestamps are UTC with six fractional
digits (`"2026-10-22T08:15:30.123456Z"`); dates are `YYYY-MM-DD`.

It works like the [New Jobs](new-jobs.md) report: the date is a creation timestamp (shown in UTC, filtered by day in `APP_TIMEZONE`) and the filter choices
come from the database. Customers have no status and no Active / Deleted tab, so there are no `status` or
`lifecycle` parameters. The summary is a count only, because customers have no monetary value to total.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/new-customers` | One page of the report, with filters, search, sorting and a count |
| `GET /api/v1/reports/new-customers/filters` | The choices behind the filter controls (the lead sources), plus defaults and limits |

## `GET /api/v1/reports/new-customers`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes `status`, `lifecycle` and the
parameters of the other reports, such as `service_type_id` and `method`.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the day the customer was **created**, read in the server's `APP_TIMEZONE` (UTC by default); `from` and `to` are days in that zone, whether sent or defaulted. If **both** are missing the range is the current calendar month (in the server's `APP_TIMEZONE`, UTC by default). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `lead_source_id` | multi | Lead source ids (from `/filters`). Customers without a lead source are only matched when no source filter is sent. | all |
| `q` | text | Case-insensitive substring search over name, account number, email and the phone shown in the report (the customer's first phone). `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `customer`, `account_number`, `email`, `phone`, `source`, `created_at` | `created_at` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first (`created_at` descending). With `sort` but no `order`: ascending. A customer without an email, phone or source sorts as an empty value: first when ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?lead_source_id=1&lead_source_id=2`, `?lead_source_id=1,2`. Values are trimmed; blanks and duplicates are ignored.

Number columns are identifiers, not quantities: `account_number` sort as text, character by character, not numerically, so `100` comes before `9`.

### Response

```json
{
  "data": [
    {
      "id": 15,
      "customer": { "id": 15, "name": "Ada Lovelace" },
      "account_number": "70001",
      "email": "ada@example.com",
      "phone": "555-0101",
      "source": { "id": 2, "name": "Google" },
      "created_at": "2026-10-22T08:15:30.123456Z"
    },
    {
      "id": 2,
      "customer": { "id": 2, "name": "Customer 1" },
      "account_number": "63733",
      "email": null,
      "phone": null,
      "source": null,
      "created_at": "2026-10-16T00:00:00.000000Z"
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoiY3JlYXRlZF9hdCIsImQiOnRydWUsInYiOiIyMDI2LTEwLTE2VDAwOjAwOjAwLjAwMDAwMFoiLCJpZCI6Mn0" },
  "summary": { "count": 14 },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

- `id` and `customer.id` are the same customer id; `customer` has the same shape as in the other reports.
- `email`, `phone` and `source` are `null` when the customer has none. A customer with several phone numbers shows
  the first one only.
- `created_at` is always UTC with exactly six fractional digits, so every value has the same shape, whatever `APP_TIMEZONE` is. Only the `from`/`to` day boundaries follow `APP_TIMEZONE`.
- `summary` counts the **whole filtered set**, not just this page, and is only present on the **first page**
  (when no `cursor` is sent). It is `null` on later pages.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`.

An empty result keeps the same shape:

```json
{
  "data": [],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "summary": { "count": 0 },
  "range": { "from": "2025-01-01", "to": "2025-01-31" }
}
```

### Paging

1. Request the first page (no `cursor`). Keep `summary` from it.
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it stays fast on deep pages and never skips or repeats a row, even when rows are added
between requests. Timestamps in cursors keep their microseconds, so customers created within the same second are
never lost at a page boundary. There is no "jump to page N".

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
GET /api/v1/reports/new-customers?status=active
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "01a166d8-78c8-730e-8798-1958f1895adc",
    "details": [
      { "field": "status", "message": "is not a supported parameter" }
    ]
  }
}
```

Invalid values are reported together, one detail per field, for example `lead_source_id=abc` gives a detail on
`lead_source_id` and `limit=0` gives one on `limit`.

#### Strictness

A typo such as `?lead_source=1` returns `422` with `{"field":"lead_source","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/new-customers/filters`

Everything needed to build the filter controls. Use the returned ids for `lead_source_id`; the static lists and
limits mean the frontend never has to hard-code them.

```json
{
  "lead_sources": [
    { "id": 1, "name": "Facebook" },
    { "id": 2, "name": "Google" },
    { "id": 3, "name": "Referral" }
  ],
  "sort_keys": ["customer", "account_number", "email", "phone", "source", "created_at"],
  "default_sort": { "key": "created_at", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Customers that came from Google, newest first (ids come from /filters)
curl.exe "http://localhost:8080/api/v1/reports/new-customers?lead_source_id=2"

# Search by name, email, account number or phone, two per page
curl.exe "http://localhost:8080/api/v1/reports/new-customers?q=ada&limit=2"

# Sorted by email, customers without one first
curl.exe "http://localhost:8080/api/v1/reports/new-customers?sort=email"

# All of September
curl.exe "http://localhost:8080/api/v1/reports/new-customers?from=2026-09-01&to=2026-09-30"
```
