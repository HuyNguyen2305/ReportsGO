//go:build integration

package totalsalesrepo_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// env is a migrated, seeded throwaway database with a Total Sales repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.TotalSalesRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewTotalSalesRepository(pool)}
}

func october() model.TotalSaleFilter {
	return model.TotalSaleFilter{DateFrom: day(2026, 10, 1), DateTo: day(2026, 10, 31)}
}

func query(f model.TotalSaleFilter, key model.TotalSaleSortKey, desc bool, limit int) model.TotalSaleQuery {
	return model.TotalSaleQuery{Filter: f, Sort: model.TotalSaleSort{Key: key, Desc: desc}, Limit: limit}
}

// listAll returns every row of the filtered set in one page.
func (e env) listAll(t *testing.T, f model.TotalSaleFilter, key model.TotalSaleSortKey, desc bool) []model.TotalSaleRow {
	t.Helper()
	page, err := e.repo.Page(context.Background(), query(f, key, desc, 1000), false)
	require.NoError(t, err)
	return page.Rows
}

func byNumber(rows []model.TotalSaleRow) map[string]model.TotalSaleRow {
	out := make(map[string]model.TotalSaleRow, len(rows))
	for _, r := range rows {
		out[r.InvoiceNumber] = r
	}
	return out
}

func numbers(rows []model.TotalSaleRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.InvoiceNumber
	}
	return out
}

