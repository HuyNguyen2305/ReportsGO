package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// documentAlias is the documents alias in the document report's queries.
const documentAlias = "d"

// createdAtText renders created_at as fixed-format UTC text, whatever the session time zone is, so a cursor
// built from it always means the same instant. It casts back with ::timestamptz.
const createdAtText = `to_char(d.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`

// documentSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the
// keyset condition comes only from this table, never from the caller.
var documentSorts = map[model.DocumentSortKey]sortSpec{
	model.DocumentSortNumber:        {expr: `d.document_number`, cast: "text"},
	model.DocumentSortType:          {expr: `lower(dt.name)`, cast: "text"},
	model.DocumentSortAccountNumber: {expr: `c.account_number`, cast: "text"},
	model.DocumentSortCustomer:      {expr: `lower(c.display_name)`, cast: "text"},
	model.DocumentSortCreatedBy:     {expr: `lower(coalesce(u.full_name, ''))`, cast: "text"},
	model.DocumentSortCreatedAt:     {expr: `d.created_at`, text: createdAtText, cast: "timestamptz"},
	model.DocumentSortStatus:        {expr: `d.status::text`, cast: "text"},
}

// documentFrom joins everything the select list, filters and sort expressions can refer to.
const documentFrom = `
FROM documents d
JOIN document_types dt ON dt.id = d.document_type_id
JOIN customers c ON c.id = d.customer_id
LEFT JOIN users u ON u.id = d.created_by`

// documentSummaryFrom is all the filter needs, without the columns only the list displays.
const documentSummaryFrom = `
FROM documents d
JOIN customers c ON c.id = d.customer_id`

// applyDocumentFilter adds the filter's conditions. List and Summary share it, so the table and its totals
// can never disagree about which documents are in scope.
func applyDocumentFilter(w *whereBuilder, f model.DocumentFilter) error {
	switch f.Lifecycle {
	case model.LifecycleActive, "":
		w.add("d.deleted_at IS NULL")
	case model.LifecycleDeleted:
		w.add("d.deleted_at IS NOT NULL")
	default:
		return fmt.Errorf("%w: unknown lifecycle %q", ErrInvalidQuery, f.Lifecycle)
	}

	if !f.CreatedFrom.IsZero() {
		w.add("d.created_at >= " + w.arg(f.CreatedFrom.UTC()) + "::timestamptz")
	}
	if !f.CreatedBefore.IsZero() {
		w.add("d.created_at < " + w.arg(f.CreatedBefore.UTC()) + "::timestamptz")
	}
	if len(f.Statuses) > 0 {
		w.add("d.status::text = ANY(" + w.arg(toStrings(f.Statuses)) + "::text[])")
	}
	if len(f.TypeIDs) > 0 {
		w.add("d.document_type_id = ANY(" + w.arg(f.TypeIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Search, "d.document_number", "c.account_number", "c.display_name")
	return nil
}

// BuildDocumentListQuery builds the statement for one page of the document report. It selects limit+1 rows so
// the caller can tell whether another page follows.
func BuildDocumentListQuery(q model.DocumentQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := documentSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	if err := applyDocumentFilter(&w, q.Filter); err != nil {
		return Statement{}, err
	}
	if q.After != nil {
		applyKeyset(&w, documentAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT d.id, d.document_number, dt.id, dt.name, c.id, c.display_name, c.account_number,
    u.full_name, d.created_at, d.status::text,
    (` + spec.sortText() + `)::text AS sort_value` +
		documentFrom + w.where() + orderAndLimit(documentAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildDocumentSummaryQuery builds the statement that counts the whole filtered set.
func BuildDocumentSummaryQuery(f model.DocumentFilter) (Statement, error) {
	var w whereBuilder
	if err := applyDocumentFilter(&w, f); err != nil {
		return Statement{}, err
	}
	return Statement{SQL: `SELECT count(*)` + documentSummaryFrom + w.where(), Args: w.args}, nil
}
