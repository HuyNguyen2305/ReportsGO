package repository_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
)

var placeholder = regexp.MustCompile(`\$(\d+)`)

// requirePlaceholdersMatchArgs checks that $1..$N all appear in the SQL, and that N equals the argument count.
func requirePlaceholdersMatchArgs(t *testing.T, stmt repository.Statement) {
	t.Helper()
	seen := map[int]bool{}
	highest := 0
	for _, m := range placeholder.FindAllStringSubmatch(stmt.SQL, -1) {
		n, err := strconv.Atoi(m[1])
		require.NoError(t, err)
		seen[n] = true
		highest = max(highest, n)
	}
	require.Equal(t, len(stmt.Args), highest, "highest placeholder must equal the number of args\nSQL:\n%s", stmt.SQL)
	for i := 1; i <= highest; i++ {
		require.True(t, seen[i], "placeholder $%d is never used\nSQL:\n%s", i, stmt.SQL)
	}
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func defaultQuery() model.InvoiceQuery {
	return model.InvoiceQuery{
		Filter: model.InvoiceFilter{Lifecycle: model.LifecycleActive},
		Sort:   model.InvoiceSort{Key: model.SortDate, Desc: true},
		Limit:  50,
	}
}

// Stored text has no length limit, so text keys must order, compare and report only a bounded prefix; otherwise a
// very long customer name or address would produce a cursor the API then refuses.
func TestBuildInvoiceListQueryBoundsTextSortValues(t *testing.T) {
	textKeys := map[model.InvoiceSortKey]bool{
		model.SortAccountNumber: true, model.SortInvoiceNumber: true, model.SortCustomer: true, model.SortJob: true,
		model.SortBillingEmail: true, model.SortStatus: true, model.SortSignatureStatus: true, model.SortPaymentTerms: true,
	}
	limit := fmt.Sprintf(", %d)", model.MaxSortTextLength)

	for _, key := range model.InvoiceSortKeys {
		t.Run(string(key), func(t *testing.T) {
			q := defaultQuery()
			q.Sort = model.InvoiceSort{Key: key}
			q.After = &model.InvoiceCursor{SortValue: "x", ID: 1}

			stmt, err := repository.BuildInvoiceListQuery(q)
			require.NoError(t, err)

			if textKeys[key] {
				require.Equal(t, 3, strings.Count(stmt.SQL, limit), "sort_value, keyset condition and ORDER BY are all cut")
				return
			}
			require.NotContains(t, stmt.SQL, "left(", "only text keys are cut")
		})
	}
}

func TestBuildInvoiceListQueryDefault(t *testing.T) {
	stmt, err := repository.BuildInvoiceListQuery(defaultQuery())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "ORDER BY i.document_date DESC, i.id DESC")
	require.Contains(t, stmt.SQL, "i.deleted_at IS NULL AND i.archived_at IS NULL")
	require.Contains(t, stmt.SQL, "LIMIT $1")
	require.Equal(t, []any{51}, stmt.Args, "one extra row is fetched to detect another page")
	requirePlaceholdersMatchArgs(t, stmt)
}

// billing_documents also holds estimates, so both statements must always restrict to invoices.
func TestInvoiceQueriesAreRestrictedToInvoices(t *testing.T) {
	list, err := repository.BuildInvoiceListQuery(defaultQuery())
	require.NoError(t, err)
	summary, err := repository.BuildInvoiceSummaryQuery(model.InvoiceFilter{})
	require.NoError(t, err)

	for name, stmt := range map[string]repository.Statement{"list": list, "summary": summary} {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, stmt.SQL, "FROM billing_documents i")
			require.Contains(t, stmt.SQL, "i.parent_type = 'invoice'")
		})
	}
}

