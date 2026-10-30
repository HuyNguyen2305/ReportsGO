package repository

import (
	"fmt"
	"strings"

	"reportsgo/internal/model"
)

// salesBreakdownAlias is the alias of the finished rows (CTE r) that the list orders and keyset-pages.
const salesBreakdownAlias = "r"

// salesBreakdownSorts is the complete list of things the grid can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller. The months and the total are numeric with two
// decimals, so their text form is the cursor value.
var salesBreakdownSorts = func() map[model.SalesBreakdownSortKey]sortSpec {
	m := map[model.SalesBreakdownSortKey]sortSpec{
		model.SalesBreakdownSortName:          {expr: `lower(r.name)`, cast: "text"},
		model.SalesBreakdownSortAccountNumber: {expr: `r.account_number`, cast: "text"},
		model.SalesBreakdownSortTotal:         {expr: `r.total`, cast: "numeric"},
	}
	for i, key := range model.SalesBreakdownMonthKeys {
		m[key] = sortSpec{expr: fmt.Sprintf("r.m%02d", i+1), cast: "numeric"}
	}
	return m
}()

// salesBreakdownDim is the table a grouping's rows come from: its name column and, for clients, the account number.
type salesBreakdownDim struct {
	table, name, account string
	search               []string
}

var salesBreakdownDims = map[model.SalesBreakdownBy]salesBreakdownDim{
	model.SalesBreakdownByClient: {table: "customers", name: "x.display_name", account: "x.account_number",
		search: []string{"x.display_name", "x.account_number"}},
	model.SalesBreakdownByService: {table: "service_types", name: "x.name", account: "NULL::text", search: []string{"x.name"}},
	model.SalesBreakdownByItem:    {table: "items", name: "x.name", account: "NULL::text", search: []string{"x.name"}},
	model.SalesBreakdownByStaff:   {table: "users", name: "x.full_name", account: "NULL::text", search: []string{"x.full_name"}},
	model.SalesBreakdownBySource:  {table: "lead_sources", name: "x.name", account: "NULL::text", search: []string{"x.name"}},
	model.SalesBreakdownByTag:     {table: "job_tags", name: "x.name", account: "NULL::text", search: []string{"x.name"}},
}

// salesBreakdownPieces turns the facts f into the pieces d that the rows add up: one piece per fact and group (a fact
// with several tags is a piece under each tag; on the item grouping a fact is split into one piece per invoice line).
// group_id is NULL for the amount that belongs to no group, which becomes the unassigned line. (kind, id, line_id)
// identifies a piece, so the summary can count each one once.
var salesBreakdownPieces = map[model.SalesBreakdownBy]string{
	model.SalesBreakdownByClient: `
    SELECT f.kind, f.id, 0::bigint AS line_id, f.customer_id AS group_id, f.month, f.amount FROM f`,
	model.SalesBreakdownByService: `
    SELECT f.kind, f.id, 0::bigint AS line_id, j.service_type_id AS group_id, f.month, f.amount
    FROM f LEFT JOIN jobs j ON j.id = f.job_id`,
	model.SalesBreakdownByStaff: `
    SELECT f.kind, f.id, 0::bigint AS line_id, j.assigned_to AS group_id, f.month, f.amount
    FROM f LEFT JOIN jobs j ON j.id = f.job_id`,
	model.SalesBreakdownBySource: `
    SELECT f.kind, f.id, 0::bigint AS line_id, c.lead_source_id AS group_id, f.month, f.amount
    FROM f JOIN customers c ON c.id = f.customer_id`,
	model.SalesBreakdownByTag: `
    SELECT f.kind, f.id, 0::bigint AS line_id, l.tag_id AS group_id, f.month, f.amount
    FROM f LEFT JOIN job_tag_links l ON l.job_id = f.job_id`,
	// A payment or a job-less invoice is split over its invoice's lines; a forecast job over the lines of its
	// non-void, non-deleted invoices. Each line takes its share of the amount, rounded to the cent; a fact whose
	// lines add up to nothing stays whole and unassigned.
	model.SalesBreakdownByItem: `
    SELECT f.kind, f.id, COALESCE(li.line_id, 0) AS line_id, li.item_id AS group_id, f.month,
        CASE WHEN li.line_id IS NULL THEN f.amount ELSE round(f.amount * li.line_total / li.lsum, 2) END AS amount
    FROM f
    LEFT JOIN LATERAL (
        SELECT x.id AS line_id, x.item_id, x.line_total, x.lsum FROM (
            SELECT ii.id, ii.item_id, ii.line_total, SUM(ii.line_total) OVER () AS lsum
            FROM invoice_items ii JOIN billing_documents b ON b.id = ii.invoice_id
            WHERE CASE WHEN f.invoice_id IS NOT NULL THEN b.id = f.invoice_id
                       ELSE b.job_id = f.job_id AND b.deleted_at IS NULL AND b.status <> 'void' END
        ) x WHERE x.lsum <> 0
    ) li ON true`,
}

