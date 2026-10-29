//go:build integration

package noactivejobsrepo_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

// env is a migrated, seeded throwaway database with a w/o Active Jobs repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.NoActiveJobsRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewNoActiveJobsRepository(pool)}
}

func query(grain model.NoActiveJobsGrain, f model.NoActiveJobsFilter, key model.NoActiveJobsSortKey, desc bool, limit int) model.NoActiveJobsQuery {
	return model.NoActiveJobsQuery{Grain: grain, Filter: f, Sort: model.NoActiveJobsSort{Key: key, Desc: desc}, Limit: limit}
}

func (e env) page(t *testing.T, grain model.NoActiveJobsGrain, f model.NoActiveJobsFilter) model.NoActiveJobsPage {
	t.Helper()
	page, err := e.repo.Page(context.Background(), query(grain, f, model.NoActiveJobsSortCustomer, false, 1000), true)
	require.NoError(t, err)
	return page
}

func accounts(rows []model.NoActiveJobsRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.AccountNumber
	}
	return out
}

func ids(rows []model.NoActiveJobsRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func (e env) row(t *testing.T, grain model.NoActiveJobsGrain, account string) model.NoActiveJobsRow {
	t.Helper()
	for _, r := range e.page(t, grain, model.NoActiveJobsFilter{}).Rows {
		if r.AccountNumber == account {
			return r
		}
	}
	t.Fatalf("account %s is not listed on the %s report", account, grain)
	return model.NoActiveJobsRow{}
}

func (e env) listed(t *testing.T, grain model.NoActiveJobsGrain, account string) bool {
	t.Helper()
	for _, r := range e.page(t, grain, model.NoActiveJobsFilter{}).Rows {
		if r.AccountNumber == account {
			return true
		}
	}
	return false
}

// addJob inserts a job of the account's location at address line1 (or its only location when line1 is empty).
// scheduled and created may be empty: no schedule, and created now.
func (e env) addJob(t *testing.T, account, line1, status, scheduled, created string, deleted bool) {
	t.Helper()
	_, err := e.pool.Exec(context.Background(), `INSERT INTO jobs
			(customer_id, location_id, service_type_id, status, scheduled_at, deleted_at, created_at)
		SELECT c.id, l.id, (SELECT min(id) FROM service_types), $2::job_status, NULLIF($3, '')::timestamptz,
		       CASE WHEN $4 THEN now() END, COALESCE(NULLIF($5, '')::timestamptz, now())
		FROM customers c JOIN service_locations l ON l.customer_id = c.id
		WHERE c.account_number = $1 AND ($6 = '' OR l.address_line1 = $6)
		ORDER BY l.id LIMIT 1`, account, status, scheduled, deleted, created, line1)
	require.NoError(t, err)
}

func (e env) addLocation(t *testing.T, account, line1, city, zip string) {
	t.Helper()
	_, err := e.pool.Exec(context.Background(), `INSERT INTO service_locations (customer_id, address_line1, city, postal_code)
		SELECT id, $2, $3, $4 FROM customers WHERE account_number = $1`, account, line1, city, zip)
	require.NoError(t, err)
}

func TestPageSeedCustomersWithoutActiveJobs(t *testing.T) {
	e := newEnv(t)

	page := e.page(t, model.NoActiveJobsCustomers, model.NoActiveJobsFilter{})

	// no job at all, or jobs that are all finished (5001) - sorted by name
	require.Equal(t, []string{
		"63716", "70001", "5002", "70003", "70002", "70005", "70008", "63730", "70004", "70006", "70007", "5001",
	}, accounts(page.Rows))
	require.Equal(t, model.NoActiveJobsSummary{Count: 12}, *page.Summary)

	t.Run("it agrees with an independent query", func(t *testing.T) {
		var n int
		require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT count(*) FROM customers c
			WHERE c.id NOT IN (SELECT customer_id FROM jobs WHERE deleted_at IS NULL AND status IN ('unconfirmed', 'confirmed'))`).Scan(&n))
		require.Equal(t, n, len(page.Rows))
	})

	t.Run("a customer with only finished jobs shows its last one", func(t *testing.T) {
		r := e.row(t, model.NoActiveJobsCustomers, "5001")
		require.NotNil(t, r.LastService)
		require.Equal(t, "Bi-Monthly Service", r.LastService.ServiceType)
		require.Equal(t, "completed", r.LastService.JobStatus)
		require.Equal(t, time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC), r.LastService.At.UTC())
		require.Nil(t, r.Address, "the customers report has no address")
	})

	t.Run("a customer without jobs has no last service", func(t *testing.T) {
		r := e.row(t, model.NoActiveJobsCustomers, "5002")
		require.Nil(t, r.LastService)
		require.Equal(t, model.CustomerStatusActive, r.Status)
		require.NotNil(t, r.Phone)
		require.Equal(t, "234325235234", *r.Phone)
	})

	t.Run("customers with an open job are left out", func(t *testing.T) {
		for _, acct := range []string{"63706", "63701", "63689", "63714", "63717", "63702"} {
			require.False(t, e.listed(t, model.NoActiveJobsCustomers, acct), acct)
		}
	})
}

func TestPageSeedLocationsWithoutActiveJobs(t *testing.T) {
	e := newEnv(t)

	page := e.page(t, model.NoActiveJobsLocations, model.NoActiveJobsFilter{})

	require.Equal(t, []string{"63716", "5002", "63730", "5001"}, accounts(page.Rows))
	require.Equal(t, model.NoActiveJobsSummary{Count: 4}, *page.Summary)
	r := e.row(t, model.NoActiveJobsLocations, "5002")
	require.Equal(t, &model.ServiceAddress{Line1: "12 Alaba Street", City: "Austin", Zip: "78701"}, r.Address)
	require.Equal(t, r.CustomerID, e.row(t, model.NoActiveJobsCustomers, "5002").ID, "customer_id is the customer's id")
}

func TestPageWhatMakesAJobActive(t *testing.T) {
	e := newEnv(t)

	// jobs that are not active, whatever their date: deleted, cancelled, completed
	e.addJob(t, "5002", "", "confirmed", "2026-11-01 08:00+00", "", true)
	e.addJob(t, "5002", "", "cancelled", "2026-11-02 08:00+00", "", false)
	e.addJob(t, "5002", "", "completed", "2026-11-03 08:00+00", "", false)
	require.True(t, e.listed(t, model.NoActiveJobsCustomers, "5002"))
	require.True(t, e.listed(t, model.NoActiveJobsLocations, "5002"))

	t.Run("an unconfirmed job makes the customer and the location active", func(t *testing.T) {
		e.addJob(t, "5002", "", "unconfirmed", "2026-11-04 08:00+00", "", false)

		require.False(t, e.listed(t, model.NoActiveJobsCustomers, "5002"))
		require.False(t, e.listed(t, model.NoActiveJobsLocations, "5002"))
	})
}

func TestPageAnOpenJobAtOneLocationLeavesTheOtherLocationListed(t *testing.T) {
	e := newEnv(t)
	e.addLocation(t, "63689", "7 Second Street", "Dallas", "75001") // 63689 has open jobs at its first location

	require.False(t, e.listed(t, model.NoActiveJobsCustomers, "63689"), "the customer has an open job")
	locations := e.page(t, model.NoActiveJobsLocations, model.NoActiveJobsFilter{})
	var second *model.NoActiveJobsRow
	for i, r := range locations.Rows {
		if r.AccountNumber == "63689" {
			second = &locations.Rows[i]
		}
	}
	require.NotNil(t, second, "the second location has no job of its own")
	require.Equal(t, "7 Second Street", second.Address.Line1)
	require.Nil(t, second.LastService)
	require.Equal(t, 1, countAccount(locations.Rows, "63689"), "the first location has open jobs and is left out")

	// an open job at the second location takes it off the list
	e.addJob(t, "63689", "7 Second Street", "confirmed", "", "", false)
	require.False(t, e.listed(t, model.NoActiveJobsLocations, "63689"))
}

func countAccount(rows []model.NoActiveJobsRow, account string) int {
	n := 0
	for _, r := range rows {
		if r.AccountNumber == account {
			n++
		}
	}
	return n
}

func TestPageLastServiceRules(t *testing.T) {
	e := newEnv(t)

	t.Run("the latest non-deleted job counts, deleted and older ones do not", func(t *testing.T) {
		e.addJob(t, "5001", "", "completed", "2026-12-01 08:00+00", "", true) // newer but deleted
		e.addJob(t, "5001", "", "completed", "2026-09-01 08:00+00", "", false)

		r := e.row(t, model.NoActiveJobsCustomers, "5001")
		require.Equal(t, time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC), r.LastService.At.UTC())
	})

	t.Run("a cancelled job counts, and an unscheduled one is dated by its creation", func(t *testing.T) {
		e.addJob(t, "5002", "", "cancelled", "", "2026-08-01 10:00+00", false)

		r := e.row(t, model.NoActiveJobsCustomers, "5002")
		require.NotNil(t, r.LastService)
		require.Equal(t, "cancelled", r.LastService.JobStatus)
		require.Equal(t, time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), r.LastService.At.UTC())
	})

	t.Run("the location report looks at the location's own jobs only", func(t *testing.T) {
		e.addLocation(t, "5001", "2 Other Road", "Austin", "78701")

		other := findAddress(t, e.page(t, model.NoActiveJobsLocations, model.NoActiveJobsFilter{}).Rows, "2 Other Road")
		require.Nil(t, other.LastService, "the customer's job was at its first location")
		first := findAddress(t, e.page(t, model.NoActiveJobsLocations, model.NoActiveJobsFilter{}).Rows, "West Loop 1604 South")
		require.NotNil(t, first.LastService)
	})
}

func findAddress(t *testing.T, rows []model.NoActiveJobsRow, line1 string) model.NoActiveJobsRow {
	t.Helper()
	for _, r := range rows {
		if r.Address != nil && r.Address.Line1 == line1 {
			return r
		}
	}
	t.Fatalf("no row at %s", line1)
	return model.NoActiveJobsRow{}
}

func TestPageStatusFilter(t *testing.T) {
	e := newEnv(t)

	for _, grain := range []model.NoActiveJobsGrain{model.NoActiveJobsCustomers, model.NoActiveJobsLocations} {
		t.Run(string(grain), func(t *testing.T) {
			inactive := e.page(t, grain, model.NoActiveJobsFilter{Statuses: []model.CustomerStatus{model.CustomerStatusInactive}})
			require.Equal(t, []string{"63716"}, accounts(inactive.Rows))
			require.Equal(t, int64(1), inactive.Summary.Count)

			two := e.page(t, grain, model.NoActiveJobsFilter{Statuses: []model.CustomerStatus{model.CustomerStatusInactive, model.CustomerStatusLead}})
			require.ElementsMatch(t, []string{"63716", "63730"}, accounts(two.Rows))

			archived := e.page(t, grain, model.NoActiveJobsFilter{Statuses: []model.CustomerStatus{model.CustomerStatusArchived}})
			require.Empty(t, archived.Rows, "the only archived customer has an open job")
			require.Equal(t, int64(0), archived.Summary.Count)
		})
	}
}

func TestPageSearch(t *testing.T) {
	e := newEnv(t)

	tests := []struct {
		name   string
		grain  model.NoActiveJobsGrain
		search string
		want   []string
	}{
		{"name", model.NoActiveJobsCustomers, "LOVELACE", []string{"70001"}},
		{"account number", model.NoActiveJobsCustomers, "7000", []string{"70001", "70003", "70002", "70005", "70008", "70004", "70006", "70007"}},
		{"phone", model.NoActiveJobsCustomers, "555-0606", []string{"70006"}},
		{"customers do not search addresses", model.NoActiveJobsCustomers, "alaba street", nil},
		{"location address", model.NoActiveJobsLocations, "alaba street", []string{"5002"}},
		{"location zip", model.NoActiveJobsLocations, "73301", []string{"63730"}},
		{"location city", model.NoActiveJobsLocations, "bridgeport", []string{"63716"}},
		{"wildcards are literal", model.NoActiveJobsCustomers, "%", []string{"70008"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := e.page(t, tt.grain, model.NoActiveJobsFilter{Search: tt.search})

			require.ElementsMatch(t, tt.want, accounts(page.Rows))
			require.Equal(t, int64(len(tt.want)), page.Summary.Count)
		})
	}
}

func TestPageNoJobRowsSortFirstAscendingByLastServiceDate(t *testing.T) {
	e := newEnv(t)

	asc, err := e.repo.Page(context.Background(), query(model.NoActiveJobsCustomers, model.NoActiveJobsFilter{}, model.NoActiveJobsSortLastServiceDate, false, 100), false)
	require.NoError(t, err)
	desc, err := e.repo.Page(context.Background(), query(model.NoActiveJobsCustomers, model.NoActiveJobsFilter{}, model.NoActiveJobsSortLastServiceDate, true, 100), false)
	require.NoError(t, err)

	require.Equal(t, "5001", asc.Rows[len(asc.Rows)-1].AccountNumber, "the only customer with a job is last")
	require.Equal(t, "5001", desc.Rows[0].AccountNumber)
	require.Equal(t, "0001-01-01T00:00:00.000000Z", asc.Rows[0].SortValue)
}

func TestPageKeysetPagingReproducesTheUnpagedOrderForEverySort(t *testing.T) {
	e := newEnv(t)
	e.addLocation(t, "5002", "9 Another Lane", "Austin", "78701")
	e.addJob(t, "5002", "9 Another Lane", "completed", "2026-09-09 09:00+00", "", false)

	for _, grain := range []model.NoActiveJobsGrain{model.NoActiveJobsCustomers, model.NoActiveJobsLocations} {
		for _, key := range model.NoActiveJobsSortKeysFor(grain) {
			for _, desc := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s %s desc=%v", grain, key, desc), func(t *testing.T) {
					f := model.NoActiveJobsFilter{}
					all, err := e.repo.Page(context.Background(), query(grain, f, key, desc, 1000), false)
					require.NoError(t, err)
					require.GreaterOrEqual(t, len(all.Rows), 5)

					var got []int64
					var after *model.NoActiveJobsCursor
					for range len(all.Rows) + 2 {
						q := query(grain, f, key, desc, 3)
						q.After = after
						page, err := e.repo.Page(context.Background(), q, false)
						require.NoError(t, err)
						rows := page.Rows
						more := len(rows) > 3
						if more {
							rows = rows[:3]
						}
						got = append(got, ids(rows)...)
						if !more {
							break
						}
						last := rows[len(rows)-1]
						after = &model.NoActiveJobsCursor{SortValue: last.SortValue, ID: last.ID}
					}

					require.Equal(t, ids(all.Rows), got)
				})
			}
		}
	}
}

func TestPageSummaryEqualsTheUnpagedLength(t *testing.T) {
	e := newEnv(t)

	for _, grain := range []model.NoActiveJobsGrain{model.NoActiveJobsCustomers, model.NoActiveJobsLocations} {
		for _, f := range []model.NoActiveJobsFilter{
			{},
			{Statuses: []model.CustomerStatus{model.CustomerStatusActive}},
			{Search: "a"},
		} {
			page := e.page(t, grain, f)
			require.Equal(t, int64(len(page.Rows)), page.Summary.Count, "%s %+v", grain, f)
		}
	}
}

func TestPageRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)

	tests := []struct {
		name string
		q    model.NoActiveJobsQuery
	}{
		{"unknown sort key", query(model.NoActiveJobsCustomers, model.NoActiveJobsFilter{}, "name; DROP TABLE customers", false, 10)},
		{"empty sort key", model.NoActiveJobsQuery{Grain: model.NoActiveJobsCustomers, Limit: 5}},
		{"a locations key on the customers report", query(model.NoActiveJobsCustomers, model.NoActiveJobsFilter{}, model.NoActiveJobsSortCity, false, 10)},
		{"unknown grain", query("addresses", model.NoActiveJobsFilter{}, model.NoActiveJobsSortCustomer, false, 10)},
		{"no limit", query(model.NoActiveJobsCustomers, model.NoActiveJobsFilter{}, model.NoActiveJobsSortCustomer, false, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.repo.Page(context.Background(), tt.q, true)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}
