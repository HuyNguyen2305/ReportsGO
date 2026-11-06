package dto

// OnlineBookingResponse is one line of the Online Bookings report: one job of a customer who came from an online booking.
// Customer.Name is shown in the requested name format. ScheduledAt is the appointment (the screen's Date and Time as one
// value), always UTC with six fractional digits, for example "2026-11-02T08:15:30.123456Z". Email and Phone are null when
// none is recorded, and Phone is the customer's first number. Address is the job's service location. Notes is the
// customer's top note, an empty string when there is none. Source is the customer's own lead source, null when none.
type OnlineBookingResponse struct {
	ID          int64                        `json:"id"`
	Customer    CustomerRef                  `json:"customer"`
	ScheduledAt string                       `json:"scheduled_at"`
	ServiceType OptionResponse               `json:"service_type"`
	Email       *string                      `json:"email"`
	Phone       *string                      `json:"phone"`
	Address     ServiceLookupAddressResponse `json:"address"`
	Notes       string                       `json:"notes"`
	Status      string                       `json:"status"`
	Source      *OptionResponse              `json:"source"`
}

// OnlineBookingListResponse is one page of the Online Bookings report.
type OnlineBookingListResponse struct {
	Data []OnlineBookingResponse `json:"data"`
	Page PageResponse            `json:"page"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// OnlineBookingFiltersResponse has everything the report screen needs to build its filter controls. A source filter also
// accepts the word "none" for the customers without a source.
type OnlineBookingFiltersResponse struct {
	Sources     []OptionResponse   `json:"sources"`
	NameFormats []string           `json:"name_formats"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
