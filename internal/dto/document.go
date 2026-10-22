package dto

// DocumentResponse is one line of the Documents report. CreatedAt is always UTC with six fractional digits,
// for example "2026-10-22T08:15:30.123456Z".
type DocumentResponse struct {
	ID             int64          `json:"id"`
	DocumentNumber string         `json:"document_number"`
	DocumentType   OptionResponse `json:"document_type"`
	Customer       CustomerRef    `json:"customer"`
	AccountNumber  string         `json:"account_number"`
	CreatedBy      *string        `json:"created_by"`
	CreatedAt      string         `json:"created_at"`
	Status         string         `json:"status"`
}

// DocumentListResponse is one page of the Documents report.
type DocumentListResponse struct {
	Data []DocumentResponse `json:"data"`
	Page PageResponse       `json:"page"`
	// Summary counts the whole filtered set. It is null on every page after the first.
	Summary *DocumentSummaryResponse `json:"summary"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// DocumentSummaryResponse counts the filtered documents.
type DocumentSummaryResponse struct {
	Count int64 `json:"count"`
}

// DocumentFiltersResponse has everything the report screen needs to build its filter controls.
type DocumentFiltersResponse struct {
	DocumentTypes []OptionResponse   `json:"document_types"`
	Statuses      []string           `json:"statuses"`
	Lifecycles    []string           `json:"lifecycles"`
	SortKeys      []string           `json:"sort_keys"`
	DefaultSort   SortResponse       `json:"default_sort"`
	Limits        PageLimitsResponse `json:"limits"`
}
