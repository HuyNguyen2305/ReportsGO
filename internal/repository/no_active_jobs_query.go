package repository

import (
	"fmt"

	"reportsgo/internal/model"
)

// noActiveJobsNever is the sort value of a row without a job: before every real date, so such rows come first when
// ascending. It is in the same fixed UTC form as noActiveJobsAtText.
const noActiveJobsNever = `0001-01-01T00:00:00.000000Z`

// noActiveJobsAtText renders the last service time as fixed-format UTC text, whatever the session time zone is, so
// a cursor built from it always means the same instant. A row without a job renders as noActiveJobsNever.
const noActiveJobsAtText = `CASE WHEN ls.at IS NULL THEN '` + noActiveJobsNever + `'
    ELSE to_char(ls.at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') END`

// noActiveJobsSorts is the complete list of things the report can be sorted by. SQL text for ORDER BY and the
// keyset condition comes only from this table, never from the caller. A row without a job sorts its last service
// columns as empty values (and a date before all others).
var noActiveJobsSorts = map[model.NoActiveJobsSortKey]sortSpec{
	model.NoActiveJobsSortStatus:          {expr: `c.status::text`, cast: "text"},
	model.NoActiveJobsSortAccountNumber:   {expr: `c.account_number`, cast: "text"},
	model.NoActiveJobsSortCustomer:        {expr: `lower(c.display_name)`, cast: "text"},
	model.NoActiveJobsSortAddress:         {expr: `lower(sl.address_line1)`, cast: "text"},
	model.NoActiveJobsSortCity:            {expr: `lower(sl.city)`, cast: "text"},
	model.NoActiveJobsSortZip:             {expr: `lower(sl.postal_code)`, cast: "text"},
	model.NoActiveJobsSortLastServiceType: {expr: `lower(coalesce(ls.service_type, ''))`, cast: "text"},
	model.NoActiveJobsSortJobStatus:       {expr: `coalesce(ls.job_status, '')`, cast: "text"},
	model.NoActiveJobsSortLastServiceDate: {
		expr: `coalesce(ls.at, '0001-01-01 00:00:00+00'::timestamptz)`, text: noActiveJobsAtText, cast: "timestamptz",
	},
}

// noActiveJobsGrain holds what differs between the customers and the locations report.
type noActiveJobsGrain struct {
	alias    string // the alias of the row's own table: the customer or the service location
	from     string // FROM and the join to the row's location, if any
	jobScope string // narrows a jobs alias "%s" to the customer, or to the location
	columns  string // the select list before sort_value
	search   []string
}

func grainFor(g model.NoActiveJobsGrain) (noActiveJobsGrain, error) {
	switch g {
	case model.NoActiveJobsCustomers, "":
		return noActiveJobsGrain{
			alias:    "c",
			from:     "FROM customers c",
			jobScope: "%[1]s.customer_id = c.id",
			columns: `c.id, c.id, c.status::text, c.account_number, c.display_name, ph.number,
    NULL::text, NULL::text, NULL::text, NULL::text`,
			search: []string{"c.display_name", "c.account_number", "ph.number"},
		}, nil
	case model.NoActiveJobsLocations:
		return noActiveJobsGrain{
			alias:    "sl",
			from:     "FROM service_locations sl\nJOIN customers c ON c.id = sl.customer_id",
			jobScope: "%[1]s.location_id = sl.id",
			columns: `sl.id, c.id, c.status::text, c.account_number, c.display_name, ph.number,
    sl.address_line1, sl.address_line2, sl.city, sl.postal_code`,
			search: []string{"c.display_name", "c.account_number", "ph.number", "sl.address_line1", "sl.city", "sl.postal_code"},
		}, nil
	default:
		return noActiveJobsGrain{}, fmt.Errorf("%w: unknown grain %q", ErrInvalidQuery, g)
	}
}

