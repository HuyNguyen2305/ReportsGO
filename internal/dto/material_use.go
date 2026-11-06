package dto

// MaterialUseResponse is one line of the Material Use report: one material added up over the jobs in scope. Qty is a
// decimal string with three places, for example "12.500". EPA is the material's EPA registration number, null when it
// has none.
type MaterialUseResponse struct {
	MaterialID   int64   `json:"material_id"`
	Material     string  `json:"material"`
	EPA          *string `json:"epa"`
	Qty          string  `json:"qty"`
	Applications int64   `json:"applications"`
}

// MaterialUseListResponse is one page of the Material Use report.
type MaterialUseListResponse struct {
	Data []MaterialUseResponse `json:"data"`
	Page PageResponse          `json:"page"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// MaterialUseFiltersResponse has everything the report screen needs to build its filter controls.
type MaterialUseFiltersResponse struct {
	Materials   []OptionResponse   `json:"materials"`
	Clients     []OptionResponse   `json:"clients"`
	Tabs        []string           `json:"tabs"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
