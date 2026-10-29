//go:build integration

package salestaxrepo_test

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

// env is a migrated, seeded throwaway database with a Sales Tax repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.SalesTaxRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testdb.SeededPool(t)
	return env{pool: pool, repo: repository.NewSalesTaxRepository(pool)}
}

func october(basis model.SalesTaxBasis) model.SalesTaxFilter {
	return model.SalesTaxFilter{DateFrom: day(2026, 10, 1), DateTo: day(2026, 10, 31), Basis: basis}
}

var byTaxableSalesDesc = model.SalesTaxSort{Key: model.SalesTaxSortTaxableSales, Desc: true}

func (e env) summary(t *testing.T, f model.SalesTaxFilter, s model.SalesTaxSort) []model.SalesTaxRow {
	t.Helper()
	rows, err := e.repo.Summary(context.Background(), f, s)
	require.NoError(t, err)
	return rows
}

func byName(rows []model.SalesTaxRow) map[string]model.SalesTaxRow {
	out := make(map[string]model.SalesTaxRow, len(rows))
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

func names(rows []model.SalesTaxRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

func ids(rows []model.SalesTaxRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func total(rows []model.SalesTaxRow) (taxable, tax, surcharge model.Money) {
	for _, r := range rows {
		taxable += r.TaxableSales
		tax += r.TaxCollected
		surcharge += r.SurchargeTax
	}
	return taxable, tax, surcharge
}

// the ten taxes of the seed, in creation order
var allTaxes = []string{"VAT", "VN", "d12", "VAT2", "d11", "tAAA", "Tax 32", "test 2", "Test", "US"}

func TestSummaryCollectedBasisInOctober(t *testing.T) {
	e := newEnv(t)

	rows := e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc)

	// the three taxes with activity first, then the idle ones in creation order
	require.Equal(t, allTaxes, names(rows))
	got := byName(rows)
	// VAT: 85009 100.00 + 85011 60.00 + the four 65.00 invoices; its 2.92 surcharge is on 85009
	require.Equal(t, model.SalesTaxRow{ID: 1, Name: "VAT", Rate: "10.00", TaxableSales: 42000, TaxCollected: 4200, SurchargeTax: 292}, got["VAT"])
	require.Equal(t, model.SalesTaxRow{ID: 2, Name: "VN", Rate: "30.00", TaxableSales: 22000, TaxCollected: 6600}, got["VN"])
	// d12: 40.00 (85009) + 17.50 (85010) + 24.00, the 30.00 part payment of 85032's 80.00 taxable and 9.60 tax
	require.Equal(t, model.SalesTaxRow{ID: 3, Name: "d12", Rate: "12.00", TaxableSales: 8150, TaxCollected: 978}, got["d12"])
	for _, idle := range allTaxes[3:] {
		require.Zero(t, got[idle].TaxableSales+got[idle].TaxCollected+got[idle].SurchargeTax, idle)
	}
	taxable, tax, surcharge := total(rows)
	require.Equal(t, []model.Money{72150, 11778, 292}, []model.Money{taxable, tax, surcharge})
}

func TestSummaryInvoicedBasisInOctober(t *testing.T) {
	e := newEnv(t)

	rows := e.summary(t, october(model.SalesTaxInvoiced), byTaxableSalesDesc)

	require.Equal(t, allTaxes, names(rows))
	got := byName(rows)
	require.Equal(t, model.SalesTaxRow{ID: 1, Name: "VAT", Rate: "10.00", TaxableSales: 51500, TaxCollected: 5150, SurchargeTax: 292}, got["VAT"])
	require.Equal(t, model.SalesTaxRow{ID: 2, Name: "VN", Rate: "30.00", TaxableSales: 31900, TaxCollected: 9570}, got["VN"])
	require.Equal(t, model.SalesTaxRow{ID: 3, Name: "d12", Rate: "12.00", TaxableSales: 13750, TaxCollected: 1650}, got["d12"])
	taxable, tax, surcharge := total(rows)
	require.Equal(t, []model.Money{97150, 16370, 292}, []model.Money{taxable, tax, surcharge})
}

func TestSummaryCollectedFollowsThePaymentDate(t *testing.T) {
	e := newEnv(t)
	f := model.SalesTaxFilter{DateFrom: day(2026, 9, 1), DateTo: day(2026, 9, 30), Basis: model.SalesTaxCollected}

	rows := byName(e.summary(t, f, byTaxableSalesDesc))

	// the only September payment is 10.00 of invoice 85000 (65.00 with 6.50 VAT): 10/65 of it, though the invoice is
	// dated in October
	require.Equal(t, model.Money(1000), rows["VAT"].TaxableSales)
	require.Equal(t, model.Money(100), rows["VAT"].TaxCollected)
	require.Zero(t, rows["VN"].TaxableSales)
}

func TestSummaryInvoicedFollowsTheInvoiceDate(t *testing.T) {
	e := newEnv(t)
	f := model.SalesTaxFilter{DateFrom: day(2026, 9, 1), DateTo: day(2026, 9, 30), Basis: model.SalesTaxInvoiced}

	rows := e.summary(t, f, byTaxableSalesDesc)

	taxable, tax, surcharge := total(rows)
	require.Zero(t, taxable+tax+surcharge, "no sold invoice is dated in September")
	require.Len(t, rows, 10, "every active tax still has a row")
}

func TestSummaryMethodFilter(t *testing.T) {
	e := newEnv(t)
	f := october(model.SalesTaxCollected)
	f.Methods = []model.PaymentMethod{model.MethodCard}

	rows := byName(e.summary(t, f, byTaxableSalesDesc))

	require.Equal(t, model.Money(22000), rows["VN"].TaxableSales, "85004 100.00 and 85005 120.00")
	require.Equal(t, model.Money(6600), rows["VN"].TaxCollected)
	require.Equal(t, model.Money(1750), rows["d12"].TaxableSales, "85010 only")
	require.Equal(t, model.Money(210), rows["d12"].TaxCollected)
	require.Equal(t, model.Money(6000), rows["VAT"].TaxableSales, "85011 only")
	require.Zero(t, rows["VAT"].SurchargeTax, "the surcharge is on 85009, paid in cash")

	t.Run("two methods", func(t *testing.T) {
		f.Methods = []model.PaymentMethod{model.MethodCash, model.MethodCard}
		rows := byName(e.summary(t, f, byTaxableSalesDesc))
		require.Equal(t, model.Money(292), rows["VAT"].SurchargeTax)
	})

	t.Run("a method nobody used", func(t *testing.T) {
		f.Methods = []model.PaymentMethod{model.MethodOther}
		taxable, tax, surcharge := total(e.summary(t, f, byTaxableSalesDesc))
		require.Zero(t, taxable+tax+surcharge)
	})
}

func TestSummaryCollectedIgnoresPaymentsThatAreNotCollected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	before, _, _ := total(e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc))

	insert := func(status, deletedAt string) {
		var deleted any
		if deletedAt != "" {
			deleted = deletedAt
		}
		_, err := e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date, status, deleted_at)
			SELECT customer_id, id, 'cash', 65, '2026-10-20', $1::payment_status, $2::timestamptz
			FROM billing_documents WHERE parent_type = 'invoice' AND document_number = '85000'`, status, deleted)
		require.NoError(t, err)
	}
	for _, s := range []string{"pending", "failed", "refunded"} {
		insert(s, "")
	}
	insert("paid", "2026-10-21 00:00:00+00")

	after, _, _ := total(e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc))
	require.Equal(t, before, after, "pending, failed, refunded and deleted payments carry no collected tax")

	insert("paid", "")
	later, _, _ := total(e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc))
	require.Equal(t, before+6500, later)
}

func TestSummaryCreditPaymentsCarryNoTax(t *testing.T) {
	e := newEnv(t)

	var credits int
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE credit_id IS NOT NULL
		AND status = 'paid' AND payment_date BETWEEN '2026-10-01' AND '2026-10-31'`).Scan(&credits))
	require.Positive(t, credits, "the seed has credit payments in October")

	rows := e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc)
	taxable, _, _ := total(rows)
	require.Equal(t, model.Money(72150), taxable, "unchanged by the credit payments")
}

