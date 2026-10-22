package repository

import (
	"errors"
	"fmt"
	"strings"

	"reportsgo/internal/model"
)

// ErrInvalidQuery is returned when an invoice query cannot be built (unknown sort key or lifecycle, bad limit).
var ErrInvalidQuery = errors.New("invalid invoice query")

// Statement is a SQL text with its bind arguments.
type Statement struct {
	SQL  string
	Args []any
}

// sortSpec is the SQL expression behind a sort key and the type its text form is cast back to for keyset
// comparison. Expressions never yield NULL, so row comparisons in the keyset condition are always defined.
type sortSpec struct {
	expr string
	cast string
}

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

// whereBuilder collects WHERE conditions and their bind arguments. Conditions are built only from constant
// fragments and "$n" placeholders; every caller-supplied value goes into args.
type whereBuilder struct {
	conds []string
	args  []any
}

// arg registers a bind argument and returns its placeholder.
func (w *whereBuilder) arg(v any) string {
	w.args = append(w.args, v)
	return fmt.Sprintf("$%d", len(w.args))
}

func (w *whereBuilder) add(cond string) { w.conds = append(w.conds, cond) }

func (w *whereBuilder) where() string {
	if len(w.conds) == 0 {
		return ""
	}
	return "\nWHERE " + strings.Join(w.conds, "\n  AND ")
}

// applyInvoiceFilter adds the filter's conditions. List and Summary share it, so the table and its totals
// can never disagree about which invoices are in scope.
func applyInvoiceFilter(w *whereBuilder, f model.InvoiceFilter) error {
	// billing_documents also holds estimates; this report is invoices only.
	w.add("i.parent_type = 'invoice'")

	switch f.Lifecycle {
	case model.LifecycleActive, "":
		w.add("i.deleted_at IS NULL AND i.archived_at IS NULL")
	case model.LifecycleArchived:
		w.add("i.deleted_at IS NULL AND i.archived_at IS NOT NULL")
	case model.LifecycleDeleted:
		w.add("i.deleted_at IS NOT NULL")
	default:
		return fmt.Errorf("%w: unknown lifecycle %q", ErrInvalidQuery, f.Lifecycle)
	}

	if !f.DateFrom.IsZero() {
		w.add("i.document_date >= " + w.arg(f.DateFrom) + "::date")
	}
	if !f.DateTo.IsZero() {
		w.add("i.document_date <= " + w.arg(f.DateTo) + "::date")
	}
	if len(f.Statuses) > 0 {
		w.add("i.status::text = ANY(" + w.arg(toStrings(f.Statuses)) + "::text[])")
	}
	if len(f.SignatureStatus) > 0 {
		w.add("i.signature_status::text = ANY(" + w.arg(toStrings(f.SignatureStatus)) + "::text[])")
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
	if f.Search != "" {
		p := w.arg("%" + escapeLike(f.Search) + "%")
		w.add(`(i.document_number ILIKE ` + p + ` ESCAPE '\' OR c.account_number ILIKE ` + p + ` ESCAPE '\'` +
			` OR c.display_name ILIKE ` + p + ` ESCAPE '\' OR i.billing_email ILIKE ` + p + ` ESCAPE '\')`)
	}
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

	dir, cmp := "ASC", ">"
	if q.Sort.Desc {
		dir, cmp = "DESC", "<"
	}
	if q.After != nil {
		// Row comparison against the cursor; the value travels as text and is cast to the key's type.
		w.add(fmt.Sprintf("(%s, i.id) %s ((%s::text)::%s, %s::bigint)",
			spec.expr, cmp, w.arg(q.After.SortValue), spec.cast, w.arg(q.After.ID)))
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT i.id, i.document_number, c.account_number, c.id, c.display_name,
    ` + jobLabel + ` AS job, i.frequency_label, i.billing_email, i.document_date,
    (i.total * 100)::bigint, (i.discount * 100)::bigint,
    ` + tagsSubquery + ` AS tags,
    pt.name, pt.description, i.status::text, i.signature_status::text,
    (` + spec.expr + `)::text AS sort_value` +
		invoiceFrom + w.where() +
		"\nORDER BY " + spec.expr + " " + dir + ", i.id " + dir +
		"\nLIMIT " + limit
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildInvoiceSummaryQuery builds the statement that totals the whole filtered set.
// Outstanding is the unpaid balance (total minus paid payments, never below zero) of "sent" invoices.
func BuildInvoiceSummaryQuery(f model.InvoiceFilter) (Statement, error) {
	var w whereBuilder
	if err := applyInvoiceFilter(&w, f); err != nil {
		return Statement{}, err
	}

	sql := `SELECT count(*),
    COALESCE(SUM(i.total * 100), 0)::bigint,
    COALESCE(SUM(i.discount * 100), 0)::bigint,
    COALESCE(SUM(GREATEST(i.total - pay.paid, 0) * 100) FILTER (WHERE i.status = 'sent'), 0)::bigint` +
		invoiceFrom + `
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(p.amount), 0) AS paid
    FROM payments p
    WHERE p.invoice_id = i.id AND p.deleted_at IS NULL AND p.status = 'paid'
) pay ON true` + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}

// escapeLike makes s safe to embed in a LIKE pattern that uses backslash as the escape character,
// so user input is matched literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func toStrings[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}
