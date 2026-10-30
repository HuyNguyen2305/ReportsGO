package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// paymentCollectedAlias is the payments alias in the Payments Collected report's queries.
const paymentCollectedAlias = "p"

// paymentCollectedWithoutTax is the payment less its share of the invoice's tax: the amount scaled by the ratio of
// the invoice total without tax to the invoice total, rounded to the cent. A credit payment, or a payment for an
// invoice with a zero total, has no tax share. The list and the summary both use this one expression, so the
// footer always adds up the rounded row values.
const paymentCollectedWithoutTax = `(CASE WHEN i.id IS NULL OR i.total = 0 THEN p.amount
    ELSE round(p.amount * (i.total - tx.tax) / i.total, 2) END)`

// paymentCollectedServiceState is the state of the service location of the invoice's job, or NULL.
const paymentCollectedServiceState = `NULLIF(sl.state, '')`

// paymentCollectedSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the
// keyset condition comes only from this table, never from the caller. A credit has no invoice number and sorts as
// the word "credit"; a missing state or staff member sorts as an empty value, first when ascending.
var paymentCollectedSorts = map[model.PaymentCollectedSortKey]sortSpec{
	model.PaymentCollectedSortPaymentFor:   {expr: `lower(coalesce(i.document_number, 'credit'))`, cast: "text"},
	model.PaymentCollectedSortCustomer:     {expr: `lower(c.display_name)`, cast: "text"},
	model.PaymentCollectedSortMethod:       {expr: `p.method_type::text`, cast: "text"},
	model.PaymentCollectedSortServiceState: {expr: `lower(coalesce(` + paymentCollectedServiceState + `, ''))`, cast: "text"},
	model.PaymentCollectedSortStaff:        {expr: `lower(coalesce(u.full_name, ''))`, cast: "text"},
	model.PaymentCollectedSortDate:         {expr: `p.payment_date`, cast: "date"},
	model.PaymentCollectedSortAmount:       {expr: `p.amount`, cast: "numeric"},
	model.PaymentCollectedSortWithoutTax:   {expr: paymentCollectedWithoutTax, cast: "numeric"},
}

// paymentInvoiceTax joins the total tax of the payment's invoice as tx.tax, which paymentCollectedWithoutTax uses.
// The payment must be aliased "p".
const paymentInvoiceTax = `
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(l.tax_amount), 0) AS tax
    FROM invoice_tax_lines l WHERE l.invoice_id = p.invoice_id) tx ON true`

// paymentCollectedFrom joins everything the select list, filters and sort expressions can refer to. The list and
// the summary share it so they always agree about which payments are in scope.
const paymentCollectedFrom = `
FROM payments p
JOIN customers c ON c.id = p.customer_id
LEFT JOIN billing_documents i ON i.id = p.invoice_id
LEFT JOIN jobs j ON j.id = i.job_id
LEFT JOIN service_locations sl ON sl.id = j.location_id
LEFT JOIN users u ON u.id = p.received_by` + paymentInvoiceTax

// applyPaymentCollectedFilter adds the filter's conditions. List and Summary share it. Only payments that were
// paid and not deleted are collected; pending, failed, refunded and deleted payments never appear.
func applyPaymentCollectedFilter(w *whereBuilder, f model.PaymentCollectedFilter) error {
	w.add("p.status = 'paid' AND p.deleted_at IS NULL")

	switch f.PaymentFor {
	case model.PaymentForInvoices, "":
		w.add("p.invoice_id IS NOT NULL")
	case model.PaymentForCredits:
		w.add("p.credit_id IS NOT NULL")
	default:
		return fmt.Errorf("%w: unknown payment_for %q", ErrInvalidQuery, f.PaymentFor)
	}

	if !f.DateFrom.IsZero() {
		w.add("p.payment_date >= " + w.arg(f.DateFrom) + "::date")
	}
	if !f.DateTo.IsZero() {
		w.add("p.payment_date <= " + w.arg(f.DateTo) + "::date")
	}
	if len(f.Methods) > 0 {
		w.add("p.method_type::text = ANY(" + w.arg(toStrings(f.Methods)) + "::text[])")
	}
	if len(f.CustomerIDs) > 0 {
		w.add("p.customer_id = ANY(" + w.arg(f.CustomerIDs) + "::bigint[])")
	}
	if len(f.StaffIDs) > 0 {
		w.add("p.received_by = ANY(" + w.arg(f.StaffIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Search, "i.document_number", "c.account_number", "c.display_name")
	return nil
}

// BuildPaymentsCollectedListQuery builds the statement for one page of the Payments Collected report. It selects
// limit+1 rows so the caller can tell whether another page follows.
func BuildPaymentsCollectedListQuery(q model.PaymentCollectedQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := paymentCollectedSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	if err := applyPaymentCollectedFilter(&w, q.Filter); err != nil {
		return Statement{}, err
	}
	if q.After != nil {
		applyKeyset(&w, paymentCollectedAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT p.id, c.id, c.display_name, i.id, i.document_number,
    p.method_type::text, p.card_brand, p.card_last4::text, ` + paymentCollectedServiceState + `,
    u.id, u.full_name, p.payment_date, (p.amount * 100)::bigint,
    (` + paymentCollectedWithoutTax + ` * 100)::bigint,
    (` + spec.sortText() + `)::text AS sort_value` +
		paymentCollectedFrom + w.where() + orderAndLimit(paymentCollectedAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildPaymentsCollectedSummaryQuery builds the statement that totals the whole filtered set.
func BuildPaymentsCollectedSummaryQuery(f model.PaymentCollectedFilter) (Statement, error) {
	var w whereBuilder
	if err := applyPaymentCollectedFilter(&w, f); err != nil {
		return Statement{}, err
	}

	sql := `SELECT count(*),
    COALESCE(SUM(p.amount * 100), 0)::bigint,
    COALESCE(SUM(` + paymentCollectedWithoutTax + ` * 100), 0)::bigint` +
		paymentCollectedFrom + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}
