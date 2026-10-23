package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// recurringAlias is the recurring_invoices alias in the recurring invoice report's queries.
const recurringAlias = "r"

// noDate stands in for a missing date when sorting, so a sort expression is never NULL and the keyset comparison
// is always defined. Rows without a date therefore sort after every real date, ascending.
const noDate = `DATE '9999-12-31'`

// periodDays is the approximate length of one repeat in days (a month counts as 30 and a year as 365). It only
// orders the Repeats column: "every day" < "every week" < "every month" < "every year".
const periodDays = `r.frequency_interval * CASE r.frequency_unit
    WHEN 'day' THEN 1 WHEN 'week' THEN 7 WHEN 'month' THEN 30 ELSE 365 END`

// recurringSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the
// keyset condition comes only from this table, never from the caller.
var recurringSorts = map[model.RecurringSortKey]sortSpec{
	model.RecurringSortCustomer:        {expr: `lower(c.display_name)`, cast: "text"},
	model.RecurringSortAccountNumber:   {expr: `c.account_number`, cast: "text"},
	model.RecurringSortRepeats:         {expr: `lpad((` + periodDays + `)::text, 10, '0')`, cast: "text"},
	model.RecurringSortAction:          {expr: `r.action::text`, cast: "text"},
	model.RecurringSortStatus:          {expr: `r.status::text`, cast: "text"},
	model.RecurringSortTotalValue:      {expr: `r.total_value`, cast: "numeric"},
	model.RecurringSortStartedOn:       {expr: `r.started_on`, cast: "date"},
	model.RecurringSortEndsOn:          {expr: `coalesce(r.ends_on, ` + noDate + `)`, cast: "date"},
	model.RecurringSortNextInvoiceDate: {expr: `coalesce(r.next_invoice_date, ` + noDate + `)`, cast: "date"},
}

// recurringFrom joins everything the select list, filters and sort expressions can refer to. The list and the
// summary share it so they always agree about which recurring invoices are in scope.
const recurringFrom = `
FROM recurring_invoices r
JOIN customers c ON c.id = r.customer_id
LEFT JOIN billing_documents i ON i.id = r.next_invoice_id`

// applyRecurringFilter adds the filter's conditions. List and Summary share it.
func applyRecurringFilter(w *whereBuilder, f model.RecurringFilter) {
	if !f.DateFrom.IsZero() {
		w.add("r.started_on >= " + w.arg(f.DateFrom) + "::date")
	}
	if !f.DateTo.IsZero() {
		w.add("r.started_on <= " + w.arg(f.DateTo) + "::date")
	}
	if len(f.Statuses) > 0 {
		w.add("r.status::text = ANY(" + w.arg(toStrings(f.Statuses)) + "::text[])")
	}
	if len(f.Actions) > 0 {
		w.add("r.action::text = ANY(" + w.arg(toStrings(f.Actions)) + "::text[])")
	}
	applySearchOn(w, f.Search, "c.display_name", "c.account_number", "i.document_number")
}

// BuildRecurringListQuery builds the statement for one page of the recurring invoice report. It selects limit+1
// rows so the caller can tell whether another page follows.
func BuildRecurringListQuery(q model.RecurringQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := recurringSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	applyRecurringFilter(&w, q.Filter)
	if q.After != nil {
		applyKeyset(&w, recurringAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT r.id, c.id, c.display_name, c.account_number,
    r.frequency_unit::text, r.frequency_interval, r.weekday, r.occurrences,
    r.action::text, r.status::text, (r.total_value * 100)::bigint, r.started_on, r.ends_on,
    i.id, i.document_number, r.next_invoice_date,
    (` + spec.sortText() + `)::text AS sort_value` +
		recurringFrom + w.where() + orderAndLimit(recurringAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildRecurringSummaryQuery builds the statement that totals the whole filtered set.
func BuildRecurringSummaryQuery(f model.RecurringFilter) (Statement, error) {
	var w whereBuilder
	applyRecurringFilter(&w, f)
	sql := `SELECT count(*), COALESCE(SUM(r.total_value * 100), 0)::bigint` + recurringFrom + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}