func ids(rows []model.TotalSaleRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// octoberSales is the sales set of October 2026 in the seed: 9 paid invoices and 3 sent ones. The draft and void
// invoices, the archived one, the deleted one and the September one are not sales.
const octoberSales = 12

func TestPageDefaultReturnsActiveSentAndPaidOctoberInvoices(t *testing.T) {
	e := newEnv(t)

	rows := e.listAll(t, october(), model.TotalSaleSortDate, true)

	require.Len(t, rows, octoberSales)
	got := byNumber(rows)
	for _, n := range []string{"85011", "85010", "85009", "85000", "85005", "85004", "84985", "84979", "84975", "85031", "85032", "85033"} {
		require.Contains(t, got, n)
	}
	for _, n := range []string{"84936", "85030", "84942", "85028", "85007", "84937", "84984", "84983", "84982", "85034", "85035", "84890"} {
		require.NotContains(t, got, n, "invoice %s is a draft, void, archived, deleted or outside October", n)
	}
	require.True(t, sort.SliceIsSorted(rows, func(i, j int) bool { return rows[i].Date.After(rows[j].Date) }),
		"newest first")
}

func TestPageDerivesTheStatusFromPayments(t *testing.T) {
	e := newEnv(t)
	got := byNumber(e.listAll(t, october(), model.TotalSaleSortDate, true))

	require.Equal(t, model.TotalSalePaid, got["85009"].Status)
	require.Equal(t, model.TotalSalePartial, got["85032"].Status, "100.00 invoice with 30.00 paid")
	require.Equal(t, model.TotalSaleSent, got["85031"].Status)
	require.Equal(t, model.TotalSaleSent, got["85033"].Status)

	counts := map[model.TotalSaleStatus]int{}
	for _, r := range got {
		counts[r.Status]++
	}
	require.Equal(t, map[model.TotalSaleStatus]int{model.TotalSalePaid: 9, model.TotalSalePartial: 1, model.TotalSaleSent: 2}, counts)
}

func TestPageAmountAndAmountWithoutTax(t *testing.T) {
	e := newEnv(t)
	got := byNumber(e.listAll(t, october(), model.TotalSaleSortDate, true))

	tests := []struct {
		number             string
		amount, withoutTax model.Money
	}{
		{"85009", 14856, 13376}, // two tax lines: 10.00 + 4.80
		{"85004", 13863, 10863}, // 30.00
		{"85010", 1963, 1753},   // 2.10
		{"85032", 10000, 9040},  // 9.60
		{"85031", 9500, 8550},   // 9.50
		{"85000", 6500, 5850},   // 6.50
	}
	for _, tt := range tests {
		t.Run(tt.number, func(t *testing.T) {
			require.Equal(t, tt.amount, got[tt.number].Amount)
			require.Equal(t, tt.withoutTax, got[tt.number].WithoutTax)
		})
	}
}

func TestPageSummaryTotalsTheWholeFilteredSet(t *testing.T) {
	e := newEnv(t)

	page, err := e.repo.Page(context.Background(), query(october(), model.TotalSaleSortDate, true, 5), true)
	require.NoError(t, err)

	require.Len(t, page.Rows, 6, "limit+1 rows: the extra one tells the caller another page exists")
	require.NotNil(t, page.Summary)
	require.Equal(t, int64(octoberSales), page.Summary.Count, "the summary covers the whole set, not the page")
	require.Equal(t, model.Money(107506), page.Summary.Amount)
	require.Equal(t, model.Money(91136), page.Summary.WithoutTax)

	t.Run("it equals the sum of every row", func(t *testing.T) {
		var amount, without model.Money
		for _, r := range e.listAll(t, october(), model.TotalSaleSortDate, true) {
			amount += r.Amount
			without += r.WithoutTax
		}
		require.Equal(t, page.Summary.Amount, amount)
		require.Equal(t, page.Summary.WithoutTax, without)
	})

	t.Run("it is left out when not requested", func(t *testing.T) {
		page, err := e.repo.Page(context.Background(), query(october(), model.TotalSaleSortDate, true, 5), false)
		require.NoError(t, err)
		require.Nil(t, page.Summary)
	})
}

func TestPageSummaryOfAnEmptySetIsZero(t *testing.T) {
	e := newEnv(t)
	f := model.TotalSaleFilter{DateFrom: day(2030, 1, 1), DateTo: day(2030, 1, 31)}

	page, err := e.repo.Page(context.Background(), query(f, model.TotalSaleSortDate, true, 10), true)

	require.NoError(t, err)
	require.Empty(t, page.Rows)
	require.Equal(t, &model.TotalSaleSummary{}, page.Summary)
}

func TestPageFilters(t *testing.T) {
	e := newEnv(t)
	var saliba int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT id FROM customers WHERE account_number = '5001'`).Scan(&saliba))
	var aaa int64
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT id FROM customers WHERE account_number = '63706'`).Scan(&aaa))

	tests := []struct {
		name   string
		mutate func(*model.TotalSaleFilter)
		want   []string // invoice numbers, any order
	}{
		{"status partial", func(f *model.TotalSaleFilter) { f.Statuses = []model.TotalSaleStatus{model.TotalSalePartial} }, []string{"85032"}},
		{"status sent", func(f *model.TotalSaleFilter) { f.Statuses = []model.TotalSaleStatus{model.TotalSaleSent} }, []string{"85031", "85033"}},
		{"status sent or partial", func(f *model.TotalSaleFilter) {
			f.Statuses = []model.TotalSaleStatus{model.TotalSaleSent, model.TotalSalePartial}
		}, []string{"85031", "85032", "85033"}},
		{"one customer", func(f *model.TotalSaleFilter) { f.CustomerIDs = []int64{saliba} },
			[]string{"85000", "84985", "84979", "84975", "85031"}},
		{"two customers and a status", func(f *model.TotalSaleFilter) {
			f.CustomerIDs = []int64{saliba, aaa}
			f.Statuses = []model.TotalSaleStatus{model.TotalSaleSent, model.TotalSalePartial}
		}, []string{"85031", "85032"}},
		{"search by invoice number", func(f *model.TotalSaleFilter) { f.Search = "85004" }, []string{"85004"}},
		{"search by customer name, any case", func(f *model.TotalSaleFilter) { f.Search = "WILLIAM sal" },
			[]string{"85000", "84985", "84979", "84975", "85031"}},
		{"search by account number", func(f *model.TotalSaleFilter) { f.Search = "63706" },
			[]string{"85011", "85010", "85009", "85004", "85032"}},
		{"search matches wildcards literally", func(f *model.TotalSaleFilter) { f.Search = "%" }, nil},
		{"a day range", func(f *model.TotalSaleFilter) { f.DateFrom, f.DateTo = day(2026, 10, 12), day(2026, 10, 12) },
			[]string{"85009", "85000"}},
		{"open start", func(f *model.TotalSaleFilter) { f.DateFrom = time.Time{}; f.DateTo = day(2026, 10, 9) },
			[]string{"84975"}},
		{"open end", func(f *model.TotalSaleFilter) { f.DateFrom = day(2026, 10, 20); f.DateTo = time.Time{} },
			[]string{"85031", "85032"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := october()
			tt.mutate(&f)
			rows := e.listAll(t, f, model.TotalSaleSortInvoiceNumber, false)
			require.ElementsMatch(t, tt.want, numbers(rows))
		})
	}
}

