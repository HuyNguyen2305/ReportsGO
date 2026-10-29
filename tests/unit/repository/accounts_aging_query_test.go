package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

var agingAsOf = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)

func agingFilter() model.AgingFilter {
	return model.AgingFilter{
		AsOf:           agingAsOf,
		MinAgeDays:     60,
		ServiceTypeIDs: []int64{3, 4},
		Email:          "@example.com",
		Search:         "saka",
	}
}

func agingQuery(f model.AgingFilter, key model.AgingSortKey, desc bool) model.AgingQuery {
	return model.AgingQuery{Filter: f, Sort: model.AgingSort{Key: key, Desc: desc}, Limit: 50}
}

func TestBuildAccountsAgingListQueryInvoiceSetAndBalance(t *testing.T) {
	stmt, err := repository.BuildAccountsAgingListQuery(agingQuery(agingFilter(), model.AgingSortCustomer, false))
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "i.parent_type = 'invoice' AND i.status IN ('sent', 'paid') AND i.deleted_at IS NULL AND i.archived_at IS NULL")
	require.Contains(t, stmt.SQL, "i.document_date <= $1::date")
	require.Contains(t, stmt.SQL, "p.status = 'paid' AND p.deleted_at IS NULL AND p.payment_date <= $1::date")
	require.Contains(t, stmt.SQL, "ca.applied_on <= $1::date", "credit applications reduce the balance")
	require.Contains(t, stmt.SQL, "i.total - COALESCE(pay.paid, 0) - COALESCE(app.applied, 0) > 0", "only open balances are listed")
	require.Contains(t, stmt.SQL, "$1::date - i.document_date AS age")
	require.Contains(t, stmt.SQL, "j.service_type_id = ANY($2::bigint[])")
	require.Contains(t, stmt.SQL, "i.billing_email ILIKE $3")
	require.Equal(t, agingAsOf, stmt.Args[0])
	require.Equal(t, []int64{3, 4}, stmt.Args[1])
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildAccountsAgingListQueryBucketsRowsAndCredit(t *testing.T) {
	stmt, err := repository.BuildAccountsAgingListQuery(agingQuery(model.AgingFilter{AsOf: agingAsOf}, model.AgingSortCustomer, false))
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "FILTER (WHERE inv.age <= 30)")
	require.Contains(t, stmt.SQL, "FILTER (WHERE inv.age BETWEEN 31 AND 60)")
	require.Contains(t, stmt.SQL, "FILTER (WHERE inv.age BETWEEN 61 AND 90)")
	require.Contains(t, stmt.SQL, "FILTER (WHERE inv.age >= 91)")
	require.Contains(t, stmt.SQL, "GROUP BY inv.customer_id, inv.location_id")
	require.Contains(t, stmt.SQL, "inv.customer_id * 4294967296 + COALESCE(inv.location_id, 0) AS id", "a positive row key per customer and location")
	require.Contains(t, stmt.SQL, "AND is_default) cd ON true", "the default saved card")
	require.Contains(t, stmt.SQL, "cr.credit_date <= $1::date")
	require.Contains(t, stmt.SQL, "GREATEST(", "available credit is never negative")
	require.NotContains(t, stmt.SQL, "HAVING", "no age filter")
	require.NotContains(t, stmt.SQL, "ILIKE", "no email or search")
	require.NotContains(t, stmt.SQL, "ANY(", "no service type filter")
	require.Len(t, stmt.Args, 2, "the as-of date and the limit")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildAccountsAgingListQueryDaysFilter(t *testing.T) {
	tests := []struct {
		days int
		want string
	}{
		{30, "HAVING SUM(inv.balance) FILTER (WHERE inv.age >= 31) > 0"},
		{60, "HAVING SUM(inv.balance) FILTER (WHERE inv.age >= 61) > 0"},
		{90, "HAVING SUM(inv.balance) FILTER (WHERE inv.age >= 91) > 0"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			f := model.AgingFilter{AsOf: agingAsOf, MinAgeDays: tt.days}
			stmt, err := repository.BuildAccountsAgingListQuery(agingQuery(f, model.AgingSortCustomer, false))
			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
		})
	}
}

