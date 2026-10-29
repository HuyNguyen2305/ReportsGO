//go:build integration

package servicelookuprepo_test

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

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// env is a migrated, seeded throwaway database with a Service Lookup repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.ServiceLookupRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewServiceLookupRepository(pool)}
}

func query(f model.ServiceLookupFilter, key model.ServiceLookupSortKey, desc bool, limit int) model.ServiceLookupQuery {
	return model.ServiceLookupQuery{Filter: f, Sort: model.ServiceLookupSort{Key: key, Desc: desc}, Limit: limit}
}

func (e env) page(t *testing.T, f model.ServiceLookupFilter) model.ServiceLookupPage {
	t.Helper()
	page, err := e.repo.Page(context.Background(), query(f, model.ServiceLookupSortScheduledAt, false, 1000), true)
	require.NoError(t, err)
	return page
}

func ids(rows []model.ServiceLookupRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func tab(t model.ServiceLookupTab) model.ServiceLookupFilter {
	return model.ServiceLookupFilter{Tab: t}
}

// scalar runs a query that returns one value.
func scalar[T any](t *testing.T, e env, sql string, args ...any) T {
	t.Helper()
	var v T
	require.NoError(t, e.pool.QueryRow(context.Background(), sql, args...).Scan(&v))
	return v
}

// job describes a job to insert for customer 5001's only location.
type job struct {
	status    string
	scheduled string // empty: unscheduled
	pool      bool
	deleted   bool
	value     float64
	tax       float64
}

func (e env) addJob(t *testing.T, j job) int64 {
	t.Helper()
	return scalar[int64](t, e, `INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, in_work_pool,
			deleted_at, invoice_value, tax_amount)
		SELECT c.id, l.id, (SELECT min(id) FROM service_types), $1::job_status, NULLIF($2, '')::timestamptz, $3,
		       CASE WHEN $4 THEN now() END, $5, $6
		FROM customers c JOIN service_locations l ON l.customer_id = c.id WHERE c.account_number = '5001'
		RETURNING id`, j.status, j.scheduled, j.pool, j.deleted, j.value, j.tax)
}

func (e env) tag(t *testing.T, jobID int64, names ...string) {
	t.Helper()
	for _, n := range names {
		_, err := e.pool.Exec(context.Background(), `INSERT INTO job_tag_links (job_id, tag_id)
			SELECT $1, id FROM job_tags WHERE name = $2`, jobID, n)
		require.NoError(t, err)
	}
}

func TestPageSeedTabsAgreeWithIndependentSQL(t *testing.T) {
	e := newEnv(t)
	conds := map[model.ServiceLookupTab]string{
		model.ServiceLookupActive:   "deleted_at IS NULL AND NOT in_work_pool",
		model.ServiceLookupWorkPool: "in_work_pool AND deleted_at IS NULL",
		model.ServiceLookupDeleted:  "deleted_at IS NOT NULL",
	}

	for _, tb := range model.ServiceLookupTabs {
		t.Run(string(tb), func(t *testing.T) {
			page := e.page(t, tab(tb))

			count := scalar[int64](t, e, "SELECT count(*) FROM jobs WHERE "+conds[tb])
			value := scalar[int64](t, e, "SELECT COALESCE(sum(invoice_value * 100), 0)::bigint FROM jobs WHERE "+conds[tb])
			without := scalar[int64](t, e, "SELECT COALESCE(sum((invoice_value - tax_amount) * 100), 0)::bigint FROM jobs WHERE "+conds[tb])
			require.Equal(t, count, int64(len(page.Rows)))
			require.Equal(t, count, page.Summary.Count)
			require.Equal(t, model.Money(value), page.Summary.InvoiceValue)
			require.Equal(t, model.Money(without), page.Summary.WithoutTax)
		})
	}

	page := e.page(t, tab(model.ServiceLookupActive))
	require.Equal(t, model.ServiceLookupTabCounts{Active: 33, WorkPool: 3, Deleted: 1}, page.Summary.Tabs)
	require.Equal(t, model.Money(161126), page.Summary.InvoiceValue)
	require.Equal(t, model.Money(158260), page.Summary.WithoutTax)
}

func TestPageSeedRowContents(t *testing.T) {
	e := newEnv(t)

	rows := e.page(t, tab(model.ServiceLookupActive)).Rows
	var callBack, fullService *model.ServiceLookupRow
	for i, r := range rows {
		switch {
		case r.ServiceType == "Call Back Service" && r.CustomerName == "AAA1 ccc1":
			callBack = &rows[i]
		case r.ServiceType == "Full service" && r.CustomerName == "AAA1 ccc1":
			fullService = &rows[i]
		}
	}

	require.NotNil(t, callBack)
	require.Equal(t, model.Money(1033), callBack.InvoiceValue)
	require.Equal(t, model.Money(1030), callBack.WithoutTax, "10.33 less 0.03 tax")
	require.Equal(t, model.JobConfirmed, callBack.Status)
	require.Equal(t, "Miami-Dade County", callBack.Address.County)
	require.Equal(t, "Miami", callBack.Address.City)
	require.Equal(t, "FL", callBack.Address.State)
	require.Equal(t, "33139", callBack.Address.Zip)
	require.NotNil(t, callBack.Phone)
	require.Equal(t, "(098) 765-4321", *callBack.Phone)
	require.NotNil(t, callBack.ScheduledAt)
	require.Len(t, callBack.Tags, 1)
	require.NotNil(t, fullService)
	require.Equal(t, model.Money(6500), fullService.WithoutTax, "79.30 less 14.30 tax")

	var unassigned int64
	for _, r := range rows {
		if r.AssignedTo == nil {
			unassigned++
		}
	}
	require.Equal(t, scalar[int64](t, e, `SELECT count(*) FROM jobs WHERE deleted_at IS NULL AND NOT in_work_pool AND assigned_to IS NULL`), unassigned)
}

func TestPageScheduledRangeAndUnscheduledJobs(t *testing.T) {
	e := newEnv(t)
	e.addJob(t, job{status: "confirmed", value: 10}) // unscheduled
	f := tab(model.ServiceLookupActive)
	all := e.page(t, f)

	f.ScheduledFrom, f.ScheduledBefore = day(2026, 10, 1), day(2026, 11, 1)
	october := e.page(t, f)

	require.Equal(t, int64(34), all.Summary.Count, "33 scheduled seed jobs and the unscheduled one")
	require.Equal(t, int64(31), october.Summary.Count, "an unscheduled job never matches a range; two seed jobs are outside October")
	require.Equal(t, int64(31), scalar[int64](t, e, `SELECT count(*) FROM jobs WHERE deleted_at IS NULL AND NOT in_work_pool
		AND scheduled_at >= '2026-10-01 00:00+00' AND scheduled_at < '2026-11-01 00:00+00'`))

	t.Run("one side open", func(t *testing.T) {
		from := tab(model.ServiceLookupActive)
		from.ScheduledFrom = day(2026, 10, 13)
		page := e.page(t, from)
		for _, r := range page.Rows {
			require.NotNil(t, r.ScheduledAt)
			require.False(t, r.ScheduledAt.Before(day(2026, 10, 13)))
		}
	})
}

func TestPageTabsAreExclusive(t *testing.T) {
	e := newEnv(t)
	pooledAndDeleted := e.addJob(t, job{status: "unconfirmed", scheduled: "2026-10-20 08:00+00", pool: true, deleted: true, value: 1})

	active := e.page(t, tab(model.ServiceLookupActive))
	pool := e.page(t, tab(model.ServiceLookupWorkPool))
	deleted := e.page(t, tab(model.ServiceLookupDeleted))

	require.NotContains(t, ids(active.Rows), pooledAndDeleted)
	require.NotContains(t, ids(pool.Rows), pooledAndDeleted, "a deleted job leaves the work pool")
	require.Contains(t, ids(deleted.Rows), pooledAndDeleted)
	require.Equal(t, model.ServiceLookupTabCounts{Active: 33, WorkPool: 3, Deleted: 2}, active.Summary.Tabs)
}

func TestPageTabCountsShareTheOtherFilters(t *testing.T) {
	e := newEnv(t)

	f := tab(model.ServiceLookupWorkPool)
	f.Statuses = []model.JobStatus{model.JobCancelled}
	page := e.page(t, f)

	require.Empty(t, page.Rows, "no pooled job is cancelled")
	require.Equal(t, model.ServiceLookupSummary{Tabs: model.ServiceLookupTabCounts{Active: 1, WorkPool: 0, Deleted: 1}}, *page.Summary,
		"the cancelled job sits in Active (id 37) and Deleted (id 32)")

	// the range narrows every tab's count
	f = tab(model.ServiceLookupActive)
	f.ScheduledFrom, f.ScheduledBefore = day(2026, 10, 22), day(2026, 10, 23)
	page = e.page(t, f)
	require.Equal(t, model.ServiceLookupTabCounts{Active: 0, WorkPool: 0, Deleted: 1}, page.Summary.Tabs)
}

func TestPageFilters(t *testing.T) {
	e := newEnv(t)
	staffID := scalar[int64](t, e, `SELECT id FROM users ORDER BY id LIMIT 1`)
	serviceTypeID := scalar[int64](t, e, `SELECT id FROM service_types WHERE name = 'Call Back Service'`)

	t.Run("status", func(t *testing.T) {
		f := tab(model.ServiceLookupActive)
		f.Statuses = []model.JobStatus{model.JobConfirmed, model.JobCancelled}
		page := e.page(t, f)
		require.Equal(t, scalar[int64](t, e, `SELECT count(*) FROM jobs WHERE deleted_at IS NULL AND NOT in_work_pool
			AND status IN ('confirmed', 'cancelled')`), page.Summary.Count)
		for _, r := range page.Rows {
			require.Contains(t, []model.JobStatus{model.JobConfirmed, model.JobCancelled}, r.Status)
		}
	})

	t.Run("service type", func(t *testing.T) {
		f := tab(model.ServiceLookupActive)
		f.ServiceTypeIDs = []int64{serviceTypeID}
		page := e.page(t, f)
		require.Len(t, page.Rows, 2)
		for _, r := range page.Rows {
			require.Equal(t, "Call Back Service", r.ServiceType)
		}
	})

	t.Run("assigned to", func(t *testing.T) {
		f := tab(model.ServiceLookupActive)
		f.AssignedToIDs = []int64{staffID}
		page := e.page(t, f)
		require.Equal(t, scalar[int64](t, e, `SELECT count(*) FROM jobs WHERE deleted_at IS NULL AND NOT in_work_pool
			AND assigned_to = $1`, staffID), page.Summary.Count)
		require.NotEmpty(t, page.Rows)
		for _, r := range page.Rows {
			require.NotNil(t, r.AssignedToID)
			require.Equal(t, staffID, *r.AssignedToID)
		}
	})
}

func TestPageTags(t *testing.T) {
	e := newEnv(t)
	vip := scalar[int64](t, e, `SELECT id FROM job_tags WHERE name = 'VIP'`)
	commercial := scalar[int64](t, e, `SELECT id FROM job_tags WHERE name = 'Commercial'`)
	both := e.addJob(t, job{status: "confirmed", scheduled: "2026-10-20 08:00+00", value: 1})
	e.tag(t, both, "VIP", "Commercial")

	t.Run("tags are listed by name", func(t *testing.T) {
		f := tab(model.ServiceLookupActive)
		f.TagIDs = []int64{vip}
		for _, r := range e.page(t, f).Rows {
			if r.ID == both {
				require.Equal(t, []model.JobTag{{ID: commercial, Name: "Commercial"}, {ID: vip, Name: "VIP"}}, r.Tags)
				return
			}
		}
		t.Fatal("the job with both tags is missing")
	})

	t.Run("any of the tags, and a job matching two appears once", func(t *testing.T) {
		f := tab(model.ServiceLookupActive)
		f.TagIDs = []int64{vip, commercial}
		page := e.page(t, f)

		seen := map[int64]bool{}
		for _, r := range page.Rows {
			require.False(t, seen[r.ID], "job %d listed twice", r.ID)
			seen[r.ID] = true
		}
		require.Equal(t, scalar[int64](t, e, `SELECT count(DISTINCT j.id) FROM jobs j JOIN job_tag_links l ON l.job_id = j.id
			WHERE j.deleted_at IS NULL AND NOT j.in_work_pool AND l.tag_id IN ($1, $2)`, vip, commercial), page.Summary.Count)
	})

	t.Run("a job without tags has an empty list", func(t *testing.T) {
		untagged := e.addJob(t, job{status: "confirmed", scheduled: "2026-10-21 08:00+00", value: 1})
		for _, r := range e.page(t, tab(model.ServiceLookupActive)).Rows {
			if r.ID == untagged {
				require.NotNil(t, r.Tags)
				require.Empty(t, r.Tags)
				return
			}
		}
		t.Fatal("the untagged job is missing")
	})
}

func TestPageSearch(t *testing.T) {
	e := newEnv(t)

	tests := []struct {
		name, search string
		wantCustomer string
	}{
		{"customer", "SAKA", "Bukayo Saka"},
		{"account number", "63701", "PerfTest"},
		{"phone", "233960066", "Bukayo Saka"},
		{"address", "Boardwalk", "AAA1 ccc1"},
		{"city", "San Antonio", "William Saliba"},
		{"service type", "Full service", "AAA1 ccc1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tab(model.ServiceLookupActive)
			f.Search = tt.search
			page := e.page(t, f)

			require.NotEmpty(t, page.Rows)
			require.Equal(t, int64(len(page.Rows)), page.Summary.Count)
			for _, r := range page.Rows {
				if tt.name == "service type" {
					require.Equal(t, "Full service", r.ServiceType)
				}
				if tt.name != "service type" {
					require.Equal(t, tt.wantCustomer, r.CustomerName)
				}
			}
		})
	}

	t.Run("wildcards are literal", func(t *testing.T) {
		f := tab(model.ServiceLookupActive)
		f.Search = "%"
		require.Empty(t, e.page(t, f).Rows)
	})
}

