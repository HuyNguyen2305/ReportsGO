package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// deviceTrackingAlias is the job_device_checks alias in the Device Tracking report's query.
const deviceTrackingAlias = "chk"

// deviceTrackingDateText renders the job's appointment as fixed-format UTC text, whatever the session time zone is, so a
// cursor built from it always means the same instant. It casts back with ::timestamptz.
const deviceTrackingDateText = `to_char(j.scheduled_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`

// deviceTrackingCoordinate orders a device by latitude, then longitude, as fixed-width text of micro-degrees (shifted so
// they are never negative), so it compares correctly as text. A device without a coordinate is the empty text, first
// when ascending.
const deviceTrackingCoordinate = `coalesce(lpad(round((d.latitude + 90) * 1000000)::bigint::text, 12, '0'), '') || ',' || ` +
	`coalesce(lpad(round((d.longitude + 180) * 1000000)::bigint::text, 12, '0'), '')`

// epochMillis is the instant as whole milliseconds since 1970, so it can be compared and kept in a cursor as a number;
// never-checked is 0, first when ascending. Ties inside a millisecond fall to the check id.
func epochMillis(column string) string {
	return `coalesce((extract(epoch from ` + column + `) * 1000)::bigint, 0)`
}

// deviceTrackingSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller. Text columns sort lower-cased, and an assignee that is not
// recorded sorts as an empty name.
var deviceTrackingSorts = map[model.DeviceTrackingSortKey]sortSpec{
	model.DeviceTrackingSortCustomer:   {expr: `lower(c.display_name)`, cast: "text"},
	model.DeviceTrackingSortService:    {expr: `lower(st.name)`, cast: "text"},
	model.DeviceTrackingSortDate:       {expr: `j.scheduled_at`, text: deviceTrackingDateText, cast: "timestamptz"},
	model.DeviceTrackingSortAddress1:   {expr: `lower(sl.address_line1)`, cast: "text"},
	model.DeviceTrackingSortAddress2:   {expr: `lower(sl.address_line2)`, cast: "text"},
	model.DeviceTrackingSortCity:       {expr: `lower(sl.city)`, cast: "text"},
	model.DeviceTrackingSortState:      {expr: `lower(sl.state)`, cast: "text"},
	model.DeviceTrackingSortZip:        {expr: `lower(sl.postal_code)`, cast: "text"},
	model.DeviceTrackingSortDeviceName: {expr: `lower(d.name)`, cast: "text"},
	model.DeviceTrackingSortBarcode:    {expr: `lower(d.barcode)`, cast: "text"},
	model.DeviceTrackingSortCoordinate: {expr: deviceTrackingCoordinate, cast: "text"},
	model.DeviceTrackingSortArea:       {expr: `lower(d.area)`, cast: "text"},
	model.DeviceTrackingSortDeviceType: {expr: `lower(d.device_type)`, cast: "text"},
	model.DeviceTrackingSortCheckIn:    {expr: epochMillis("chk.checked_in_at"), cast: "bigint"},
	model.DeviceTrackingSortStatus:     {expr: `lower(chk.status)`, cast: "text"},
	model.DeviceTrackingSortActivity:   {expr: `lower(chk.activity)`, cast: "text"},
	model.DeviceTrackingSortCheckOut:   {expr: epochMillis("chk.checked_out_at"), cast: "bigint"},
	model.DeviceTrackingSortNotes:      {expr: `lower(chk.notes)`, cast: "text"},
	model.DeviceTrackingSortAssignedTo: {expr: `lower(coalesce(u.full_name, ''))`, cast: "text"},
}

// deviceTrackingFrom joins what the select list, filters and sort expressions refer to. The address is the job's own
// location; the staff member is optional.
const deviceTrackingFrom = `
FROM job_device_checks chk
JOIN jobs j ON j.id = chk.job_id
JOIN customers c ON c.id = j.customer_id
JOIN service_types st ON st.id = j.service_type_id
JOIN service_locations sl ON sl.id = j.location_id
JOIN devices d ON d.id = chk.device_id
LEFT JOIN users u ON u.id = chk.assigned_to`

// applyDeviceTrackingFilter adds the filter's conditions. Only the checks of jobs that are not deleted and have an
// appointment are listed.
func applyDeviceTrackingFilter(w *whereBuilder, f model.DeviceTrackingFilter) {
	w.add("j.deleted_at IS NULL")
	w.add("j.scheduled_at IS NOT NULL")
	if !f.ScheduledFrom.IsZero() {
		w.add("j.scheduled_at >= " + w.arg(f.ScheduledFrom.UTC()) + "::timestamptz")
	}
	if !f.ScheduledBefore.IsZero() {
		w.add("j.scheduled_at < " + w.arg(f.ScheduledBefore.UTC()) + "::timestamptz")
	}
	if len(f.CustomerIDs) > 0 {
		w.add("j.customer_id = ANY(" + w.arg(f.CustomerIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Search,
		"c.display_name", "st.name", "sl.address_line1", "sl.address_line2", "sl.city", "sl.state", "sl.postal_code",
		"d.name", "d.barcode", "d.area", "d.device_type", "chk.status", "chk.activity", "chk.notes", "u.full_name")
}

// BuildDeviceTrackingListQuery builds the statement for one page of the Device Tracking report. It selects limit+1 rows so
// the caller can tell whether another page follows.
func BuildDeviceTrackingListQuery(q model.DeviceTrackingQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := deviceTrackingSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	applyDeviceTrackingFilter(&w, q.Filter)
	if q.After != nil {
		applyKeyset(&w, deviceTrackingAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT chk.id, c.id, c.display_name, st.id, st.name, j.scheduled_at,
    sl.address_line1, sl.address_line2, sl.city, sl.state, sl.postal_code, sl.county,
    d.id, d.name, d.barcode, d.latitude::float8, d.longitude::float8, d.area, d.device_type,
    chk.checked_in_at, chk.checked_out_at, chk.status, chk.activity, chk.notes, u.id, u.full_name,
    (` + spec.sortText() + `)::text AS sort_value` +
		deviceTrackingFrom + w.where() + orderAndLimit(deviceTrackingAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}
