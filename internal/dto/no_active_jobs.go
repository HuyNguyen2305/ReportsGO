package dto

// LastServiceResponse is the latest non-deleted job of a customer or location. Date is YYYY-MM-DD, the day of the
// job in the server's business time zone.
type LastServiceResponse struct {
	ServiceType string `json:"service_type"`
	JobStatus   string `json:"job_status"`
	Date        string `json:"date"`
}

// NoActiveJobsCustomerResponse is one line of the Customers w/o Active Jobs report. LastService is null when the
// customer has no job.
type NoActiveJobsCustomerResponse struct {
	ID            int64                `json:"id"`
	Status        string               `json:"status"`
	AccountNumber string               `json:"account_number"`
	Customer      string               `json:"customer"`
	Phone         *string              `json:"phone"`
	LastService   *LastServiceResponse `json:"last_service"`
}

// ServiceAddressResponse is the address of a service location.
type ServiceAddressResponse struct {
	Line1 string `json:"line1"`
	Line2 string `json:"line2"`
	City  string `json:"city"`
	Zip   string `json:"zip"`
}

// NoActiveJobsLocationResponse is one line of the Locations w/o Active Jobs report. LastService is null when the
// location has no job.
type NoActiveJobsLocationResponse struct {
	ID            int64                  `json:"id"`
	CustomerID    int64                  `json:"customer_id"`
	Status        string                 `json:"status"`
	AccountNumber string                 `json:"account_number"`
	Customer      string                 `json:"customer"`
	Phone         *string                `json:"phone"`
	Address       ServiceAddressResponse `json:"address"`
	LastService   *LastServiceResponse   `json:"last_service"`
}

// NoActiveJobsSummaryResponse counts the filtered rows across all pages.
type NoActiveJobsSummaryResponse struct {
	Count int64 `json:"count"`
}

// NoActiveJobsCustomersListResponse is one page of the Customers w/o Active Jobs report.
type NoActiveJobsCustomersListResponse struct {
	Data []NoActiveJobsCustomerResponse `json:"data"`
	Page PageResponse                   `json:"page"`
	// Summary counts the whole filtered set. It is null on every page after the first.
	Summary *NoActiveJobsSummaryResponse `json:"summary"`
}

// NoActiveJobsLocationsListResponse is one page of the Locations w/o Active Jobs report.
type NoActiveJobsLocationsListResponse struct {
	Data []NoActiveJobsLocationResponse `json:"data"`
	Page PageResponse                   `json:"page"`
	// Summary counts the whole filtered set. It is null on every page after the first.
	Summary *NoActiveJobsSummaryResponse `json:"summary"`
}

// NoActiveJobsFiltersResponse has everything the report screen needs to build its filter controls.
type NoActiveJobsFiltersResponse struct {
	Statuses    []string           `json:"statuses"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
