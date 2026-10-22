package dto

// CreditResponse is one line of the Credits report. Amount is a decimal string and CreditDate is YYYY-MM-DD.
type CreditResponse struct {
	ID            int64                `json:"id"`
	Customer      CustomerRef          `json:"customer"`
	AccountNumber string               `json:"account_number"`
	Method        CreditMethodResponse `json:"method"`
	Amount        string               `json:"amount"`
	CreditDate    string               `json:"credit_date"`
	CreatedBy     *string              `json:"created_by"`
}

// CreditMethodResponse is how a credit was given. Detail is the stored description, such as
// "Stripe **** 3222 Credit", and is an empty string when none was stored.
type CreditMethodResponse struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
}

// CreditListResponse is one page of the Credits report.
type CreditListResponse struct {
	Data []CreditResponse `json:"data"`
	Page PageResponse     `json:"page"`
	// Summary counts and totals the whole filtered set. It is null on every page after the first.
	Summary *CreditSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// CreditSummaryResponse counts and totals the filtered credits.
type CreditSummaryResponse struct {
	Count int64  `json:"count"`
	Total string `json:"total"`
}

// CreditFiltersResponse has everything the report screen needs to build its filter controls. Credits have no
// status and no Active/Deleted tab, so there are no lists for them.
type CreditFiltersResponse struct {
	Methods     []string           `json:"methods"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
