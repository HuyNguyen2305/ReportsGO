package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// jobAlias is the jobs alias in the new jobs report's queries.
const jobAlias = "j"

// jobCreatedAtText renders created_at as fixed-format UTC text, whatever the session time zone is, so a cursor
// built from it always means the same instant. It casts back with ::timestamptz.
const jobCreatedAtText = `to_char(j.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`

// jobSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller. A job without a lead source sorts as an empty
// name.
var jobSorts = map[model.JobSortKey]sortSpec{
	model.JobSortCustomer:      {expr: `lower(c.display_name)`, cast: "text"},
	model.JobSortAccountNumber: {expr: `c.account_number`, cast: "text"},
	model.JobSortServiceType:   {expr: `lower(st.name)`, cast: "text"},
	model.JobSortSource:        {expr: `lower(coalesce(ls.name, ''))`, cast: "text"},
	model.JobSortInvoiceValue:  {expr: `j.invoice_value`, cast: "numeric"},
	model.JobSortCreatedAt:     {expr: `j.created_at`, text: jobCreatedAtText, cast: "timestamptz"},
	model.JobSortStatus:        {expr: `j.status::text`, cast: "text"},
}

// jobFrom joins everything the select list, filters and sort expressions can refer to. The list and the summary
// share it so they always agree about which jobs are in scope.
const jobFrom = `
FROM jobs j
JOIN customers c ON c.id = j.customer_id
JOIN service_types st ON st.id = j.service_type_id
LEFT JOIN lead_sources ls ON ls.id = j.lead_source_id`

// applyJobFilter adds the filter's conditions. List and Summary share it.
func applyJobFilter(w *whereBuilder, f model.JobFilter) {
	if !f.CreatedFrom.IsZero() {
		w.add("j.created_at >= " + w.arg(f.CreatedFrom.UTC()) + "::timestamptz")
	}
	if !f.CreatedBefore.IsZero() {
		w.add("j.created_at < " + w.arg(f.CreatedBefore.UTC()) + "::timestamptz")
	}
	if len(f.Statuses) > 0 {
		w.add("j.status::text = ANY(" + w.arg(toStrings(f.Statuses)) + "::text[])")
	}
	if len(f.ServiceTypeIDs) > 0 {
		w.add("j.service_type_id = ANY(" + w.arg(f.ServiceTypeIDs) + "::bigint[])")
	}
	if len(f.LeadSourceIDs) > 0 {
		w.add("j.lead_source_id = ANY(" + w.arg(f.LeadSourceIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Search, "c.display_name", "c.account_number", "st.name")
}

// BuildJobListQuery builds the statement for one page of the new jobs report. It selects limit+1 rows so the
// caller can tell whether another page follows.
func BuildJobListQuery(q model.JobQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := jobSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	applyJobFilter(&w, q.Filter)
	if q.After != nil {
		applyKeyset(&w, jobAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT j.id, c.id, c.display_name, c.account_number, st.id, st.name, ls.id, ls.name,
    (j.invoice_value * 100)::bigint, j.created_at, j.status::text,
    (` + spec.sortText() + `)::text AS sort_value` +
		jobFrom + w.where() + orderAndLimit(jobAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildJobSummaryQuery builds the statement that totals the whole filtered set.
func BuildJobSummaryQuery(f model.JobFilter) (Statement, error) {
	var w whereBuilder
	applyJobFilter(&w, f)
	sql := `SELECT count(*), COALESCE(SUM(j.invoice_value * 100), 0)::bigint` + jobFrom + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}
