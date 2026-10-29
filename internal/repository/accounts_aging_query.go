package repository

import (
	"fmt"
	"strings"

	"reportsgo/internal/model"
)

// agingAlias is the alias of the finished rows (CTE r) that the list orders and keyset-pages.
const agingAlias = "r"

// agingSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller. Money columns are numeric with two decimals, so
// their text form is the cursor value.
var agingSorts = map[model.AgingSortKey]sortSpec{
	model.AgingSortCustomer:        {expr: `lower(r.customer)`, cast: "text"},
	model.AgingSortTotal:           {expr: `r.total`, cast: "numeric"},
	model.AgingSortD0to30:          {expr: `r.d0_30`, cast: "numeric"},
	model.AgingSortD31to60:         {expr: `r.d31_60`, cast: "numeric"},
	model.AgingSortD61to90:         {expr: `r.d61_90`, cast: "numeric"},
	model.AgingSortD90Plus:         {expr: `r.d90_plus`, cast: "numeric"},
	model.AgingSortAvailableCredit: {expr: `r.available_credit`, cast: "numeric"},
}

// agingMinAge maps the Days filter to the age, in days, that a balance must reach. A "30" filter keeps rows with a
// balance in the 31-60 bucket or older, and so on.
var agingMinAge = map[int]int{30: 31, 60: 61, 90: 91}

// takeConds joins the conditions collected so far and clears them, keeping the bind arguments. The aging query has
// three places for conditions (the invoices, the finished rows and the page), all sharing one argument list.
func takeConds(w *whereBuilder) string {
	s := strings.Join(w.conds, "\n  AND ")
	w.conds = nil
	return s
}

