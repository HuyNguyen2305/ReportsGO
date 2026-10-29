package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
	servicemocks "reportsgo/tests/mocks/service"
)

// runSalesTax calls List on a service whose repository returns rows, and returns the filter and sort it received.
func runSalesTax(t *testing.T, p service.ListSalesTaxParams, rows []model.SalesTaxRow) (model.SalesTaxFilter, model.SalesTaxSort, *service.ListSalesTaxResult) {
	t.Helper()

	repo := servicemocks.NewMockSalesTaxRepository(t)
	var (
		gotFilter model.SalesTaxFilter
		gotSort   model.SalesTaxSort
	)
	repo.EXPECT().Summary(mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ context.Context, f model.SalesTaxFilter, s model.SalesTaxSort) { gotFilter, gotSort = f, s }).
		Return(rows, nil).Once()

	res, err := service.NewSalesTaxService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return gotFilter, gotSort, res
}

// rejectsSalesTax asserts that p is invalid and that the repository is never touched.
func rejectsSalesTax(t *testing.T, p service.ListSalesTaxParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockSalesTaxRepository(t) // no expectations: any call fails the test
	_, err := service.NewSalesTaxService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestSalesTaxListDefaults(t *testing.T) {
	f, s, res := runSalesTax(t, service.ListSalesTaxParams{}, nil)

	require.Equal(t, model.SalesTaxFilter{
		DateFrom: date(2026, 10, 1), DateTo: date(2026, 10, 31), Basis: model.SalesTaxCollected,
	}, f)
	require.Equal(t, model.SalesTaxSort{Key: model.SalesTaxSortTaxableSales, Desc: true}, s)
	require.Equal(t, model.SalesTaxCollected, res.Basis)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
	require.NotNil(t, res.Items, "an empty report is an empty list, never nil")
	require.Empty(t, res.Items)
	require.Equal(t, model.SalesTaxSummary{}, res.Summary)
}

func TestSalesTaxListDateRange(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{"defaulted month", "", "", date(2026, 10, 1), date(2026, 10, 31)},
		{"explicit", "2026-09-01", "2026-09-30", date(2026, 9, 1), date(2026, 9, 30)},
		{"only from leaves the end open", "2026-10-05", "", date(2026, 10, 5), time.Time{}},
		{"only to leaves the start open", "", "2026-10-05", time.Time{}, date(2026, 10, 5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _, res := runSalesTax(t, service.ListSalesTaxParams{From: tt.from, To: tt.to}, nil)

			require.Equal(t, tt.wantFrom, f.DateFrom)
			require.Equal(t, tt.wantTo, f.DateTo)
			require.Equal(t, tt.wantFrom, res.From)
			require.Equal(t, tt.wantTo, res.To)
		})
	}
}

func TestSalesTaxListBasis(t *testing.T) {
	tests := []struct {
		raw  string
		want model.SalesTaxBasis
	}{
		{"", model.SalesTaxCollected},
		{"collected", model.SalesTaxCollected},
		{"invoiced", model.SalesTaxInvoiced},
		{" INVOICED ", model.SalesTaxInvoiced},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			f, _, res := runSalesTax(t, service.ListSalesTaxParams{Basis: tt.raw}, nil)
			require.Equal(t, tt.want, f.Basis)
			require.Equal(t, tt.want, res.Basis)
		})
	}

	for _, bad := range []string{"cash", "accrual", "all"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			got := rejectsSalesTax(t, service.ListSalesTaxParams{Basis: bad})
			require.Equal(t, "must be one of: collected, invoiced", got["basis"])
		})
	}
}

func TestSalesTaxListMethods(t *testing.T) {
	t.Run("normalized on the collected basis", func(t *testing.T) {
		f, _, _ := runSalesTax(t, service.ListSalesTaxParams{Method: []string{" CARD ", "check", "card", ""}}, nil)
		require.Equal(t, []model.PaymentMethod{model.MethodCard, model.MethodCheck}, f.Methods)
	})

	t.Run("rejected on the invoiced basis", func(t *testing.T) {
		got := rejectsSalesTax(t, service.ListSalesTaxParams{Basis: "invoiced", Method: []string{"card"}})
		require.Equal(t, map[string]string{"method": "can only be used with basis collected"}, got)
	})

	t.Run("blank methods are fine on the invoiced basis", func(t *testing.T) {
		f, _, _ := runSalesTax(t, service.ListSalesTaxParams{Basis: "invoiced", Method: []string{"", " "}}, nil)
		require.Empty(t, f.Methods)
	})

	t.Run("an unknown method is reported once, not as a basis problem", func(t *testing.T) {
		got := rejectsSalesTax(t, service.ListSalesTaxParams{Basis: "invoiced", Method: []string{"wire"}})
		require.Equal(t, map[string]string{"method": "must be one of: cash, check, card, other"}, got)
	})

	t.Run("a bad basis does not add a method complaint", func(t *testing.T) {
		got := rejectsSalesTax(t, service.ListSalesTaxParams{Basis: "bogus", Method: []string{"card"}})
		require.Equal(t, map[string]string{"basis": "must be one of: collected, invoiced"}, got)
	})
}

