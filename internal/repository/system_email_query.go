package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// systemEmailAlias is the system_emails alias in the System Email History report's query.
const systemEmailAlias = "e"

// systemEmailSentAtText renders sent_at as fixed-format UTC text, whatever the session time zone is, so a cursor built
// from it always means the same instant. It casts back with ::timestamptz.
const systemEmailSentAtText = `to_char(e.sent_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`

// systemEmailSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the keyset
// condition comes only from this table, never from the caller. The customer sorts by the full display name, whatever
// name format is shown.
var systemEmailSorts = map[model.SystemEmailSortKey]sortSpec{
	model.SystemEmailSortTimeStamp: {expr: `e.sent_at`, text: systemEmailSentAtText, cast: "timestamptz"},
	model.SystemEmailSortCustomer:  {expr: `lower(c.display_name)`, cast: "text"},
	model.SystemEmailSortEmail:     {expr: `lower(e.email)`, cast: "text"},
	model.SystemEmailSortType:      {expr: `e.email_type::text`, cast: "text"},
	model.SystemEmailSortStatus:    {expr: `e.status::text`, cast: "text"},
}

// systemEmailNames is the SQL that shows the customer's name in each format. A customer with no first (or last) name
// shows the full display name instead. The fragments are constants chosen here, never caller input.
var systemEmailNames = map[model.NameFormat]string{
	model.NameFirst: `COALESCE(NULLIF(c.first_name, ''), c.display_name)`,
	model.NameLast:  `COALESCE(NULLIF(c.last_name, ''), c.display_name)`,
	model.NameFull:  `c.display_name`,
}

// systemEmailFrom joins what the select list, filters and sort expressions refer to.
const systemEmailFrom = `
FROM system_emails e
JOIN customers c ON c.id = e.customer_id`

// applySystemEmailFilter adds the filter's conditions.
func applySystemEmailFilter(w *whereBuilder, f model.SystemEmailFilter) {
	if !f.SentFrom.IsZero() {
		w.add("e.sent_at >= " + w.arg(f.SentFrom.UTC()) + "::timestamptz")
	}
	if !f.SentBefore.IsZero() {
		w.add("e.sent_at < " + w.arg(f.SentBefore.UTC()) + "::timestamptz")
	}
	if len(f.Statuses) > 0 {
		w.add("e.status::text = ANY(" + w.arg(toStrings(f.Statuses)) + "::text[])")
	}
	if len(f.CustomerIDs) > 0 {
		w.add("e.customer_id = ANY(" + w.arg(f.CustomerIDs) + "::bigint[])")
	}
	applySearchOn(w, f.Search, "c.display_name", "c.account_number", "e.email")
}

// BuildSystemEmailListQuery builds the statement for one page of the System Email History report. It selects limit+1
// rows so the caller can tell whether another page follows. An unset name format is the first name.
func BuildSystemEmailListQuery(q model.SystemEmailQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	spec, ok := systemEmailSorts[q.Sort.Key]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}
	format := q.NameFormat
	if format == "" {
		format = model.NameFirst
	}
	name, ok := systemEmailNames[format]
	if !ok {
		return Statement{}, fmt.Errorf("%w: unknown name format %q", ErrInvalidQuery, q.NameFormat)
	}

	var w whereBuilder
	applySystemEmailFilter(&w, q.Filter)
	if q.After != nil {
		applyKeyset(&w, systemEmailAlias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT e.id, c.id, ` + name + `, e.email, e.email_type::text, e.status::text, e.sent_at,
    (` + spec.sortText() + `)::text AS sort_value` +
		systemEmailFrom + w.where() + orderAndLimit(systemEmailAlias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}