// salesBreakdownMonthSums returns the twelve month sums of amount over the rows of src (filtered by FILTER), as the
// given column names or, when names is empty, as one bigint array in cents.
func salesBreakdownMonthSums(amount, month string, names bool) string {
	parts := make([]string, 12)
	for m := 1; m <= 12; m++ {
		sum := fmt.Sprintf("COALESCE(SUM(%s) FILTER (WHERE %s = %d), 0)", amount, month, m)
		if names {
			parts[m-1] = fmt.Sprintf("%s::numeric(14,2) AS m%02d", sum, m)
		} else {
			parts[m-1] = fmt.Sprintf("(%s * 100)::bigint", sum)
		}
	}
	if names {
		return strings.Join(parts, ",\n        ")
	}
	return "ARRAY[" + strings.Join(parts, ", ") + "]::bigint[]"
}

// salesBreakdownFacts builds the facts f of the report type: one row per job or job-less invoice (forecast) or per
// payment (revenue), with its customer, job, invoice, month (1 to 12) and amount. It registers its arguments on w.
func salesBreakdownFacts(w *whereBuilder, f model.SalesBreakdownFilter) (string, error) {
	if f.YearFrom.IsZero() || f.YearTo.IsZero() {
		return "", fmt.Errorf("%w: the year is required", ErrInvalidQuery)
	}
	switch f.Type {
	case model.SalesBreakdownForecast:
		if f.MonthStarts[0].IsZero() || f.MonthStarts[12].IsZero() {
			return "", fmt.Errorf("%w: the month starts are required", ErrInvalidQuery)
		}
		if f.Measure != "" || len(f.Methods) > 0 {
			return "", fmt.Errorf("%w: measure and methods apply to revenue only", ErrInvalidQuery)
		}
		bounds := w.arg(f.MonthStarts[:]) + "::timestamptz[]"
		from, to := w.arg(f.YearFrom)+"::date", w.arg(f.YearTo)+"::date"
		// A scheduled job counts at its value in the month it is scheduled, read in the business time zone through
		// the month starts; an invoice without a job counts at its total in its invoice month.
		return `
    SELECT 1 AS kind, j.id, j.customer_id, j.id AS job_id, NULL::bigint AS invoice_id,
        width_bucket(j.scheduled_at, ` + bounds + `) AS month, j.invoice_value AS amount
    FROM jobs j
    WHERE j.deleted_at IS NULL AND j.status <> 'cancelled'
      AND j.scheduled_at >= (` + bounds + `)[1] AND j.scheduled_at < (` + bounds + `)[13]
    UNION ALL
    SELECT 2, i.id, i.customer_id, NULL::bigint, i.id, extract(month FROM i.document_date)::int, i.total
    FROM billing_documents i
    WHERE i.parent_type = 'invoice' AND i.job_id IS NULL AND i.status IN ('sent', 'paid')
      AND i.deleted_at IS NULL AND i.archived_at IS NULL
      AND i.document_date >= ` + from + ` AND i.document_date <= ` + to, nil

	case model.SalesBreakdownRevenue:
		amount := "p.amount"
		switch f.Measure {
		case model.SalesBreakdownCollected, "":
		case model.SalesBreakdownCollectedWithoutTax:
			amount = paymentCollectedWithoutTax
		default:
			return "", fmt.Errorf("%w: unknown measure %q", ErrInvalidQuery, f.Measure)
		}
		from, to := w.arg(f.YearFrom)+"::date", w.arg(f.YearTo)+"::date"
		method := ""
		if len(f.Methods) > 0 {
			method = "\n      AND p.method_type::text = ANY(" + w.arg(toStrings(f.Methods)) + "::text[])"
		}
		// A paid, not deleted payment for an invoice counts in its payment month. Credit payments are not revenue.
		return `
    SELECT 3 AS kind, p.id, p.customer_id, i.job_id, i.id AS invoice_id,
        extract(month FROM p.payment_date)::int AS month, ` + amount + ` AS amount
    FROM payments p
    JOIN billing_documents i ON i.id = p.invoice_id` + paymentInvoiceTax + `
    WHERE p.status = 'paid' AND p.deleted_at IS NULL
      AND p.payment_date >= ` + from + ` AND p.payment_date <= ` + to + method, nil

	default:
		return "", fmt.Errorf("%w: unknown type %q", ErrInvalidQuery, f.Type)
	}
}

