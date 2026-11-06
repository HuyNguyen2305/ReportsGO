# Material Use report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Dates are `YYYY-MM-DD`.

Every job adds its own materials, independently of other jobs. The report adds them up: **one row per material**, with
the total quantity and the number of times it was added (its applications) over the jobs in scope.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/material-use` | One page of the report, with filters, search and sorting |
| `GET /api/v1/reports/material-use/filters` | The choices behind the filter controls (materials and clients), plus tabs, sort keys and limits |

## What is listed

- A **row** is a material that was added to at least one job in scope. A material nobody used in the range is not listed.
- **Qty** is the sum of the quantities added, **# of applications** (`applications`) the number of additions.
  The same material added twice to one job counts as two applications.
- **The date** is the day the job occurs, `jobs.scheduled_at`, read in the server's `APP_TIMEZONE`. A job that was never
  scheduled is left out as soon as `from` or `to` is sent. With neither, the range is the current month and so also
  leaves it out.
- A **deleted job** adds nothing. A cancelled job that is not deleted still counts.
- **Tabs.** A material can be added with a *device*, the equipment the company lent to the customer on request. `jobs`
  keeps the additions without a device, `devices` the additions with one, `all` both.

## `GET /api/v1/reports/material-use`

### Query parameters

Anything not listed here is rejected (see [Strictness](#strictness)).

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the day the job occurs (scheduled), in the server's `APP_TIMEZONE`. If **both** are missing the range is the current calendar month. If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `tab` | `all` \| `jobs` \| `devices` | See [What is listed](#what-is-listed). Case-insensitive. | `all` |
| `client_id` | multi | Customer ids (`clients` from `/filters`): only the jobs of those customers. | all |
| `material_id` | multi | Material ids (`materials` from `/filters`). | all |
| `q` | text | Case-insensitive substring search over the material name and its EPA number. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `material`, `epa`, `qty`, `applications` | `material` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: material name A to Z. With `sort` but no `order`: ascending. A material without an EPA number sorts as an empty one: first when ascending. Ties break by material id in the same direction. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both: `?client_id=1&client_id=2` and
`?client_id=1,2` are the same. Blanks and duplicates are ignored; at most 100 ids.

### Response

```json
{
  "data": [
    { "material_id": 1, "material": "Demand CS", "epa": "100-1066", "qty": "4.250", "applications": 3 },
    { "material_id": 4, "material": "Glue Boards", "epa": null, "qty": "4.000", "applications": 1 }
  ],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "range": { "from": "2026-11-01", "to": "2026-11-30" }
}
```

- `epa` is the material's EPA registration number, `null` when it has none.
- `qty` is a decimal string with three places.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`:
  an empty range answers with `"data": []` (the screen's "There is no data to display").
- There is no summary row: the screen has no totals line.

### Paging

1. Request the first page (no `cursor`).
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based over the per-material totals, so it never skips or repeats a material, even when two materials
have the same quantity. There is no "jump to page N".

### Errors

All errors use the same envelope as the other reports.

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid, a parameter is unknown, or a single-value parameter is repeated. `details` names each field. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

### Strictness

Unknown parameters, a repeated single-value parameter (`tab`, `from`, `limit`, ...) and invalid values are rejected
with a 422 that lists every problem together, instead of being ignored.

## `GET /api/v1/reports/material-use/filters`

```json
{
  "materials": [{ "id": 1, "name": "Demand CS" }],
  "clients": [{ "id": 7, "name": "William Saliba" }],
  "tabs": ["all", "jobs", "devices"],
  "sort_keys": ["material", "epa", "qty", "applications"],
  "default_sort": { "key": "material", "order": "asc" },
  "limits": { "default": 50, "max": 200 }
}
```

`materials` and `clients` are every material and every customer, ordered by name.
