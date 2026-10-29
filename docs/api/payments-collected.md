# Payments Collected report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`; money is a
decimal string with two places (`"148.56"`), never a JSON number.

It is the money-in view of the [Payments](payments.md) report: the same payments, narrowed to those actually
collected, with the service state, the staff member and the amount without tax.

**What counts as collected.** Payments with status **paid** that are not deleted. Pending, failed and refunded
payments and deleted ones are never listed, so there is no `status` or `lifecycle` parameter.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/payments-collected` | One page of the report, with filters, search, sorting, a count and totals |
| `GET /api/v1/reports/payments-collected/filters` | The payment-for choices, methods, staff, sort keys, default sort and limits |

## `GET /api/v1/reports/payments-collected`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes `status`, `lifecycle` and the
parameters of the other reports.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the **payment date**. If **both** are missing the range is the current calendar month (in the server's `APP_TIMEZONE`, UTC by default). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `payment_for` | `invoices` \| `credits` | Single value. `invoices` lists payments made for invoices; `credits` lists payments made for credits. | `invoices` |
| `method` | multi | `cash`, `check`, `card`, `other` | all |
| `customer_id` | multi | Customer ids. There is no list of customers in `/filters`; find ids with `q` first. | all |
| `staff_id` | multi | Staff ids (from `/filters`): the staff member who took the payment. Payments nobody took, such as online card payments, are only matched when no staff filter is sent. | all |
| `q` | text | Case-insensitive substring search over the invoice number, the account number and the customer name. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `payment_for`, `customer`, `method`, `service_state`, `staff`, `date`, `amount`, `without_tax` | `date` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first (`date` descending). With `sort` but no `order`: ascending. A missing service state or staff member sorts as an empty value: first when ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?method=cash&method=check`, `?method=cash,check`. Values are trimmed and case-insensitive; blanks and duplicates
are ignored.

`payment_for` sorts by the invoice number, and a credit sorts as the word `credit`, so in ascending order the
invoice payments come first and the credits last. `amount` and `without_tax` sort numerically; the other keys sort
as text.

### Response

```json
{
  "data": [
    {
      "id": 21,
      "payment_for": { "type": "invoice", "invoice_id": 40, "label": "85009" },
      "customer": { "id": 6, "name": "AAA1 ccc1" },
      "method": { "type": "cash", "card_brand": null, "card_last4": null },
      "service_state": "FL",
      "staff": { "id": 1, "name": "Thọ Nguyễn Quang" },
      "date": "2026-10-15",
      "amount": "148.56",
      "without_tax": "133.76"
    },
    {
      "id": 22,
      "payment_for": { "type": "invoice", "invoice_id": 41, "label": "85004" },
      "customer": { "id": 6, "name": "AAA1 ccc1" },
      "method": { "type": "card", "card_brand": "Credit", "card_last4": "3222" },
      "service_state": "FL",
      "staff": null,
      "date": "2026-10-15",
      "amount": "138.63",
      "without_tax": "108.63"
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoiZGF0ZSIsImQiOnRydWUsInYiOiIyMDI2LTEwLTE1IiwiaWQiOjIyfQ" },
  "summary": { "count": 27, "amount": "1896.70", "without_tax": "1695.22" },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

- `payment_for` is `{"type": "invoice", "invoice_id": 40, "label": "<invoice number>"}` or, for a credit,
  `{"type": "credit", "invoice_id": null, "label": "Credit"}`.
- `method` is how the payment was made; the card fields are `null` unless it was a card. The check number is not
  stored, so the screen's "Check #" has nothing to show.
- `service_state` is the state of the service location of the invoice's job. It is `null` when the invoice has no
  job, the location has no state, or the payment is for a credit.
- `staff` is the staff member who took the payment, or `null` when nobody did (online card payments, credits).
- `without_tax` is the part of the payment that is not tax: the amount times the invoice's total without tax, divided
  by the invoice total, rounded to the cent per row. A payment for a credit, or for an invoice with a zero total or
  no tax lines, has `without_tax` equal to `amount`. Surcharges are not tax and stay in both figures.
- `summary` totals the **whole filtered set**, not just this page, and is only present on the **first page** (when no
  `cursor` is sent); it is `null` on later pages. Its `without_tax` adds up the rounded row values, so the footer
  always equals the column. The rows and the totals are read from one database snapshot, so the totals always
  describe exactly the set the rows were taken from, even while payments are being added.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`.

An empty result keeps the same shape:

```json
{
  "data": [],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "summary": { "count": 0, "amount": "0.00", "without_tax": "0.00" },
  "range": { "from": "2025-01-01", "to": "2025-01-31" }
}
```

### Paging

1. Request the first page (no `cursor`). Keep `summary` from it.
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it stays fast on deep pages and never skips or repeats a row, even when rows are added
between requests. Ties are broken by the payment id, so payments on the same date keep a stable order. There is no
"jump to page N".

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
GET /api/v1/reports/payments-collected?payment_for=all
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "01a166d8-78c8-730e-8798-1958f1895adc",
    "details": [
      { "field": "payment_for", "message": "must be one of: invoices, credits" }
    ]
  }
}
```

Invalid values are reported together, one detail per field, for example `staff_id=abc` gives a detail on
`staff_id` and `limit=0` gives one on `limit`.

#### Strictness

A typo such as `?methods=cash` returns `422` with `{"field":"methods","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/payments-collected/filters`

Everything needed to build the filter controls. The staff list is read from the database; use the returned ids for
`staff_id`. The static lists and limits mean the frontend never has to hard-code them. The customer filter has no list
here; use `q` to find customers.

```json
{
  "payment_for": ["invoices", "credits"],
  "methods": ["cash", "check", "card", "other"],
  "staff": [
    { "id": 2, "name": "thon nguyen" },
    { "id": 1, "name": "Thọ Nguyễn Quang" }
  ],
  "sort_keys": ["payment_for", "customer", "method", "service_state", "staff", "date", "amount", "without_tax"],
  "default_sort": { "key": "date", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# This month's invoice payments, newest first
curl.exe "http://localhost:8080/api/v1/reports/payments-collected"

# Card payments only
curl.exe "http://localhost:8080/api/v1/reports/payments-collected?method=card"

# What one staff member collected in September (ids come from /filters)
curl.exe "http://localhost:8080/api/v1/reports/payments-collected?staff_id=1&from=2026-09-01&to=2026-09-30"

# The credit payments, biggest first
curl.exe "http://localhost:8080/api/v1/reports/payments-collected?payment_for=credits&sort=amount&order=desc"
```