func TestBuildInvoiceListQueryEverySortKeyAndDirection(t *testing.T) {
	wantExpr := map[model.InvoiceSortKey]struct{ expr, cast string }{
		model.SortAccountNumber:   {"c.account_number", "text"},
		model.SortInvoiceNumber:   {"i.document_number", "text"},
		model.SortCustomer:        {"lower(c.display_name)", "text"},
		model.SortJob:             {"lower(coalesce(CASE WHEN j.id IS NULL", "text"},
		model.SortBillingEmail:    {"lower(coalesce(i.billing_email, ''))", "text"},
		model.SortDate:            {"i.document_date", "date"},
		model.SortTotal:           {"i.total", "numeric"},
		model.SortDiscount:        {"i.discount", "numeric"},
		model.SortStatus:          {"i.status::text", "text"},
		model.SortSignatureStatus: {"i.signature_status::text", "text"},
		model.SortPaymentTerms:    {"lower(coalesce(pt.name, ''))", "text"},
	}
	require.Len(t, wantExpr, len(model.InvoiceSortKeys), "every sort key must be covered by this test")

	for _, key := range model.InvoiceSortKeys {
		for _, desc := range []bool{false, true} {
			dir, cmp := "ASC", ">"
			if desc {
				dir, cmp = "DESC", "<"
			}
			t.Run(string(key)+"/"+dir, func(t *testing.T) {
				want := wantExpr[key]
				q := defaultQuery()
				q.Sort = model.InvoiceSort{Key: key, Desc: desc}

				// First page: no cursor.
				first, err := repository.BuildInvoiceListQuery(q)
				require.NoError(t, err)
				require.Contains(t, first.SQL, "ORDER BY "+sortKeyPrefix(want.expr, want.cast))
				require.Contains(t, first.SQL, " "+dir+", i.id "+dir+"\nLIMIT")
				require.NotContains(t, first.SQL, "i.id) "+cmp, "no keyset condition without a cursor")
				requirePlaceholdersMatchArgs(t, first)

				// Later page: keyset condition with the matching operator and cast.
				q.After = &model.InvoiceCursor{SortValue: "cursor-value", ID: 77}
				next, err := repository.BuildInvoiceListQuery(q)
				require.NoError(t, err)
				require.Contains(t, next.SQL, "("+sortKeyPrefix(want.expr, want.cast))
				require.Contains(t, next.SQL, ", i.id) "+cmp+" (($1::text)::"+want.cast+", $2::bigint)")
				require.Equal(t, []any{"cursor-value", int64(77), 51}, next.Args)
				requirePlaceholdersMatchArgs(t, next)
			})
		}
	}
}

func TestBuildInvoiceListQueryLifecycle(t *testing.T) {
	tests := []struct {
		name      string
		lifecycle model.Lifecycle
		want      string
	}{
		{"active", model.LifecycleActive, "i.deleted_at IS NULL AND i.archived_at IS NULL"},
		{"empty means active", "", "i.deleted_at IS NULL AND i.archived_at IS NULL"},
		{"archived", model.LifecycleArchived, "i.deleted_at IS NULL AND i.archived_at IS NOT NULL"},
		{"deleted", model.LifecycleDeleted, "i.deleted_at IS NOT NULL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultQuery()
			q.Filter.Lifecycle = tt.lifecycle
			stmt, err := repository.BuildInvoiceListQuery(q)
			require.NoError(t, err)
			require.Contains(t, stmt.SQL, tt.want)
		})
	}
}

func fullFilter() model.InvoiceFilter {
	return model.InvoiceFilter{
		DateFrom:        day(2026, 10, 1),
		DateTo:          day(2026, 10, 31),
		Lifecycle:       model.LifecycleActive,
		Statuses:        []model.InvoiceStatus{model.InvoicePaid, model.InvoiceSent},
		SignatureStatus: []model.SignatureStatus{model.SignatureSigned},
		ServiceTypeIDs:  []int64{1, 2},
		TagIDs:          []int64{3},
		PaymentTermIDs:  []int64{4, 5},
		Search:          "saliba",
	}
}

func TestBuildInvoiceListQueryFiltersAddClausesAndArgsInOrder(t *testing.T) {
	q := defaultQuery()
	q.Filter = fullFilter()
	q.After = &model.InvoiceCursor{SortValue: "2026-10-12", ID: 9}

	stmt, err := repository.BuildInvoiceListQuery(q)
	require.NoError(t, err)

	require.Equal(t, []any{
		day(2026, 10, 1),
		day(2026, 10, 31),
		[]string{"paid", "sent"},
		[]string{"signed"},
		[]int64{1, 2},
		[]int64{3},
		[]int64{4, 5},
		"%saliba%",
		"2026-10-12",
		int64(9),
		51,
	}, stmt.Args)

	for _, want := range []string{
		"i.document_date >= $1::date",
		"i.document_date <= $2::date",
		"i.status::text = ANY($3::text[])",
		"i.signature_status::text = ANY($4::text[])",
		"j.service_type_id = ANY($5::bigint[])",
		"tl.tag_id = ANY($6::bigint[])",
		"i.payment_terms_id = ANY($7::bigint[])",
		"i.document_number ILIKE $8",
		"c.account_number ILIKE $8",
		"c.display_name ILIKE $8",
		"i.billing_email ILIKE $8",
		"(($9::text)::date, $10::bigint)",
		"LIMIT $11",
	} {
		require.Contains(t, stmt.SQL, want)
	}
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestBuildInvoiceListQueryEmptyFiltersAddNothing(t *testing.T) {
	stmt, err := repository.BuildInvoiceListQuery(defaultQuery())
	require.NoError(t, err)

	for _, absent := range []string{"document_date >=", "document_date <=", "ANY(", "EXISTS", "ILIKE", "(($"} {
		require.NotContains(t, stmt.SQL, absent)
	}
	require.Len(t, stmt.Args, 1)
}

func TestBuildInvoiceListQuerySearchEscapesLikeWildcards(t *testing.T) {
	tests := []struct {
		search string
		want   string
	}{
		{"saliba", `%saliba%`},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
		{"  spaced  ", `%  spaced  %`},
	}

	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			q := defaultQuery()
			q.Filter.Search = tt.search
			stmt, err := repository.BuildInvoiceListQuery(q)
			require.NoError(t, err)
			require.Equal(t, tt.want, stmt.Args[0])
			require.Contains(t, stmt.SQL, `ESCAPE '\'`)
		})
	}
}

func TestBuildInvoiceListQueryRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.InvoiceQuery)
	}{
		{"unknown sort key", func(q *model.InvoiceQuery) { q.Sort.Key = "id; DROP TABLE invoices" }},
		{"empty sort key", func(q *model.InvoiceQuery) { q.Sort.Key = "" }},
		{"zero limit", func(q *model.InvoiceQuery) { q.Limit = 0 }},
		{"negative limit", func(q *model.InvoiceQuery) { q.Limit = -5 }},
		{"unknown lifecycle", func(q *model.InvoiceQuery) { q.Filter.Lifecycle = "everything" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := defaultQuery()
			tt.mutate(&q)
			_, err := repository.BuildInvoiceListQuery(q)
			require.ErrorIs(t, err, repository.ErrInvalidQuery)
		})
	}
}

func TestBuildInvoiceListQueryNeverPutsCallerTextInSQL(t *testing.T) {
	hostile := `'; DROP TABLE invoices; --`
	q := defaultQuery()
	q.Filter.Search = hostile
	q.Filter.Statuses = []model.InvoiceStatus{model.InvoiceStatus(hostile)}
	q.Filter.SignatureStatus = []model.SignatureStatus{model.SignatureStatus(hostile)}
	q.After = &model.InvoiceCursor{SortValue: hostile, ID: 1}

	stmt, err := repository.BuildInvoiceListQuery(q)
	require.NoError(t, err)

	require.NotContains(t, stmt.SQL, "DROP")
	require.NotContains(t, stmt.SQL, hostile)
	flat := ""
	for _, a := range stmt.Args {
		flat += strings.ToLower(strings.TrimSpace(toString(a))) + "|"
	}
	require.Contains(t, flat, "drop table invoices", "hostile text must travel as a bind argument")
	requirePlaceholdersMatchArgs(t, stmt)
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []string:
		return strings.Join(x, ",")
	default:
		return ""
	}
}

func TestBuildInvoiceSummaryQuery(t *testing.T) {
	stmt, err := repository.BuildInvoiceSummaryQuery(fullFilter())
	require.NoError(t, err)

	require.Contains(t, stmt.SQL, "count(*)")
	require.Contains(t, stmt.SQL, "CASE WHEN i.status = 'sent' THEN")
	require.Contains(t, stmt.SQL, "p.deleted_at IS NULL AND p.status = 'paid'")
	require.NotContains(t, stmt.SQL, "LATERAL", "payments must be summed per sent invoice, not joined for every invoice")
	require.NotContains(t, stmt.SQL, "ORDER BY")
	require.NotContains(t, stmt.SQL, "LIMIT")
	requirePlaceholdersMatchArgs(t, stmt)
}

func TestListAndSummaryShareTheSameFilterConditions(t *testing.T) {
	f := fullFilter()

	q := defaultQuery()
	q.Filter = f
	list, err := repository.BuildInvoiceListQuery(q)
	require.NoError(t, err)
	summary, err := repository.BuildInvoiceSummaryQuery(f)
	require.NoError(t, err)

	// Same arguments for the filter part (the list adds only its trailing LIMIT).
	require.Equal(t, list.Args[:len(list.Args)-1], summary.Args)

	// Same WHERE clause text.
	where := func(sql string) string {
		_, after, _ := strings.Cut(sql, "\nWHERE ")
		before, _, _ := strings.Cut(after, "\nORDER BY")
		return before
	}
	require.Equal(t, where(list.SQL), where(summary.SQL))
}

func TestBuildInvoiceSummaryQueryRejectsUnknownLifecycle(t *testing.T) {
	_, err := repository.BuildInvoiceSummaryQuery(model.InvoiceFilter{Lifecycle: "bogus"})
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}
