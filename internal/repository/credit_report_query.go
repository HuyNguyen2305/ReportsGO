package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// creditAlias is the credits alias in the credit report's queries.
const creditAlias = "cr"

// creditSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller.
var creditSorts = map[model.CreditSortKey]sortSpec{
	model.CreditSortCustomer:      {expr: `lower(c.display_name)`, cast: "text"},
	model.CreditSortAccountNumber: {expr: `c.account_number`, cast: "text"},
	model.CreditSortMethod:        {expr: `cr.method_type::text`, cast: "text"},
	model.CreditSortAmount:        {expr: `cr.amount`, cast: "numeric"},
	model.CreditSortDate:          {expr: `cr.credit_date`, cast: "date"},
	model.CreditSortCreatedBy:     {expr: `lower(coalesce(u.full_name, ''))`, cast: "text"},
}

// creditFrom joins everything the select list, filters and sort expressions can refer to. The list and the
// summary share it so they always agree about which credits are in scope.
const creditFrom = `
FROM credits cr
JOIN customers c ON c.id = cr.customer_id
LEFT JOIN users u ON u.id = cr.created_by`

// applyCreditFilter adds the filter's conditions. List and Summary share it.
func applyCreditFilter(w *whereBuilder, f model.CreditFilter) {
	if !f.DateFrom.IsZero() {
		w.add("cr.credit_date >= " + w.arg(f.DateFrom) + "::date")
	}
	if !f.DateTo.IsZero() {
		w.add("cr.credit_date <= " + w.arg(f.DateTo) + "::date")
	}
	if len(f.Methods) > 0 {
		w.add("cr.method_type::text = ANY(" + w.arg(toStrings(f.Methods)) + "::text[])")
	}
	applySearchOn(w, f.Search, "c.display_name", "c.account_number", "cr.method_detail")
}

// BuildCreditListQuery builds the statement for one page of the credit report. It selects limit+1 rows so
// the caller can tell whether another page follows.
func BuildCreditListQuery(q model.CreditQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := creditSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	applyCreditFilter(&w, q.Filter)
	if q.After != nil {
		applyKeyset(&w, creditAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT cr.id, c.id, c.display_name, c.account_number, cr.method_type::text, cr.method_detail,
    (cr.amount * 100)::bigint, cr.credit_date, u.full_name,
    (` + spec.sortText() + `)::text AS sort_value` +
		creditFrom + w.where() + orderAndLimit(creditAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildCreditSummaryQuery builds the statement that totals the whole filtered set.
func BuildCreditSummaryQuery(f model.CreditFilter) (Statement, error) {
	var w whereBuilder
	applyCreditFilter(&w, f)
	sql := `SELECT count(*), COALESCE(SUM(cr.amount * 100), 0)::bigint` + creditFrom + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}
