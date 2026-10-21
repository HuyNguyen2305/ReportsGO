package dto

// Money is always a decimal string such as "148.56", never a JSON number, so no client can lose cents
// to floating point. Dates are YYYY-MM-DD.

// InvoiceResponse is one line of the Invoices report.
type InvoiceResponse struct {
	ID              int64            `json:"id"`
	InvoiceNumber   string           `json:"invoice_number"`
	AccountNumber   string           `json:"account_number"`
	Customer        CustomerRef      `json:"customer"`
	Job             *string          `json:"job"`
	Frequency       string           `json:"frequency"`
	BillingEmail    *string          `json:"billing_email"`
	Date            string           `json:"date"`
	Total           string           `json:"total"`
	Discount        string           `json:"discount"`
	Tags            []string         `json:"tags"`
	PaymentTerms    *PaymentTermsRef `json:"payment_terms"`
	Status          string           `json:"status"`
	SignatureStatus string           `json:"signature_status"`
}

// CustomerRef identifies the customer an invoice belongs to.
type CustomerRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PaymentTermsRef is the payment terms attached to an invoice.
type PaymentTermsRef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// InvoiceListResponse is one page of the Invoices report.
type InvoiceListResponse struct {
	Data []InvoiceResponse `json:"data"`
	Page PageResponse      `json:"page"`
	// Summary totals the whole filtered set. It is null on every page after the first.
	Summary *InvoiceSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// PageResponse describes the position in a paged list.
type PageResponse struct {
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

// DateRangeResponse is an inclusive date range.
type DateRangeResponse struct {
	From *string `json:"from"`
	To   *string `json:"to"`
}

// InvoiceSummaryResponse totals the filtered invoices.
type InvoiceSummaryResponse struct {
	Count       int64  `json:"count"`
	Total       string `json:"total"`
	Discount    string `json:"discount"`
	Outstanding string `json:"outstanding"`
}

// InvoiceFiltersResponse has everything the report screen needs to build its filter controls.
type InvoiceFiltersResponse struct {
	ServiceTypes      []OptionResponse    `json:"service_types"`
	Tags              []OptionResponse    `json:"tags"`
	PaymentTerms      []PaymentTermOption `json:"payment_terms"`
	Statuses          []string            `json:"statuses"`
	SignatureStatuses []string            `json:"signature_statuses"`
	Lifecycles        []string            `json:"lifecycles"`
	SortKeys          []string            `json:"sort_keys"`
	DefaultSort       SortResponse        `json:"default_sort"`
	Limits            PageLimitsResponse  `json:"limits"`
}

// OptionResponse is an id and display name.
type OptionResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PaymentTermOption is a payment terms choice.
type PaymentTermOption struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SortResponse is a sort key and direction.
type SortResponse struct {
	Key   string `json:"key"`
	Order string `json:"order"`
}

// PageLimitsResponse tells clients the page-size bounds.
type PageLimitsResponse struct {
	Default int `json:"default"`
	Max     int `json:"max"`
}
