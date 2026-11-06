package dto

// InboundLeadResponse is one line of the Inbound Leads report. Customer.Name is shown in the requested name format.
// Source is null when the lead has none (the screen's "NA"); Email and Phone are null when none is recorded, and Phone is
// the customer's first number. Notes is the customer's top note, an empty string when there is none. DateAdded is always
// UTC with six fractional digits, for example "2026-11-03T08:15:30.123456Z". ActiveJob is the earliest scheduled job that
// is not deleted and still open (unconfirmed or confirmed), null when the lead has none.
type InboundLeadResponse struct {
	ID        int64                   `json:"id"`
	Customer  CustomerRef             `json:"customer"`
	Source    *OptionResponse         `json:"source"`
	Email     *string                 `json:"email"`
	Phone     *string                 `json:"phone"`
	Notes     string                  `json:"notes"`
	DateAdded string                  `json:"date_added"`
	ActiveJob *InboundLeadJobResponse `json:"active_job"`
}

// InboundLeadJobResponse is a lead's active job.
type InboundLeadJobResponse struct {
	ServiceType OptionResponse `json:"service_type"`
	ScheduledAt string         `json:"scheduled_at"`
}

// InboundLeadListResponse is one page of the Inbound Leads report.
type InboundLeadListResponse struct {
	Data []InboundLeadResponse `json:"data"`
	Page PageResponse          `json:"page"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// InboundLeadFiltersResponse has everything the report screen needs to build its filter controls. A source filter also
// accepts the word "none" for the leads without a source.
type InboundLeadFiltersResponse struct {
	Sources     []OptionResponse   `json:"sources"`
	NameFormats []string           `json:"name_formats"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
