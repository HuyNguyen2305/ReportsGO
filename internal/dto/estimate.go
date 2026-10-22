package dto

// EstimateResponse is one line of the Estimates report. Money is a decimal string and dates are YYYY-MM-DD,
// as everywhere else.
type EstimateResponse struct {
	ID              int64       `json:"id"`
	EstimateNumber  string      `json:"estimate_number"`
	AccountNumber   string      `json:"account_number"`
	Customer        CustomerRef `json:"customer"`
	Phone           *string     `json:"phone"`
	BillingEmail    *string     `json:"billing_email"`
	CreatedBy       *string     `json:"created_by"`
	Date            string      `json:"date"`
	Total           string      `json:"total"`
	Status          string      `json:"status"`
	SignatureStatus string      `json:"signature_status"`
}

// EstimateListResponse is one page of the Estimates report.
type EstimateListResponse struct {
	Data []EstimateResponse `json:"data"`
	Page PageResponse       `json:"page"`
	// Summary totals the whole filtered set. It is null on every page after the first.
	Summary *EstimateSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// EstimateSummaryResponse totals the filtered estimates.
type EstimateSummaryResponse struct {
	Count int64  `json:"count"`
	Total string `json:"total"`
}

// EstimateFiltersResponse has everything the report screen needs to build its filter controls.
type EstimateFiltersResponse struct {
	Statuses          []string           `json:"statuses"`
	SignatureStatuses []string           `json:"signature_statuses"`
	Lifecycles        []string           `json:"lifecycles"`
	SortKeys          []string           `json:"sort_keys"`
	DefaultSort       SortResponse       `json:"default_sort"`
	Limits            PageLimitsResponse `json:"limits"`
}