func TestSummaryRoundsEachPaymentShareToTheCent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// 33.33 of invoice 85031 (95.00 with 9.50 VAT): tax share 9.50 * 33.33 / 95.00 = 3.333, rounded to 3.33
	_, err := e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date)
		SELECT customer_id, id, 'cash', 33.33, '2026-10-20' FROM billing_documents
		WHERE parent_type = 'invoice' AND document_number = '85031'`)
	require.NoError(t, err)

	rows := byName(e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc))

	require.Equal(t, model.Money(42000+3333), rows["VAT"].TaxableSales)
	require.Equal(t, model.Money(4200+333), rows["VAT"].TaxCollected)
}

func TestSummaryCollectedSkipsInvoicesWithAZeroTotal(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.pool.Exec(ctx, `INSERT INTO billing_documents (document_number, customer_id, document_date, total, status)
		SELECT '99001', customer_id, '2026-10-20', 0, 'paid' FROM billing_documents WHERE document_number = '85031'`)
	require.NoError(t, err)
	_, err = e.pool.Exec(ctx, `INSERT INTO invoice_tax_lines (invoice_id, tax_rate_id, taxable_amount, tax_amount)
		SELECT b.id, tr.id, 10, 1 FROM billing_documents b, tax_rates tr WHERE b.document_number = '99001' AND tr.name = 'VAT'`)
	require.NoError(t, err)
	_, err = e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date)
		SELECT customer_id, id, 'cash', 5, '2026-10-20' FROM billing_documents WHERE document_number = '99001'`)
	require.NoError(t, err)

	rows := byName(e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc))

	require.Equal(t, model.Money(42000), rows["VAT"].TaxableSales, "nothing to share, and no division by zero")
}

