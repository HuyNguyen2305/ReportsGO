package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// inboundLeadAlias is the customers alias in the Inbound Leads report's query.
const inboundLeadAlias = "c"

// inboundLeadDateAddedText renders created_at as fixed-format UTC text, whatever the session time zone is, so a cursor
// built from it always means the same instant. It casts back with ::timestamptz.
const inboundLeadDateAddedText = `to_char(c.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`

// inboundLeadSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller. A lead without an email, phone or source sorts as an
// empty value, and the customer sorts by the full display name whatever name format is shown.
var inboundLeadSorts = map[model.InboundLeadSortKey]sortSpec{
	model.InboundLeadSortCustomer:  {expr: `lower(c.display_name)`, cast: "text"},
	model.InboundLeadSortSource:    {expr: `lower(coalesce(ls.name, ''))`, cast: "text"},
	model.InboundLeadSortEmail:     {expr: `lower(coalesce(c.email, ''))`, cast: "text"},
	model.InboundLeadSortPhone:     {expr: `coalesce(ph.number, '')`, cast: "text"},
	model.InboundLeadSortNotes:     {expr: `lower(c.top_note)`, cast: "text"},
	model.InboundLeadSortDateAdded: {expr: `c.created_at`, text: inboundLeadDateAddedText, cast: "timestamptz"},
}

// inboundLeadFrom joins what the select list, filters and sort expressions refer to. The phone is the customer's first
// one (lowest id) and the active job the earliest scheduled job that is not deleted and still open (unconfirmed or
// confirmed), each taken in a lateral join that yields at most one row, so each lead stays one row.
const inboundLeadFrom = `
FROM customers c
LEFT JOIN lead_sources ls ON ls.id = c.lead_source_id
LEFT JOIN LATERAL (
    SELECT p.number FROM customer_phones p WHERE p.customer_id = c.id ORDER BY p.id LIMIT 1
) ph ON true
LEFT JOIN LATERAL (
    SELECT st.id AS service_type_id, st.name AS service_type, j.scheduled_at
    FROM jobs j JOIN service_types st ON st.id = j.service_type_id
    WHERE j.customer_id = c.id AND j.deleted_at IS NULL AND j.status IN ('unconfirmed', 'confirmed')
      AND j.scheduled_at IS NOT NULL
    ORDER BY j.scheduled_at, j.id LIMIT 1
) aj ON true`

// applyInboundLeadFilter adds the filter's conditions. Only customers whose status is lead are ever listed.
func applyInboundLeadFilter(w *whereBuilder, f model.InboundLeadFilter) {
	w.add("c.status = 'lead'")
	if !f.CreatedFrom.IsZero() {
		w.add("c.created_at >= " + w.arg(f.CreatedFrom.UTC()) + "::timestamptz")
	}
	if !f.CreatedBefore.IsZero() {
		w.add("c.created_at < " + w.arg(f.CreatedBefore.UTC()) + "::timestamptz")
	}
	switch {
	case len(f.LeadSourceIDs) > 0 && f.NoSource:
		w.add("(c.lead_source_id = ANY(" + w.arg(f.LeadSourceIDs) + "::bigint[]) OR c.lead_source_id IS NULL)")
	case len(f.LeadSourceIDs) > 0:
		w.add("c.lead_source_id = ANY(" + w.arg(f.LeadSourceIDs) + "::bigint[])")
	case f.NoSource:
		w.add("c.lead_source_id IS NULL")
	}
	applySearchOn(w, f.Search, "c.display_name", "c.email", "ph.number", "c.top_note")
}

// BuildInboundLeadListQuery builds the statement for one page of the Inbound Leads report. It selects limit+1 rows so
// the caller can tell whether another page follows. An unset name format is the first name.
func BuildInboundLeadListQuery(q model.InboundLeadQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := inboundLeadSorts[q.Sort.Key]
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
	applyInboundLeadFilter(&w, q.Filter)
	if q.After != nil {
		applyKeyset(&w, inboundLeadAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT c.id, ` + name + `, ls.id, ls.name, c.email, ph.number, c.top_note, c.created_at,
    aj.service_type_id, aj.service_type, aj.scheduled_at,
    (` + spec.sortText() + `)::text AS sort_value` +
		inboundLeadFrom + w.where() + orderAndLimit(inboundLeadAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}
