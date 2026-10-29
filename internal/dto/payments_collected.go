package dto

// PaymentCollectedResponse is one line of the Payments Collected report. Money is a decimal string and Date is
// YYYY-MM-DD.
type PaymentCollectedResponse struct {
	ID           int64                 `json:"id"`
	PaymentFor   PaymentForResponse    `json:"payment_for"`
	Customer     CustomerRef           `json:"customer"`
	Method       PaymentMethodResponse `json:"method"`
	ServiceState *string               `json:"service_state"`
	Staff        *StaffRef             `json:"staff"`
	Date         string                `json:"date"`
	Amount       string                `json:"amount"`
	WithoutTax   string                `json:"without_tax"`
}

// StaffRef identifies the staff member who took a payment.
type StaffRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PaymentCollectedListResponse is one page of the Payments Collected report.
type PaymentCollectedListResponse struct {
	Data []PaymentCollectedResponse `json:"data"`
	Page PageResponse               `json:"page"`
	// Summary totals the whole filtered set. It is null on every page after the first.
	Summary *PaymentCollectedSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// PaymentCollectedSummaryResponse totals the filtered payments.
type PaymentCollectedSummaryResponse struct {
	Count      int64  `json:"count"`
	Amount     string `json:"amount"`
	WithoutTax string `json:"without_tax"`
}

// PaymentCollectedFiltersResponse has everything the report screen needs to build its filter controls.
type PaymentCollectedFiltersResponse struct {
	PaymentFor  []string           `json:"payment_for"`
	Methods     []string           `json:"methods"`
	Staff       []OptionResponse   `json:"staff"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
