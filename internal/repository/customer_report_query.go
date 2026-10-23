package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// customerAlias is the customers alias in the new customers report's queries.
const customerAlias = "c"

// customerCreatedAtText renders created_at as fixed-format UTC text, whatever the session time zone is, so a
// cursor built from it always means the same instant. It casts back with ::timestamptz.
const customerCreatedAtText = `to_char(c.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`

// customerSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller. A customer without an email, phone or lead
// source sorts as an empty value.
var customerSorts = map[model.CustomerSortKey]sortSpec{
	model.CustomerSortCustomer:      {expr: `lower(c.display_name)`, cast: "text"},
	model.CustomerSortAccountNumber: {expr: `c.account_number`, cast: "text"},
	model.CustomerSortEmail:         {expr: `lower(coalesce(c.email, ''))`, cast: "text"},
	model.CustomerSortPhone:         {expr: `coalesce(ph.number, '')`, cast: "text"},
	model.CustomerSortSource:        {expr: `lower(coalesce(ls.name, ''))`, cast: "text"},
	model.CustomerSortCreatedAt:     {expr: `c.created_at`, text: customerCreatedAtText, cast: "timestamptz"},
}

// customerFrom joins everything the select list, filters and sort expressions can refer to. The phone is the
// customer's first one (lowest id) taken in a lateral join, so each customer stays one row. The list and the
// summary share it so they always agree about which customers are in scope.
const customerFrom = `
FROM customers c
LEFT JOIN lead_sources ls ON ls.id = c.lead_source_id
LEFT JOIN LATERAL (
    SELECT p.number FROM customer_phones p WHERE p.customer_id = c.id ORDER BY p.id LIMIT 1
) ph ON true`

// applyCustomerFilter adds the filter's conditions. List and Summary share it.
func applyCustomerFilter(w *whereBuilder, f model.CustomerFilter) {
	if !f.CreatedFrom.IsZero() {
		w.add("c.created_at >= " + w.arg(f.CreatedFrom.UTC()) + "::timestamptz")
	}
	if !f.CreatedBefore.IsZero() {
		w.add("c.created_at < " + w.arg(f.CreatedBefore.UTC()) + "::timestamptz")
	}
	if len(f.LeadSourceIDs) > 0 {
		w.add("c.lead_source_id = ANY(" + w.arg(f.LeadSourceIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Search, "c.display_name", "c.account_number", "c.email", "ph.number")
}

// BuildCustomerListQuery builds the statement for one page of the new customers report. It selects limit+1 rows
// so the caller can tell whether another page follows.
func BuildCustomerListQuery(q model.CustomerQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := customerSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	applyCustomerFilter(&w, q.Filter)
	if q.After != nil {
		applyKeyset(&w, customerAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT c.id, c.display_name, c.account_number, c.email, ph.number, ls.id, ls.name, c.created_at,
    (` + spec.sortText() + `)::text AS sort_value` +
		customerFrom + w.where() + orderAndLimit(customerAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildCustomerSummaryQuery builds the statement that counts the whole filtered set.
func BuildCustomerSummaryQuery(f model.CustomerFilter) (Statement, error) {
	var w whereBuilder
	applyCustomerFilter(&w, f)
	return Statement{SQL: `SELECT count(*)` + customerFrom + w.where(), Args: w.args}, nil
}
