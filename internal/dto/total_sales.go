package dto

// TotalSaleResponse is one line of the Total Sales report. Money is a decimal string, as elsewhere.
type TotalSaleResponse struct {
	ID            int64       `json:"id"`
	InvoiceNumber string      `json:"invoice_number"`
	Customer      CustomerRef `json:"customer"`
	Date          string      `json:"date"`
	Status        string      `json:"status"`
	Amount        string      `json:"amount"`
	WithoutTax    string      `json:"without_tax"`
}

// TotalSaleListResponse is one page of the Total Sales report.
type TotalSaleListResponse struct {
	Data []TotalSaleResponse `json:"data"`
	Page PageResponse        `json:"page"`
	// Summary totals the whole filtered set. It is null on every page after the first.
	Summary *TotalSaleSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// TotalSaleSummaryResponse totals the filtered sales.
type TotalSaleSummaryResponse struct {
	Count      int64  `json:"count"`
	Amount     string `json:"amount"`
	WithoutTax string `json:"without_tax"`
}

// TotalSaleFiltersResponse has everything the report screen needs to build its filter controls.
type TotalSaleFiltersResponse struct {
	Statuses    []string           `json:"statuses"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
