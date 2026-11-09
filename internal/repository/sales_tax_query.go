package repository

import (
	"fmt"
	"strings"

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

// salesTaxPaymentsFrom and salesTaxCreditsFrom are the two sources of the cash basis. Every paid, not deleted payment
// of an invoice brings its share of each of the invoice's tax lines, which is the payment amount over the invoice
// total; so does every credit application (credit that was paid for and is now used against the invoice), at its
// amount. Each share is rounded to the cent per payment or application and tax line, then added up per tax. A credit
// payment has no invoice, so it carries no tax; an invoice with a zero total has nothing to share.
const (
	salesTaxPaymentsFrom = `
    SELECT l.tax_rate_id,
           round(l.taxable_amount * LEAST(p.amount, i.total) / i.total, 2) AS taxable,
           round(l.tax_amount * LEAST(p.amount, i.total) / i.total, 2) AS tax,
           round(l.surcharge_amount * LEAST(p.amount, i.total) / i.total, 2) AS surcharge
    FROM payments p
    JOIN billing_documents i ON i.id = p.invoice_id
    JOIN invoice_tax_lines l ON l.invoice_id = i.id`

	salesTaxCreditsFrom = `
    SELECT l.tax_rate_id,
           round(l.taxable_amount * LEAST(ca.amount, i.total) / i.total, 2) AS taxable,
           round(l.tax_amount * LEAST(ca.amount, i.total) / i.total, 2) AS tax,
           round(l.surcharge_amount * LEAST(ca.amount, i.total) / i.total, 2) AS surcharge
    FROM credit_applications ca
    JOIN credits cr ON cr.id = ca.credit_id
    JOIN billing_documents i ON i.id = ca.invoice_id
    JOIN invoice_tax_lines l ON l.invoice_id = i.id`
)

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
		// The same bounds and methods apply to both sources, each on its own date and method column, so every
		// argument is registered once and its placeholder reused. A credit application follows the method its
		// credit was bought with.
		payments := []string{"p.status = 'paid' AND p.deleted_at IS NULL AND i.status <> 'void' AND i.total > 0"}
		credits := []string{creditIsPaid, "i.total > 0"}
		if !f.DateFrom.IsZero() {
			from := w.arg(f.DateFrom) + "::date"
			payments = append(payments, "p.payment_date >= "+from)
			credits = append(credits, "ca.applied_on >= "+from)
		}
		if !f.DateTo.IsZero() {
			to := w.arg(f.DateTo) + "::date"
			payments = append(payments, "p.payment_date <= "+to)
			credits = append(credits, "ca.applied_on <= "+to)
		}
		if len(f.Methods) > 0 {
			methods := w.arg(toStrings(f.Methods)) + "::text[]"
			payments = append(payments, "p.method_type::text = ANY("+methods+")")
			credits = append(credits, "cr.method_type::text = ANY("+methods+")")
		}
		from = salesTaxPaymentsFrom + "\nWHERE " + strings.Join(payments, "\n  AND ") +
			"\nUNION ALL" + salesTaxCreditsFrom + "\nWHERE " + strings.Join(credits, "\n  AND ")
	case model.SalesTaxInvoiced:
		if len(f.Methods) > 0 {
			return Statement{}, fmt.Errorf("%w: payment methods only apply to the collected basis", ErrInvalidQuery)
		}
		w.add("i.parent_type = 'invoice' AND i.status IN ('sent', 'paid') AND i.deleted_at IS NULL AND i.archived_at IS NULL")
		if !f.DateFrom.IsZero() {
			w.add("i.document_date >= " + w.arg(f.DateFrom) + "::date")
		}
		if !f.DateTo.IsZero() {
			w.add("i.document_date <= " + w.arg(f.DateTo) + "::date")
		}
		from = salesTaxInvoicedFrom + w.where()
	default:
		return Statement{}, fmt.Errorf("%w: unknown basis %q", ErrInvalidQuery, f.Basis)
	}

	dir, _ := direction(s.Desc)
	sql := `SELECT tr.id, tr.name, tr.rate::text,
    (COALESCE(SUM(x.taxable), 0) * 100)::bigint,
    (COALESCE(SUM(x.tax), 0) * 100)::bigint,
    (COALESCE(SUM(x.surcharge), 0) * 100)::bigint
FROM tax_rates tr
LEFT JOIN (` + from + `
) x ON x.tax_rate_id = tr.id
GROUP BY tr.id
HAVING tr.active OR count(x.tax_rate_id) > 0
ORDER BY ` + orderExpr + ` ` + dir + `, tr.id`
	return Statement{SQL: sql, Args: w.args}, nil
}
