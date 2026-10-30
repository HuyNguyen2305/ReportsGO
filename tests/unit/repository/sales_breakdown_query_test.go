package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

func breakdownFilter(ty model.SalesBreakdownType, by model.SalesBreakdownBy) model.SalesBreakdownFilter {
	f := model.SalesBreakdownFilter{
		Type: ty, By: by, Year: 2026,
		YearFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		YearTo:   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
	}
	for m := range f.MonthStarts {
		f.MonthStarts[m] = time.Date(2026, time.Month(m+1), 1, 0, 0, 0, 0, time.UTC)
	}
	if ty == model.SalesBreakdownRevenue {
		f.Measure = model.SalesBreakdownCollected
	}
	return f
}

func breakdownQuery(f model.SalesBreakdownFilter, key model.SalesBreakdownSortKey, desc bool) model.SalesBreakdownQuery {
	return model.SalesBreakdownQuery{Filter: f, Sort: model.SalesBreakdownSort{Key: key, Desc: desc}, Limit: 50}
}

func TestBuildSalesBreakdownListQueryForecastFacts(t *testing.T) {
	f := breakdownFilter(model.SalesBreakdownForecast, model.SalesBreakdownByClient)

	stmt, err := repository.BuildSalesBreakdownListQuery(breakdownQuery(f, model.SalesBreakdownSortName, false))
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "width_bucket(j.scheduled_at, $1::timestamptz[]) AS month, j.invoice_value AS amount")
	require.Contains(t, stmt.SQL, "j.deleted_at IS NULL AND j.status <> 'cancelled'")
	require.Contains(t, stmt.SQL, "j.scheduled_at >= ($1::timestamptz[])[1] AND j.scheduled_at < ($1::timestamptz[])[13]")
	require.Contains(t, stmt.SQL, "i.parent_type = 'invoice' AND i.job_id IS NULL AND i.status IN ('sent', 'paid')",
		"invoices without a job are forecast too")
	require.Contains(t, stmt.SQL, "i.document_date >= $2::date AND i.document_date <= $3::date")
	require.NotContains(t, stmt.SQL, "FROM payments", "the forecast involves no payment")
	require.Equal(t, f.MonthStarts[:], stmt.Args[0])
	require.Equal(t, []any{f.YearFrom, f.YearTo, 51}, stmt.Args[1:])
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildSalesBreakdownListQueryRevenueFacts(t *testing.T) {
	f := breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient)
	f.Methods = []model.PaymentMethod{model.MethodCard}

	stmt, err := repository.BuildSalesBreakdownListQuery(breakdownQuery(f, model.SalesBreakdownSortName, false))
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FROM payments p\n    JOIN billing_documents i ON i.id = p.invoice_id", "credit payments have no invoice")
	require.Contains(t, stmt.SQL, "p.status = 'paid' AND p.deleted_at IS NULL")
	require.Contains(t, stmt.SQL, "p.payment_date >= $1::date AND p.payment_date <= $2::date")
	require.Contains(t, stmt.SQL, "p.method_type::text = ANY($3::text[])")
	require.Contains(t, stmt.SQL, "extract(month FROM p.payment_date)::int AS month, p.amount AS amount")
	require.NotContains(t, stmt.SQL, "width_bucket", "payment dates are plain dates")
	requirePlaceholdersMatchArgs(t, stmt)

	t.Run("without tax", func(t *testing.T) {
		f.Measure = model.SalesBreakdownCollectedWithoutTax
		stmt, err := repository.BuildSalesBreakdownListQuery(breakdownQuery(f, model.SalesBreakdownSortName, false))
		require.NoError(t, err)
		require.Contains(t, stmt.SQL, "round(p.amount * (i.total - tx.tax) / i.total, 2)")
		require.Contains(t, stmt.SQL, "FROM invoice_tax_lines l WHERE l.invoice_id = p.invoice_id) tx ON true")
	})
}

