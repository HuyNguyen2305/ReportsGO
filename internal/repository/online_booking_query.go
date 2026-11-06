package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// onlineBookingAlias is the jobs alias in the Online Bookings report's query.
const onlineBookingAlias = "j"

// onlineBookingScheduledText renders scheduled_at as fixed-format UTC text, whatever the session time zone is, so a
// cursor built from it always means the same instant. It casts back with ::timestamptz.
const onlineBookingScheduledText = `to_char(j.scheduled_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`

// onlineBookingSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller. A customer without an email or phone sorts as an empty
// value, and the customer sorts by the full display name whatever name format is shown. The appointment is never null:
// only scheduled jobs are listed.
var onlineBookingSorts = map[model.OnlineBookingSortKey]sortSpec{
	model.OnlineBookingSortCustomer:    {expr: `lower(c.display_name)`, cast: "text"},
	model.OnlineBookingSortScheduledAt: {expr: `j.scheduled_at`, text: onlineBookingScheduledText, cast: "timestamptz"},
	model.OnlineBookingSortService:     {expr: `lower(st.name)`, cast: "text"},
	model.OnlineBookingSortEmail:       {expr: `lower(coalesce(c.email, ''))`, cast: "text"},
	model.OnlineBookingSortPhone:       {expr: `coalesce(ph.number, '')`, cast: "text"},
	model.OnlineBookingSortAddress:     {expr: `lower(sl.address_line1)`, cast: "text"},
	model.OnlineBookingSortNotes:       {expr: `lower(c.top_note)`, cast: "text"},
	model.OnlineBookingSortStatus:      {expr: `j.status::text`, cast: "text"},
}

// onlineBookingFrom joins what the select list, filters and sort expressions refer to. The address is the job's own
// location; the phone is the customer's first one (lowest id) taken in a lateral join, so each job stays one row.
const onlineBookingFrom = `
FROM jobs j
JOIN customers c ON c.id = j.customer_id
JOIN service_types st ON st.id = j.service_type_id
JOIN service_locations sl ON sl.id = j.location_id
LEFT JOIN lead_sources ls ON ls.id = c.lead_source_id
LEFT JOIN LATERAL (
    SELECT p.number FROM customer_phones p WHERE p.customer_id = c.id ORDER BY p.id LIMIT 1
) ph ON true`

// applyOnlineBookingFilter adds the filter's conditions. Only the jobs of customers who came from an online booking are
// listed, and only when they are not deleted and have an appointment.
func applyOnlineBookingFilter(w *whereBuilder, f model.OnlineBookingFilter) {
	w.add("c.booked_online")
	w.add("j.deleted_at IS NULL")
	w.add("j.scheduled_at IS NOT NULL")
	if !f.ScheduledFrom.IsZero() {
		w.add("j.scheduled_at >= " + w.arg(f.ScheduledFrom.UTC()) + "::timestamptz")
	}
	if !f.ScheduledBefore.IsZero() {
		w.add("j.scheduled_at < " + w.arg(f.ScheduledBefore.UTC()) + "::timestamptz")
	}
	switch {
	case len(f.LeadSourceIDs) > 0 && f.NoSource:
		w.add("(c.lead_source_id = ANY(" + w.arg(f.LeadSourceIDs) + "::bigint[]) OR c.lead_source_id IS NULL)")
	case len(f.LeadSourceIDs) > 0:
		w.add("c.lead_source_id = ANY(" + w.arg(f.LeadSourceIDs) + "::bigint[])")
	case f.NoSource:
		w.add("c.lead_source_id IS NULL")
	}
	applySearchOn(w, f.Search, "c.display_name", "c.email", "ph.number", "sl.address_line1", "sl.city", "st.name", "c.top_note")
}

// BuildOnlineBookingListQuery builds the statement for one page of the Online Bookings report. It selects limit+1 rows
// so the caller can tell whether another page follows. An unset name format is the first name.
func BuildOnlineBookingListQuery(q model.OnlineBookingQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := onlineBookingSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}
	format := q.NameFormat
	if format == "" {
		format = model.NameFirst
	}
	name, ok := customerNameSQL[format]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown name format %q", ErrInvalidQuery, q.NameFormat)
	}

	var w whereBuilder
	applyOnlineBookingFilter(&w, q.Filter)
	if q.After != nil {
		applyKeyset(&w, onlineBookingAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT j.id, c.id, ` + name + `, j.scheduled_at, st.id, st.name, c.email, ph.number,
    sl.address_line1, sl.address_line2, sl.city, sl.state, sl.postal_code, sl.county, c.top_note, j.status::text,
    ls.id, ls.name,
    (` + spec.sortText() + `)::text AS sort_value` +
		onlineBookingFrom + w.where() + orderAndLimit(onlineBookingAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}
