# Revenue and Sales Forecast report API

Read-only. All endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Money is a decimal string with two places
(`"148.56"`), never a JSON number.

Revenue and Sales Forecast are the same report: a month grid with one row per group (client, service, item, staff,
source or tag), the twelve months of a year and a total. The `type` parameter picks which one, the way `parent_type`
tells invoices and estimates apart.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/sales-breakdown` | One page of the grid, plus the unassigned line and the totals on the first page |
| `GET /api/v1/reports/sales-breakdown/filters` | The types, groupings, measures, methods, group options, sort keys and year range |

## `GET /api/v1/reports/sales-breakdown`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)).

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `type` | `forecast` \| `revenue` | Which report; see [What counts](#what-counts) | **required** |
| `by` | `client` \| `service` \| `item` \| `staff` \| `source` \| `tag` | What one row is; see [Groupings](#groupings) | **required** |
| `year` | int, 2000 – 2100 | The calendar year. Months are read in the server's `APP_TIMEZONE` (UTC by default). | current year |
| `group` | multi | Ids of the chosen grouping (service type, item, staff, lead source or tag ids; customer ids for clients) | all |
| `measure` | `collected` \| `collected_without_tax` | Revenue only; with `type=forecast` it is a 422 | `collected` |
| `method` | multi | `cash`, `check`, `card`, `other`. Revenue only; with `type=forecast` it is a 422 | all |
| `q` | text | Case-insensitive substring of the group name, and of the account number for clients. LIKE wildcards are matched literally. | none |
| `sort` | key | `name`, `account_number` (clients only), `jan` … `dec`, `total` | `name` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: name A to Z. With `sort` but no `order`: ascending. Ties break by group id in the same direction. | see left |
| `limit` | int | Page size, 1 to 200 | 50 |
| `cursor` | text | The `next_cursor` of the previous page, valid only with the same `sort` and `order` | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both: `?method=cash&method=card` and
`?method=cash,card` are the same. Values are trimmed and case-insensitive; blanks and duplicates are ignored.

### What counts

| `type` | Each amount is | In the month of |
|---|---|---|
| `forecast` | every job that is **not deleted, not cancelled and scheduled**, at its invoice value; plus every **invoice without a job** that is sent or paid and not archived or deleted, at its total | the job's scheduled time (business time zone); the invoice date |
| `revenue` | every **payment** for an invoice that is paid and not deleted: its amount, or with `measure=collected_without_tax` its amount less its share of the invoice's tax (as in [Payments Collected](payments-collected.md)) | the payment date |

Credit payments are not revenue. Pending, failed and refunded payments do not count. Recurring invoices are not
projected into the forecast. Unscheduled jobs are not forecast.

### Groupings

| `by` | An amount belongs to | It goes to `unassigned` when |
|---|---|---|
| `client` | the job's, invoice's or payment's customer | never |
| `service` | the service type of the job (for revenue, of the paid invoice's job) | there is no job |
| `item` | each line item of the invoice, at its share of the amount: `amount × line total ÷ sum of the invoice's line totals`, rounded to the cent. A forecast job is split over the lines of its invoices that are not void or deleted. | there are no lines (for a job: no invoice yet) |
| `staff` | the job's assignee | there is no job, or nobody is assigned |
| `source` | the customer's lead source | the customer has no source |
| `tag` | **each** of the job's tags | there is no job, or it has no tags |

A job with two tags counts under both tag rows, so the tag rows can add up to more than the total. On the item
grouping the rounded shares can differ from the other groupings' totals by a few cents.

### Response

```json
{
  "data": [
    {
      "id": 1,
      "name": "Facebook",
      "account_number": null,
      "months": ["0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","152.26","0.00","0.00"],
      "total": "152.26"
    }
  ],
  "unassigned": {
    "label": "Revenue w/o a source",
    "months": ["0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","10.00","658.80","0.00","0.00"],
    "total": "668.80"
  },
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "summary": {
    "count": 1,
    "months": ["0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","10.00","811.06","0.00","0.00"],
    "total": "821.06"
  },
  "year": 2026,
  "type": "revenue",
  "by": "source"
}
```

- `months` always has twelve entries, January first. `account_number` is set for clients and `null` otherwise.
- A group is listed when it has at least one amount in the year (even if they add up to zero).
- `unassigned` is the line of amounts that belong to no group, with the label the screen shows (for example
  "Sales forecast w/o services", "Additional invoice revenue", "Revenue w/o a source", "Untagged"). It is `null` on
  later pages, when it is zero, and when a `group` or `q` filter is set.
- `summary` is present on the first page (no `cursor`) and `null` on later pages. `count` is the number of rows over
  all pages. `months` and `total` are the **true totals**: every row plus `unassigned`, each amount counted once (a
  job under two tags is counted once). With a `group` or `q` filter they cover the kept rows only.
- Rows, unassigned line and summary are read in one database snapshot.
- The "First name" setting and the Revenue "Details" tab are not part of this endpoint: the first is a display choice,
  the second is the [Payments Collected](payments-collected.md) list.

### Paging

Keyset pagination. Send `next_cursor` back as `cursor` with the same `sort` and `order`. A cursor is opaque: do not
build or edit one. A cursor used with another sort is a 422.

### Errors

All errors use the same envelope as the other reports. `request_id` equals the `X-Request-ID` response header (a
well-formed `X-Request-ID` sent by the client is reused).

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid or missing, a parameter is unknown, or a single-value parameter is repeated. `details` names each field. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

```http
GET /api/v1/reports/sales-breakdown?type=forecast&by=item&method=card
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "01a166d8-78c8-730e-8798-1958f1895adc",
    "details": [
      { "field": "method", "message": "can only be used with type revenue" }
    ]
  }
}
```

#### Strictness

A typo such as `?methods=card` returns `422` with `{"field":"methods","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any value is
validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/sales-breakdown/filters`

Everything needed to build the filter controls. `groups` lists the options of the group filter per grouping; clients
are found with `q` instead. `sort_keys` is the client list; the other groupings have the same keys without
`account_number`.

```json
{
  "types": ["forecast", "revenue"],
  "bys": ["client", "service", "item", "staff", "source", "tag"],
  "measures": ["collected", "collected_without_tax"],
  "methods": ["cash", "check", "card", "other"],
  "groups": {
    "service": [{ "id": 1, "name": "Bi-Monthly Service" }],
    "item": [{ "id": 7, "name": "Travel Fee" }],
    "staff": [{ "id": 2, "name": "thon nguyen" }],
    "source": [{ "id": 3, "name": "Facebook" }],
    "tag": [{ "id": 1, "name": "VIP" }]
  },
  "sort_keys": ["name", "account_number", "jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec", "total"],
  "default_sort": { "key": "name", "order": "asc" },
  "years": { "min": 2000, "max": 2100 },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Sales Forecast by client for this year
curl.exe "http://localhost:8080/api/v1/reports/sales-breakdown?type=forecast&by=client"

# Revenue by service in 2026, without tax, card payments only
curl.exe "http://localhost:8080/api/v1/reports/sales-breakdown?type=revenue&by=service&year=2026&measure=collected_without_tax&method=card"

# Revenue by staff, biggest October first
curl.exe "http://localhost:8080/api/v1/reports/sales-breakdown?type=revenue&by=staff&sort=oct&order=desc"

# Forecast for two tags only
curl.exe "http://localhost:8080/api/v1/reports/sales-breakdown?type=forecast&by=tag&group=1,2"
```