func TestSummaryInvoicedLeavesOutInvoicesThatAreNotSales(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	before, _, _ := total(e.summary(t, october(model.SalesTaxInvoiced), byTaxableSalesDesc))

	// a draft, a void, an archived and a deleted invoice, each with a tax line, are not sales
	for i, state := range []string{"status = 'draft'", "status = 'void'", "archived_at = now()", "deleted_at = now()"} {
		number := "9900" + string(rune('1'+i))
		_, err := e.pool.Exec(ctx, `INSERT INTO billing_documents (document_number, customer_id, document_date, total, status)
			SELECT $1, customer_id, '2026-10-20', 50, 'sent' FROM billing_documents WHERE document_number = '85031'`, number)
		require.NoError(t, err)
		_, err = e.pool.Exec(ctx, `UPDATE billing_documents SET `+state+` WHERE parent_type = 'invoice' AND document_number = $1`, number)
		require.NoError(t, err)
		_, err = e.pool.Exec(ctx, `INSERT INTO invoice_tax_lines (invoice_id, tax_rate_id, taxable_amount, tax_amount)
			SELECT b.id, tr.id, 50, 5 FROM billing_documents b, tax_rates tr WHERE b.document_number = $1 AND tr.name = 'VAT'`, number)
		require.NoError(t, err)
	}

	after, _, _ := total(e.summary(t, october(model.SalesTaxInvoiced), byTaxableSalesDesc))
	require.Equal(t, before, after)
}

func TestSummaryInactiveTaxesAppearOnlyWithActivity(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.pool.Exec(ctx, `UPDATE tax_rates SET active = false WHERE name IN ('VAT2', 'VN')`)
	require.NoError(t, err)

	rows := e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc)

	require.NotContains(t, names(rows), "VAT2", "an inactive tax with no activity is hidden")
	require.Contains(t, names(rows), "VN", "an inactive tax with activity is still reported")
	require.Len(t, rows, 9)

	idle := e.summary(t, model.SalesTaxFilter{DateFrom: day(2030, 1, 1), DateTo: day(2030, 1, 31), Basis: model.SalesTaxCollected}, byTaxableSalesDesc)
	require.Len(t, idle, 8, "only the active taxes when nothing happened")
}

func TestSummaryEmptyRangeListsEveryActiveTaxWithZeros(t *testing.T) {
	e := newEnv(t)
	for _, basis := range model.SalesTaxBases {
		rows := e.summary(t, model.SalesTaxFilter{DateFrom: day(2030, 1, 1), DateTo: day(2030, 1, 31), Basis: basis}, byTaxableSalesDesc)

		require.Equal(t, allTaxes, names(rows), string(basis))
		taxable, tax, surcharge := total(rows)
		require.Zero(t, taxable+tax+surcharge)
	}
}

func TestSummaryOpenDateRangeCoversEverything(t *testing.T) {
	e := newEnv(t)

	rows := e.summary(t, model.SalesTaxFilter{Basis: model.SalesTaxCollected}, byTaxableSalesDesc)

	taxable, _, _ := total(rows)
	require.Equal(t, model.Money(72150+1000), taxable, "October plus the September payment")
}