// noActiveJobsFrom joins everything the select list, filters and sort expressions can refer to, and keeps only
// rows that have no active job (unconfirmed or confirmed, not deleted). withLast adds the latest non-deleted job, any
// status. The phone is the customer's first one (lowest id) taken in a lateral join, so each row stays one row. The
// list and the summary share it so they always agree about which rows are in scope.
func noActiveJobsFrom(g noActiveJobsGrain, withLast bool) string {
	out := "\n" + g.from + `
LEFT JOIN LATERAL (
    SELECT p.number FROM customer_phones p WHERE p.customer_id = c.id ORDER BY p.id LIMIT 1
) ph ON true`
	if withLast {
		out += `
LEFT JOIN LATERAL (
    SELECT st.name AS service_type, j.status::text AS job_status, coalesce(j.scheduled_at, j.created_at) AS at
    FROM jobs j JOIN service_types st ON st.id = j.service_type_id
    WHERE ` + fmt.Sprintf(g.jobScope, "j") + ` AND j.deleted_at IS NULL
    ORDER BY coalesce(j.scheduled_at, j.created_at) DESC, j.id DESC LIMIT 1
) ls ON true`
	}
	return out
}

// applyNoActiveJobsFilter adds the filter's conditions. List and Summary share it.
func applyNoActiveJobsFilter(w *whereBuilder, g noActiveJobsGrain, f model.NoActiveJobsFilter) {
	w.add(`NOT EXISTS (SELECT 1 FROM jobs a WHERE ` + fmt.Sprintf(g.jobScope, "a") +
		` AND a.status IN ('unconfirmed', 'confirmed') AND a.deleted_at IS NULL)`)
	if len(f.Statuses) > 0 {
		w.add("c.status::text = ANY(" + w.arg(toStrings(f.Statuses)) + "::text[])")
	}
	applySearchOn(w, f.Search, g.search...)
}

// BuildNoActiveJobsListQuery builds the statement for one page of the report. It selects limit+1 rows so the caller
// can tell whether another page follows.
func BuildNoActiveJobsListQuery(q model.NoActiveJobsQuery) (Statement, error) {
	if q.Limit < 1 {
		return Statement{}, fmt.Errorf("%w: limit must be at least 1", ErrInvalidQuery)
	}
	g, err := grainFor(q.Grain)
	if err != nil {
		return Statement{}, err
	}
	spec, ok := noActiveJobsSorts[q.Sort.Key]
	if !ok || !validForGrain(q.Grain, q.Sort.Key) {
		return Statement{}, fmt.Errorf("%w: unknown sort key %q", ErrInvalidQuery, q.Sort.Key)
	}

	var w whereBuilder
	applyNoActiveJobsFilter(&w, g, q.Filter)
	if q.After != nil {
		applyKeyset(&w, g.alias, spec, q.Sort.Desc, q.After.SortValue, q.After.ID)
	}
	limit := w.arg(q.Limit + 1)

	sql := `SELECT ` + g.columns + `,
    ls.service_type, ls.job_status, ls.at,
    (` + spec.sortText() + `)::text AS sort_value` +
		noActiveJobsFrom(g, true) + w.where() + orderAndLimit(g.alias, spec, q.Sort.Desc, limit)
	return Statement{SQL: sql, Args: w.args}, nil
}

// BuildNoActiveJobsSummaryQuery builds the statement that counts the whole filtered set.
func BuildNoActiveJobsSummaryQuery(grain model.NoActiveJobsGrain, f model.NoActiveJobsFilter) (Statement, error) {
	g, err := grainFor(grain)
	if err != nil {
		return Statement{}, err
	}
	var w whereBuilder
	applyNoActiveJobsFilter(&w, g, f)
	return Statement{SQL: `SELECT count(*)` + noActiveJobsFrom(g, false) + w.where(), Args: w.args}, nil
}

// validForGrain says whether key exists on the grain: the address columns are the locations grain's alone.
func validForGrain(grain model.NoActiveJobsGrain, key model.NoActiveJobsSortKey) bool {
	for _, k := range model.NoActiveJobsSortKeysFor(grain) {
		if k == key {
			return true
		}
	}
	return false
}