func TestBuildAccountsAgingListQuerySearchAndKeyset(t *testing.T) {
	q := agingQuery(model.AgingFilter{AsOf: agingAsOf, Search: "saka"}, model.AgingSortTotal, true)
	q.After = &model.AgingCursor{SortValue: "99.00", ID: 4294967297}

	stmt, err := repository.BuildAccountsAgingListQuery(q)
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "(c.display_name ILIKE $2 ESCAPE '\\' OR c.account_number ILIKE $2 ESCAPE '\\' OR sl.address_line1 ILIKE $2 ESCAPE '\\' OR sl.city ILIKE $2 ESCAPE '\\')")
	require.Contains(t, stmt.SQL, "(r.total, r.id) < (($3::text)::numeric, $4::bigint)")
	require.True(t, strings.HasSuffix(stmt.SQL, "ORDER BY r.total DESC, r.id DESC\nLIMIT $5"), stmt.SQL)
	require.Equal(t, []any{agingAsOf, "%saka%", "99.00", int64(4294967297), 51}, stmt.Args)
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildAccountsAgingListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.AgingSortKey]string{
		model.AgingSortCustomer:        "left(lower(r.customer), 200)",
		model.AgingSortTotal:           "r.total",
		model.AgingSortD0to30:          "r.d0_30",
		model.AgingSortD31to60:         "r.d31_60",
		model.AgingSortD61to90:         "r.d61_90",
		model.AgingSortD90Plus:         "r.d90_plus",
		model.AgingSortAvailableCredit: "r.available_credit",
	}
	require.Len(t, wantExpr, len(model.AgingSortKeys), "every key must be covered by this test")

	for _, key := range model.AgingSortKeys {
		want, ok := wantExpr[key]
		require.True(t, ok, "no expectation for sort key %s", key)

		for _, desc := range []bool{false, true} {
			dir := "ASC"
			if desc {
				dir = "DESC"
			}
			t.Run(string(key)+" "+dir, func(t *testing.T) {
				stmt, err := repository.BuildAccountsAgingListQuery(agingQuery(model.AgingFilter{AsOf: agingAsOf}, key, desc))
				require.NoError(t, err)
				require.Contains(t, stmt.SQL, "ORDER BY "+want+" "+dir+", r.id "+dir+"\nLIMIT $2")
			})
		}
	}
}

func TestBuildAccountsAgingSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildAccountsAgingSummaryQuery(agingFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "WITH inv AS (", "the summary sees the same rows as the list")
	require.Contains(t, stmt.SQL, "HAVING SUM(inv.balance) FILTER (WHERE inv.age >= 61) > 0")
	require.Contains(t, stmt.SQL, "SELECT DISTINCT ON (r2.customer_id) r2.available_credit FROM r r2", "credit counts once per customer")
	require.Contains(t, stmt.SQL, "SELECT count(*),")
	require.NotContains(t, stmt.SQL, "LIMIT $", "the summary covers every page")
	require.NotContains(t, stmt.SQL, "ORDER BY r.")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildAccountsAgingQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		q    model.AgingQuery
	}{
		{"no as_of", agingQuery(model.AgingFilter{}, model.AgingSortCustomer, false)},
		{"unknown sort key", agingQuery(model.AgingFilter{AsOf: agingAsOf}, "name; DROP TABLE customers", false)},
		{"empty sort key", model.AgingQuery{Filter: model.AgingFilter{AsOf: agingAsOf}, Limit: 5}},
		{"another report's sort key", agingQuery(model.AgingFilter{AsOf: agingAsOf}, "amount", false)},
		{"unknown days", agingQuery(model.AgingFilter{AsOf: agingAsOf, MinAgeDays: 45}, model.AgingSortCustomer, false)},
		{"no limit", model.AgingQuery{Filter: model.AgingFilter{AsOf: agingAsOf}, Sort: model.AgingSort{Key: model.AgingSortCustomer}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repository.BuildAccountsAgingListQuery(tt.q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}

	t.Run("the summary needs an as_of date too", func(t *testing.T) {
		_, err := repository.BuildAccountsAgingSummaryQuery(model.AgingFilter{})
		require.ErrorIs(t, err, repository.ErrInvalidQuery)
	})
}

func TestBuildAccountsAgingQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE customers; --`
	f := model.AgingFilter{AsOf: agingAsOf, Email: hostile, Search: hostile}

	stmt, err := repository.BuildAccountsAgingListQuery(agingQuery(f, model.AgingSortCustomer, false))
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