func TestSummarySortOrders(t *testing.T) {
	e := newEnv(t)
	f := october(model.SalesTaxCollected)

	tests := []struct {
		name string
		sort model.SalesTaxSort
		want []int64
	}{
		{"taxable sales desc", model.SalesTaxSort{Key: model.SalesTaxSortTaxableSales, Desc: true}, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{"taxable sales asc", model.SalesTaxSort{Key: model.SalesTaxSortTaxableSales}, []int64{4, 5, 6, 7, 8, 9, 10, 3, 2, 1}},
		{"tax rate asc, ties by id", model.SalesTaxSort{Key: model.SalesTaxSortTaxRate}, []int64{7, 8, 1, 3, 4, 5, 6, 9, 2, 10}},
		{"tax rate desc, ties still by id ascending", model.SalesTaxSort{Key: model.SalesTaxSortTaxRate, Desc: true}, []int64{2, 10, 6, 9, 4, 5, 3, 1, 8, 7}},
		{"tax collected desc", model.SalesTaxSort{Key: model.SalesTaxSortTaxCollected, Desc: true}, []int64{2, 1, 3, 4, 5, 6, 7, 8, 9, 10}},
		{"surcharge desc", model.SalesTaxSort{Key: model.SalesTaxSortSurchargeTax, Desc: true}, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ids(e.summary(t, f, tt.sort)))
		})
	}

	t.Run("by name", func(t *testing.T) {
		asc := names(e.summary(t, f, model.SalesTaxSort{Key: model.SalesTaxSortName}))
		desc := names(e.summary(t, f, model.SalesTaxSort{Key: model.SalesTaxSortName, Desc: true}))
		require.Equal(t, "d11", asc[0])
		require.Equal(t, "VN", asc[len(asc)-1])
		require.Equal(t, "VN", desc[0])
		require.Equal(t, "d11", desc[len(desc)-1])
	})
}

func TestSummaryRatesAreReportedWithTwoDecimals(t *testing.T) {
	e := newEnv(t)

	got := byName(e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc))

	require.Equal(t, "10.00", got["VAT"].Rate)
	require.Equal(t, "0.00", got["Tax 32"].Rate)
	require.Equal(t, "2.00", got["test 2"].Rate)
}

func TestSummaryRejectsInvalidQueries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.repo.Summary(ctx, october(model.SalesTaxCollected), model.SalesTaxSort{Key: "nope"})
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	_, err = e.repo.Summary(ctx, model.SalesTaxFilter{Basis: "everything"}, byTaxableSalesDesc)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	f := october(model.SalesTaxInvoiced)
	f.Methods = []model.PaymentMethod{model.MethodCash}
	_, err = e.repo.Summary(ctx, f, byTaxableSalesDesc)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

func TestSummaryReadsOneSnapshotInsideATransaction(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	var warm int
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM payments`).Scan(&warm)) // the snapshot starts here

	_, err = e.pool.Exec(ctx, `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date)
		SELECT customer_id, id, 'cash', 65, '2026-10-20' FROM billing_documents WHERE document_number = '85000'`)
	require.NoError(t, err)

	inside, err := repository.NewSalesTaxRepository(tx).Summary(ctx, october(model.SalesTaxCollected), byTaxableSalesDesc)
	require.NoError(t, err)
	insideTaxable, _, _ := total(inside)
	require.Equal(t, model.Money(72150), insideTaxable, "the payment committed after the snapshot is not seen")

	outside := e.summary(t, october(model.SalesTaxCollected), byTaxableSalesDesc)
	outsideTaxable, _, _ := total(outside)
	require.Equal(t, model.Money(72150+6500), outsideTaxable)
}

func TestRatesListsEveryTaxRateByName(t *testing.T) {
	e := newEnv(t)
	_, err := e.pool.Exec(context.Background(), `UPDATE tax_rates SET active = false WHERE name = 'US'`)
	require.NoError(t, err)

	rates, err := e.repo.Rates(context.Background())

	require.NoError(t, err)
	require.Len(t, rates, 10, "inactive rates are listed too")
	got := map[string]model.TaxRateOption{}
	for _, r := range rates {
		require.Positive(t, r.ID)
		got[r.Name] = r
	}
	require.Equal(t, model.TaxRateOption{ID: 1, Name: "VAT", Rate: "10.00", Active: true}, got["VAT"])
	require.Equal(t, model.TaxRateOption{ID: 10, Name: "US", Rate: "30.00", Active: false}, got["US"])
	require.Equal(t, "0.00", got["Tax 32"].Rate)
}
