# Device Tracking report API

Read-only. Both endpoints are `GET` and live under `/api/v1/reports`. Every response (errors included) is
JSON and carries `Cache-Control: no-store` and an `X-Request-ID` header. Timestamps are UTC with six fractional
digits (`"2026-11-03T03:45:00.000000Z"`); dates are `YYYY-MM-DD`.

One row is one device checked during one job visit: a bait box, a trap or a laptop checked on a job. The devices stay the
same, installed at a customer's service location, and every visit adds its own checks. It works like
[Material Use](material-use.md) (the date range is the job's appointment, a `client_id` filter), and like
[Online Bookings](online-bookings.md) for the job's customer, service and address.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/reports/device-tracking` | One page of the report, with filters, search and sorting |
| `GET /api/v1/reports/device-tracking/filters` | The choices behind the filter controls (clients), plus sort keys and limits |

## What is listed

- A **row** is a check on a job that has an appointment and is not deleted. A deleted job's checks are never listed, and a
  job that was never scheduled is not listed (the Date column needs an appointment).
- A cancelled job's checks are listed.
- The **date** is the job's appointment, `jobs.scheduled_at`, read in the server's `APP_TIMEZONE` for the `from`/`to` days.
- The **address** is the job's own service location, not the device's.

## `GET /api/v1/reports/device-tracking`

### Query parameters

Anything not listed here is rejected (see [Errors](#errors)). That includes `name_format` and `source_id`, which this
screen does not have.

| Parameter | Type | Meaning | Default |
|---|---|---|---|
| `from`, `to` | `YYYY-MM-DD` | Inclusive range on the day of the job's appointment, in the server's `APP_TIMEZONE`. If **both** are missing the range is the current calendar month. If only one is sent, the other end is open. `to` must not be before `from`. | current month |
| `client_id` | multi | Customer ids (`clients` from `/filters`), the screen's Clients filter. | all |
| `q` | text | Case-insensitive substring search over the customer name, service type, the address lines, city, state and zip, the device name, barcode, area and type, the status, activity and notes, and the assignee's name. `%`, `_` and `\` are matched literally. At most 100 characters. | none |
| `sort` | key | `customer`, `service`, `date`, `address_1`, `address_2`, `city`, `state`, `zip`, `device_name`, `barcode`, `coordinate`, `area`, `device_type`, `check_in`, `status`, `activity`, `check_out`, `notes`, `assigned_to`: every column of the screen. | `date` |
| `order` | `asc` \| `desc` | Sort direction. With neither `sort` nor `order`: latest appointment first. With `sort` but no `order`: ascending. Ties break by check id in the same direction. | see left |
| `limit` | integer 1 to 200 | Page size. Out of range is an error, not clamped. | 50 |
| `cursor` | opaque | `page.next_cursor` from the previous response. Must be used with the same `sort` and `order`. | first page |

**Multi-value parameters** accept repeated keys, comma-separated lists, or both: `?client_id=1&client_id=2` and
`?client_id=1,2` are the same. Blanks and duplicates are ignored; at most 100 ids.

**Sorting.** Text columns sort case-insensitively. `coordinate` sorts by latitude, then longitude, and a device without a
coordinate is first when ascending. `check_in` and `check_out` sort by the instant (to the millisecond), and a device that was
not checked is first when ascending. `assigned_to` sorts by the staff member's name, and a check with none is first.
`address_1` sorts by address line 1.

### Response

```json
{
  "data": [
    {
      "id": 17,
      "customer": { "id": 6, "name": "AAA1 ccc1" },
      "service_type": { "id": 3, "name": "Every 21 Days" },
      "date": "2026-11-03T03:45:00.000000Z",
      "address": { "line1": "Terminal 4 Arrivals", "line2": "", "city": "Queens", "state": "NY", "zip": "11430", "county": "" },
      "device": {
        "id": 9,
        "name": "QA device 32d92e23",
        "barcode": "BC-2001",
        "coordinate": { "latitude": 37.421998, "longitude": -122.084 },
        "area": "QA area bf9a99b2",
        "device_type": "Bait Box"
      },
      "check_in": "2026-11-03T03:50:00.000000Z",
      "status": "Good",
      "activity": "Inspected",
      "check_out": "2026-11-03T04:05:00.000000Z",
      "notes": "Bait taken, refilled",
      "assigned_to": { "id": 1, "name": "Thọ Nguyễn Quang" }
    },
    {
      "id": 16,
      "customer": { "id": 11, "name": "Alaba David" },
      "service_type": { "id": 1, "name": "Blank Service" },
      "date": "2026-11-04T02:30:00.000000Z",
      "address": { "line1": "Española Way", "line2": "", "city": "Miami Beach", "state": "FL", "zip": "33139", "county": "" },
      "device": { "id": 8, "name": "", "barcode": "", "coordinate": null, "area": "Area 1", "device_type": "Laptop" },
      "check_in": null,
      "status": "40",
      "activity": "",
      "check_out": null,
      "notes": "",
      "assigned_to": null
    }
  ],
  "page": { "limit": 50, "has_more": false, "next_cursor": null },
  "range": { "from": "2026-11-01", "to": "2026-11-30" }
}
```

- `id` is the check's id; `customer` is the job's customer, shown by display name.
- `date` is the job's appointment; the screen's weekday and 12-hour time are display only. It is always UTC with exactly six
  fractional digits; only the `from`/`to` day boundaries follow `APP_TIMEZONE`.
- `address` is the job's own service location, the same object as in [Service Lookup](service-lookup.md).
- `device.name`, `barcode` and `area` are empty strings when not recorded. `device.coordinate` is `null` when the device has none.
- `check_in` and `check_out` are `null` until the device is checked in or out.
- `status`, `activity` and `notes` are free text (the screen shows values such as Good, 40 and 10), empty strings when nothing
  was written. `notes` is the screen's Finding/Notes.
- `assigned_to` is the staff member who made the check, `null` when none is recorded.
- `range` is the date range actually applied, so a client can show the defaulted month. An open end is `null`.
- `page.next_cursor` is `null` and `has_more` is `false` on the last page. `data` is always an array, never `null`:
  an empty range answers with `"data": []` (the screen's "There is no data to display").
- There is no summary row: the screen's "Export 21 records" label is a count a client can take from the rows it fetched.

### Paging

1. Request the first page (no `cursor`).
2. While `page.next_cursor` is not `null`, request the same URL with `cursor=<next_cursor>`.
3. Changing any filter, `sort` or `order` starts a new first page. A cursor made for another `sort`/`order` is
   rejected with a 422 on the `cursor` field.

Paging is keyset-based: it never skips or repeats a row, even when checks are added between requests. The `date` cursor keeps
its microseconds, so checks on jobs booked for the same instant are never lost at a page boundary.

### Errors

All errors use the same envelope as the other reports.

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | The query string itself is malformed (bad percent-encoding, `;` separators). |
| 422 | `invalid_input` | A value is invalid, a parameter is unknown, or a single-value parameter is repeated. `details` names each field, all together. |
| 500 | `internal_error` | Unexpected failure; the message is generic and the cause is only in the server log. |
| 504 | `timeout` | The request or its database query timed out. |

## `GET /api/v1/reports/device-tracking/filters`

```json
{
  "clients": [{ "id": 7, "name": "William Saliba" }],
  "sort_keys": ["customer", "service", "date", "address_1", "address_2", "city", "state", "zip", "device_name", "barcode",
                "coordinate", "area", "device_type", "check_in", "status", "activity", "check_out", "notes", "assigned_to"],
  "default_sort": { "key": "date", "order": "desc" },
  "limits": { "default": 50, "max": 200 }
}
```

`clients` is every customer, ordered by name.
