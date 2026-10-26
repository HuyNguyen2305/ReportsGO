package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// estimateAlias is the billing_documents alias in the estimate report's queries.
const estimateAlias = "e"

// estimateSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the
// keyset condition comes only from this table, never from the caller.
var estimateSorts = map[model.EstimateSortKey]sortSpec{
	model.EstimateSortAccountNumber:   {expr: `c.account_number`, cast: "text"},
	model.EstimateSortNumber:          {expr: `e.document_number`, cast: "text"},
	model.EstimateSortCustomer:        {expr: `lower(c.display_name)`, cast: "text"},
	model.EstimateSortPhone:           {expr: `coalesce(ph.number, '')`, cast: "text"},
	model.EstimateSortBillingEmail:    {expr: `lower(coalesce(e.billing_email, ''))`, cast: "text"},
	model.EstimateSortCreatedBy:       {expr: `lower(coalesce(u.full_name, ''))`, cast: "text"},
	model.EstimateSortDate:            {expr: `e.document_date`, cast: "date"},
	model.EstimateSortTotal:           {expr: `e.total`, cast: "numeric"},
	model.EstimateSortStatus:          {expr: `e.status::text`, cast: "text"},
	model.EstimateSortSignatureStatus: {expr: `e.signature_status::text`, cast: "text"},
}

// estimateFrom joins everything the select list, filters and sort expressions can refer to. The phone is
// the customer's first one (lowest id) taken in a lateral join, so each estimate stays one row.
const estimateFrom = `
FROM billing_documents e
JOIN customers c ON c.id = e.customer_id
LEFT JOIN users u ON u.id = e.created_by
LEFT JOIN LATERAL (
    SELECT p.number FROM customer_phones p WHERE p.customer_id = c.id ORDER BY p.id LIMIT 1
) ph ON true`

// estimateSummaryFrom is all the filter needs, without the columns only the list displays.
const estimateSummaryFrom = `
FROM billing_documents e
JOIN customers c ON c.id = e.customer_id`

// applyEstimateFilter adds the filter's conditions. List and Summary share it, so the table and its totals
// can never disagree about which estimates are in scope.
func applyEstimateFilter(w *whereBuilder, f model.EstimateFilter) error {
	if err := applyCommonFilter(w, estimateAlias, commonFilter{
		parent:          parentEstimate,
		dateFrom:        f.DateFrom,
		dateTo:          f.DateTo,
		lifecycle:       f.Lifecycle,
		statuses:        toStrings(f.Statuses),
		signatureStatus: f.SignatureStatus,
	}); err != nil {
		return err
	}
	applySearch(w, estimateAlias, f.Search)
	return nil
}

// BuildEstimateListQuery builds the statement for one page of the estimate report. It selects limit+1 rows so
// the caller can tell whether another page follows.
func BuildEstimateListQuery(q model.EstimateQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := estimateSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	if err := applyEstimateFilter(&w, q.Filter); err != nil {
		return Statement{}, err
	}
	if q.After != nil {
		applyKeyset(&w, estimateAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT e.id, e.document_number, c.account_number, c.id, c.display_name,
    ph.number, e.billing_email, u.full_name, e.document_date,
    (e.total * 100)::bigint, e.status::text, e.signature_status::text,
    (` + spec.sortText() + `)::text AS sort_value` +
		estimateFrom + w.where() + orderAndLimit(estimateAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildEstimateSummaryQuery builds the statement that totals the whole filtered set.
func BuildEstimateSummaryQuery(f model.EstimateFilter) (Statement, error) {
	var w whereBuilder
	if err := applyEstimateFilter(&w, f); err != nil {
		return Statement{}, err
	}

	sql := `SELECT count(*), COALESCE(SUM(e.total * 100), 0)::bigint` + estimateSummaryFrom + w.where()
	return Statement{SQL: sql, Args: w.args}, nil
}
