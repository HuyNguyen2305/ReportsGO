package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// serviceLookupAlias is the jobs alias in the Service Lookup queries.
const serviceLookupAlias = "j"

// serviceLookupNever is the sort value of a job without a schedule: before every real time, so such jobs come first
// when ascending. It is in the same fixed UTC form as serviceLookupAtText.
const serviceLookupNever = `0001-01-01T00:00:00.000000Z`

// serviceLookupAtText renders the scheduled time as fixed-format UTC text, whatever the session time zone is, so a
// cursor built from it always means the same instant.
const serviceLookupAtText = `CASE WHEN j.scheduled_at IS NULL THEN '` + serviceLookupNever + `'
    ELSE to_char(j.scheduled_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') END`

// serviceLookupWithoutTax is the job's value less its tax.
const serviceLookupWithoutTax = `(j.invoice_value - j.tax_amount)`

// serviceLookupSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the
// keyset condition comes only from this table, never from the caller. An unscheduled job and an unassigned one sort
// as the earliest and the empty name.
var serviceLookupSorts = map[model.ServiceLookupSortKey]sortSpec{
	model.ServiceLookupSortCustomer:    {expr: `lower(c.display_name)`, cast: "text"},
	model.ServiceLookupSortServiceType: {expr: `lower(st.name)`, cast: "text"},
	model.ServiceLookupSortScheduledAt: {
		expr: `coalesce(j.scheduled_at, '0001-01-01 00:00:00+00'::timestamptz)`, text: serviceLookupAtText, cast: "timestamptz",
	},
	model.ServiceLookupSortAddress:      {expr: `lower(sl.address_line1)`, cast: "text"},
	model.ServiceLookupSortCity:         {expr: `lower(sl.city)`, cast: "text"},
	model.ServiceLookupSortZip:          {expr: `lower(sl.postal_code)`, cast: "text"},
	model.ServiceLookupSortCounty:       {expr: `lower(sl.county)`, cast: "text"},
	model.ServiceLookupSortInvoiceValue: {expr: `j.invoice_value`, cast: "numeric"},
	model.ServiceLookupSortWithoutTax:   {expr: serviceLookupWithoutTax, cast: "numeric"},
	model.ServiceLookupSortStatus:       {expr: `j.status::text`, cast: "text"},
	model.ServiceLookupSortAssignedTo:   {expr: `lower(coalesce(u.full_name, ''))`, cast: "text"},
}

// serviceLookupTabConds are the conditions of the tabs. They are constants, so they are safe to embed in SQL text.
var serviceLookupTabConds = map[model.ServiceLookupTab]string{
	model.ServiceLookupActive:   `(j.deleted_at IS NULL AND NOT j.in_work_pool)`,
	model.ServiceLookupWorkPool: `(j.in_work_pool AND j.deleted_at IS NULL)`,
	model.ServiceLookupDeleted:  `(j.deleted_at IS NOT NULL)`,
}

// serviceLookupBaseFrom joins what the filters, the search and the sort expressions can refer to, for both the list
// and the summary, so they always agree about which jobs are in scope. The phone is the customer's first one
// (lowest id) taken in a lateral join, so each job stays one row.
const serviceLookupBaseFrom = `
FROM jobs j
JOIN customers c ON c.id = j.customer_id
JOIN service_types st ON st.id = j.service_type_id
JOIN service_locations sl ON sl.id = j.location_id
LEFT JOIN LATERAL (
    SELECT p.number FROM customer_phones p WHERE p.customer_id = c.id ORDER BY p.id LIMIT 1
) ph ON true`

// serviceLookupListFrom adds what only the list shows: the assignee (also sortable) and the job's tags, ordered by
// name, in one lateral join so a job with several tags stays one row.
const serviceLookupListFrom = serviceLookupBaseFrom + `
LEFT JOIN users u ON u.id = j.assigned_to
LEFT JOIN LATERAL (
    SELECT array_agg(t.id ORDER BY lower(t.name), t.id) AS ids, array_agg(t.name ORDER BY lower(t.name), t.id) AS names
    FROM job_tag_links l JOIN job_tags t ON t.id = l.tag_id WHERE l.job_id = j.id
) tg ON true`