func TestBuildSalesBreakdownListQueryPiecesPerGrouping(t *testing.T) {
	tests := []struct {
		by        model.SalesBreakdownBy
		piece     string
		dimension string
	}{
		{model.SalesBreakdownByClient, "f.customer_id AS group_id", "JOIN customers x ON x.id = g.id"},
		{model.SalesBreakdownByService, "j.service_type_id AS group_id", "JOIN service_types x ON x.id = g.id"},
		{model.SalesBreakdownByStaff, "j.assigned_to AS group_id", "JOIN users x ON x.id = g.id"},
		{model.SalesBreakdownBySource, "c.lead_source_id AS group_id", "JOIN lead_sources x ON x.id = g.id"},
		{model.SalesBreakdownByTag, "l.tag_id AS group_id", "JOIN job_tags x ON x.id = g.id"},
		{model.SalesBreakdownByItem, "round(f.amount * li.line_total / li.lsum, 2)", "JOIN items x ON x.id = g.id"},
	}
	for _, tt := range tests {
		t.Run(string(tt.by), func(t *testing.T) {
			stmt, err := repository.BuildSalesBreakdownListQuery(breakdownQuery(breakdownFilter(model.SalesBreakdownRevenue, tt.by), model.SalesBreakdownSortName, false))
			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.piece)
			require.Contains(t, stmt.SQL, tt.dimension)
			require.Contains(t, stmt.SQL, "FROM d WHERE d.group_id IS NOT NULL", "the unassigned pieces are not a row")
		})
	}

	t.Run("item lines come from the payment's invoice or the job's live invoices", func(t *testing.T) {
		stmt, err := repository.BuildSalesBreakdownListQuery(breakdownQuery(breakdownFilter(model.SalesBreakdownForecast, model.SalesBreakdownByItem), model.SalesBreakdownSortName, false))
		require.NoError(t, err)
		require.Contains(t, stmt.SQL, "WHERE CASE WHEN f.invoice_id IS NOT NULL THEN b.id = f.invoice_id")
		require.Contains(t, stmt.SQL, "ELSE b.job_id = f.job_id AND b.deleted_at IS NULL AND b.status <> 'void' END")
		require.Contains(t, stmt.SQL, ") x WHERE x.lsum <> 0", "lines that add up to nothing leave the fact unassigned")
	})
}

func TestBuildSalesBreakdownListQueryMonthsAndKeyset(t *testing.T) {
	q := breakdownQuery(breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient), model.SalesBreakdownSortOct, true)
	q.After = &model.SalesBreakdownCursor{SortValue: "99.00", ID: 7}

	stmt, err := repository.BuildSalesBreakdownListQuery(q)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "COALESCE(SUM(d.amount) FILTER (WHERE d.month = 1), 0)::numeric(14,2) AS m01")
	require.Contains(t, stmt.SQL, "COALESCE(SUM(d.amount) FILTER (WHERE d.month = 12), 0)::numeric(14,2) AS m12")
	require.Contains(t, stmt.SQL, "x.account_number AS account_number")
	require.Contains(t, stmt.SQL, "(r.m10, r.id) < (($3::text)::numeric, $4::bigint)")
	require.True(t, strings.HasSuffix(stmt.SQL, "ORDER BY r.m10 DESC, r.id DESC\nLIMIT $5"), stmt.SQL)
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildSalesBreakdownListQueryEverySortKeyAndDirection(t *testing.T) {
	want := map[model.SalesBreakdownSortKey]string{
		model.SalesBreakdownSortName:          "left(lower(r.name), 200)",
		model.SalesBreakdownSortAccountNumber: "left(r.account_number, 200)",
		model.SalesBreakdownSortTotal:         "r.total",
	}
	for i, key := range model.SalesBreakdownMonthKeys {
		want[key] = "r.m" + []string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12"}[i]
	}
	require.Len(t, want, len(model.SalesBreakdownSortKeysFor(model.SalesBreakdownByClient)), "every key must be covered")

	for _, key := range model.SalesBreakdownSortKeysFor(model.SalesBreakdownByClient) {
		for _, desc := range []bool{false, true} {
			dir := "ASC"
			if desc {
				dir = "DESC"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				stmt, err := repository.BuildSalesBreakdownListQuery(breakdownQuery(breakdownFilter(model.SalesBreakdownForecast, model.SalesBreakdownByClient), key, desc))
				require.NoError(t, err)
				require.True(t, strings.HasSuffix(stmt.SQL, "ORDER BY "+want[key]+" "+dir+", r.id "+dir+"\nLIMIT $4"), stmt.SQL)
			})
		}
	}
}

