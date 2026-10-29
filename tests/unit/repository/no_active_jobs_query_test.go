package repository_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func noActiveJobsQuery(grain model.NoActiveJobsGrain, f model.NoActiveJobsFilter, key model.NoActiveJobsSortKey, desc bool) model.NoActiveJobsQuery {
	return model.NoActiveJobsQuery{Grain: grain, Filter: f, Sort: model.NoActiveJobsSort{Key: key, Desc: desc}, Limit: 50}
}

func TestBuildNoActiveJobsListQueryCustomersGrain(t *testing.T) {
	f := model.NoActiveJobsFilter{Statuses: []model.CustomerStatus{model.CustomerStatusLead, model.CustomerStatusInactive}, Search: "saka"}

	stmt, err := repository.BuildNoActiveJobsListQuery(noActiveJobsQuery(model.NoActiveJobsCustomers, f, model.NoActiveJobsSortCustomer, false))
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM customers c\nLEFT JOIN LATERAL (")
	require.NotContains(t, stmt.SQL, "service_locations", "the customers report has no location")
	require.Contains(t, stmt.SQL, "NOT EXISTS (SELECT 1 FROM jobs a WHERE a.customer_id = c.id AND a.status IN ('unconfirmed', 'confirmed') AND a.deleted_at IS NULL)")
	require.Contains(t, stmt.SQL, "WHERE j.customer_id = c.id AND j.deleted_at IS NULL\n    ORDER BY coalesce(j.scheduled_at, j.created_at) DESC, j.id DESC LIMIT 1")
	require.Contains(t, stmt.SQL, "c.status::text = ANY($1::text[])")
	require.Contains(t, stmt.SQL, "(c.display_name ILIKE $2 ESCAPE '\\' OR c.account_number ILIKE $2 ESCAPE '\\' OR ph.number ILIKE $2 ESCAPE '\\')")
	require.True(t, strings.HasSuffix(stmt.SQL, "ORDER BY left(lower(c.display_name), 200) ASC, c.id ASC\nLIMIT $3"), stmt.SQL)
	require.Equal(t, []any{[]string{"lead", "inactive"}, "%saka%", 51}, stmt.Args)
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildNoActiveJobsListQueryLocationsGrain(t *testing.T) {
	stmt, err := repository.BuildNoActiveJobsListQuery(noActiveJobsQuery(model.NoActiveJobsLocations,
		model.NoActiveJobsFilter{Search: "elm"}, model.NoActiveJobsSortCity, true))
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM service_locations sl\nJOIN customers c ON c.id = sl.customer_id")
	require.Contains(t, stmt.SQL, "NOT EXISTS (SELECT 1 FROM jobs a WHERE a.location_id = sl.id AND")
	require.Contains(t, stmt.SQL, "WHERE j.location_id = sl.id AND j.deleted_at IS NULL", "the location's own jobs")
	require.Contains(t, stmt.SQL, "sl.address_line1 ILIKE $1 ESCAPE '\\'")
	require.Contains(t, stmt.SQL, "sl.postal_code ILIKE $1 ESCAPE '\\'")
	require.True(t, strings.HasSuffix(stmt.SQL, "ORDER BY left(lower(sl.city), 200) DESC, sl.id DESC\nLIMIT $2"), stmt.SQL)
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildNoActiveJobsListQueryDefaultGrainIsCustomers(t *testing.T) {
	stmt, err := repository.BuildNoActiveJobsListQuery(noActiveJobsQuery("", model.NoActiveJobsFilter{}, model.NoActiveJobsSortCustomer, false))

	require.NoError(t, err)
	require.Contains(t, stmt.SQL, "FROM customers c")
}

func TestBuildNoActiveJobsListQueryKeyset(t *testing.T) {
	q := noActiveJobsQuery(model.NoActiveJobsCustomers, model.NoActiveJobsFilter{}, model.NoActiveJobsSortLastServiceDate, false)
	q.After = &model.NoActiveJobsCursor{SortValue: "2026-10-15T08:00:00.000000Z", ID: 11}

	stmt, err := repository.BuildNoActiveJobsListQuery(q)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "(coalesce(ls.at, '0001-01-01 00:00:00+00'::timestamptz), c.id) > (($1::text)::timestamptz, $2::bigint)")
	require.Contains(t, stmt.SQL, "THEN '0001-01-01T00:00:00.000000Z'", "a customer without a job has a fixed sort value")
	require.Equal(t, []any{"2026-10-15T08:00:00.000000Z", int64(11), 51}, stmt.Args)
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildNoActiveJobsListQueryEverySortKeyAndDirectionPerGrain(t *testing.T) {
	wantExpr := map[model.NoActiveJobsSortKey]string{
		model.NoActiveJobsSortStatus:          "left(c.status::text, 200)",
		model.NoActiveJobsSortAccountNumber:   "left(c.account_number, 200)",
		model.NoActiveJobsSortCustomer:        "left(lower(c.display_name), 200)",
		model.NoActiveJobsSortAddress:         "left(lower(sl.address_line1), 200)",
		model.NoActiveJobsSortCity:            "left(lower(sl.city), 200)",
		model.NoActiveJobsSortZip:             "left(lower(sl.postal_code), 200)",
		model.NoActiveJobsSortLastServiceType: "left(lower(coalesce(ls.service_type, '')), 200)",
		model.NoActiveJobsSortJobStatus:       "left(coalesce(ls.job_status, ''), 200)",
		model.NoActiveJobsSortLastServiceDate: "coalesce(ls.at, '0001-01-01 00:00:00+00'::timestamptz)",
	}
	require.Len(t, wantExpr, len(model.NoActiveJobsLocationSortKeys), "every key must be covered by this test")

	for _, grain := range []model.NoActiveJobsGrain{model.NoActiveJobsCustomers, model.NoActiveJobsLocations} {
		alias := "c"
		if grain == model.NoActiveJobsLocations {
			alias = "sl"
		}
		for _, key := range model.NoActiveJobsSortKeysFor(grain) {
			for _, desc := range []bool{false, true} {
				dir := "ASC"
				if desc {
					dir = "DESC"
				}
				t.Run(string(grain)+" "+string(key)+" "+dir, func(t *testing.T) {
					stmt, err := repository.BuildNoActiveJobsListQuery(noActiveJobsQuery(grain, model.NoActiveJobsFilter{}, key, desc))
					require.NoError(t, err)
					require.True(t, strings.HasSuffix(stmt.SQL, "ORDER BY "+wantExpr[key]+" "+dir+", "+alias+".id "+dir+"\nLIMIT $1"), stmt.SQL)
				})
			}
		}
	}
}

func TestBuildNoActiveJobsSummaryQuery(t *testing.T) {
	f := model.NoActiveJobsFilter{Statuses: []model.CustomerStatus{model.CustomerStatusActive}, Search: "saka"}

	for _, grain := range []model.NoActiveJobsGrain{model.NoActiveJobsCustomers, model.NoActiveJobsLocations} {
		t.Run(string(grain), func(t *testing.T) {
			stmt, err := repository.BuildNoActiveJobsSummaryQuery(grain, f)
			require.NoError(t, err)

			require.True(t, strings.HasPrefix(stmt.SQL, "SELECT count(*)\n"), stmt.SQL)
			require.Contains(t, stmt.SQL, "NOT EXISTS (SELECT 1 FROM jobs a WHERE", "the same rows as the list")
			require.Contains(t, stmt.SQL, "c.status::text = ANY($1::text[])")
			require.Contains(t, stmt.SQL, "ph.number ILIKE $2", "the phone is still joined, the search needs it")
			require.NotContains(t, stmt.SQL, "ls.", "the summary needs no last service")
			require.NotContains(t, stmt.SQL, "LIMIT $")
			requirePlaceholdersMatchArgs(t, stmt)
		})
	}
}

func TestBuildNoActiveJobsQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		q    model.NoActiveJobsQuery
	}{
		{"unknown sort key", noActiveJobsQuery(model.NoActiveJobsCustomers, model.NoActiveJobsFilter{}, "name; DROP TABLE customers", false)},
		{"empty sort key", model.NoActiveJobsQuery{Grain: model.NoActiveJobsCustomers, Limit: 5}},
		{"another report's sort key", noActiveJobsQuery(model.NoActiveJobsCustomers, model.NoActiveJobsFilter{}, "amount", false)},
		{"a locations key on the customers report", noActiveJobsQuery(model.NoActiveJobsCustomers, model.NoActiveJobsFilter{}, model.NoActiveJobsSortZip, false)},
		{"unknown grain", noActiveJobsQuery("addresses", model.NoActiveJobsFilter{}, model.NoActiveJobsSortCustomer, false)},
		{"no limit", model.NoActiveJobsQuery{Grain: model.NoActiveJobsCustomers, Sort: model.NoActiveJobsSort{Key: model.NoActiveJobsSortCustomer}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repository.BuildNoActiveJobsListQuery(tt.q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}

	t.Run("the summary rejects an unknown grain", func(t *testing.T) {
		_, err := repository.BuildNoActiveJobsSummaryQuery("addresses", model.NoActiveJobsFilter{})
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestBuildNoActiveJobsQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE customers; --`
	f := model.NoActiveJobsFilter{Search: hostile, Statuses: []model.CustomerStatus{model.CustomerStatus(hostile)}}

	stmt, err := repository.BuildNoActiveJobsListQuery(noActiveJobsQuery(model.NoActiveJobsLocations, f, model.NoActiveJobsSortCustomer, false))
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(toString(a)) + "|"
	}
	require.Contains(t, flat, "drop table customers", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}
