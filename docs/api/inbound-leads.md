# Inbound Leads report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Timestamps are UTC with six fractional
digits (`"2026-11-03T09:15:30.123456Z"`); dates are `YYYY-MM-DD`.

An inbound lead is a potential customer: a customer whose status is `lead`. There is no separate leads table, and no
`status` parameter, because the report only ever lists leads. It works like the [New Customers](new-customers.md) report
(the date is when the customer was added, filtered by day in `APP_TIMEZONE`) and like
[System Email History](system-email-history.md) for `name_format`. The screen's year chart and source pie are not part
of this API yet.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/inbound-leads` | One page of the report, with filters, search and sorting |
| `GET /api/v1/reports/inbound-leads/filters` | The choices behind the filter controls (sources), plus name formats, sort keys and limits |

## `GET /api/v1/reports/inbound-leads`

### Query parameters

Anything not listed here is rejected (see [Errors](#errors)). That includes `status`, `year` and `lead_source_id`.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the day the lead was **added**, read in the server's `APP_TIMEZONE`. If **both** are missing the range is the current calendar month. If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `source_id` | multi | Lead source ids (`sources` from `/filters`), the screen's All Sources. The word `none` (any case) also keeps the leads without a source, the screen's "NA": `?source_id=none`, `?source_id=2,none`. | all |
| `name_format` | `first_name` \| `last_name` \| `full_name` | How `customer.name` is shown (the screen's "First name" dropdown). It changes the display only. A lead with no first (or last) name shows the full display name instead. | `first_name` |
| `q` | text | Case-insensitive substring search over the customer's display name, email, phone and notes. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `customer`, `source`, `email`, `phone`, `notes`, `date_added`. `customer` sorts by the full display name, whatever `name_format` is. The active job is not sortable. | `date_added` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: newest first. With `sort` but no `order`: ascending. A lead without a source, email or phone sorts as an empty value: first when ascending. Ties break by customer id in the same direction. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both: `?source_id=1&source_id=2` and
`?source_id=1,2` are the same. Blanks and duplicates are ignored; at most 100 ids.

### Response

```json
{
  "data": [
    {
      "id": 23,
      "customer": { "id": 23, "name": "Noah" },
      "source": { "id": 1, "name": "Facebook" },
      "email": "noah.berg@example.com",
      "phone": "(555) 010-0003",
      "notes": "Wants a Bi-Monthly Service",
      "date_added": "2026-11-03T09:15:30.123456Z",
      "active_job": {
        "service_type": { "id": 2, "name": "Bi-Monthly Service" },
        "scheduled_at": "2026-11-12T09:00:00.000000Z"
      }
    },
    {
      "id": 22,
      "customer": { "id": 22, "name": "Cold Call Co" },
      "source": null,
      "email": null,
      "phone": null,
      "notes": "",
      "date_added": "2026-11-03T09:15:30.123456Z",
      "active_job": null
    }
  ],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "range": { "from": "2026-11-01", "to": "2026-11-30" }
}
```

- `customer.name` follows `name_format`; `customer.id` is the lead's customer id (the same as `id`).
- `source` is `null` when the lead has none (the screen's "NA").
- `email` and `phone` are `null` when none is recorded. `phone` is the customer's first number (the one added first).
- `notes` is the customer's top note, an empty string when there is none.
- `date_added` is the day and time the lead was added, one value: the screen's Date Added and Time Added columns are one
  column here. It is always UTC with exactly six fractional digits; only the `from`/`to` day boundaries follow `APP_TIMEZONE`.
- `active_job` is the lead's earliest scheduled job that is not deleted and still open (`unconfirmed` or `confirmed`),
  or `null`. Cancelled, completed and never-scheduled jobs do not count. A lead stays one row however many jobs it has.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`:
  an empty range answers with `"data": []` (the screen's "There is no data to display").
- There is no summary row.

### Paging

1. Request the first page (no `cursor`).
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it never skips or repeats a row, even when leads are added between requests. The `date_added`
cursor keeps its microseconds, so leads added at the same instant are never lost at a page boundary.

### Errors

All errors use the same envelope as the other reports.

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid, a parameter is unknown, or a single-value parameter is repeated. `details` names each field, all together. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

## `GET /api/v1/reports/inbound-leads/filters`

```json
{
  "sources": [{ "id": 1, "name": "Facebook" }, { "id": 2, "name": "Google" }],
  "name_formats": ["first_name", "last_name", "full_name"],
  "sort_keys": ["customer", "source", "email", "phone", "notes", "date_added"],
  "default_sort": { "key": "date_added", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

`sources` is every lead source, ordered by name. A source filter also accepts `none` for the leads without one, which is
not listed here because it has no id.
