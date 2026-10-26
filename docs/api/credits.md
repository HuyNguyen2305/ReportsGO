# Credits report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`; money is a
decimal string with two places (`"200.00"`), never a JSON number.

It works like the [Invoices](invoices.md), [Estimates](estimates.md), [Documents](documents.md) and
[Payments](payments.md) reports, with one difference: a credit has **no status** and cannot be deleted or
archived, so this report has **no `status` filter and no `lifecycle` (Active / Deleted) tab**.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/credits` | One page of the report, with filters, search, sorting, a count and a total |
| `GET /api/v1/reports/credits/filters` | The choices behind the filter controls, plus defaults and limits (fixed; no database lookup) |

## `GET /api/v1/reports/credits`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes `lifecycle` and `status`,
and the parameters of the other reports: `signature_status`, `service_id`, `tag_id`, `payment_term_id` and
`document_type_id`.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the **credit date**. If **both** are missing the range is the current calendar month (in the server's `APP_TIMEZONE`, UTC by default). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `method` | multi | `cash`, `check`, `card`, `other` | all |
| `q` | text | Case-insensitive substring search over the customer name, the account number and the method detail text (for example `stripe` or `3222`). `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `customer`, `account_number`, `method`, `amount`, `credit_date`, `created_by` | `credit_date` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first (`credit_date` descending). With `sort` but no `order`: ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?method=check&method=other`, `?method=check,other`, `?method=check,other&method=check`. Values are trimmed and
case-insensitive; blanks and duplicates are ignored.

`method` sorts by the method type (`card`, `cash`, `check`, `other`, in database order); the detail text is not a
sort key. Every sort is stable: ties are broken by credit id in the same direction. A credit without a known
creator sorts as an empty name under `created_by`.

Number columns are identifiers, not quantities: `account_number` sort as text, character by character, not numerically, so `100` comes before `9`.

### Response

```json
{
  "data": [
    {
      "id": 1,
      "customer": { "id": 6, "name": "AAA1 ccc1" },
      "account_number": "63706",
      "method": { "type": "card", "detail": "Stripe **** 3222 Credit" },
      "amount": "200.00",
      "credit_date": "2026-10-15",
      "created_by": "Thọ Nguyễn Quang"
    },
    {
      "id": 8,
      "customer": { "id": 2, "name": "Customer 1" },
      "account_number": "63733",
      "method": { "type": "other", "detail": "Wire transfer" },
      "amount": "120.00",
      "credit_date": "2026-10-21",
      "created_by": null
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoiYW1vdW50IiwiZCI6dHJ1ZSwidiI6IjEyMC4wMCIsImlkIjo4fQ" },
  "summary": { "count": 7, "total": "575.25" },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

(from `?limit=2&sort=amount&order=desc`)

- `method.type` is `cash`, `check`, `card` or `other`. `method.detail` is the stored description of the method,
  such as `Stripe **** 3222 Credit`; it is an empty string `""` when none was stored.
- `created_by` is `null` when the creator is unknown.
- `summary` counts and totals the **whole filtered set**, not just this page, and is only present on the
  **first page** (when no `cursor` is sent). It is `null` on later pages. The rows and the summary are read from
  one database snapshot, so the count and total always describe exactly the set the rows were taken from, even
  while credits are being added.
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

All errors use the same envelope as the other reports. `request_id` equals the `X-Request-ID` response header (a
well-formed `X-Request-ID` sent by the client is reused).

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid, a parameter is unknown, or a single-value parameter is repeated. `details` names each field. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

```http
GET /api/v1/reports/credits?method=wire&sort=status&limit=0
X-Request-ID: demo-124

HTTP/1.1 422 Unprocessable Entity
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "demo-124",
    "details": [
      { "field": "method", "message": "must be one of: cash, check, card, other" },
      { "field": "sort", "message": "must be one of: customer, account_number, method, amount, credit_date, created_by" },
      { "field": "limit", "message": "must be a whole number between 1 and 200" }
    ]
  }
}
```

#### Strictness

A typo such as `?methodd=cash`, or a parameter this report does not have, returns `422` with
`{"field":"methodd","message":"is not a supported parameter"}` instead of silently returning unfiltered data.
This applies to `lifecycle` and `status` too:

```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "demo-123",
    "details": [ { "field": "lifecycle", "message": "is not a supported parameter" } ]
  }
}
```

Unsupported parameters are reported on their own, before any value is validated; fix them, and any remaining
value errors are reported next.

## `GET /api/v1/reports/credits/filters`

Everything needed to build the filter controls. The lists and limits are fixed, so the frontend never has to
hard-code them. There are no statuses or tabs to list.

```json
{
  "methods": ["cash", "check", "card", "other"],
  "sort_keys": ["customer", "account_number", "method", "amount", "credit_date", "created_by"],
  "default_sort": { "key": "credit_date", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Biggest credits of the month first
curl.exe "http://localhost:8080/api/v1/reports/credits?sort=amount&order=desc"

# Card and cash credits, two per page
curl.exe "http://localhost:8080/api/v1/reports/credits?method=card,cash&limit=2"

# Credits whose method text mentions Stripe, in any month
curl.exe "http://localhost:8080/api/v1/reports/credits?q=stripe&from=2000-01-01&to=2099-12-31"
```
