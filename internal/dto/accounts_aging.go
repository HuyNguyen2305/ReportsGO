package dto

// AgingCardResponse is a customer's default saved card. Expires is MM/YYYY.
type AgingCardResponse struct {
	Processor string `json:"processor"`
	Last4     string `json:"last4"`
	Expires   string `json:"expires"`
}

// AgingResponse is one line of the Accounts Aging report: a customer and service location with their open balance.
// Money is a decimal string.
type AgingResponse struct {
	CustomerID   int64              `json:"customer_id"`
	LocationID   *int64             `json:"location_id"`
	Customer     string             `json:"customer"`
	Location     *string            `json:"location"`
	BillingEmail *string            `json:"billing_email"`
	Phone        *string            `json:"phone"`
	CreditCard   *AgingCardResponse `json:"credit_card"`

	D0to30  string `json:"d0_30"`
	D31to60 string `json:"d31_60"`
	D61to90 string `json:"d61_90"`
	D90Plus string `json:"d90_plus"`
	Total   string `json:"total"`
	// AvailableCredit belongs to the customer and repeats on each of the customer's rows.
	AvailableCredit string `json:"available_credit"`
}

// AgingListResponse is one page of the Accounts Aging report.
type AgingListResponse struct {
	Data []AgingResponse `json:"data"`
	Page PageResponse    `json:"page"`
	// Summary totals the whole filtered set. It is null on every page after the first.
	Summary *AgingSummaryResponse `json:"summary"`
	// AsOf is the day the balances are read at (YYYY-MM-DD), including a defaulted today.
	AsOf string `json:"as_of"`
}

// AgingSummaryResponse totals the filtered rows. AvailableCredit counts each customer once.
type AgingSummaryResponse struct {
	Count           int64  `json:"count"`
	D0to30          string `json:"d0_30"`
	D31to60         string `json:"d31_60"`
	D61to90         string `json:"d61_90"`
	D90Plus         string `json:"d90_plus"`
	Total           string `json:"total"`
	AvailableCredit string `json:"available_credit"`
}

// AgingFiltersResponse has everything the report screen needs to build its filter controls.
type AgingFiltersResponse struct {
	Days         []int              `json:"days"`
	ServiceTypes []OptionResponse   `json:"service_types"`
	SortKeys     []string           `json:"sort_keys"`
	DefaultSort  SortResponse       `json:"default_sort"`
	Limits       PageLimitsResponse `json:"limits"`
}
