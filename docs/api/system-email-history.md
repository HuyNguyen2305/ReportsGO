# System Email History report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Timestamps are UTC with six fractional
digits (`"2026-11-02T08:15:30.123456Z"`); dates are `YYYY-MM-DD`.

One row is one email the system sent to a customer, so every row belongs to a customer and the customer filter works on
that link. It works like the [New Jobs](new-jobs.md) report: the date is a timestamp (shown in UTC, filtered by day in
`APP_TIMEZONE`), there is no Active / Deleted tab, and the filter choices come from the database.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/system-email-history` | One page of the report, with filters, search and sorting |
| `GET /api/v1/reports/system-email-history/filters` | The choices behind the filter controls, plus name formats, sort keys and limits |

## `GET /api/v1/reports/system-email-history`

### Query parameters

Anything not listed here is rejected (see [Errors](#errors)).

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the day the email was **sent**, read in the server's `APP_TIMEZONE`. If **both** are missing the range is the current calendar month. If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `status` | multi | `unconfirmed`, `confirmed`, `cancelled` (the screen's All Statuses). | all |
| `client_id` | multi | Customer ids (`clients` from `/filters`), the screen's All customers. | all |
| `name_format` | `first_name` \| `last_name` \| `full_name` | How `customer.name` is shown (the screen's "First name" dropdown). It changes the display only. A customer with no first (or last) name shows the full display name instead. | `first_name` |
| `q` | text | Case-insensitive substring search over the customer's display name, account number and the email address. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `time_stamp`, `customer`, `email`, `email_type`, `status`. `customer` sorts by the full display name, whatever `name_format` is. | `time_stamp` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first. With `sort` but no `order`: ascending. Ties break by email id in the same direction. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both: `?status=confirmed&status=cancelled`
and `?status=confirmed,cancelled` are the same. Values are trimmed and case-insensitive for `status`; blanks and
duplicates are ignored.

### Response

```json
{
  "data": [
    {
      "id": 9,
      "customer": { "id": 2, "name": "Customer" },
      "email": "billing.alt@example.com",
      "email_type": "payment_receipt",
      "status": "confirmed",
      "time_stamp": "2026-11-03T12:00:00.000000Z"
    }
  ],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "range": { "from": "2026-11-01", "to": "2026-11-30" }
}
```

- `customer.name` follows `name_format`.
- `email` is the address the email was sent to. It can differ from the customer's own address.
- `email_type` is one of `welcome`, `invoice`, `estimate`, `appointment_confirmation`, `appointment_reminder`,
  `payment_receipt`, `review_request`.
- `time_stamp` is always UTC with exactly six fractional digits, whatever `APP_TIMEZONE` is. Only the `from`/`to` day
  boundaries follow `APP_TIMEZONE`.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`:
  an empty range answers with `"data": []` (the screen's "There is no data to display").
- There is no summary row: the screen has no totals line.

### Paging

1. Request the first page (no `cursor`).
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it never skips or repeats a row, even when rows are added between requests. Timestamps in
cursors keep their microseconds, so emails sent at the same instant are never lost at a page boundary.

### Errors

All errors use the same envelope as the other reports.

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid, a parameter is unknown, or a single-value parameter is repeated. `details` names each field, all together. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

## `GET /api/v1/reports/system-email-history/filters`

```json
{
  "statuses": ["unconfirmed", "confirmed", "cancelled"],
  "email_types": ["welcome", "invoice", "estimate", "appointment_confirmation", "appointment_reminder", "payment_receipt", "review_request"],
  "clients": [{ "id": 7, "name": "William Saliba" }],
  "name_formats": ["first_name", "last_name", "full_name"],
  "sort_keys": ["time_stamp", "customer", "email", "email_type", "status"],
  "default_sort": { "key": "time_stamp", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

`clients` is every customer, ordered by name. The statuses, email types and name formats are fixed sets.
`email_types` is listed for reference: the report has no email-type filter, as the screen has none.
