package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// totalSaleAlias is the billing_documents alias in the Total Sales report's queries.
const totalSaleAlias = "i"

// totalSaleStatus is the report's derived status: a paid invoice is paid, a sent invoice whose payments cover its
// total is paid too, one with some payments is partial, and the rest are sent. Only payments count (not credit
// applications), like the Invoices report's outstanding figure.
const totalSaleStatus = `CASE
    WHEN i.status = 'paid' THEN 'paid'
    WHEN pay.paid > 0 AND pay.paid >= i.total THEN 'paid'
    WHEN pay.paid > 0 THEN 'partial'
    ELSE 'sent' END`

// totalSaleWithoutTax is the invoice total less the tax on its tax lines.
const totalSaleWithoutTax = `(i.total - tx.tax)`

// totalSaleSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the
// keyset condition comes only from this table, never from the caller.
var totalSaleSorts = map[model.TotalSaleSortKey]sortSpec{
	model.TotalSaleSortInvoiceNumber: {expr: `i.document_number`, cast: "text"},
	model.TotalSaleSortCustomer:      {expr: `lower(c.display_name)`, cast: "text"},
	model.TotalSaleSortDate:          {expr: `i.document_date`, cast: "date"},
	model.TotalSaleSortStatus:        {expr: `(` + totalSaleStatus + `)`, cast: "text"},
	model.TotalSaleSortAmount:        {expr: `i.total`, cast: "numeric"},
	model.TotalSaleSortWithoutTax:    {expr: totalSaleWithoutTax, cast: "numeric"},
}

// totalSaleFrom joins everything the select list, filters and sort expressions can refer to. The list and the
// summary share it so they always agree about which invoices are in scope. The two lateral subqueries add up the
// invoice's tax and its paid payments from the indexed foreign keys.
const totalSaleFrom = `
FROM billing_documents i
JOIN customers c ON c.id = i.customer_id
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(l.tax_amount), 0) AS tax
    FROM invoice_tax_lines l WHERE l.invoice_id = i.id) tx ON true
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(p.amount), 0) AS paid
    FROM payments p
    WHERE p.invoice_id = i.id AND p.deleted_at IS NULL AND p.status = 'paid') pay ON true`

// applyTotalSaleFilter adds the filter's conditions. List and Summary share it. Only active invoices that were
// sent or paid are sales; drafts, voids, archived and deleted invoices never appear.
func applyTotalSaleFilter(w *whereBuilder, f model.TotalSaleFilter) error {
	err := applyCommonFilter(w, totalSaleAlias, commonFilter{
		parent:   parentInvoice,
		dateFrom: f.DateFrom,
		dateTo:   f.DateTo,
		statuses: []string{string(model.InvoiceSent), string(model.InvoicePaid)},
	})
	if err != nil {
		return err
	}

	if len(f.Statuses) > 0 {
		w.add("(" + totalSaleStatus + ") = ANY(" + w.arg(toStrings(f.Statuses)) + "::text[])")
	}
	if len(f.CustomerIDs) > 0 {
		w.add("i.customer_id = ANY(" + w.arg(f.CustomerIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Search, "i.document_number", "c.account_number", "c.display_name")
	return nil
}

// BuildTotalSalesListQuery builds the statement for one page of the Total Sales report. It selects limit+1 rows
// so the caller can tell whether another page follows.
func BuildTotalSalesListQuery(q model.TotalSaleQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := totalSaleSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	if err := applyTotalSaleFilter(&w, q.Filter); err != nil {
		return Statement{}, err
	}
	if q.After != nil {
		applyKeyset(&w, totalSaleAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT i.id, i.document_number, c.id, c.display_name, i.document_date,
    ` + totalSaleStatus + ` AS status,
    (i.total * 100)::bigint, (` + totalSaleWithoutTax + ` * 100)::bigint,
    (` + spec.sortText() + `)::text AS sort_value` +
		totalSaleFrom + w.where() + orderAndLimit(totalSaleAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildTotalSalesSummaryQuery builds the statement that totals the whole filtered set.
func BuildTotalSalesSummaryQuery(f model.TotalSaleFilter) (Statement, error) {
	var w whereBuilder
	if err := applyTotalSaleFilter(&w, f); err != nil {
		return Statement{}, err
	}

	sql := `SELECT count(*),
    COALESCE(SUM(i.total * 100), 0)::bigint,
    COALESCE(SUM(` + totalSaleWithoutTax + ` * 100), 0)::bigint` +
		totalSaleFrom + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}
