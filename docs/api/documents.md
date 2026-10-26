# Documents report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Timestamps are UTC with six fractional
digits (`"2026-10-22T08:15:30.123456Z"`); dates are `YYYY-MM-DD`.

It works like the [Invoices](invoices.md) and [Estimates](estimates.md) reports; only the columns, the statuses,
the tabs and the filters differ.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/documents` | One page of the report, with filters, search, sorting and a count |
| `GET /api/v1/reports/documents/filters` | The choices behind the filter controls (including the document types), plus defaults and limits |

## `GET /api/v1/reports/documents`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)). That includes the parameters of the other
reports: `signature_status`, `service_id`, `tag_id` and `payment_term_id`.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the day the document was **created**, read in the server's `APP_TIMEZONE` (UTC by default); `from` and `to` are days in that zone, whether sent or defaulted. If **both** are missing the range is the current calendar month (in the server's `APP_TIMEZONE`, UTC by default). If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `lifecycle` | `active` \| `deleted` | The Active / Deleted tab. Documents cannot be archived, so there is no `archived` tab. | `active` |
| `status` | multi | `draft`, `sent`, `signed` | all |
| `document_type_id` | multi | Document type ids (from `/filters`). | all |
| `q` | text | Case-insensitive substring search over document number, account number and customer name. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `document_number`, `type`, `account_number`, `customer`, `created_by`, `created_at`, `status` | `created_at` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first (`created_at` descending). With `sort` but no `order`: ascending. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both; these are equivalent:
`?status=sent&status=signed`, `?status=sent,signed`, `?status=sent,signed&status=sent`. Values are trimmed and
case-insensitive; blanks and duplicates are ignored.

A document's status is one of `draft`, `sent` or `signed`. The invoice statuses (`paid`, `void`) and the estimate
statuses (`accepted`, `declined`) are rejected here.

### Response

```json
{
  "data": [
    {
      "id": 17,
      "document_number": "5982",
      "document_type": { "id": 1, "name": "1-Time Treatment (PestPros of MI)" },
      "customer": { "id": 2, "name": "Customer 1" },
      "account_number": "63733",
      "created_by": "Thọ Nguyễn Quang",
      "created_at": "2026-10-22T08:15:30.123456Z",
      "status": "signed"
    },
    {
      "id": 16,
      "document_number": "5981",
      "document_type": { "id": 2, "name": "1 Year SubTermite Agreement (LibertyPS)" },
      "customer": { "id": 1, "name": "#F Last Name" },
      "account_number": "63714",
      "created_by": "Thọ Nguyễn Quang",
      "created_at": "2026-10-21T12:30:00.000000Z",
      "status": "signed"
    }
  ],
  "page": { "limit": 2, "has_more": true, "next_cursor": "eyJrIjoiY3JlYXRlZF9hdCIsImQiOnRydWUsInYiOiIyMDI2LTEwLTIxVDEyOjMwOjAwLjAwMDAwMFoiLCJpZCI6MTZ9" },
  "summary": { "count": 18 },
  "range": { "from": "2026-10-01", "to": "2026-10-31" }
}
```

- `created_by` is `null` when the creator is unknown.
- `created_at` is always UTC with exactly six fractional digits, so every value has the same shape, whatever `APP_TIMEZONE` is. Only the `from`/`to` day boundaries follow `APP_TIMEZONE`.
- `summary` counts the **whole filtered set**, not just this page, and is only present on the **first page**
  (when no `cursor` is sent). It is `null` on later pages.
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
GET /api/v1/reports/documents?limit=0&lifecycle=archived&status=paid
X-Request-ID: demo-123

HTTP/1.1 422 Unprocessable Entity
```
```json
{
  "error": {
    "code": "invalid_input",
    "message": "request contains invalid values",
    "request_id": "demo-123",
    "details": [
      { "field": "lifecycle", "message": "must be one of: active, deleted" },
      { "field": "status", "message": "must be one of: draft, sent, signed" },
      { "field": "limit", "message": "must be a whole number between 1 and 200" }
    ]
  }
}
```

#### Strictness

A typo such as `?statuss=draft` returns `422` with `{"field":"statuss","message":"is not a supported parameter"}`
instead of silently returning unfiltered data. Unsupported parameters are reported on their own, before any
value is validated; fix them, and any remaining value errors are reported next.

## `GET /api/v1/reports/documents/filters`

Everything needed to build the filter controls. Use the returned ids for `document_type_id`; the static lists and
limits mean the frontend never has to hard-code them.

```json
{
  "document_types": [
    { "id": 1, "name": "1-Time Treatment (PestPros of MI)" },
    { "id": 2, "name": "1 Year SubTermite Agreement (LibertyPS)" },
    { "id": 3, "name": "1 Year Termite Warranty (Spidermen)" }
  ],
  "statuses": ["draft", "sent", "signed"],
  "lifecycles": ["active", "deleted"],
  "sort_keys": ["document_number", "type", "account_number", "customer", "created_by", "created_at", "status"],
  "default_sort": { "key": "created_at", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

## CORS

Browsers may call these endpoints from the origins listed in `CORS_ALLOWED_ORIGINS`. Only `GET` and
`OPTIONS` are allowed; `X-Request-ID` is exposed to scripts. No credentials are accepted.

## Examples

```powershell
# Sent or signed documents, highest number first
curl.exe "http://localhost:8080/api/v1/reports/documents?status=sent,signed&sort=document_number&order=desc"

# One document type (ids come from /filters), in the deleted tab
curl.exe "http://localhost:8080/api/v1/reports/documents?document_type_id=3&lifecycle=deleted"

# Search by customer, two per page
curl.exe "http://localhost:8080/api/v1/reports/documents?q=alaba&limit=2"
```
