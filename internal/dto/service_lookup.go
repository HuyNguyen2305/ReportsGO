package dto

// ServiceLookupAddressResponse is the service address of a job.
type ServiceLookupAddressResponse struct {
	Line1  string `json:"line1"`
	Line2  string `json:"line2"`
	City   string `json:"city"`
	State  string `json:"state"`
	Zip    string `json:"zip"`
	County string `json:"county"`
}

// ServiceLookupResponse is one job of Service Lookup. Money is a decimal string; ScheduledAt is a UTC timestamp.
type ServiceLookupResponse struct {
	ID          int64                        `json:"id"`
	Customer    CustomerRef                  `json:"customer"`
	Phone       *string                      `json:"phone"`
	ServiceType OptionResponse               `json:"service_type"`
	ScheduledAt *string                      `json:"scheduled_at"`
	Address     ServiceLookupAddressResponse `json:"address"`
	// InvoiceValue is the job's value; WithoutTax is the same less the job's tax.
	InvoiceValue string           `json:"invoice_value"`
	WithoutTax   string           `json:"without_tax"`
	Status       string           `json:"status"`
	AssignedTo   *StaffRef        `json:"assigned_to"`
	Tags         []OptionResponse `json:"tags"`
}

// ServiceLookupTabCountsResponse is the number of jobs each tab would show under the same filters.
type ServiceLookupTabCountsResponse struct {
	Active   int64 `json:"active"`
	WorkPool int64 `json:"work_pool"`
	Deleted  int64 `json:"deleted"`
}

// ServiceLookupSummaryResponse totals the selected tab's filtered jobs and counts every tab.
type ServiceLookupSummaryResponse struct {
	Count        int64                          `json:"count"`
	InvoiceValue string                         `json:"invoice_value"`
	WithoutTax   string                         `json:"without_tax"`
	Tabs         ServiceLookupTabCountsResponse `json:"tabs"`
}

// ServiceLookupListResponse is one page of Service Lookup.
type ServiceLookupListResponse struct {
	Data []ServiceLookupResponse `json:"data"`
	Page PageResponse            `json:"page"`
	// Summary totals the whole filtered set. It is null on every page after the first.
	Summary *ServiceLookupSummaryResponse `json:"summary"`
	// Range is the scheduled-date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// ServiceLookupFiltersResponse has everything the screen needs to build its filter controls.
type ServiceLookupFiltersResponse struct {
	Tabs         []string           `json:"tabs"`
	Statuses     []string           `json:"statuses"`
	ServiceTypes []OptionResponse   `json:"service_types"`
	Staff        []OptionResponse   `json:"staff"`
	Tags         []OptionResponse   `json:"tags"`
	SortKeys     []string           `json:"sort_keys"`
	DefaultSort  SortResponse       `json:"default_sort"`
	Limits       PageLimitsResponse `json:"limits"`
}