// salesBreakdownCTEs builds the WITH clause shared by the list and the summary, so they always agree about which
// rows are in scope:
//
//	f: the facts of the report type
//	d: the pieces of the grouping (group_id NULL = unassigned)
//	g: the pieces added up per group and month
//	r: g with the group's name (and account number), narrowed by the group and search filters
func salesBreakdownCTEs(w *whereBuilder, f model.SalesBreakdownFilter) (string, error) {
	dim, ok := salesBreakdownDims[f.By]
	if !ok {
		return "", fmt.Errorf("%w: unknown grouping %q", ErrInvalidQuery, f.By)
	}
	facts, err := salesBreakdownFacts(w, f)
	if err != nil {
		return "", err
	}

	if len(f.GroupIDs) > 0 {
		w.add("x.id = ANY(" + w.arg(f.GroupIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Search, dim.search...)
	rWhere := ""
	if cond := takeConds(w); cond != "" {
		rWhere = "\n    WHERE " + cond
	}

	return `WITH f AS (` + facts + `
), d AS (` + salesBreakdownPieces[f.By] + `
), g AS (
    SELECT d.group_id AS id,
        ` + salesBreakdownMonthSums("d.amount", "d.month", true) + `,
        COALESCE(SUM(d.amount), 0)::numeric(14,2) AS total
    FROM d WHERE d.group_id IS NOT NULL
    GROUP BY d.group_id
), r AS (
    SELECT g.*, ` + dim.name + ` AS name, ` + dim.account + ` AS account_number
    FROM g JOIN ` + dim.table + ` x ON x.id = g.id` + rWhere + `
)
`, nil
}

// BuildSalesBreakdownListQuery builds the statement for one page of the grid. It selects limit+1 rows so the caller
// can tell whether another page follows.
func BuildSalesBreakdownListQuery(q model.SalesBreakdownQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := salesBreakdownSorts[q.Sort.Key]
	if !ok || (q.Sort.Key == model.SalesBreakdownSortAccountNumber && q.Filter.By != model.SalesBreakdownByClient) {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	ctes, err := salesBreakdownCTEs(&w, q.Filter)
	if err != nil {
		return Statement{}, err
	}
	if q.After != nil {
		applyKeyset(&w, salesBreakdownAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	months := make([]string, 12)
	for m := 1; m <= 12; m++ {
		months[m-1] = fmt.Sprintf("(r.m%02d * 100)::bigint", m)
	}
	sql := ctes + `SELECT r.id, r.name, r.account_number, ARRAY[` + strings.Join(months, ", ") + `]::bigint[],
    (r.total * 100)::bigint, (` + spec.sortText() + `)::text AS sort_value
FROM r` + w.where() + orderAndLimit(salesBreakdownAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildSalesBreakdownSummaryQuery builds the statement for the summary and the unassigned line. The summary counts
// each piece once (a fact under two tags is one amount), over the kept groups and, when no group or search filter
// narrows the report, the unassigned pieces too: it is then the report's true total. The unassigned line is NULL
// when a group or search filter is set.
func BuildSalesBreakdownSummaryQuery(f model.SalesBreakdownFilter) (Statement, error) {
	var w whereBuilder
	ctes, err := salesBreakdownCTEs(&w, f)
	if err != nil {
		return Statement{}, err
	}

	filtered := len(f.GroupIDs) > 0 || f.Search != ""
	kept := "d.group_id IN (SELECT r.id FROM r)"
	unassigned := `(SELECT ` + salesBreakdownMonthSums("d.amount", "d.month", false) + ` FROM d WHERE d.group_id IS NULL),
    (SELECT (COALESCE(SUM(d.amount), 0) * 100)::bigint FROM d WHERE d.group_id IS NULL)`
	if !filtered {
		kept += " OR d.group_id IS NULL"
	} else {
		unassigned = "NULL::bigint[], NULL::bigint"
	}

	sql := ctes + `SELECT (SELECT count(*) FROM r),
    ` + salesBreakdownMonthSums("s.amount", "s.month", false) + `,
    (COALESCE(SUM(s.amount), 0) * 100)::bigint,
    ` + unassigned + `
FROM (SELECT DISTINCT ON (d.kind, d.id, d.line_id) d.month, d.amount FROM d WHERE ` + kept + `) s`
	return Statement{SQL: sql, Args: w.args}, nil
}
