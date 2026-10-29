package dto

// SalesTaxResponse is one tax of the Sales Tax Summary. Money is a decimal string and TaxRate is the percentage as
// a two-decimal string, such as "10.00".
type SalesTaxResponse struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	TaxableSales string `json:"taxable_sales"`
	TaxRate      string `json:"tax_rate"`
	TaxCollected string `json:"tax_collected"`
	SurchargeTax string `json:"surcharge_tax"`
}

// SalesTaxListResponse is the whole Sales Tax Summary; it has no paging.
type SalesTaxListResponse struct {
	Data    []SalesTaxResponse      `json:"data"`
	Summary SalesTaxSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
	// Basis is the basis actually applied, collected or invoiced.
	Basis string `json:"basis"`
}

// SalesTaxSummaryResponse totals the taxes of the report; it equals the sum of the rows.
type SalesTaxSummaryResponse struct {
	Count        int64  `json:"count"`
	TaxableSales string `json:"taxable_sales"`
	TaxCollected string `json:"tax_collected"`
	SurchargeTax string `json:"surcharge_tax"`
}

// TaxRateResponse is one line of the Tax Rate tab.
type TaxRateResponse struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	TaxRate string `json:"tax_rate"`
	Active  bool   `json:"active"`
}

// TaxRatesResponse is the Tax Rate tab.
type TaxRatesResponse struct {
	Data []TaxRateResponse `json:"data"`
}

// SalesTaxFiltersResponse has everything the report screen needs to build its filter controls.
type SalesTaxFiltersResponse struct {
	Bases       []string     `json:"bases"`
	Methods     []string     `json:"methods"`
	SortKeys    []string     `json:"sort_keys"`
	DefaultSort SortResponse `json:"default_sort"`
}
