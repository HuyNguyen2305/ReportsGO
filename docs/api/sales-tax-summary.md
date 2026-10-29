# Sales Tax Summary report API

Read-only. All endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`; money is a
decimal string with two places (`"148.56"`), never a JSON number.

The report adds up the tax on invoices, one row per tax. Unlike the other reports it has **no paging**: taxes are few,
so one response holds every row, and the summary is exactly the sum of those rows. It has two tabs:

| Endpoint | Tab | Purpose |
|---|---|---|
| `GET /api/v1/reports/sales-tax-summary` | Sales Tax | Each tax with its taxable sales, rate, tax collected and surcharge, plus the totals |
| `GET /api/v1/reports/sales-tax-summary/rates` | Tax Rate | The list of tax rates |
| `GET /api/v1/reports/sales-tax-summary/filters` | | The bases, methods, sort keys and default sort (fixed; no database lookup) |

## `GET /api/v1/reports/sales-tax-summary`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes `limit`, `cursor` and `q`: the
report has no paging and no search.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive date range: the **payment date** on the `collected` basis and the **invoice date** on the `invoiced` one. If **both** are missing the range is the current calendar month (in the server's `APP_TIMEZONE`, UTC by default). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `basis` | `collected` \| `invoiced` | Single value; see [Bases](#bases) | `collected` |
| `method` | multi | `cash`, `check`, `card`, `other`: only the payments made this way count. **Only valid with `basis=collected`**; with `invoiced` it is a 422, because no payment is involved. | all |
| `sort` | key | `name`, `taxable_sales`, `tax_rate`, `tax_collected`, `surcharge_tax` | `taxable_sales` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: biggest first (`taxable_sales` descending). With `sort` but no `order`: ascending. Taxes that tie keep the order they were created in, in either direction. | see left |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?method=cash&method=check`, `?method=cash,check`. Values are trimmed and case-insensitive; blanks and duplicates
are ignored.

### Bases

| Basis | The tax follows | Counts |
|---|---|---|
| `collected` (cash based) | the money received | Every payment that is **paid** and not deleted, made for an invoice, dated in range. Each payment brings its share of each of the invoice's tax lines: the payment amount divided by the invoice total. |
| `invoiced` (accrual) | the invoices | The whole tax lines of the invoices that are sent or paid, not archived or deleted, dated in range. These are the same invoices as in [Total Sales](total-sales.md). |

On the `collected` basis each share (taxable sales, tax and surcharge) is rounded to the cent per payment and tax line,
and then added up per tax. Credit payments carry no tax, and an invoice with a zero total has nothing to share.
Pending, failed and refunded payments are not collected. Credits applied to an invoice are not counted yet.

Taxable sales, tax collected and surcharge are the amounts of the invoice's tax lines, so a payment that covers part of
an invoice carries that part of its tax.

### Response

```json
{
  "data": [
    {
      "id": 1,
      "name": "VAT",
      "taxable_sales": "553.22",
      "tax_rate": "10.00",
      "tax_collected": "55.33",
      "surcharge_tax": "2.92"
    },
    {
      "id": 4,
      "name": "VAT2",
      "taxable_sales": "0.00",
      "tax_rate": "15.00",
      "tax_collected": "0.00",
      "surcharge_tax": "0.00"
    }
  ],
  "summary": { "count": 2, "taxable_sales": "553.22", "tax_collected": "55.33", "surcharge_tax": "2.92" },
  "range": { "from": "2026-10-01", "to": "2026-10-31" },
  "basis": "collected"
}
```

- Every **active** tax has a row, with zeros when it saw no activity in the range. An **inactive** tax appears only
  when it did.
- `tax_rate` is the tax percentage with two decimals, a string such as `"10.00"`.
- `summary` is always present. It adds up the rows exactly, so the footer always equals the columns, and `count` is the
  number of rows. All rows come from one database statement, so they are one consistent snapshot.
- `range` is the date range actually applied, so a client can show the defaulted month; an open end is `null`.
  `basis` is the basis actually applied.
- `data` is always an array, never `null`.

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
GET /api/v1/reports/sales-tax-summary?basis=invoiced&method=card
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "01a166d8-78c8-730e-8798-1958f1895adc",
    "details": [
      { "field": "method", "message": "can only be used with basis collected" }
    ]
  }
}
```

Invalid values are reported together, one detail per field, for example `basis=cash` gives a detail on `basis` and
`sort=total` gives one on `sort`.

#### Strictness

A typo such as `?methods=cash` returns `422` with `{"field":"methods","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/sales-tax-summary/rates`

The Tax Rate tab: every tax rate, active or not, ordered by name. It takes no parameters; any query parameter is
rejected like on the list endpoint.

```json
{
  "data": [
    { "id": 3, "name": "d12", "tax_rate": "12.00", "active": true },
    { "id": 10, "name": "US", "tax_rate": "30.00", "active": false }
  ]
}
```

## `GET /api/v1/reports/sales-tax-summary/filters`

Everything needed to build the filter controls. The static lists mean the frontend never has to hard-code them.

```json
{
  "bases": ["collected", "invoiced"],
  "methods": ["cash", "check", "card", "other"],
  "sort_keys": ["name", "taxable_sales", "tax_rate", "tax_collected", "surcharge_tax"],
  "default_sort": { "key": "taxable_sales", "order": "desc" }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# This month's tax on the cash basis, biggest first
curl.exe "http://localhost:8080/api/v1/reports/sales-tax-summary"

# The same month by invoice date
curl.exe "http://localhost:8080/api/v1/reports/sales-tax-summary?basis=invoiced"

# Tax on card payments only in September
curl.exe "http://localhost:8080/api/v1/reports/sales-tax-summary?from=2026-09-01&to=2026-09-30&method=card"

# The tax rates, highest first
curl.exe "http://localhost:8080/api/v1/reports/sales-tax-summary?sort=tax_rate&order=desc"

# The Tax Rate tab
curl.exe "http://localhost:8080/api/v1/reports/sales-tax-summary/rates"
```
