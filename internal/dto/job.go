package dto

// JobResponse is one line of the New Jobs report. InvoiceValue is a decimal string and CreatedAt is always UTC
// with six fractional digits, for example "2026-10-22T08:15:30.123456Z". Source is null when the job has no
// lead source.
type JobResponse struct {
	ID            int64           `json:"id"`
	Customer      CustomerRef     `json:"customer"`
	AccountNumber string          `json:"account_number"`
	ServiceType   OptionResponse  `json:"service_type"`
	Source        *OptionResponse `json:"source"`
	InvoiceValue  string          `json:"invoice_value"`
	CreatedAt     string          `json:"created_at"`
	Status        string          `json:"status"`
}

// JobListResponse is one page of the New Jobs report.
type JobListResponse struct {
	Data []JobResponse `json:"data"`
	Page PageResponse  `json:"page"`
	// Summary counts and totals the whole filtered set. It is null on every page after the first.
	Summary *JobSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// JobSummaryResponse counts and totals the filtered jobs, whatever their status.
type JobSummaryResponse struct {
	Count int64  `json:"count"`
	Total string `json:"total"`
}

// JobFiltersResponse has everything the report screen needs to build its filter controls.
type JobFiltersResponse struct {
	ServiceTypes []OptionResponse   `json:"service_types"`
	LeadSources  []OptionResponse   `json:"lead_sources"`
	Statuses     []string           `json:"statuses"`
	SortKeys     []string           `json:"sort_keys"`
	DefaultSort  SortResponse       `json:"default_sort"`
	Limits       PageLimitsResponse `json:"limits"`
}
