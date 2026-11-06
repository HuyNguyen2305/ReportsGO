package dto

// SystemEmailResponse is one line of the System Email History report. Customer.Name is shown in the requested name
// format. TimeStamp is always UTC with six fractional digits, for example "2026-11-02T08:15:30.123456Z".
type SystemEmailResponse struct {
	ID        int64       `json:"id"`
	Customer  CustomerRef `json:"customer"`
	Email     string      `json:"email"`
	EmailType string      `json:"email_type"`
	Status    string      `json:"status"`
	TimeStamp string      `json:"time_stamp"`
}

// SystemEmailListResponse is one page of the System Email History report.
type SystemEmailListResponse struct {
	Data []SystemEmailResponse `json:"data"`
	Page PageResponse          `json:"page"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// SystemEmailFiltersResponse has everything the report screen needs to build its filter controls.
type SystemEmailFiltersResponse struct {
	Statuses    []string           `json:"statuses"`
	EmailTypes  []string           `json:"email_types"`
	Clients     []OptionResponse   `json:"clients"`
	NameFormats []string           `json:"name_formats"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