// agingCTEs builds the WITH clause shared by the list and the summary, so they always agree about which rows are in
// scope. It registers the filter's bind arguments on w and leaves w without conditions.
//
//	inv: one row per invoice with an open balance at the as-of date: the total less the paid payments and the credit
//	     applications dated up to it.
//	a:   inv grouped per customer and service location, split into age buckets.
//	r:   a with the customer, address, phone, default card and available credit.
func agingCTEs(w *whereBuilder, f model.AgingFilter) (string, error) {
	if f.AsOf.IsZero() {
		return "", fmt.Errorf("%w: as_of is required", ErrInvalidQuery)
	}
	having := ""
	if f.MinAgeDays != 0 {
		age, ok := agingMinAge[f.MinAgeDays]
		if !ok {
			return "", fmt.Errorf("%w: unknown days %d", ErrInvalidQuery, f.MinAgeDays)
		}
		having = fmt.Sprintf("\nHAVING SUM(inv.balance) FILTER (WHERE inv.age >= %d) > 0", age)
	}

	asOf := w.arg(f.AsOf) + "::date"

	// inv
	w.add("i.parent_type = 'invoice' AND i.status IN ('sent', 'paid') AND i.deleted_at IS NULL AND i.archived_at IS NULL")
	w.add("i.document_date <= " + asOf)
	w.add("i.total - COALESCE(pay.paid, 0) - COALESCE(app.applied, 0) > 0")
	if len(f.ServiceTypeIDs) > 0 {
		w.add("j.service_type_id = ANY(" + w.arg(f.ServiceTypeIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Email, "i.billing_email")
	invWhere := takeConds(w)

	// r
	applySearchOn(w, f.Search, "c.display_name", "c.account_number", "sl.address_line1", "sl.city")
	rWhere := ""
	if cond := takeConds(w); cond != "" {
		rWhere = "\nWHERE " + cond
	}

	return `WITH inv AS (
    SELECT i.id, i.customer_id, j.location_id, i.document_date, i.billing_email,
        i.total - COALESCE(pay.paid, 0) - COALESCE(app.applied, 0) AS balance,
        ` + asOf + ` - i.document_date AS age
    FROM billing_documents i
    LEFT JOIN jobs j ON j.id = i.job_id
    LEFT JOIN LATERAL (
        SELECT SUM(p.amount) AS paid FROM payments p
        WHERE p.invoice_id = i.id AND p.status = 'paid' AND p.deleted_at IS NULL AND p.payment_date <= ` + asOf + `) pay ON true
    LEFT JOIN LATERAL (
        SELECT SUM(ca.amount) AS applied FROM credit_applications ca
        WHERE ca.invoice_id = i.id AND ca.applied_on <= ` + asOf + `) app ON true
    WHERE ` + invWhere + `
), a AS (
    SELECT inv.customer_id, inv.location_id,
        inv.customer_id * 4294967296 + COALESCE(inv.location_id, 0) AS id,
        COALESCE(SUM(inv.balance) FILTER (WHERE inv.age <= 30), 0)::numeric(14,2) AS d0_30,
        COALESCE(SUM(inv.balance) FILTER (WHERE inv.age BETWEEN 31 AND 60), 0)::numeric(14,2) AS d31_60,
        COALESCE(SUM(inv.balance) FILTER (WHERE inv.age BETWEEN 61 AND 90), 0)::numeric(14,2) AS d61_90,
        COALESCE(SUM(inv.balance) FILTER (WHERE inv.age >= 91), 0)::numeric(14,2) AS d90_plus,
        SUM(inv.balance)::numeric(14,2) AS total,
        (array_agg(inv.billing_email ORDER BY inv.document_date DESC, inv.id DESC)
            FILTER (WHERE inv.billing_email <> ''))[1] AS email
    FROM inv
    GROUP BY inv.customer_id, inv.location_id` + having + `
), r AS (
    SELECT a.id, a.customer_id, a.location_id, c.display_name AS customer,
        CASE WHEN sl.id IS NULL THEN NULL
            ELSE sl.address_line1 || CASE WHEN sl.city <> '' THEN ', ' || sl.city ELSE '' END END AS location,
        a.email, ph.number AS phone, cd.processor, cd.last4::text AS last4, cd.exp_month, cd.exp_year,
        a.d0_30, a.d31_60, a.d61_90, a.d90_plus, a.total, ac.amount AS available_credit
    FROM a
    JOIN customers c ON c.id = a.customer_id
    LEFT JOIN service_locations sl ON sl.id = a.location_id
    LEFT JOIN LATERAL (
        SELECT number FROM customer_phones WHERE customer_id = a.customer_id ORDER BY id LIMIT 1) ph ON true
    LEFT JOIN LATERAL (
        SELECT processor, last4, exp_month, exp_year FROM customer_payment_methods
        WHERE customer_id = a.customer_id AND is_default) cd ON true
    CROSS JOIN LATERAL (
        SELECT GREATEST(
            COALESCE((SELECT SUM(cr.amount) FROM credits cr
                WHERE cr.customer_id = a.customer_id AND cr.credit_date <= ` + asOf + `), 0)
            - COALESCE((SELECT SUM(ca.amount) FROM credit_applications ca JOIN credits cr ON cr.id = ca.credit_id
                WHERE cr.customer_id = a.customer_id AND ca.applied_on <= ` + asOf + `), 0),
            0)::numeric(14,2) AS amount) ac` + rWhere + `
)
`, nil
}

// BuildAccountsAgingListQuery builds the statement for one page of the Accounts Aging report. It selects limit+1
// rows so the caller can tell whether another page follows.
func BuildAccountsAgingListQuery(q model.AgingQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := agingSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	ctes, err := agingCTEs(&w, q.Filter)
	if err != nil {
		return Statement{}, err
	}
	if q.After != nil {
		applyKeyset(&w, agingAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := ctes + `SELECT r.id, r.customer_id, r.location_id, r.customer, r.location, r.email, r.phone,
    r.processor, r.last4, r.exp_month, r.exp_year,
    (r.d0_30 * 100)::bigint, (r.d31_60 * 100)::bigint, (r.d61_90 * 100)::bigint, (r.d90_plus * 100)::bigint,
    (r.total * 100)::bigint, (r.available_credit * 100)::bigint,
    (` + spec.sortText() + `)::text AS sort_value
FROM r` + w.where() + orderAndLimit(agingAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildAccountsAgingSummaryQuery builds the statement that totals the whole filtered set. Available credit is added
// once per customer, however many rows the customer has.
func BuildAccountsAgingSummaryQuery(f model.AgingFilter) (Statement, error) {
	var w whereBuilder
	ctes, err := agingCTEs(&w, f)
	if err != nil {
		return Statement{}, err
	}

	sql := ctes + `SELECT count(*),
    COALESCE(SUM(r.d0_30) * 100, 0)::bigint, COALESCE(SUM(r.d31_60) * 100, 0)::bigint,
    COALESCE(SUM(r.d61_90) * 100, 0)::bigint, COALESCE(SUM(r.d90_plus) * 100, 0)::bigint,
    COALESCE(SUM(r.total) * 100, 0)::bigint,
    COALESCE((SELECT SUM(x.available_credit) FROM (
        SELECT DISTINCT ON (r2.customer_id) r2.available_credit FROM r r2) x) * 100, 0)::bigint
FROM r`
	return Statement{SQL: sql, Args: w.args}, nil
}
