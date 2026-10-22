package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// paymentAlias is the payments alias in the payment report's queries.
const paymentAlias = "p"

// paymentSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the
// keyset condition comes only from this table, never from the caller. A credit has no invoice number and
// sorts as the word "credit".
var paymentSorts = map[model.PaymentSortKey]sortSpec{
	model.PaymentSortPaymentFor: {expr: `lower(coalesce(i.document_number, 'credit'))`, cast: "text"},
	model.PaymentSortCustomer:   {expr: `lower(c.display_name)`, cast: "text"},
	model.PaymentSortMethod:     {expr: `p.method_type::text`, cast: "text"},
	model.PaymentSortDate:       {expr: `p.payment_date`, cast: "date"},
	model.PaymentSortAmount:     {expr: `p.amount`, cast: "numeric"},
	model.PaymentSortStatus:     {expr: `p.status::text`, cast: "text"},
}

// paymentFrom joins everything the select list, filters and sort expressions can refer to. The list and the
// summary share it so they always agree about which payments are in scope.
const paymentFrom = `
FROM payments p
JOIN customers c ON c.id = p.customer_id
LEFT JOIN billing_documents i ON i.id = p.invoice_id`

// applyPaymentFilter adds the filter's conditions. List and Summary share it.
func applyPaymentFilter(w *whereBuilder, f model.PaymentFilter) error {
	switch f.Lifecycle {
	case model.LifecycleActive, "":
		w.add("p.deleted_at IS NULL")
	case model.LifecycleDeleted:
		w.add("p.deleted_at IS NOT NULL")
	default:
		return fmt.Errorf("%w: unknown lifecycle %q", ErrInvalidQuery, f.Lifecycle)
	}

	if !f.DateFrom.IsZero() {
		w.add("p.payment_date >= " + w.arg(f.DateFrom) + "::date")
	}
	if !f.DateTo.IsZero() {
		w.add("p.payment_date <= " + w.arg(f.DateTo) + "::date")
	}
	if len(f.Statuses) > 0 {
		w.add("p.status::text = ANY(" + w.arg(toStrings(f.Statuses)) + "::text[])")
	}
	if len(f.Methods) > 0 {
		w.add("p.method_type::text = ANY(" + w.arg(toStrings(f.Methods)) + "::text[])")
	}
	applySearchOn(w, f.Search, "i.document_number", "c.account_number", "c.display_name")
	return nil
}

// BuildPaymentListQuery builds the statement for one page of the payment report. It selects limit+1 rows so
// the caller can tell whether another page follows.
func BuildPaymentListQuery(q model.PaymentQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := paymentSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	if err := applyPaymentFilter(&w, q.Filter); err != nil {
		return Statement{}, err
	}
	if q.After != nil {
		applyKeyset(&w, paymentAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT p.id, c.id, c.display_name, c.account_number, i.id, i.document_number,
    p.method_type::text, p.card_brand, p.card_last4::text, (p.amount * 100)::bigint, p.payment_date,
    p.status::text,
    (` + spec.sortText() + `)::text AS sort_value` +
		paymentFrom + w.where() + orderAndLimit(paymentAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildPaymentSummaryQuery builds the statement that totals the whole filtered set.
func BuildPaymentSummaryQuery(f model.PaymentFilter) (Statement, error) {
	var w whereBuilder
	if err := applyPaymentFilter(&w, f); err != nil {
		return Statement{}, err
	}
	sql := `SELECT count(*), COALESCE(SUM(p.amount * 100), 0)::bigint` + paymentFrom + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}