func TestPagePaymentsDecideTheStatus(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	status := func() model.TotalSaleStatus {
		rows := e.listAll(t, model.TotalSaleFilter{Search: "85032", DateFrom: day(2026, 10, 1), DateTo: day(2026, 10, 31)}, model.TotalSaleSortDate, true)
		require.Len(t, rows, 1)
		return rows[0].Status
	}
	insert := func(amount, paymentStatus, deletedAt string) {
		_, err := e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date, status, deleted_at)
			SELECT customer_id, id, 'check', $1::numeric, '2026-10-22', $2::payment_status, $3::timestamptz
			FROM billing_documents WHERE parent_type = 'invoice' AND document_number = '85032'`, amount, paymentStatus, nullable(deletedAt))
		require.NoError(t, err)
	}

	require.Equal(t, model.TotalSalePartial, status(), "30.00 of 100.00 paid")

	insert("70.00", "failed", "")
	require.Equal(t, model.TotalSalePartial, status(), "a failed payment does not count")
	insert("70.00", "pending", "")
	require.Equal(t, model.TotalSalePartial, status(), "a pending payment does not count")
	insert("70.00", "paid", "2026-10-23 00:00:00+00")
	require.Equal(t, model.TotalSalePartial, status(), "a deleted payment does not count")

	insert("69.99", "paid", "")
	require.Equal(t, model.TotalSalePartial, status(), "99.99 of 100.00 is still partial")
	insert("0.01", "paid", "")
	require.Equal(t, model.TotalSalePaid, status(), "the payments now cover the total")
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func TestPageWithoutTaxEqualsTheAmountWhenAnInvoiceHasNoTaxLines(t *testing.T) {
	e := newEnv(t)
	_, err := e.pool.Exec(context.Background(), `DELETE FROM invoice_tax_lines
		WHERE invoice_id = (SELECT id FROM billing_documents WHERE parent_type = 'invoice' AND document_number = '85031')`)
	require.NoError(t, err)

	rows := e.listAll(t, model.TotalSaleFilter{Search: "85031", DateFrom: day(2026, 10, 1), DateTo: day(2026, 10, 31)}, model.TotalSaleSortDate, true)

	require.Len(t, rows, 1)
	require.Equal(t, model.Money(9500), rows[0].Amount)
	require.Equal(t, rows[0].Amount, rows[0].WithoutTax)
}

func TestPageLeavesOutEstimatesAndInvoicesInOtherStates(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// an estimate that shares a number with a sale, and an invoice that is archived after the fact
	_, err := e.pool.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total, status)
		SELECT 'estimate', '85032', customer_id, '2026-10-20', 50, 'sent' FROM billing_documents WHERE document_number = '85031'`)
	require.NoError(t, err)
	_, err = e.pool.Exec(ctx, `UPDATE billing_documents SET archived_at = now() WHERE parent_type = 'invoice' AND document_number = '85033'`)
	require.NoError(t, err)

	got := byNumber(e.listAll(t, october(), model.TotalSaleSortDate, true))

	require.Len(t, got, octoberSales-1)
	require.NotContains(t, got, "85033", "an archived invoice is not a sale")
	require.Equal(t, model.Money(10000), got["85032"].Amount, "the estimate with the same number is not the sale")
}

func TestPagePagesThroughEverySortKeyInBothDirectionsWithoutGapsOrRepeats(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	for _, key := range model.TotalSaleSortKeys {
		for _, desc := range []bool{false, true} {
			name := string(key)
			if desc {
				name += " desc"
			}
			t.Run(name, func(t *testing.T) {
				want := ids(e.listAll(t, october(), key, desc))
				require.Len(t, want, octoberSales)

				var got []int64
				var after *model.TotalSaleCursor
				for pages := 0; pages < 10; pages++ {
					q := query(october(), key, desc, 5)
					q.After = after
					page, err := e.repo.Page(ctx, q, false)
					require.NoError(t, err)

					rows := page.Rows
					more := len(rows) > 5
					if more {
						rows = rows[:5]
					}
					got = append(got, ids(rows)...)
					if !more {
						break
					}
					last := rows[len(rows)-1]
					after = &model.TotalSaleCursor{SortValue: last.SortValue, ID: last.ID}
				}
				require.Equal(t, want, got, "paging must visit every row exactly once, in the same order")
			})
		}
	}
}

func TestPageRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)

	_, err := e.repo.Page(context.Background(), query(october(), "nope", false, 10), false)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	_, err = e.repo.Page(context.Background(), query(october(), model.TotalSaleSortDate, false, 0), true)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

func TestPageTreatsHostileSearchTextAsData(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = `'; DROP TABLE billing_documents; --`

	rows := e.listAll(t, f, model.TotalSaleSortDate, true)

	require.Empty(t, rows)
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT count(*) FROM billing_documents`).Scan(&n))
	require.Positive(t, n, "the table is still there")
}

func TestPageSummaryAndRowsComeFromOneSnapshot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// A repository bound to a transaction uses the caller's snapshot: a sale committed by another connection after
	// the transaction began is in neither the rows nor the summary.
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	var warm int
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM billing_documents`).Scan(&warm)) // the snapshot starts here

	_, err = e.pool.Exec(ctx, `INSERT INTO billing_documents (document_number, customer_id, document_date, total, status)
		SELECT '99001', customer_id, '2026-10-20', 10, 'sent' FROM billing_documents WHERE document_number = '85031'`)
	require.NoError(t, err)

	page, err := repository.NewTotalSalesRepository(tx).Page(ctx, query(october(), model.TotalSaleSortDate, true, 100), true)

	require.NoError(t, err)
	require.Len(t, page.Rows, octoberSales)
	require.Equal(t, int64(octoberSales), page.Summary.Count)

	// outside the snapshot the new sale is visible, in rows and summary alike
	after, err := e.repo.Page(ctx, query(october(), model.TotalSaleSortDate, true, 100), true)
	require.NoError(t, err)
	require.Len(t, after.Rows, octoberSales+1)
	require.Equal(t, int64(octoberSales+1), after.Summary.Count)
}
