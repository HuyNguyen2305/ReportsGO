//go:build integration

package paymentscollectedrepo_test

import (
	"context"
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

// env is a migrated, seeded throwaway database with a Payments Collected repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.PaymentsCollectedRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewPaymentsCollectedRepository(pool)}
}

func october() model.PaymentCollectedFilter {
	return model.PaymentCollectedFilter{
		DateFrom:   day(2026, 10, 1),
		DateTo:     day(2026, 10, 31),
		PaymentFor: model.PaymentForInvoices,
	}
}

func query(f model.PaymentCollectedFilter, key model.PaymentCollectedSortKey, desc bool, limit int) model.PaymentCollectedQuery {
	return model.PaymentCollectedQuery{Filter: f, Sort: model.PaymentCollectedSort{Key: key, Desc: desc}, Limit: limit}
}

// listAll returns every row of the filtered set in one page.
func (e env) listAll(t *testing.T, f model.PaymentCollectedFilter, key model.PaymentCollectedSortKey, desc bool) []model.PaymentCollectedRow {
	t.Helper()
	page, err := e.repo.Page(context.Background(), query(f, key, desc, 1000), false)
	require.NoError(t, err)
	return page.Rows
}

func ids(rows []model.PaymentCollectedRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func labels(rows []model.PaymentCollectedRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		if r.InvoiceNumber != nil {
			out[i] = *r.InvoiceNumber
		} else {
			out[i] = "credit"
		}
	}
	return out
}

// the paid, not deleted invoice payments of October 2026 in the seed: 9 for the paid invoices plus the 30.00 part
// payment of invoice 85032. The pending, failed, refunded and deleted payments and the one dated 09-30 are outside.
const octoberInvoicePayments = 10

