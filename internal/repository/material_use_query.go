package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// materialUseAlias is the alias of the grouped rows in the Material Use report's outer query.
const materialUseAlias = "a"

// materialUseSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller. A material without an EPA number sorts as an empty
// one. The expressions read the grouped rows (alias a), which carry the totals.
var materialUseSorts = map[model.MaterialUseSortKey]sortSpec{
	model.MaterialUseSortMaterial:     {expr: `lower(a.name)`, cast: "text"},
	model.MaterialUseSortEPA:          {expr: `lower(coalesce(a.epa_reg_no, ''))`, cast: "text"},
	model.MaterialUseSortQty:          {expr: `a.qty`, cast: "numeric"},
	model.MaterialUseSortApplications: {expr: `a.applications`, cast: "bigint"},
}

// applyMaterialUseFilter adds the filter's conditions to the inner query, which joins job_materials (jm), jobs (j)
// and materials (m). Materials added to a deleted job are never part of the report.
func applyMaterialUseFilter(w *whereBuilder, f model.MaterialUseFilter) error {
	w.add("j.deleted_at IS NULL")
	if !f.ScheduledFrom.IsZero() {
		w.add("j.scheduled_at >= " + w.arg(f.ScheduledFrom.UTC()) + "::timestamptz")
	}
	if !f.ScheduledBefore.IsZero() {
		w.add("j.scheduled_at < " + w.arg(f.ScheduledBefore.UTC()) + "::timestamptz")
	}
	switch f.Tab {
	case model.MaterialUseAll, "":
	case model.MaterialUseJobs:
		w.add("jm.device IS NULL")
	case model.MaterialUseDevices:
		w.add("jm.device IS NOT NULL")
	default:
		return fmt.Errorf("%w: unknown tab %q", ErrInvalidQuery, f.Tab)
	}
	if len(f.CustomerIDs) > 0 {
		w.add("j.customer_id = ANY(" + w.arg(f.CustomerIDs) + "::bigint[])")
	}
	if len(f.MaterialIDs) > 0 {
		w.add("jm.material_id = ANY(" + w.arg(f.MaterialIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Search, "m.name", "m.epa_reg_no")
	return nil
}

// BuildMaterialUseListQuery builds the statement for one page of the Material Use report. The materials are
// grouped in an inner query so the keyset condition and the ordering can use the totals; it selects limit+1 rows
// so the caller can tell whether another page follows.
func BuildMaterialUseListQuery(q model.MaterialUseQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := materialUseSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var inner whereBuilder
	if err := applyMaterialUseFilter(&inner, q.Filter); err != nil {
		return Statement{}, err
	}
	// The outer conditions keep numbering the bind arguments where the inner ones stopped.
	outer := whereBuilder{args: inner.args}
	if q.After != nil {
		applyKeyset(&outer, materialUseAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := outer.arg(q.Limit + 1)

	sql := `SELECT a.id, a.name, a.epa_reg_no, a.qty::text, a.applications,
    (` + spec.sortText() + `)::text AS sort_value
FROM (
    SELECT m.id, m.name, m.epa_reg_no, SUM(jm.qty) AS qty, COUNT(*) AS applications
    FROM job_materials jm
    JOIN jobs j ON j.id = jm.job_id
    JOIN materials m ON m.id = jm.material_id` + inner.where() + `
    GROUP BY m.id, m.name, m.epa_reg_no
) a` + outer.where() + orderAndLimit(materialUseAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: outer.args}, nil
}
