package dto

// PaymentResponse is one line of the Payments report. Amount is a decimal string and PaymentDate is YYYY-MM-DD.
type PaymentResponse struct {
	ID            int64                 `json:"id"`
	Customer      CustomerRef           `json:"customer"`
	AccountNumber string                `json:"account_number"`
	PaymentFor    PaymentForResponse    `json:"payment_for"`
	Method        PaymentMethodResponse `json:"method"`
	Amount        string                `json:"amount"`
	PaymentDate   string                `json:"payment_date"`
	Status        string                `json:"status"`
}

// PaymentForResponse says what a payment was made for: an invoice (Label is its number) or a credit.
type PaymentForResponse struct {
	Type      string `json:"type"`
	InvoiceID *int64 `json:"invoice_id"`
	Label     string `json:"label"`
}

// PaymentMethodResponse is how a payment was made. The card fields are null unless it was a card.
type PaymentMethodResponse struct {
	Type      string  `json:"type"`
	CardBrand *string `json:"card_brand"`
	CardLast4 *string `json:"card_last4"`
}

// PaymentListResponse is one page of the Payments report.
type PaymentListResponse struct {
	Data []PaymentResponse `json:"data"`
	Page PageResponse      `json:"page"`
	// Summary totals the whole filtered set. It is null on every page after the first.
	Summary *PaymentSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// PaymentSummaryResponse counts and totals the filtered payments, whatever their status.
type PaymentSummaryResponse struct {
	Count int64  `json:"count"`
	Total string `json:"total"`
}

// PaymentFiltersResponse has everything the report screen needs to build its filter controls.
type PaymentFiltersResponse struct {
	Statuses    []string           `json:"statuses"`
	Methods     []string           `json:"methods"`
	Lifecycles  []string           `json:"lifecycles"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