func TestPageDefaultReturnsCollectedOctoberInvoicePayments(t *testing.T) {
	e := newEnv(t)

	rows := e.listAll(t, october(), model.PaymentCollectedSortDate, true)

	require.Len(t, rows, octoberInvoicePayments)
	require.ElementsMatch(t,
		[]string{"85009", "85004", "85010", "85011", "85000", "85005", "84985", "84979", "84975", "85032"}, labels(rows))
	for _, r := range rows {
		require.NotNil(t, r.InvoiceID, "the default view is the invoices view")
	}

	t.Run("it agrees with an independent count", func(t *testing.T) {
		var n int
		require.NoError(t, e.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM payments WHERE status = 'paid' AND deleted_at IS NULL AND invoice_id IS NOT NULL
			   AND payment_date BETWEEN '2026-10-01' AND '2026-10-31'`).Scan(&n))
		require.Equal(t, n, len(rows))
	})
}

func TestPageCreditsView(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.PaymentFor = model.PaymentForCredits

	page, err := e.repo.Page(context.Background(), query(f, model.PaymentCollectedSortAmount, true, 100), true)

	require.NoError(t, err)
	require.Len(t, page.Rows, 2)
	require.Equal(t, []string{"credit", "credit"}, labels(page.Rows))
	for _, r := range page.Rows {
		require.Nil(t, r.InvoiceID)
		require.Nil(t, r.ServiceState, "a credit has no job, so no service state")
		require.Nil(t, r.StaffID, "the seed has no staff on credit payments")
		require.Equal(t, r.Amount, r.WithoutTax, "a credit has no tax")
	}
	require.Equal(t, model.Money(20000), page.Rows[0].Amount)
	require.Equal(t, &model.PaymentCollectedSummary{Count: 2, Amount: 30000, WithoutTax: 30000}, page.Summary)
}

func TestPageAmountAndAmountWithoutTax(t *testing.T) {
	e := newEnv(t)
	got := map[string]model.PaymentCollectedRow{}
	for _, r := range e.listAll(t, october(), model.PaymentCollectedSortDate, true) {
		got[*r.InvoiceNumber] = r
	}

	tests := []struct {
		number             string
		amount, withoutTax model.Money
	}{
		{"85009", 14856, 13376}, // tax 14.80 of 148.56
		{"85004", 13863, 10863}, // tax 30.00
		{"85010", 1963, 1753},   // tax 2.10
		{"85011", 6198, 5598},   // tax 6.00
		{"85005", 15226, 11626}, // tax 36.00
		{"85000", 6500, 5850},   // tax 6.50
		// a 30.00 part payment of a 100.00 invoice with 9.60 tax: 30.00 * 90.40 / 100.00 = 27.12
		{"85032", 3000, 2712},
	}
	for _, tt := range tests {
		t.Run(tt.number, func(t *testing.T) {
			require.Equal(t, tt.amount, got[tt.number].Amount)
			require.Equal(t, tt.withoutTax, got[tt.number].WithoutTax)
		})
	}
}

func TestPageWithoutTaxForInvoicesWithNoTaxOrAZeroTotal(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.pool.Exec(ctx, `INSERT INTO billing_documents (document_number, customer_id, document_date, total, status)
		SELECT '99001', customer_id, '2026-10-20', 0, 'paid' FROM billing_documents WHERE document_number = '85031'`)
	require.NoError(t, err)
	_, err = e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date)
		SELECT customer_id, id, 'cash', 5.00, '2026-10-20' FROM billing_documents WHERE document_number = '99001'`)
	require.NoError(t, err)
	_, err = e.pool.Exec(ctx, `DELETE FROM invoice_tax_lines
		WHERE invoice_id = (SELECT id FROM billing_documents WHERE parent_type = 'invoice' AND document_number = '85000')`)
	require.NoError(t, err)

	for _, r := range e.listAll(t, october(), model.PaymentCollectedSortDate, true) {
		switch *r.InvoiceNumber {
		case "99001":
			require.Equal(t, model.Money(500), r.WithoutTax, "a zero-total invoice has no tax share")
		case "85000":
			require.Equal(t, r.Amount, r.WithoutTax, "an invoice with no tax lines")
		}
	}
}

func TestPageSummaryTotalsTheWholeFilteredSet(t *testing.T) {
	e := newEnv(t)

	page, err := e.repo.Page(context.Background(), query(october(), model.PaymentCollectedSortDate, true, 4), true)
	require.NoError(t, err)

	require.Len(t, page.Rows, 5, "limit+1 rows: the extra one tells the caller another page exists")
	require.Equal(t, &model.PaymentCollectedSummary{Count: octoberInvoicePayments, Amount: 81106, WithoutTax: 69328}, page.Summary,
		"the summary covers the whole set, not the page")

	t.Run("it equals the sum of every row", func(t *testing.T) {
		var amount, without model.Money
		for _, r := range e.listAll(t, october(), model.PaymentCollectedSortDate, true) {
			amount += r.Amount
			without += r.WithoutTax
		}
		require.Equal(t, page.Summary.Amount, amount)
		require.Equal(t, page.Summary.WithoutTax, without, "the footer adds up the rounded row values")
	})

	t.Run("it is left out when not requested", func(t *testing.T) {
		page, err := e.repo.Page(context.Background(), query(october(), model.PaymentCollectedSortDate, true, 4), false)
		require.NoError(t, err)
		require.Nil(t, page.Summary)
	})
}

func TestPageSummaryOfAnEmptySetIsZero(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.DateFrom, f.DateTo = day(2030, 1, 1), day(2030, 1, 31)

	page, err := e.repo.Page(context.Background(), query(f, model.PaymentCollectedSortDate, true, 10), true)

	require.NoError(t, err)
	require.Empty(t, page.Rows)
	require.Equal(t, &model.PaymentCollectedSummary{}, page.Summary)
}

func TestPageLeavesOutPaymentsThatAreNotCollected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	count := func() int { return len(e.listAll(t, october(), model.PaymentCollectedSortDate, true)) }
	require.Equal(t, octoberInvoicePayments, count())

	insert := func(status, deletedAt string) {
		var deleted any
		if deletedAt != "" {
			deleted = deletedAt
		}
		_, err := e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date, status, deleted_at)
			SELECT customer_id, id, 'cash', 1, '2026-10-20', $1::payment_status, $2::timestamptz
			FROM billing_documents WHERE parent_type = 'invoice' AND document_number = '85000'`, status, deleted)
		require.NoError(t, err)
	}
	for _, s := range []string{"pending", "failed", "refunded"} {
		insert(s, "")
		require.Equal(t, octoberInvoicePayments, count(), "a %s payment is not collected", s)
	}
	insert("paid", "2026-10-21 00:00:00+00")
	require.Equal(t, octoberInvoicePayments, count(), "a deleted payment is not collected")

	insert("paid", "")
	require.Equal(t, octoberInvoicePayments+1, count())
}

func TestPageServiceStateComesFromTheInvoicesJobLocation(t *testing.T) {
	e := newEnv(t)
	byInvoice := map[string]model.PaymentCollectedRow{}
	for _, r := range e.listAll(t, october(), model.PaymentCollectedSortDate, true) {
		byInvoice[*r.InvoiceNumber] = r
	}

	require.Equal(t, "FL", *byInvoice["85009"].ServiceState, "customer 63706 is served in Miami")
	require.Equal(t, "TX", *byInvoice["85000"].ServiceState, "customer 5001 is served in San Antonio")
	require.Equal(t, "State", *byInvoice["85005"].ServiceState)

	t.Run("an empty state is null", func(t *testing.T) {
		_, err := e.pool.Exec(context.Background(), `UPDATE service_locations SET state = '' WHERE customer_id =
			(SELECT customer_id FROM billing_documents WHERE document_number = '85000' AND parent_type = 'invoice')`)
		require.NoError(t, err)

		for _, r := range e.listAll(t, october(), model.PaymentCollectedSortDate, true) {
			if *r.InvoiceNumber == "85000" {
				require.Nil(t, r.ServiceState)
			}
		}
	})

	t.Run("no job means no state", func(t *testing.T) {
		_, err := e.pool.Exec(context.Background(), `UPDATE billing_documents SET job_id = NULL
			WHERE parent_type = 'invoice' AND document_number = '85009'`)
		require.NoError(t, err)

		for _, r := range e.listAll(t, october(), model.PaymentCollectedSortDate, true) {
			if *r.InvoiceNumber == "85009" {
				require.Nil(t, r.ServiceState)
				require.Equal(t, model.Money(14856), r.Amount, "the payment itself is still listed")
			}
		}
	})
}

func TestPageStaffIsOnCashAndCheckPaymentsOnly(t *testing.T) {
	e := newEnv(t)

	for _, r := range e.listAll(t, october(), model.PaymentCollectedSortDate, true) {
		switch r.Method {
		case model.MethodCard:
			require.Nil(t, r.StaffID, "online card payments have no staff: %s", *r.InvoiceNumber)
			require.Nil(t, r.StaffName)
		default:
			require.NotNil(t, r.StaffID, "payment for %s", *r.InvoiceNumber)
			require.NotEmpty(t, *r.StaffName)
		}
	}
}

func TestPageFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	var saliba, aaa int64
	require.NoError(t, e.pool.QueryRow(ctx, `SELECT id FROM customers WHERE account_number = '5001'`).Scan(&saliba))
	require.NoError(t, e.pool.QueryRow(ctx, `SELECT id FROM customers WHERE account_number = '63706'`).Scan(&aaa))
	var staff int64
	require.NoError(t, e.pool.QueryRow(ctx, `SELECT received_by FROM payments
		WHERE received_by IS NOT NULL ORDER BY id LIMIT 1`).Scan(&staff))
	var staffCount int
	require.NoError(t, e.pool.QueryRow(ctx, `SELECT count(*) FROM payments WHERE received_by = $1 AND status = 'paid'
		AND deleted_at IS NULL AND invoice_id IS NOT NULL AND payment_date BETWEEN '2026-10-01' AND '2026-10-31'`, staff).Scan(&staffCount))
	require.Positive(t, staffCount)

	tests := []struct {
		name   string
		mutate func(*model.PaymentCollectedFilter)
		want   []string // invoice numbers, any order
	}{
		{"card", func(f *model.PaymentCollectedFilter) { f.Methods = []model.PaymentMethod{model.MethodCard} },
			[]string{"85004", "85010", "85011", "85005"}},
		{"cash", func(f *model.PaymentCollectedFilter) { f.Methods = []model.PaymentMethod{model.MethodCash} }, []string{"85009"}},
		{"cash or check", func(f *model.PaymentCollectedFilter) {
			f.Methods = []model.PaymentMethod{model.MethodCash, model.MethodCheck}
		}, []string{"85009", "85000", "84985", "84979", "84975", "85032"}},
		{"one customer", func(f *model.PaymentCollectedFilter) { f.CustomerIDs = []int64{saliba} },
			[]string{"85000", "84985", "84979", "84975"}},
		{"another customer and a method", func(f *model.PaymentCollectedFilter) {
			f.CustomerIDs = []int64{aaa}
			f.Methods = []model.PaymentMethod{model.MethodCard}
		}, []string{"85004", "85010", "85011"}},
		{"search by invoice number", func(f *model.PaymentCollectedFilter) { f.Search = "85004" }, []string{"85004"}},
		{"search by customer name, any case", func(f *model.PaymentCollectedFilter) { f.Search = "WILLIAM sal" },
			[]string{"85000", "84985", "84979", "84975"}},
		{"search matches wildcards literally", func(f *model.PaymentCollectedFilter) { f.Search = "%" }, nil},
		{"a day range", func(f *model.PaymentCollectedFilter) { f.DateFrom, f.DateTo = day(2026, 10, 15), day(2026, 10, 15) },
			[]string{"85009", "85004", "85010", "85011"}},
		{"open start", func(f *model.PaymentCollectedFilter) { f.DateFrom = time.Time{}; f.DateTo = day(2026, 10, 8) },
			[]string{"84975", "85000"}}, // 85000's 10.00 check on 09-30 is paid too
		{"open end", func(f *model.PaymentCollectedFilter) { f.DateFrom = day(2026, 10, 21); f.DateTo = time.Time{} }, []string{"85032"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := october()
			tt.mutate(&f)
			require.ElementsMatch(t, tt.want, labels(e.listAll(t, f, model.PaymentCollectedSortDate, true)))
		})
	}

	t.Run("staff", func(t *testing.T) {
		f := october()
		f.StaffIDs = []int64{staff}
		rows := e.listAll(t, f, model.PaymentCollectedSortDate, true)
		require.Len(t, rows, staffCount)
		for _, r := range rows {
			require.Equal(t, staff, *r.StaffID)
		}
	})

	t.Run("staff that took nothing", func(t *testing.T) {
		f := october()
		f.StaffIDs = []int64{999999}
		require.Empty(t, e.listAll(t, f, model.PaymentCollectedSortDate, true))
	})
}

func TestPagePagesThroughEverySortKeyInBothDirectionsWithoutGapsOrRepeats(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	for _, key := range model.PaymentCollectedSortKeys {
		for _, desc := range []bool{false, true} {
			name := string(key)
			if desc {
				name += " desc"
			}
			t.Run(name, func(t *testing.T) {
				want := ids(e.listAll(t, october(), key, desc))
				require.Len(t, want, octoberInvoicePayments)

				var got []int64
				var after *model.PaymentCollectedCursor
				for pages := 0; pages < 10; pages++ {
					q := query(october(), key, desc, 4)
					q.After = after
					page, err := e.repo.Page(ctx, q, false)
					require.NoError(t, err)

					rows := page.Rows
					more := len(rows) > 4
					if more {
						rows = rows[:4]
					}
					got = append(got, ids(rows)...)
					if !more {
						break
					}
					last := rows[len(rows)-1]
					after = &model.PaymentCollectedCursor{SortValue: last.SortValue, ID: last.ID}
				}
				require.Equal(t, want, got, "paging must visit every row exactly once, in the same order")
			})
		}
	}
}

func TestPageSortsMissingStaffAndStateAsEmptyValuesFirstWhenAscending(t *testing.T) {
	e := newEnv(t)

	byStaff := e.listAll(t, october(), model.PaymentCollectedSortStaff, false)
	require.Nil(t, byStaff[0].StaffID, "payments without staff sort first")
	require.NotNil(t, byStaff[len(byStaff)-1].StaffID)

	desc := e.listAll(t, october(), model.PaymentCollectedSortStaff, true)
	require.NotNil(t, desc[0].StaffID)
}

func TestStaffListsEveryUserByName(t *testing.T) {
	e := newEnv(t)

	staff, err := e.repo.Staff(context.Background())

	require.NoError(t, err)
	require.Len(t, staff, 2)
	names := []string{staff[0].Name, staff[1].Name}
	require.ElementsMatch(t, []string{"Thọ Nguyễn Quang", "thon nguyen"}, names)
	for _, s := range staff {
		require.Positive(t, s.ID)
	}
}

func TestPageRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.repo.Page(ctx, query(october(), "nope", false, 10), false)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	_, err = e.repo.Page(ctx, query(october(), model.PaymentCollectedSortDate, false, 0), true)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	f := october()
	f.PaymentFor = "everything"
	_, err = e.repo.Page(ctx, query(f, model.PaymentCollectedSortDate, false, 10), true)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

func TestPageTreatsHostileSearchTextAsData(t *testing.T) {
	e := newEnv(t)
	f := october()
	f.Search = `'; DROP TABLE payments; --`

	rows := e.listAll(t, f, model.PaymentCollectedSortDate, true)

	require.Empty(t, rows)
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT count(*) FROM payments`).Scan(&n))
	require.Positive(t, n, "the table is still there")
}

func TestPageSummaryAndRowsComeFromOneSnapshot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// A repository bound to a transaction uses the caller's snapshot: a payment committed by another connection
	// after the transaction began is in neither the rows nor the summary.
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	var warm int
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM payments`).Scan(&warm)) // the snapshot starts here

	_, err = e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date)
		SELECT customer_id, id, 'cash', 10, '2026-10-20' FROM billing_documents WHERE document_number = '85031'`)
	require.NoError(t, err)

	page, err := repository.NewPaymentsCollectedRepository(tx).Page(ctx, query(october(), model.PaymentCollectedSortDate, true, 100), true)

	require.NoError(t, err)
	require.Len(t, page.Rows, octoberInvoicePayments)
	require.Equal(t, int64(octoberInvoicePayments), page.Summary.Count)

	// outside the snapshot the new payment is visible, in rows and summary alike
	after, err := e.repo.Page(ctx, query(october(), model.PaymentCollectedSortDate, true, 100), true)
	require.NoError(t, err)
	require.Len(t, after.Rows, octoberInvoicePayments+1)
	require.Equal(t, int64(octoberInvoicePayments+1), after.Summary.Count)
}
