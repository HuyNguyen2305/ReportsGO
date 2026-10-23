package dto

// RecurringResponse is one line of the Recurring Invoices report. TotalValue is a decimal string; StartedOn and
// EndsOn are YYYY-MM-DD.
type RecurringResponse struct {
	ID            int64               `json:"id"`
	Customer      CustomerRef         `json:"customer"`
	AccountNumber string              `json:"account_number"`
	Repeats       RepeatsResponse     `json:"repeats"`
	Action        string              `json:"action"`
	Status        string              `json:"status"`
	TotalValue    string              `json:"total_value"`
	StartedOn     string              `json:"started_on"`
	EndsOn        *string             `json:"ends_on"`
	NextInvoice   NextInvoiceResponse `json:"next_invoice"`
}

// RepeatsResponse is the repeat rule. Unit, Interval, Weekday (0 = Sunday to 6 = Saturday) and Occurrences are
// the contract; Label is a ready-made description such as "Every week on Thursday, 3 times".
type RepeatsResponse struct {
	Unit        string `json:"unit"`
	Interval    int    `json:"interval"`
	Weekday     *int   `json:"weekday"`
	Occurrences *int   `json:"occurrences"`
	Label       string `json:"label"`
}

// NextInvoiceResponse is the next invoice a recurring invoice will produce. The object is always present; each
// part is null when it is not known.
type NextInvoiceResponse struct {
	InvoiceID     *int64  `json:"invoice_id"`
	InvoiceNumber *string `json:"invoice_number"`
	Date          *string `json:"date"`
}

// RecurringListResponse is one page of the Recurring Invoices report.
type RecurringListResponse struct {
	Data []RecurringResponse `json:"data"`
	Page PageResponse        `json:"page"`
	// Summary counts and totals the whole filtered set. It is null on every page after the first.
	Summary *RecurringSummaryResponse `json:"summary"`
	// Range is the start-date range actually applied. Both ends are null when no dates were sent: this report
	// has no default month.
	Range DateRangeResponse `json:"range"`
}

// RecurringSummaryResponse counts and totals the filtered recurring invoices.
type RecurringSummaryResponse struct {
	Count int64  `json:"count"`
	Total string `json:"total"`
}

// RecurringFiltersResponse has everything the report screen needs to build its filter controls. Recurring
// invoices have no Active/Deleted tab, so there is no list for one.
type RecurringFiltersResponse struct {
	Statuses    []string           `json:"statuses"`
	Actions     []string           `json:"actions"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
