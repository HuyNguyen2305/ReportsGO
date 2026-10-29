package model

import "time"

// SalesTaxBasis decides which dates and amounts the Sales Tax Summary adds up.
type SalesTaxBasis string

// Sales tax bases.
const (
	// SalesTaxCollected is the cash basis: the tax follows the money received, by payment date.
	SalesTaxCollected SalesTaxBasis = "collected"
	// SalesTaxInvoiced is the accrual basis: the tax follows the invoices, by invoice date.
	SalesTaxInvoiced SalesTaxBasis = "invoiced"
)

// SalesTaxBases lists every valid SalesTaxBasis.
var SalesTaxBases = []SalesTaxBasis{SalesTaxCollected, SalesTaxInvoiced}

// SalesTaxSortKey names a column the Sales Tax Summary can be sorted by. It is a closed set: the repository maps
// each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type SalesTaxSortKey string

// Sales tax sort keys.
const (
	SalesTaxSortName         SalesTaxSortKey = "name"
	SalesTaxSortTaxableSales SalesTaxSortKey = "taxable_sales"
	SalesTaxSortTaxRate      SalesTaxSortKey = "tax_rate"
	SalesTaxSortTaxCollected SalesTaxSortKey = "tax_collected"
	SalesTaxSortSurchargeTax SalesTaxSortKey = "surcharge_tax"
)

// SalesTaxSortKeys lists every valid SalesTaxSortKey.
var SalesTaxSortKeys = []SalesTaxSortKey{
	SalesTaxSortName, SalesTaxSortTaxableSales, SalesTaxSortTaxRate, SalesTaxSortTaxCollected, SalesTaxSortSurchargeTax,
}

// SalesTaxSort is a sort key and direction. Ties are always broken by tax rate id, ascending.
type SalesTaxSort struct {
	Key  SalesTaxSortKey
	Desc bool
}

// SalesTaxFilter narrows the Sales Tax Summary. Dates are inclusive calendar dates: payment dates on the collected
// basis and invoice dates on the invoiced one.
type SalesTaxFilter struct {
	DateFrom, DateTo time.Time
	Basis            SalesTaxBasis
	// Methods narrows the payments that count; an empty list means "All". It only applies to the collected basis,
	// where money is involved.
	Methods []PaymentMethod
}

// SalesTaxRow is one tax of the summary.
type SalesTaxRow struct {
	ID   int64
	Name string
	// Rate is the tax percentage as text with two decimals, such as "10.00".
	Rate         string
	TaxableSales Money
	TaxCollected Money
	SurchargeTax Money
}

// SalesTaxSummary totals the rows of the report.
type SalesTaxSummary struct {
	Count        int64
	TaxableSales Money
	TaxCollected Money
	SurchargeTax Money
}

// TaxRateOption is a tax rate as listed on the Tax Rate tab.
type TaxRateOption struct {
	ID     int64
	Name   string
	Rate   string
	Active bool
}
