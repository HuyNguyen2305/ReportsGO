package dto

// SalesBreakdownRowResponse is one group of the Revenue or Sales Forecast grid. Months holds January to December;
// money is a decimal string.
type SalesBreakdownRowResponse struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	AccountNumber *string  `json:"account_number"`
	Months        []string `json:"months"`
	Total         string   `json:"total"`
}

// SalesBreakdownUnassignedResponse is the amount that belongs to no group, such as invoices not attached to a job.
type SalesBreakdownUnassignedResponse struct {
	Label  string   `json:"label"`
	Months []string `json:"months"`
	Total  string   `json:"total"`
}

// SalesBreakdownSummaryResponse is the true total of the report, each amount counted once.
type SalesBreakdownSummaryResponse struct {
	Count  int64    `json:"count"`
	Months []string `json:"months"`
	Total  string   `json:"total"`
}

// SalesBreakdownListResponse is one page of the grid.
type SalesBreakdownListResponse struct {
	Data []SalesBreakdownRowResponse `json:"data"`
	// Unassigned is null on later pages, when it is zero, and when a group or search filter is set.
	Unassigned *SalesBreakdownUnassignedResponse `json:"unassigned"`
	Page       PageResponse                      `json:"page"`
	// Summary is null on every page after the first.
	Summary *SalesBreakdownSummaryResponse `json:"summary"`
	Year    int                            `json:"year"`
	Type    string                         `json:"type"`
	By      string                         `json:"by"`
}

// SalesBreakdownGroupsResponse lists the group filter's choices per grouping. Clients are found by search instead.
type SalesBreakdownGroupsResponse struct {
	Service []OptionResponse `json:"service"`
	Item    []OptionResponse `json:"item"`
	Staff   []OptionResponse `json:"staff"`
	Source  []OptionResponse `json:"source"`
	Tag     []OptionResponse `json:"tag"`
}

// SalesBreakdownFiltersResponse has everything the screens need to build their filter controls.
type SalesBreakdownFiltersResponse struct {
	Types       []string                     `json:"types"`
	Bys         []string                     `json:"bys"`
	Measures    []string                     `json:"measures"`
	Methods     []string                     `json:"methods"`
	Groups      SalesBreakdownGroupsResponse `json:"groups"`
	SortKeys    []string                     `json:"sort_keys"`
	DefaultSort SortResponse                 `json:"default_sort"`
	Years       YearsResponse                `json:"years"`
	Limits      PageLimitsResponse           `json:"limits"`
}

// YearsResponse is the range of the year parameter.
type YearsResponse struct {
	Min int `json:"min"`
	Max int `json:"max"`
}
