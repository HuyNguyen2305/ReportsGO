package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// salesTaxSorts is the complete list of things the summary can be sorted by. SQL text for ORDER BY comes only from
// this table, never from the caller. The totals are never NULL, so zero rows order predictably; ties fall to the
// tax rate id, always ascending, so taxes with nothing to tell them apart keep the order they were created in.
var salesTaxSorts = map[model.SalesTaxSortKey]string{
	model.SalesTaxSortName:         `lower(tr.name)`,
	model.SalesTaxSortTaxableSales: `COALESCE(SUM(x.taxable), 0)`,
	model.SalesTaxSortTaxRate:      `tr.rate`,
	model.SalesTaxSortTaxCollected: `COALESCE(SUM(x.tax), 0)`,
	model.SalesTaxSortSurchargeTax: `COALESCE(SUM(x.surcharge), 0)`,
}

// salesTaxCollectedFrom is the cash basis: every paid, not deleted payment of an invoice brings its share of each
// of the invoice's tax lines, which is the payment amount over the invoice total. Each share is rounded to the cent
// per payment and tax line, then added up per tax. A credit payment has no invoice, so it carries no tax; an invoice
// with a zero total has nothing to share.
const salesTaxCollectedFrom = `
    SELECT l.tax_rate_id,
           round(l.taxable_amount * p.amount / i.total, 2) AS taxable,
           round(l.tax_amount * p.amount / i.total, 2) AS tax,
           round(l.surcharge_amount * p.amount / i.total, 2) AS surcharge
    FROM payments p
    JOIN billing_documents i ON i.id = p.invoice_id
    JOIN invoice_tax_lines l ON l.invoice_id = i.id`

// salesTaxInvoicedFrom is the accrual basis: the whole tax lines of the invoices that count as sales.
const salesTaxInvoicedFrom = `
    SELECT l.tax_rate_id, l.taxable_amount AS taxable, l.tax_amount AS tax, l.surcharge_amount AS surcharge
    FROM invoice_tax_lines l
    JOIN billing_documents i ON i.id = l.invoice_id`

// BuildSalesTaxQuery builds the statement for the Sales Tax Summary: one row per tax rate with its totals for the
// filter's basis. Every active tax rate has a row, zeros when it saw no activity; an inactive one appears only when
// it did. The statement is a single query, so the rows are one consistent snapshot.
func BuildSalesTaxQuery(f model.SalesTaxFilter, s model.SalesTaxSort) (Statement, error) {
	orderExpr, ok := salesTaxSorts[s.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, s.Key)
	}

	var (
		w    whereBuilder
		from string
	)
	switch f.Basis {
	case model.SalesTaxCollected, "":
		from = salesTaxCollectedFrom
		w.add("p.status = 'paid' AND p.deleted_at IS NULL AND i.total > 0")
		if !f.DateFrom.IsZero() {
			w.add("p.payment_date >= " + w.arg(f.DateFrom) + "::date")
		}
		if !f.DateTo.IsZero() {
			w.add("p.payment_date <= " + w.arg(f.DateTo) + "::date")
		}
		if len(f.Methods) > 0 {
			w.add("p.method_type::text = ANY(" + w.arg(toStrings(f.Methods)) + "::text[])")
		}
	case model.SalesTaxInvoiced:
		if len(f.Methods) > 0 {
			return Statement{}, fmt.Errorf("%w: payment methods only apply to the collected basis", ErrInvalidQuery)
		}
		from = salesTaxInvoicedFrom
		w.add("i.parent_type = 'invoice' AND i.status IN ('sent', 'paid') AND i.deleted_at IS NULL AND i.archived_at IS NULL")
		if !f.DateFrom.IsZero() {
			w.add("i.document_date >= " + w.arg(f.DateFrom) + "::date")
		}
		if !f.DateTo.IsZero() {
			w.add("i.document_date <= " + w.arg(f.DateTo) + "::date")
		}
	default:
		return Statement{}, fmt.Errorf("%w: unknown basis %q", ErrInvalidQuery, f.Basis)
	}

	dir, _ := direction(s.Desc)
	sql := `SELECT tr.id, tr.name, tr.rate::text,
    (COALESCE(SUM(x.taxable), 0) * 100)::bigint,
    (COALESCE(SUM(x.tax), 0) * 100)::bigint,
    (COALESCE(SUM(x.surcharge), 0) * 100)::bigint
FROM tax_rates tr
LEFT JOIN (` + from + w.where() + `
) x ON x.tax_rate_id = tr.id
GROUP BY tr.id
HAVING tr.active OR count(x.tax_rate_id) > 0
ORDER BY ` + orderExpr + ` ` + dir + `, tr.id`
	return Statement{SQL: sql, Args: w.args}, nil
}