// applyServiceLookupFilter adds the filter's conditions except the tab, which the list adds as a plain condition and
// the summary only as the selector of its aggregates. List and Summary share it.
func applyServiceLookupFilter(w *whereBuilder, f model.ServiceLookupFilter) {
	if !f.ScheduledFrom.IsZero() {
		w.add("j.scheduled_at >= " + w.arg(f.ScheduledFrom.UTC()) + "::timestamptz")
	}
	if !f.ScheduledBefore.IsZero() {
		w.add("j.scheduled_at < " + w.arg(f.ScheduledBefore.UTC()) + "::timestamptz")
	}
	if len(f.Statuses) > 0 {
		w.add("j.status::text = ANY(" + w.arg(toStrings(f.Statuses)) + "::text[])")
	}
	if len(f.ServiceTypeIDs) > 0 {
		w.add("j.service_type_id = ANY(" + w.arg(f.ServiceTypeIDs) + "::bigint[])")
	}
	if len(f.AssignedToIDs) > 0 {
		w.add("j.assigned_to = ANY(" + w.arg(f.AssignedToIDs) + "::bigint[])")
	}
	if len(f.TagIDs) > 0 {
		w.add("EXISTS (SELECT 1 FROM job_tag_links l WHERE l.job_id = j.id AND l.tag_id = ANY(" + w.arg(f.TagIDs) + "::bigint[]))")
	}
	applySearchOn(w, f.Search, "c.display_name", "c.account_number", "ph.number", "st.name", "sl.address_line1", "sl.city")
}

// serviceLookupTabCond returns the condition of tab; the empty tab is Active.
func serviceLookupTabCond(tab model.ServiceLookupTab) (string, error) {
	if tab == "" {
		tab = model.ServiceLookupActive
	}
	cond, ok := serviceLookupTabConds[tab]
	if !ok {
		return "", fmt.Errorf("%w: unknown tab %q", ErrInvalidQuery, tab)
	}
	return cond, nil
}

// BuildServiceLookupListQuery builds the statement for one page of Service Lookup. It selects limit+1 rows so the
// caller can tell whether another page follows.
func BuildServiceLookupListQuery(q model.ServiceLookupQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := serviceLookupSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}
	tab, err := serviceLookupTabCond(q.Filter.Tab)
	if err != nil {
		return Statement{}, err
	}

	var w whereBuilder
	w.add(tab)
	applyServiceLookupFilter(&w, q.Filter)
	if q.After != nil {
		applyKeyset(&w, serviceLookupAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT j.id, c.id, c.display_name, ph.number, st.id, st.name, j.scheduled_at,
    sl.address_line1, sl.address_line2, sl.city, sl.state, sl.postal_code, sl.county,
    (j.invoice_value * 100)::bigint, (` + serviceLookupWithoutTax + ` * 100)::bigint, j.status::text,
    u.id, u.full_name, tg.ids, tg.names,
    (` + spec.sortText() + `)::text AS sort_value` +
		serviceLookupListFrom + w.where() + orderAndLimit(serviceLookupAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildServiceLookupSummaryQuery builds the statement that totals the selected tab's filtered set and counts every
// tab under the same filters. The tab is not a condition here: each aggregate picks its own tab with FILTER, so all
// four numbers come from the same rows.
func BuildServiceLookupSummaryQuery(f model.ServiceLookupFilter) (Statement, error) {
	selected, err := serviceLookupTabCond(f.Tab)
	if err != nil {
		return Statement{}, err
	}

	var w whereBuilder
	applyServiceLookupFilter(&w, f)

	sql := `SELECT count(*) FILTER (WHERE ` + selected + `),
    COALESCE(SUM(j.invoice_value * 100) FILTER (WHERE ` + selected + `), 0)::bigint,
    COALESCE(SUM(` + serviceLookupWithoutTax + ` * 100) FILTER (WHERE ` + selected + `), 0)::bigint,
    count(*) FILTER (WHERE ` + serviceLookupTabConds[model.ServiceLookupActive] + `),
    count(*) FILTER (WHERE ` + serviceLookupTabConds[model.ServiceLookupWorkPool] + `),
    count(*) FILTER (WHERE ` + serviceLookupTabConds[model.ServiceLookupDeleted] + `)` +
		serviceLookupBaseFrom + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}
