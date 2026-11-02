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
| `group` | multi | Ids of the chosen grouping (service type, item, staff, lead source or tag ids; customer ids for clients). A tag that exists only as an invoice tag has a **negative** id (see [Groupings](#groupings)); no other id is negative. | all |
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
| | every **credit application** to an invoice, i.e. credit that was paid for and is now used: its amount (or less its share of the invoice's tax, as above) | the day it is applied |

A payment counts whatever became of its invoice (voided, archived or deleted), as in
[Payments Collected](payments-collected.md): it is cash that was collected, unless the payment itself was refunded,
failed or deleted.

Credit payments (buying a credit) are not revenue; the credit is revenue when it is applied to an invoice, and only a
credit with a paid, not deleted payment counts. A credit application follows the payment method of its credit (the
`method` filter). Pending, failed and refunded payments do not count. Recurring invoices are not
projected into the forecast. Unscheduled jobs are not forecast.

### Groupings

| `by` | An amount belongs to | It goes to `unassigned` when |
|---|---|---|
| `client` | the job's, invoice's or payment's customer | never |
| `service` | the service type of the job (for revenue, of the paid invoice's job) | there is no job |
| `item` | each line item of the invoice, at its share of the amount: `amount × line total ÷ sum of the invoice's line totals`, rounded to the cent. A forecast job is split over the lines of its invoices that are not void or deleted. | there are no lines (for a job: no invoice yet) |
| `staff` | forecast: the job's assignee. Revenue: the staff member who took the payment, as in [Payments Collected](payments-collected.md) | forecast: there is no job, or nobody is assigned. Revenue: nobody took the payment (an online card payment) or it is used credit |
| `source` | the customer's lead source | the customer has no source |
| `tag` | **each** distinct tag of the job and of its invoice (a forecast job: of its invoices). Only invoices that are not void or deleted lend their tags, archived ones do | there is no tag on either |

Tags are told apart by name (ignoring case): a job tag and an invoice tag with the same name are one row, with the job
tag's id. An invoice tag that no job tag shares a name with is a row of its own, with the **negated** id of the lowest
invoice tag of that name (so `VIP` and `vip` are one row, and the other ids of that name are not valid in `group`).
A payment on a void or deleted invoice is still revenue, but it takes the tags of its job only.
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
    "months": ["0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","10.00","708.80","0.00","0.00"],
    "total": "718.80"
  },
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "summary": {
    "count": 1,
    "months": ["0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","10.00","861.06","0.00","0.00"],
    "total": "871.06"
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
  the second is the [Payments Collected](payments-collected.md) list for the payments, plus the credit applications of
  the year, which Revenue counts on the day they are applied and Payments Collected does not list (they are not cash
  received; the [Sales Tax Summary](sales-tax-summary.md) counts them too).

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
    "tag": [{ "id": -1, "name": "a" }, { "id": 1, "name": "VIP" }]
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