func TestPageUnscheduledAndUnassignedJobsSortFirstAscending(t *testing.T) {
	e := newEnv(t)
	unscheduled := e.addJob(t, job{status: "confirmed", value: 1})

	bySchedule, err := e.repo.Page(context.Background(), query(tab(model.ServiceLookupActive), model.ServiceLookupSortScheduledAt, false, 5), false)
	require.NoError(t, err)
	byScheduleDesc, err := e.repo.Page(context.Background(), query(tab(model.ServiceLookupActive), model.ServiceLookupSortScheduledAt, true, 5), false)
	require.NoError(t, err)
	byAssignee, err := e.repo.Page(context.Background(), query(tab(model.ServiceLookupActive), model.ServiceLookupSortAssignedTo, false, 3), false)
	require.NoError(t, err)

	require.Equal(t, unscheduled, bySchedule.Rows[0].ID)
	require.Nil(t, bySchedule.Rows[0].ScheduledAt)
	require.Equal(t, "0001-01-01T00:00:00.000000Z", bySchedule.Rows[0].SortValue)
	require.NotContains(t, ids(byScheduleDesc.Rows), unscheduled)
	require.Nil(t, byAssignee.Rows[0].AssignedTo, "unassigned jobs come first")
}

func TestPageKeysetPagingReproducesTheUnpagedOrderForEverySort(t *testing.T) {
	e := newEnv(t)
	e.addJob(t, job{status: "confirmed", value: 5, tax: 1}) // unscheduled, unassigned

	for _, tb := range []model.ServiceLookupTab{model.ServiceLookupActive, model.ServiceLookupWorkPool} {
		for _, key := range model.ServiceLookupSortKeys {
			for _, desc := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s %s desc=%v", tb, key, desc), func(t *testing.T) {
					f := tab(tb)
					all, err := e.repo.Page(context.Background(), query(f, key, desc, 1000), false)
					require.NoError(t, err)
					require.GreaterOrEqual(t, len(all.Rows), 3)

					var got []int64
					var after *model.ServiceLookupCursor
					for range len(all.Rows) + 2 {
						q := query(f, key, desc, 7)
						q.After = after
						page, err := e.repo.Page(context.Background(), q, false)
						require.NoError(t, err)
						rows := page.Rows
						more := len(rows) > 7
						if more {
							rows = rows[:7]
						}
						got = append(got, ids(rows)...)
						if !more {
							break
						}
						last := rows[len(rows)-1]
						after = &model.ServiceLookupCursor{SortValue: last.SortValue, ID: last.ID}
					}

					require.Equal(t, ids(all.Rows), got)
				})
			}
		}
	}
}

