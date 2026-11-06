# Online Bookings report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Timestamps are UTC with six fractional
digits (`"2026-11-12T09:00:00.123456Z"`); dates are `YYYY-MM-DD`.

An online booking is a job of a customer who came from an online booking: a customer whose `booked_online` flag is set.
The flag lives on the customer and is not derived from the lead source. It works like [New Jobs](new-jobs.md) (one row per
job, the job's own service location), [Inbound Leads](inbound-leads.md) (`source_id`, including `none`) and
[System Email History](system-email-history.md) (`name_format`). The screen's bookings-per-month chart is not part of this
API yet.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/online-bookings` | One page of the report, with filters, search and sorting |
| `GET /api/v1/reports/online-bookings/filters` | The choices behind the filter controls (sources), plus name formats, sort keys and limits |

## What is listed

- A **row** is a job of an online customer that has an appointment and is not deleted. A deleted job is never listed, and a
  job that was never scheduled is not a booking row (it has no Date and Time), even for an online customer.
- A cancelled job is listed, with its `status`.
- The **date** is the appointment, `jobs.scheduled_at`, read in the server's `APP_TIMEZONE` for the `from`/`to` days.
- The **source** is the customer's own lead source, not the job's.

## `GET /api/v1/reports/online-bookings`

### Query parameters

Anything not listed here is rejected (see [Errors](#errors)). That includes `status`, `year` and `lead_source_id`.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the day of the appointment, in the server's `APP_TIMEZONE`. If **both** are missing the range is the current calendar month. If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `source_id` | multi | The customer's lead source ids (`sources` from `/filters`), the screen's All Sources. The word `none` (any case) also keeps the customers without a source: `?source_id=none`, `?source_id=2,none`. | all |
| `name_format` | `first_name` \| `last_name` \| `full_name` | How `customer.name` is shown (the screen's "First name" dropdown). It changes the display only. A customer with no first (or last) name shows the full display name instead. | `first_name` |
| `q` | text | Case-insensitive substring search over the customer's display name, email, phone, the job's address line 1 and city, the service type and the notes. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `customer`, `scheduled_at`, `service`, `email`, `phone`, `address`, `notes`, `status`. `customer` sorts by the full display name, whatever `name_format` is. `address` sorts by address line 1. | `scheduled_at` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: latest appointment first. With `sort` but no `order`: ascending. A customer without an email or phone sorts as an empty value: first when ascending. Ties break by job id in the same direction. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both: `?source_id=1&source_id=2` and
`?source_id=1,2` are the same. Blanks and duplicates are ignored; at most 100 ids.

### Response

```json
{
  "data": [
    {
      "id": 31,
      "customer": { "id": 5, "name": "Mia" },
      "scheduled_at": "2026-11-12T09:00:00.123456Z",
      "service_type": { "id": 2, "name": "Bi-Monthly Service" },
      "email": "mia@example.com",
      "phone": "(555) 010-0001",
      "address": { "line1": "1 Main St", "line2": "", "city": "Austin", "state": "TX", "zip": "73301", "county": "Travis" },
      "notes": "Gate code 1234",
      "status": "confirmed",
      "source": { "id": 1, "name": "Facebook" }
    },
    {
      "id": 30,
      "customer": { "id": 6, "name": "# D" },
      "scheduled_at": "2026-11-02T09:00:00.000000Z",
      "service_type": { "id": 1, "name": "Blank Service 123" },
      "email": null,
      "phone": null,
      "address": { "line1": "2 Side St", "line2": "", "city": "Dallas", "state": "TX", "zip": "75001", "county": "" },
      "notes": "",
      "status": "cancelled",
      "source": null
    }
  ],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "range": { "from": "2026-11-01", "to": "2026-11-30" }
}
```

- `id` is the job's id; `customer.name` follows `name_format`.
- `scheduled_at` is the appointment: the screen's Date and Time columns are one column here. It is always UTC with exactly
  six fractional digits; only the `from`/`to` day boundaries follow `APP_TIMEZONE`.
- `email` and `phone` are `null` when none is recorded. `phone` is the customer's first number (the one added first).
- `address` is the job's own service location, the same object as in [Service Lookup](service-lookup.md).
- `notes` is the customer's top note, an empty string when there is none.
- `status` is the job's status: `unconfirmed`, `confirmed`, `completed` or `cancelled`.
- `source` is the customer's lead source, `null` when the customer has none.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`:
  an empty range answers with `"data": []` (the screen's "There is no data to display").
- There is no summary row.

### Paging

1. Request the first page (no `cursor`).
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it never skips or repeats a row, even when bookings are added between requests. The
`scheduled_at` cursor keeps its microseconds, so jobs booked for the same instant are never lost at a page boundary.

### Errors

All errors use the same envelope as the other reports.

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid, a parameter is unknown, or a single-value parameter is repeated. `details` names each field, all together. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

## `GET /api/v1/reports/online-bookings/filters`

```json
{
  "sources": [{ "id": 1, "name": "Facebook" }, { "id": 2, "name": "Google" }],
  "name_formats": ["first_name", "last_name", "full_name"],
  "sort_keys": ["customer", "scheduled_at", "service", "email", "phone", "address", "notes", "status"],
  "default_sort": { "key": "scheduled_at", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

`sources` is every lead source, ordered by name. A source filter also accepts `none` for the customers without one, which
is not listed here because it has no id.
