package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// invoiceAlias is the billing_documents alias in the invoice report's queries.
const invoiceAlias = "i"

// jobLabel is the report's "Job" column: the service type with the location's first address line.
const jobLabel = `CASE WHEN j.id IS NULL THEN NULL ELSE st.name || ' (' || sl.address_line1 || ')' END`

// invoiceSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller.
var invoiceSorts = map[model.InvoiceSortKey]sortSpec{
	model.SortAccountNumber:   {expr: `c.account_number`, cast: "text"},
	model.SortInvoiceNumber:   {expr: `i.document_number`, cast: "text"},
	model.SortCustomer:        {expr: `lower(c.display_name)`, cast: "text"},
	model.SortJob:             {expr: `lower(coalesce(` + jobLabel + `, ''))`, cast: "text"},
	model.SortBillingEmail:    {expr: `lower(coalesce(i.billing_email, ''))`, cast: "text"},
	model.SortDate:            {expr: `i.document_date`, cast: "date"},
	model.SortTotal:           {expr: `i.total`, cast: "numeric"},
	model.SortDiscount:        {expr: `i.discount`, cast: "numeric"},
	model.SortStatus:          {expr: `i.status::text`, cast: "text"},
	model.SortSignatureStatus: {expr: `i.signature_status::text`, cast: "text"},
	model.SortPaymentTerms:    {expr: `lower(coalesce(pt.name, ''))`, cast: "text"},
}

// invoiceFrom joins everything the select list, filters and sort expressions can refer to.
const invoiceFrom = `
FROM billing_documents i
JOIN customers c ON c.id = i.customer_id
LEFT JOIN jobs j ON j.id = i.job_id
LEFT JOIN service_types st ON st.id = j.service_type_id
LEFT JOIN service_locations sl ON sl.id = j.location_id
LEFT JOIN payment_terms pt ON pt.id = i.payment_terms_id`

// tagsSubquery aggregates an invoice's tag names in one correlated subquery (no per-row queries from Go).
const tagsSubquery = `COALESCE((
    SELECT array_agg(t.name ORDER BY t.name)
    FROM invoice_tag_links l JOIN invoice_tags t ON t.id = l.tag_id
    WHERE l.invoice_id = i.id), '{}'::text[])`

// applyInvoiceFilter adds the filter's conditions. List and Summary share it, so the table and its totals
// can never disagree about which invoices are in scope.
func applyInvoiceFilter(w *whereBuilder, f model.InvoiceFilter) error {
	err := applyCommonFilter(w, invoiceAlias, commonFilter{
		parent:          parentInvoice,
		dateFrom:        f.DateFrom,
		dateTo:          f.DateTo,
		lifecycle:       f.Lifecycle,
		statuses:        toStrings(f.Statuses),
		signatureStatus: f.SignatureStatus,
	})
	if err != nil {
		return err
	}

	if len(f.ServiceTypeIDs) > 0 {
		w.add("j.service_type_id = ANY(" + w.arg(f.ServiceTypeIDs) + "::bigint[])")
	}
	if len(f.TagIDs) > 0 {
		w.add("EXISTS (SELECT 1 FROM invoice_tag_links tl WHERE tl.invoice_id = i.id AND tl.tag_id = ANY(" +
			w.arg(f.TagIDs) + "::bigint[]))")
	}
	if len(f.PaymentTermIDs) > 0 {
		w.add("i.payment_terms_id = ANY(" + w.arg(f.PaymentTermIDs) + "::bigint[])")
	}
	applySearch(w, invoiceAlias, f.Search)
	return nil
}

// BuildInvoiceListQuery builds the statement for one page of the invoice report. It selects limit+1 rows so
// the caller can tell whether another page follows.
func BuildInvoiceListQuery(q model.InvoiceQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := invoiceSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	if err := applyInvoiceFilter(&w, q.Filter); err != nil {
		return Statement{}, err
	}
	if q.After != nil {
		applyKeyset(&w, invoiceAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT i.id, i.document_number, c.account_number, c.id, c.display_name,
    ` + jobLabel + ` AS job, i.frequency_label, i.billing_email, i.document_date,
    (i.total * 100)::bigint, (i.discount * 100)::bigint,
    ` + tagsSubquery + ` AS tags,
    pt.name, pt.description, i.status::text, i.signature_status::text,
    (` + spec.expr + `)::text AS sort_value` +
		invoiceFrom + w.where() + orderAndLimit(invoiceAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildInvoiceSummaryQuery builds the statement that totals the whole filtered set.
// Outstanding is the unpaid balance (total minus paid payments, never below zero) of "sent" invoices.
// The payments subquery sits inside a CASE branch, so it runs only for "sent" invoices; a join would sum the
// payments of every invoice in scope just to ignore most of the results.
func BuildInvoiceSummaryQuery(f model.InvoiceFilter) (Statement, error) {
	var w whereBuilder
	if err := applyInvoiceFilter(&w, f); err != nil {
		return Statement{}, err
	}

	sql := `SELECT count(*),
    COALESCE(SUM(i.total * 100), 0)::bigint,
    COALESCE(SUM(i.discount * 100), 0)::bigint,
    COALESCE(SUM(CASE WHEN i.status = 'sent' THEN GREATEST(i.total - COALESCE((
        SELECT SUM(p.amount)
        FROM payments p
        WHERE p.invoice_id = i.id AND p.deleted_at IS NULL AND p.status = 'paid'), 0), 0) * 100 END), 0)::bigint` +
		invoiceFrom + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}