func TestSalesTaxListSort(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.SalesTaxSort
	}{
		{"neither: biggest taxable sales first", "", "", model.SalesTaxSort{Key: model.SalesTaxSortTaxableSales, Desc: true}},
		{"a key alone is ascending", "name", "", model.SalesTaxSort{Key: model.SalesTaxSortName}},
		{"a key and desc", "tax_rate", "desc", model.SalesTaxSort{Key: model.SalesTaxSortTaxRate, Desc: true}},
		{"an order alone sorts by taxable sales", "", "asc", model.SalesTaxSort{Key: model.SalesTaxSortTaxableSales}},
		{"case is ignored", " Surcharge_Tax ", " DESC ", model.SalesTaxSort{Key: model.SalesTaxSortSurchargeTax, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, s, _ := runSalesTax(t, service.ListSalesTaxParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, s)
		})
	}

	t.Run("every sort key is accepted", func(t *testing.T) {
		for _, key := range model.SalesTaxSortKeys {
			_, s, _ := runSalesTax(t, service.ListSalesTaxParams{Sort: string(key)}, nil)
			require.Equal(t, key, s.Key)
		}
	})
}

func TestSalesTaxListSummaryAddsUpTheRows(t *testing.T) {
	rows := []model.SalesTaxRow{
		{ID: 1, Name: "VAT", Rate: "10.00", TaxableSales: 42000, TaxCollected: 4200, SurchargeTax: 292},
		{ID: 2, Name: "VN", Rate: "30.00", TaxableSales: 22000, TaxCollected: 6600},
		{ID: 3, Name: "d12", Rate: "12.00", TaxableSales: 8150, TaxCollected: 978},
		{ID: 4, Name: "VAT2", Rate: "15.00"},
	}

	_, _, res := runSalesTax(t, service.ListSalesTaxParams{}, rows)

	require.Equal(t, rows, res.Items)
	require.Equal(t, model.SalesTaxSummary{Count: 4, TaxableSales: 72150, TaxCollected: 11778, SurchargeTax: 292}, res.Summary)
}

func TestSalesTaxListRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		p     service.ListSalesTaxParams
		field string
		msg   string
	}{
		{"bad from", service.ListSalesTaxParams{From: "10/01/2026"}, "from", "must be a date in YYYY-MM-DD format"},
		{"to before from", service.ListSalesTaxParams{From: "2026-10-05", To: "2026-10-04"}, "to", "must not be before from"},
		{"unknown method", service.ListSalesTaxParams{Method: []string{"wire"}}, "method", "must be one of: cash, check, card, other"},
		{"unknown sort", service.ListSalesTaxParams{Sort: "total"}, "sort",
			"must be one of: name, taxable_sales, tax_rate, tax_collected, surcharge_tax"},
		{"bad order", service.ListSalesTaxParams{Order: "up"}, "order", "must be asc or desc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectsSalesTax(t, tt.p)
			require.Equal(t, tt.msg, got[tt.field])
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsSalesTax(t, service.ListSalesTaxParams{From: "x", Basis: "cash", Order: "up", Sort: "nope"})
		require.Len(t, got, 4)
	})
}

func TestSalesTaxListWrapsStorageErrors(t *testing.T) {
	boom := errors.New("connection reset")
	repo := servicemocks.NewMockSalesTaxRepository(t)
	repo.EXPECT().Summary(mock.Anything, mock.Anything, mock.Anything).Return(nil, boom).Once()

	_, err := service.NewSalesTaxService(repo, fixedClock(october21)).List(context.Background(), service.ListSalesTaxParams{})

	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, model.ErrInvalidInput)
	require.Contains(t, err.Error(), "list sales tax summary")
}

func TestSalesTaxRates(t *testing.T) {
	t.Run("returns the tax rates", func(t *testing.T) {
		want := []model.TaxRateOption{{ID: 1, Name: "VAT", Rate: "10.00", Active: true}, {ID: 2, Name: "US", Rate: "30.00"}}
		repo := servicemocks.NewMockSalesTaxRepository(t)
		repo.EXPECT().Rates(mock.Anything).Return(want, nil).Once()

		got, err := service.NewSalesTaxService(repo, fixedClock(october21)).Rates(context.Background())

		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("no tax rates is an empty list, never nil", func(t *testing.T) {
		repo := servicemocks.NewMockSalesTaxRepository(t)
		repo.EXPECT().Rates(mock.Anything).Return(nil, nil).Once()

		got, err := service.NewSalesTaxService(repo, fixedClock(october21)).Rates(context.Background())

		require.NoError(t, err)
		require.NotNil(t, got)
		require.Empty(t, got)
	})

	t.Run("wraps storage errors", func(t *testing.T) {
		boom := errors.New("down")
		repo := servicemocks.NewMockSalesTaxRepository(t)
		repo.EXPECT().Rates(mock.Anything).Return(nil, boom).Once()

		_, err := service.NewSalesTaxService(repo, fixedClock(october21)).Rates(context.Background())

		require.ErrorIs(t, err, boom)
		require.Contains(t, err.Error(), "list tax rates")
	})
}

func TestNewSalesTaxServiceDefaultsToTheCurrentMonthInUTC(t *testing.T) {
	repo := servicemocks.NewMockSalesTaxRepository(t)
	var got model.SalesTaxFilter
	repo.EXPECT().Summary(mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ context.Context, f model.SalesTaxFilter, _ model.SalesTaxSort) { got = f }).
		Return(nil, nil).Once()

	_, err := service.NewSalesTaxService(repo, nil).List(context.Background(), service.ListSalesTaxParams{})

	require.NoError(t, err)
	now := time.Now().UTC()
	thisMonth := date(now.Year(), now.Month(), 1)
	// the test could straddle midnight on the last day of a month
	require.Contains(t, []time.Time{thisMonth, thisMonth.AddDate(0, -1, 0)}, got.DateFrom)
}