func TestPageSummaryEqualsTheUnpagedTotals(t *testing.T) {
	e := newEnv(t)
	e.addJob(t, job{status: "confirmed", scheduled: "2026-10-20 08:00+00", value: 12.34, tax: 2.34})

	for _, tb := range model.ServiceLookupTabs {
		page := e.page(t, tab(tb))

		var value, without model.Money
		for _, r := range page.Rows {
			value += r.InvoiceValue
			without += r.WithoutTax
		}
		require.Equal(t, int64(len(page.Rows)), page.Summary.Count, tb)
		require.Equal(t, value, page.Summary.InvoiceValue, tb)
		require.Equal(t, without, page.Summary.WithoutTax, tb)
	}
}

func TestPageRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)

	tests := []struct {
		name string
		q    model.ServiceLookupQuery
	}{
		{"unknown sort key", query(tab(model.ServiceLookupActive), "name; DROP TABLE jobs", false, 10)},
		{"empty sort key", model.ServiceLookupQuery{Filter: tab(model.ServiceLookupActive), Limit: 5}},
		{"another report's sort key", query(tab(model.ServiceLookupActive), "amount", false, 10)},
		{"unknown tab", query(tab("archived"), model.ServiceLookupSortCustomer, false, 10)},
		{"no limit", query(tab(model.ServiceLookupActive), model.ServiceLookupSortCustomer, false, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.repo.Page(context.Background(), tt.q, true)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestFilterLookups(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	types, err := e.repo.ServiceTypes(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, types)

	staff, err := e.repo.Staff(ctx)
	require.NoError(t, err)
	require.Len(t, staff, 2)

	tags, err := e.repo.Tags(ctx)
	require.NoError(t, err)
	names := make([]string, len(tags))
	for i, tg := range tags {
		names[i] = tg.Name
	}
	require.Equal(t, []string{"Commercial", "Follow Up", "Residential", "VIP"}, names, "ordered by name")
}
