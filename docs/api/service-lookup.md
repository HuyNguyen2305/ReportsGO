# Service Lookup report API

Read-only. All endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`; money is a
decimal string with two places (`"65.00"`), never a JSON number.

The report lists jobs with their customer, address, value and tags. One row is one job. Every field is always
returned: the choice of which columns to show is a display setting of the screen.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/service-lookup` | One page of jobs, plus the totals and tab counts on the first page |
| `GET /api/v1/reports/service-lookup/filters` | The tabs, statuses, service types, staff, tags, sort keys and default sort |

## `GET /api/v1/reports/service-lookup`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes the location filters `state`,
`city`, `zip` and `county`: those columns are returned and sortable, not filters.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `tab` | `active` \| `work_pool` \| `deleted` | See [Tabs](#tabs) | `active` |
| `from`, `to` | `YYYY-MM-DD` | Inclusive range of the job's **scheduled day**, in the server's `APP_TIMEZONE` (UTC by default). If **both** are missing: the Active and Deleted tabs use the current month and the Work Pool is open. If only one is sent, the other end is open. `to` must not be before `from`. A job that was never scheduled never matches a range. | see left |
| `status` | multi | `unconfirmed`, `confirmed`, `completed`, `cancelled` | all |
| `service_type` | multi | Service type ids | all |
| `assigned_to` | multi | Staff ids | all |
| `tag` | multi | Tag ids. A job matches if it has **any** of them, and is listed once. | all |
| `q` | text | Case-insensitive substring of the customer name, account number, phone, service type, address line and city. LIKE wildcards are matched literally. | none |
| `sort` | key | `customer`, `service_type`, `scheduled_at`, `address`, `city`, `zip`, `county`, `invoice_value`, `without_tax`, `status`, `assigned_to` | `scheduled_at` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: earliest scheduled first. With `sort` but no `order`: ascending. Unscheduled jobs and unassigned jobs sort first when ascending. Ties break by job id in the same direction. | see left |
| `limit` | int | Page size, 1 to 200 | 50 |
| `cursor` | text | The `next_cursor` of the previous page, valid only with the same `sort` and `order` | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?tag=1&tag=2`, `?tag=1,2`. Values are trimmed and case-insensitive; blanks and duplicates are ignored.

### Tabs

| Tab | Shows |
|---|---|
| `active` | Jobs that are not deleted and not in the work pool |
| `work_pool` | Jobs in the work pool that are not deleted |
| `deleted` | Deleted jobs, whether or not they were in the work pool |

Every job is on exactly one tab.

### Response

```json
{
  "data": [
    {
      "id": 98,
      "customer": { "id": 12, "name": "Wayne Enterprises" },
      "phone": "555-0100",
      "service_type": { "id": 3, "name": "Bi-Monthly Service" },
      "scheduled_at": "2026-10-15T08:00:00.000000Z",
      "address": { "line1": "12 Elm St", "line2": "", "city": "Austin", "state": "TX", "zip": "78701", "county": "Travis County" },
      "invoice_value": "65.00",
      "without_tax": "60.00",
      "status": "confirmed",
      "assigned_to": { "id": 2, "name": "thon nguyen" },
      "tags": [{ "id": 4, "name": "Follow Up" }, { "id": 1, "name": "VIP" }]
    }
  ],
  "page": { "limit": 50, "has_more": true, "next_cursor": "eyJr..." },
  "summary": {
    "count": 120, "invoice_value": "7800.00", "without_tax": "7200.00",
    "tabs": { "active": 120, "work_pool": 8, "deleted": 3 }
  },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

- `scheduled_at` is a UTC timestamp, or `null` for a job that was never scheduled. `phone` is the customer's first
  phone, or `null`. `assigned_to` is `null` for an unassigned job. `tags` is `[]` for an untagged job, ordered by name.
- `without_tax` is the job's `invoice_value` less its tax.
- `summary` totals the **whole filtered set of the selected tab**, not just the page. It is present on the first page
  (no `cursor`) and `null` on later pages. Rows and summary are read in one database snapshot.
- `summary.tabs` is how many jobs each tab would show under the **same filters and the same effective date range**,
  including the defaulted month. A client that switches tab should send an explicit range, because the Work Pool's
  default range is open and the other tabs' is the current month.
- `range` is the date range actually applied, so a client can show the defaulted month; an open end is `null`.
- `data` is always an array, never `null`.

### Paging

Keyset pagination. Send `next_cursor` back as `cursor` with the same `sort` and `order`. A cursor is opaque: do not
build or edit one. A cursor used with another sort is a 422.

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
GET /api/v1/reports/service-lookup?tab=archived&tag=x
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "01a166d8-78c8-730e-8798-1958f1895adc",
    "details": [
      { "field": "tab", "message": "must be one of: active, work_pool, deleted" },
      { "field": "tag", "message": "must be positive whole numbers" }
    ]
  }
}
```

#### Strictness

A typo such as `?tags=1` returns `422` with `{"field":"tags","message":"is not a supported parameter"}` instead of
silently returning unfiltered data. Unsupported parameters are reported on their own, before any value is validated;
fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/service-lookup/filters`

Everything needed to build the filter controls.

```json
{
  "tabs": ["active", "work_pool", "deleted"],
  "statuses": ["unconfirmed", "confirmed", "completed", "cancelled"],
  "service_types": [{ "id": 2, "name": "Bi-Monthly Service" }],
  "staff": [{ "id": 1, "name": "Thọ Nguyễn Quang" }],
  "tags": [{ "id": 4, "name": "Follow Up" }],
  "sort_keys": ["customer", "service_type", "scheduled_at", "address", "city", "zip", "county", "invoice_value", "without_tax", "status", "assigned_to"],
  "default_sort": { "key": "scheduled_at", "order": "asc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# This month's scheduled jobs, earliest first
curl.exe "http://localhost:8080/api/v1/reports/service-lookup"

# The work pool, biggest jobs first
curl.exe "http://localhost:8080/api/v1/reports/service-lookup?tab=work_pool&sort=invoice_value&order=desc"

# Confirmed jobs with a VIP or Commercial tag assigned to staff 2, on a date range
curl.exe "http://localhost:8080/api/v1/reports/service-lookup?from=2026-10-01&to=2026-10-15&status=confirmed&tag=1,2&assigned_to=2"

# Next page
curl.exe "http://localhost:8080/api/v1/reports/service-lookup?limit=50&cursor=<next_cursor>"
```