func TestBuildSalesBreakdownListQueryGroupAndSearch(t *testing.T) {
	f := breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient)
	f.GroupIDs = []int64{3, 4}
	f.Search = "saka"

	stmt, err := repository.BuildSalesBreakdownListQuery(breakdownQuery(f, model.SalesBreakdownSortName, false))
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "x.id = ANY($3::bigint[])")
	require.Contains(t, stmt.SQL, "(x.display_name ILIKE $4 ESCAPE '\\' OR x.account_number ILIKE $4 ESCAPE '\\')")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildSalesBreakdownSummaryQuery(t *testing.T) {
	t.Run("without a filter it includes the unassigned pieces", func(t *testing.T) {
		stmt, err := repository.BuildSalesBreakdownSummaryQuery(breakdownFilter(model.SalesBreakdownForecast, model.SalesBreakdownByTag))
		require.NoError(t, err)

		require.Contains(t, stmt.SQL, "SELECT DISTINCT ON (d.kind, d.id, d.line_id) d.month, d.amount FROM d WHERE d.group_id IN (SELECT r.id FROM r) OR d.group_id IS NULL",
			"each piece once: a job under two tags is one amount")
		require.Contains(t, stmt.SQL, "FROM d WHERE d.group_id IS NULL)", "the unassigned line")
		require.Contains(t, stmt.SQL, "SELECT (SELECT count(*) FROM r)")
		require.NotContains(t, stmt.SQL, "LIMIT $")
		requirePlaceholdersMatchArgs(t, stmt)
	})

	t.Run("a group or search filter drops the unassigned line", func(t *testing.T) {
		f := breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownBySource)
		f.Search = "face"
		stmt, err := repository.BuildSalesBreakdownSummaryQuery(f)
		require.NoError(t, err)

		require.Contains(t, stmt.SQL, "WHERE d.group_id IN (SELECT r.id FROM r)) s")
		require.Contains(t, stmt.SQL, "NULL::bigint[], NULL::bigint")
		requirePlaceholdersMatchArgs(t, stmt)
	})
}

func TestBuildSalesBreakdownQueryRejectsInvalidInput(t *testing.T) {
	measured := breakdownFilter(model.SalesBreakdownForecast, model.SalesBreakdownByClient)
	measured.Measure = model.SalesBreakdownCollected
	methods := breakdownFilter(model.SalesBreakdownForecast, model.SalesBreakdownByClient)
	methods.Methods = []model.PaymentMethod{model.MethodCash}
	noYear := breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient)
	noYear.YearTo = time.Time{}
	noMonths := breakdownFilter(model.SalesBreakdownForecast, model.SalesBreakdownByClient)
	noMonths.MonthStarts = [13]time.Time{}
	badMeasure := breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient)
	badMeasure.Measure = "invoiced"

	tests := []struct {
		name string
		q    model.SalesBreakdownQuery
	}{
		{"unknown type", breakdownQuery(breakdownFilter("budget", model.SalesBreakdownByClient), model.SalesBreakdownSortName, false)},
		{"unknown grouping", breakdownQuery(breakdownFilter(model.SalesBreakdownRevenue, "region"), model.SalesBreakdownSortName, false)},
		{"unknown sort key", breakdownQuery(breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient), "name; DROP TABLE jobs", false)},
		{"account number outside clients", breakdownQuery(breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownByTag), model.SalesBreakdownSortAccountNumber, false)},
		{"a measure on the forecast", breakdownQuery(measured, model.SalesBreakdownSortName, false)},
		{"methods on the forecast", breakdownQuery(methods, model.SalesBreakdownSortName, false)},
		{"unknown measure", breakdownQuery(badMeasure, model.SalesBreakdownSortName, false)},
		{"no year", breakdownQuery(noYear, model.SalesBreakdownSortName, false)},
		{"no month starts", breakdownQuery(noMonths, model.SalesBreakdownSortName, false)},
		{"no limit", model.SalesBreakdownQuery{Filter: breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient), Sort: model.SalesBreakdownSort{Key: model.SalesBreakdownSortName}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repository.BuildSalesBreakdownListQuery(tt.q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}

	t.Run("the summary rejects an unknown grouping", func(t *testing.T) {
		_, err := repository.BuildSalesBreakdownSummaryQuery(breakdownFilter(model.SalesBreakdownRevenue, "region"))
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestBuildSalesBreakdownQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE payments; --`
	f := breakdownFilter(model.SalesBreakdownRevenue, model.SalesBreakdownByClient)
	f.Search = hostile
	f.Methods = []model.PaymentMethod{model.PaymentMethod(hostile)}

	stmt, err := repository.BuildSalesBreakdownListQuery(breakdownQuery(f, model.SalesBreakdownSortName, false))
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(toString(a)) + "|"
	}
	require.Contains(t, flat, "drop table payments", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}
