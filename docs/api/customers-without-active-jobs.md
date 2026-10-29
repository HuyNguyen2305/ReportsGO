# Customers and Locations w/o Active Jobs report API

Read-only. All endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`.

Two lists share one rule and the same parameters: customers, and service locations, that have no active job.

| Endpoint | Tab | One row is |
|---|---|---|
| `GET /api/v1/reports/customers-without-active-jobs` | Customers w/o Active Jobs | a customer |
| `GET /api/v1/reports/locations-without-active-jobs` | Locations w/o Active Jobs | a service location |
| `GET /api/v1/reports/customers-without-active-jobs/filters` | | The statuses, sort keys and default sort of the customers list (fixed; no database lookup) |
| `GET /api/v1/reports/locations-without-active-jobs/filters` | | The same for the locations list |

## What is listed

- An **active job** is a job that is `unconfirmed` or `confirmed` and not deleted. Work-pool jobs count. Completed,
  cancelled and deleted jobs do not.
- The customers list has every customer with **no active job at all**.
- The locations list has every service location with **no active job of its own**. A customer with an open job at one
  location is not on the customers list, but its other locations are still on the locations list.
- Customers and locations that never had a job are listed, with no last service.
- **Last service** is the latest non-deleted job, whatever its status (cancelled included), by its scheduled time, or its
  creation time when it was never scheduled. Equal times go to the higher job id. The customers list looks at all of the
  customer's jobs, the locations list only at the location's own. With no job, `last_service` is `null`.

## Query parameters

The same for both lists. Anything not listed here is rejected (see [Strictness](#strictness)).

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `status` | multi | The customer's status: `active`, `inactive`, `lead`, `archived`. On the locations list it is the status of the location's customer. | all |
| `q` | text | Case-insensitive substring of the customer name, account number and phone. The locations list also searches the address, city and zip. LIKE wildcards are matched literally. | none |
| `sort` | key | Customers: `status`, `account_number`, `customer`, `last_service_type`, `job_status`, `last_service_date`. Locations add `address`, `city` and `zip`. | `customer` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: customer name A to Z. With `sort` but no `order`: ascending. Rows with no job sort first when ascending. Ties break by row id in the same direction. | see left |
| `limit` | int | Page size, 1 to 200 | 50 |
| `cursor` | text | The `next_cursor` of the previous page, valid only with the same `sort` and `order` | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both: `?status=lead&status=inactive` and
`?status=lead,inactive` are the same. Values are trimmed and case-insensitive; blanks and duplicates are ignored.

## Responses

Customers list:

```json
{
  "data": [
    {
      "id": 11,
      "status": "active",
      "account_number": "5001",
      "customer": "William Saliba",
      "phone": "555-0100",
      "last_service": { "service_type": "Bi-Monthly Service", "job_status": "completed", "date": "2026-10-15" }
    },
    { "id": 10, "status": "active", "account_number": "5002", "customer": "Alaba David", "phone": null, "last_service": null }
  ],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "summary": { "count": 12 }
}
```

Locations list: the same fields, plus the customer and the address.

```json
{
  "data": [
    {
      "id": 31,
      "customer_id": 11,
      "status": "active",
      "account_number": "5001",
      "customer": "William Saliba",
      "phone": "555-0100",
      "address": { "line1": "West Loop 1604 South", "line2": "", "city": "San Antonio", "zip": "78264" },
      "last_service": null
    }
  ],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "summary": { "count": 4 }
}
```

- `id` is the customer's id on the customers list and the location's id on the locations list.
- `phone` is the customer's first phone, or `null`.
- `last_service.date` is the day of the job in the server's `APP_TIMEZONE` (UTC by default).
- `summary.count` is the number of rows in the **whole filtered set**, not just the page. It is present on the first page
  (no `cursor`) and `null` on later pages. Rows and count are read in one database snapshot.
- `data` is always an array, never `null`.
- The statuses `active`, `inactive`, `lead` and `archived` are values inferred from the screens; they come from the
  database, so a change there is a one-line change here.

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
GET /api/v1/reports/customers-without-active-jobs?status=vip&sort=city
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "01a166d8-78c8-730e-8798-1958f1895adc",
    "details": [
      { "field": "status", "message": "must be one of: active, inactive, lead, archived" },
      { "field": "sort", "message": "must be one of: status, account_number, customer, last_service_type, job_status, last_service_date" }
    ]
  }
}
```

#### Strictness

A typo such as `?statuses=lead` returns `422` with `{"field":"statuses","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET .../filters`

Everything needed to build the filter controls. The customers list and the locations list differ in their sort keys.

```json
{
  "statuses": ["active", "inactive", "lead", "archived"],
  "sort_keys": ["status", "account_number", "customer", "last_service_type", "job_status", "last_service_date"],
  "default_sort": { "key": "customer", "order": "asc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Customers with nothing booked, A to Z
curl.exe "http://localhost:8080/api/v1/reports/customers-without-active-jobs"

# Those who were served longest ago first (customers that never had a job come before them)
curl.exe "http://localhost:8080/api/v1/reports/customers-without-active-jobs?sort=last_service_date"

# Idle locations of leads and inactive customers, searching an address
curl.exe "http://localhost:8080/api/v1/reports/locations-without-active-jobs?status=lead,inactive&q=elm"
```
