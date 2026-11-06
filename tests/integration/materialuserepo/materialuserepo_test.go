//go:build integration

package materialuserepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

func utc(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

type env struct {
	pool *pgxpool.Pool
	repo *repository.MaterialUseRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewMaterialUseRepository(pool)}
}

// november is the whole month, the range of the screen.
func november() model.MaterialUseFilter {
	return model.MaterialUseFilter{ScheduledFrom: utc(2026, 11, 1), ScheduledBefore: utc(2026, 12, 1)}
}

func query(f model.MaterialUseFilter, key model.MaterialUseSortKey, desc bool, limit int) model.MaterialUseQuery {
	return model.MaterialUseQuery{Filter: f, Sort: model.MaterialUseSort{Key: key, Desc: desc}, Limit: limit}
}

func list(t *testing.T, e env, f model.MaterialUseFilter) []model.MaterialUseRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), query(f, model.MaterialUseSortMaterial, false, 100))
	require.NoError(t, err)
	return rows
}

// byName indexes rows by material, as "qty/applications".
func byName(rows []model.MaterialUseRow) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		out[r.Material] = r.Qty + "/" + itoa(r.Applications)
	}
	return out
}

func itoa(n int64) string {
	s := ""
	for n > 0 || s == "" {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestSeededNovemberTotalsEachMaterialOverItsJobs(t *testing.T) {
	e := newEnv(t)

	rows := list(t, e, november())

	require.Equal(t, map[string]string{
		"Demand CS":   "4.250/3", // 2.5 + 1.25 + 0.5
		"Glue Boards": "4.000/1",
		"Temprid SC":  "3.750/2", // 3 + 0.75
	}, byName(rows), "the deleted, pre-November and never-scheduled jobs add nothing, and Gentrol IGR has no row at all")
	require.Equal(t, []string{"Demand CS", "Glue Boards", "Temprid SC"}, []string{rows[0].Material, rows[1].Material, rows[2].Material})
}

func TestEPAIsNilWhenTheMaterialHasNone(t *testing.T) {
	rows := list(t, newEnv(t), november())

	var glue, demand model.MaterialUseRow
	for _, r := range rows {
		switch r.Material {
		case "Glue Boards":
			glue = r
		case "Demand CS":
			demand = r
		}
	}
	require.Nil(t, glue.EPA)
	require.NotNil(t, demand.EPA)
	require.Equal(t, "100-1066", *demand.EPA)
}

func TestAnOpenRangeIncludesNeverScheduledAndEarlierJobsButNotDeletedOnes(t *testing.T) {
	e := newEnv(t)

	got := byName(list(t, e, model.MaterialUseFilter{}))

	require.Equal(t, map[string]string{
		"Demand CS":   "4.250/3",
		"Glue Boards": "4.000/1",
		"Temprid SC":  "8.750/3", // + 5 on the job that was never scheduled
		"Gentrol IGR": "1.000/1", // the job before November; the 9 on the deleted job is left out
	}, got)
}

func TestTheRangeIsHalfOpenOnTheScheduledInstant(t *testing.T) {
	e := newEnv(t)
	var jobID int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT min(job_id) FROM job_materials WHERE material_id = (SELECT id FROM materials WHERE name = 'Gentrol IGR') AND job_id IN (SELECT id FROM jobs WHERE deleted_at IS NULL)`).Scan(&jobID))

	for _, tt := range []struct {
		name    string
		at      string
		inRange bool
	}{
		{"the first instant of the range", "2026-11-01 00:00:00+00", true},
		{"the last microsecond of the range", "2026-11-30 23:59:59.999999+00", true},
		{"the first instant after the range", "2026-12-01 00:00:00+00", false},
		{"the last instant before the range", "2026-10-31 23:59:59.999999+00", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.pool.Exec(context.Background(), `UPDATE jobs SET scheduled_at = $2::timestamptz WHERE id = $1`, jobID, tt.at)
			require.NoError(t, err)

			_, found := byName(list(t, e, november()))["Gentrol IGR"]

			require.Equal(t, tt.inRange, found)
		})
	}
}

func TestTabsSplitRowsByDevice(t *testing.T) {
	e := newEnv(t)

	jobs := november()
	jobs.Tab = model.MaterialUseJobs
	devices := november()
	devices.Tab = model.MaterialUseDevices
	all := november()
	all.Tab = model.MaterialUseAll

	require.Equal(t, map[string]string{
		"Demand CS": "3.750/2", "Glue Boards": "4.000/1", "Temprid SC": "3.000/1",
	}, byName(list(t, e, jobs)), "rows without a device")
	require.Equal(t, map[string]string{
		"Demand CS": "0.500/1", "Temprid SC": "0.750/1",
	}, byName(list(t, e, devices)), "rows with a device")
	require.Equal(t, byName(list(t, e, november())), byName(list(t, e, all)))
}

func TestFilterByClientAndMaterial(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	var customerID int64
	require.NoError(t, e.pool.QueryRow(ctx, `
		SELECT j.customer_id FROM job_materials jm JOIN jobs j ON j.id = jm.job_id
		JOIN materials m ON m.id = jm.material_id
		WHERE m.name = 'Glue Boards' AND j.deleted_at IS NULL`).Scan(&customerID))

	t.Run("by client", func(t *testing.T) {
		f := november()
		f.CustomerIDs = []int64{customerID}

		rows := list(t, e, f)

		require.NotEmpty(t, rows)
		var total int64
		require.NoError(t, e.pool.QueryRow(ctx, `
			SELECT count(*) FROM job_materials jm JOIN jobs j ON j.id = jm.job_id
			WHERE j.customer_id = $1 AND j.deleted_at IS NULL AND j.scheduled_at >= $2 AND j.scheduled_at < $3`,
			customerID, utc(2026, 11, 1), utc(2026, 12, 1)).Scan(&total))
		var apps int64
		for _, r := range rows {
			apps += r.Applications
		}
		require.Equal(t, total, apps, "every row of that client's jobs, and no one else's")
		require.Contains(t, byName(rows), "Glue Boards")
	})

	t.Run("by a client with no materials", func(t *testing.T) {
		f := november()
		f.CustomerIDs = []int64{-1}
		require.Empty(t, list(t, e, f))
	})

	t.Run("by material", func(t *testing.T) {
		var id int64
		require.NoError(t, e.pool.QueryRow(ctx, `SELECT id FROM materials WHERE name = 'Temprid SC'`).Scan(&id))
		f := november()
		f.MaterialIDs = []int64{id}

		require.Equal(t, map[string]string{"Temprid SC": "3.750/2"}, byName(list(t, e, f)))
	})
}

func TestSearchMatchesNameAndEPAAndTreatsWildcardsLiterally(t *testing.T) {
	e := newEnv(t)
	search := func(s string) map[string]string {
		f := november()
		f.Search = s
		return byName(list(t, e, f))
	}

	require.Equal(t, map[string]string{"Demand CS": "4.250/3"}, search("deMAND"), "case-insensitive, by name")
	require.Equal(t, map[string]string{"Temprid SC": "3.750/2"}, search("432-13"), "by EPA number")
	require.Empty(t, search("%"), "a percent sign is not a wildcard")
	require.Empty(t, search("_emand"), "an underscore is not a wildcard")
	require.Empty(t, search("'; DROP TABLE materials; --"))
}

func TestEverySortKeyOrdersInBothDirections(t *testing.T) {
	e := newEnv(t)
	f := model.MaterialUseFilter{} // the open range: four materials, one with no EPA

	for _, key := range model.MaterialUseSortKeys {
		for _, desc := range []bool{false, true} {
			rows, err := e.repo.List(context.Background(), query(f, key, desc, 100))
			require.NoError(t, err)
			require.Len(t, rows, 4)

			for i := 1; i < len(rows); i++ {
				a, b := rows[i-1], rows[i]
				var cmp int
				switch key {
				case model.MaterialUseSortMaterial:
					cmp = compareText(lower(a.Material), lower(b.Material))
				case model.MaterialUseSortEPA:
					cmp = compareText(epa(a), epa(b))
				case model.MaterialUseSortQty:
					cmp = compareText(pad(a.Qty), pad(b.Qty))
				case model.MaterialUseSortApplications:
					cmp = compareInt(a.Applications, b.Applications)
				}
				if desc {
					cmp = -cmp
				}
				require.LessOrEqual(t, cmp, 0, "%s desc=%v: %v before %v", key, desc, a, b)
			}
		}
	}
}

func TestKeysetPagingWalksEveryRowOnceForEverySortKey(t *testing.T) {
	e := newEnv(t)
	// applications ties (Demand CS and Temprid SC both 3 over the open range) make the id tie-break matter
	for _, key := range model.MaterialUseSortKeys {
		for _, desc := range []bool{false, true} {
			full, err := e.repo.List(context.Background(), query(model.MaterialUseFilter{}, key, desc, 100))
			require.NoError(t, err)

			var walked []int64
			var after *model.MaterialUseCursor
			for pages := 0; pages < 10; pages++ {
				q := query(model.MaterialUseFilter{}, key, desc, 1)
				q.After = after
				rows, err := e.repo.List(context.Background(), q)
				require.NoError(t, err)
				require.LessOrEqual(t, len(rows), 2, "limit+1 rows at most")
				if len(rows) == 0 {
					break
				}
				walked = append(walked, rows[0].MaterialID)
				after = &model.MaterialUseCursor{SortValue: rows[0].SortValue, ID: rows[0].MaterialID}
				if len(rows) == 1 {
					break
				}
			}

			var want []int64
			for _, r := range full {
				want = append(want, r.MaterialID)
			}
			require.Equal(t, want, walked, "%s desc=%v", key, desc)
		}
	}
}

func TestListFollowsTheContextAndRejectsBadQueries(t *testing.T) {
	e := newEnv(t)

	t.Run("a cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := e.repo.List(ctx, query(november(), model.MaterialUseSortMaterial, false, 10))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("an invalid query", func(t *testing.T) {
		_, err := e.repo.List(context.Background(), query(november(), model.MaterialUseSortMaterial, false, 0))
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestFilterLookups(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	materials, err := e.repo.Materials(ctx)
	require.NoError(t, err)
	var names []string
	for _, m := range materials {
		names = append(names, m.Name)
	}
	require.Equal(t, []string{"Demand CS", "Gentrol IGR", "Glue Boards", "Temprid SC"}, names, "every material, by name")

	clients, err := e.repo.Clients(ctx)
	require.NoError(t, err)
	require.Len(t, clients, 20, "every seeded customer")

	// ordered by name in the database's own collation, which is not Go's byte order
	var want []string
	dbRows, err := e.pool.Query(ctx, `SELECT display_name FROM customers ORDER BY display_name, id`)
	require.NoError(t, err)
	defer dbRows.Close()
	for dbRows.Next() {
		var name string
		require.NoError(t, dbRows.Scan(&name))
		want = append(want, name)
	}
	require.NoError(t, dbRows.Err())
	got := make([]string, len(clients))
	for i, c := range clients {
		got[i] = c.Name
	}
	require.Equal(t, want, got)
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func epa(r model.MaterialUseRow) string {
	if r.EPA == nil {
		return ""
	}
	return lower(*r.EPA)
}

// pad makes a decimal text comparable as text: the integer part is left-padded to a fixed width.
func pad(s string) string {
	dot := len(s)
	for i, c := range s {
		if c == '.' {
			dot = i
			break
		}
	}
	for len(s[:dot]) < 12 {
		s = "0" + s
		dot++
	}
	return s
}

func compareText(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
