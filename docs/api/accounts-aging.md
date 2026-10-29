# Accounts Aging report API

Read-only. All endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`; money is a
decimal string with two places (`"148.56"`), never a JSON number.

The report lists what customers still owe: the unpaid balance of their invoices, split by how old the invoices are,
with the customer's default saved card and available credit. One row is one **customer and service location**.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/accounts-aging` | One page of rows, plus the totals on the first page |
| `GET /api/v1/reports/accounts-aging/filters` | The Days options, the service types, the sort keys and default sort |

The **Current** and **Point in Time** tabs are one endpoint: without `as_of` the report is read as of today, and with
`as_of` it is read as of that day.

## `GET /api/v1/reports/accounts-aging`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes `from` and `to`: the report has an
as-of date, not a date range.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `as_of` | `YYYY-MM-DD` | The day the balances are read at (the **Point in Time** tab). It must not be in the future. | today in the server's `APP_TIMEZONE` (UTC by default) |
| `days` | `30` \| `60` \| `90` | Keep only rows that have a balance older than that many days: 31 or more, 61 or more, 91 or more. | all rows |
| `service_type` | multi | Service type ids. Only invoices whose job is of one of these types count, so the buckets cover only those invoices. | all |
| `email` | text | Case-insensitive substring of the invoice billing email. Only matching invoices count. | none |
| `q` | text | Case-insensitive substring of the customer name, account number, service address or city. LIKE wildcards are matched literally. | none |
| `sort` | key | `customer`, `total`, `d0_30`, `d31_60`, `d61_90`, `d90_plus`, `available_credit` | `customer` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: customer name A to Z. With `sort` but no `order`: ascending. Ties break by row key in the same direction. | see left |
| `limit` | int | Page size, 1 to 200 | 50 |
| `cursor` | text | The `next_cursor` of the previous page, valid only with the same `sort` and `order` | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?service_type=1&service_type=2`, `?service_type=1,2`. Blanks and duplicates are ignored.

### What counts

| Rule | Detail |
|---|---|
| Invoices | Invoices that are **sent** or **paid**, not archived or deleted, dated on or before `as_of`. Drafts, voids and estimates are not receivables. |
| Balance | The invoice total, less its paid, non-deleted payments dated on or before `as_of`, less the credit applied to it on or before `as_of`. Only balances above zero are listed. A paid invoice therefore shows up for an earlier `as_of` if it was not yet paid then. |
| Age | `as_of` minus the **invoice date**, in days. There is no due date. |
| Buckets | `d0_30` is 0 to 30 days old, `d31_60` 31 to 60, `d61_90` 61 to 90, `d90_plus` 91 or more. Every balance falls in exactly one bucket, so the four add up to `total`. |
| Rows | One per customer and service location of the invoice's job. An invoice whose job has no location, or that has no job, falls under its customer with `location_id` and `location` set to `null`. |
| Available credit | Per customer, not per location: the customer's credits dated on or before `as_of`, less the credit applications dated on or before `as_of`, never below zero. It repeats on each of the customer's rows. |

The status of an invoice is its current status, also for a Point in Time view. Payments and credit applications are
replayed by their dates, but an invoice that was voided, archived or deleted since then is gone from the earlier
days too.

### Response

```json
{
  "data": [
    {
      "customer_id": 6,
      "location_id": 4,
      "customer": "AAA1 ccc1",
      "location": "Miami Beach Boardwalk 2, Miami",
      "billing_email": "ap@example.com",
      "phone": "555-0100",
      "credit_card": { "processor": "stripe", "last4": "3222", "expires": "12/2032" },
      "d0_30": "70.00",
      "d31_60": "0.00",
      "d61_90": "0.00",
      "d90_plus": "0.00",
      "total": "70.00",
      "available_credit": "300.00"
    }
  ],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "summary": {
    "count": 1, "d0_30": "70.00", "d31_60": "0.00", "d61_90": "0.00", "d90_plus": "0.00",
    "total": "70.00", "available_credit": "300.00"
  },
  "as_of": "2026-10-21"
}
```

- `billing_email` is the billing email of the row's most recent invoice that has one, or `null`. `phone` is the
  customer's first phone, or `null`.
- `credit_card` is the customer's default saved card (`stripe` or `square`; `expires` is `MM/YYYY`), or `null`.
- `summary` covers the **whole filtered set**, not just the page. It is present on the first page (no `cursor`) and
  `null` on later pages. Its available credit counts each customer once, however many rows the customer has. Rows and
  summary are read in one database snapshot.
- `as_of` is the day actually applied, so a client can show the defaulted today.
- `data` is always an array, never `null`.

### Paging

Keyset pagination. Send `next_cursor` back as `cursor` with the same `sort` and `order`. A cursor is opaque: do not
build or edit one. A cursor used with another sort is a 422.

The row key behind the cursor is `customer_id * 2^32 + location_id`, so it assumes customer and location ids below
2^31.

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
GET /api/v1/reports/accounts-aging?as_of=2030-01-01&days=45
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "01a166d8-78c8-730e-8798-1958f1895adc",
    "details": [
      { "field": "as_of", "message": "must not be in the future" },
      { "field": "days", "message": "must be one of: 30, 60, 90" }
    ]
  }
}
```

#### Strictness

A typo such as `?service_types=1` returns `422` with `{"field":"service_types","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/accounts-aging/filters`

Everything needed to build the filter controls.

```json
{
  "days": [30, 60, 90],
  "service_types": [{ "id": 2, "name": "Bi-Monthly Service" }],
  "sort_keys": ["customer", "total", "d0_30", "d31_60", "d61_90", "d90_plus", "available_credit"],
  "default_sort": { "key": "customer", "order": "asc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Who owes money today
curl.exe "http://localhost:8080/api/v1/reports/accounts-aging"

# The same report as it stood on 30 September
curl.exe "http://localhost:8080/api/v1/reports/accounts-aging?as_of=2026-09-30"

# Only customers with a balance older than 90 days, biggest 90+ first
curl.exe "http://localhost:8080/api/v1/reports/accounts-aging?days=90&sort=d90_plus&order=desc"

# Next page
curl.exe "http://localhost:8080/api/v1/reports/accounts-aging?limit=50&cursor=<next_cursor>"
```
