package dto

// CustomerResponse is one line of the New Customers report. CreatedAt is always UTC with six fractional digits,
// for example "2026-10-22T08:15:30.123456Z". Email, Phone and Source are null when the customer has none; Phone
// is the customer's first phone number.
type CustomerResponse struct {
	ID            int64           `json:"id"`
	Customer      CustomerRef     `json:"customer"`
	AccountNumber string          `json:"account_number"`
	Email         *string         `json:"email"`
	Phone         *string         `json:"phone"`
	Source        *OptionResponse `json:"source"`
	CreatedAt     string          `json:"created_at"`
	// TopNote is "" when the customer has none. Address is the first service location, null when there is none.
	TopNote string                        `json:"top_note"`
	Address *ServiceLookupAddressResponse `json:"address"`
	// FirstJob is the earliest created job that is not deleted, null when there is none. CompletedJobs counts the
	// completed jobs that are not deleted.
	FirstJob      *CustomerFirstJobResponse `json:"first_job"`
	CompletedJobs int64                     `json:"completed_jobs"`
}

// CustomerFirstJobResponse is the job a new customer booked first. CreatedAt is UTC with six fractional digits.
type CustomerFirstJobResponse struct {
	ID          int64          `json:"id"`
	ServiceType OptionResponse `json:"service_type"`
	CreatedAt   string         `json:"created_at"`
}

// CustomerListResponse is one page of the New Customers report.
type CustomerListResponse struct {
	Data []CustomerResponse `json:"data"`
	Page PageResponse       `json:"page"`
	// Summary counts the whole filtered set. It is null on every page after the first.
	Summary *CustomerSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// CustomerSummaryResponse counts the filtered customers.
type CustomerSummaryResponse struct {
	Count int64 `json:"count"`
}

// CustomerFiltersResponse has everything the report screen needs to build its filter controls.
type CustomerFiltersResponse struct {
	LeadSources []OptionResponse   `json:"lead_sources"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
