package model

import "time"

// SalesBreakdownType says which month grid is read: the Sales Forecast (scheduled jobs, plus invoices without a job)
// or Revenue (payments received for invoices). The two share one report, like invoices and estimates share
// billing_documents.
type SalesBreakdownType string

// Sales breakdown types.
const (
	SalesBreakdownForecast SalesBreakdownType = "forecast"
	SalesBreakdownRevenue  SalesBreakdownType = "revenue"
)

// SalesBreakdownTypes lists every valid SalesBreakdownType.
var SalesBreakdownTypes = []SalesBreakdownType{SalesBreakdownForecast, SalesBreakdownRevenue}

// SalesBreakdownBy is what one row of the grid is.
type SalesBreakdownBy string

// Sales breakdown groupings.
const (
	SalesBreakdownByClient  SalesBreakdownBy = "client"
	SalesBreakdownByService SalesBreakdownBy = "service"
	SalesBreakdownByItem    SalesBreakdownBy = "item"
	SalesBreakdownByStaff   SalesBreakdownBy = "staff"
	SalesBreakdownBySource  SalesBreakdownBy = "source"
	SalesBreakdownByTag     SalesBreakdownBy = "tag"
)

// SalesBreakdownBys lists every valid SalesBreakdownBy.
var SalesBreakdownBys = []SalesBreakdownBy{
	SalesBreakdownByClient, SalesBreakdownByService, SalesBreakdownByItem,
	SalesBreakdownByStaff, SalesBreakdownBySource, SalesBreakdownByTag,
}

// SalesBreakdownMeasure is the amount Revenue adds up.
type SalesBreakdownMeasure string

// Revenue measures.
const (
	SalesBreakdownCollected           SalesBreakdownMeasure = "collected"
	SalesBreakdownCollectedWithoutTax SalesBreakdownMeasure = "collected_without_tax"
)

// SalesBreakdownMeasures lists every valid SalesBreakdownMeasure.
var SalesBreakdownMeasures = []SalesBreakdownMeasure{SalesBreakdownCollected, SalesBreakdownCollectedWithoutTax}

// SalesBreakdownSortKey names a column the grid can be sorted by. It is a closed set: the repository maps each key to
// a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type SalesBreakdownSortKey string

// Sales breakdown sort keys. AccountNumber exists for the client grouping only.
const (
	SalesBreakdownSortName          SalesBreakdownSortKey = "name"
	SalesBreakdownSortAccountNumber SalesBreakdownSortKey = "account_number"
	SalesBreakdownSortJan           SalesBreakdownSortKey = "jan"
	SalesBreakdownSortFeb           SalesBreakdownSortKey = "feb"
	SalesBreakdownSortMar           SalesBreakdownSortKey = "mar"
	SalesBreakdownSortApr           SalesBreakdownSortKey = "apr"
	SalesBreakdownSortMay           SalesBreakdownSortKey = "may"
	SalesBreakdownSortJun           SalesBreakdownSortKey = "jun"
	SalesBreakdownSortJul           SalesBreakdownSortKey = "jul"
	SalesBreakdownSortAug           SalesBreakdownSortKey = "aug"
	SalesBreakdownSortSep           SalesBreakdownSortKey = "sep"
	SalesBreakdownSortOct           SalesBreakdownSortKey = "oct"
	SalesBreakdownSortNov           SalesBreakdownSortKey = "nov"
	SalesBreakdownSortDec           SalesBreakdownSortKey = "dec"
	SalesBreakdownSortTotal         SalesBreakdownSortKey = "total"
)

// SalesBreakdownMonthKeys are the month sort keys, January first.
var SalesBreakdownMonthKeys = []SalesBreakdownSortKey{
	SalesBreakdownSortJan, SalesBreakdownSortFeb, SalesBreakdownSortMar, SalesBreakdownSortApr,
	SalesBreakdownSortMay, SalesBreakdownSortJun, SalesBreakdownSortJul, SalesBreakdownSortAug,
	SalesBreakdownSortSep, SalesBreakdownSortOct, SalesBreakdownSortNov, SalesBreakdownSortDec,
}

// SalesBreakdownSortKeysFor returns the valid sort keys of a grouping.
func SalesBreakdownSortKeysFor(by SalesBreakdownBy) []SalesBreakdownSortKey {
	keys := []SalesBreakdownSortKey{SalesBreakdownSortName}
	if by == SalesBreakdownByClient {
		keys = append(keys, SalesBreakdownSortAccountNumber)
	}
	keys = append(keys, SalesBreakdownMonthKeys...)
	return append(keys, SalesBreakdownSortTotal)
}

// SalesBreakdownSort is a sort key and direction. Ties are always broken by the group id in the same direction.
type SalesBreakdownSort struct {
	Key  SalesBreakdownSortKey
	Desc bool
}

// SalesBreakdownFilter narrows the grid.
type SalesBreakdownFilter struct {
	Type SalesBreakdownType
	By   SalesBreakdownBy
	Year int
	// MonthStarts are the first instants of January to December of Year and of the next January, in the business
	// time zone. They place a scheduled job in its month.
	MonthStarts [13]time.Time
	// YearFrom and YearTo are January 1 and December 31 of Year, for the plain calendar dates of invoices and payments.
	YearFrom, YearTo time.Time
	// GroupIDs keeps only these groups (clients, service types, items, staff, sources or tags). Empty means all.
	GroupIDs []int64
	// Measure and Methods apply to Revenue only.
	Measure SalesBreakdownMeasure
	Methods []PaymentMethod
	// Search matches, case-insensitively and as a substring, the group name (and a client's account number). It is
	// plain text: LIKE wildcards in it are matched literally.
	Search string
}

// SalesBreakdownCursor identifies the last row of a page: its sort value (as text) and group id.
type SalesBreakdownCursor struct {
	SortValue string
	ID        int64
}

// SalesBreakdownQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1
// so the caller can tell whether another page exists.
type SalesBreakdownQuery struct {
	Filter SalesBreakdownFilter
	Sort   SalesBreakdownSort
	Limit  int
	After  *SalesBreakdownCursor
}

// SalesBreakdownAmounts is one line of money: each month of the year and their total.
type SalesBreakdownAmounts struct {
	Months [12]Money
	Total  Money
}

// SalesBreakdownRow is one group of the grid.
type SalesBreakdownRow struct {
	ID   int64
	Name string
	// AccountNumber is set for the client grouping only.
	AccountNumber *string
	SalesBreakdownAmounts
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// SalesBreakdownSummary is the true total of the report: every amount counted once, even when the grouping (tags)
// puts it under several rows.
type SalesBreakdownSummary struct {
	Count int64
	SalesBreakdownAmounts
}

// SalesBreakdownPage is one page of the grid read from a single database snapshot. Unassigned and Summary are nil
// unless the summary was requested; Unassigned is also nil when a group or search filter is set.
type SalesBreakdownPage struct {
	Rows       []SalesBreakdownRow
	Unassigned *SalesBreakdownAmounts
	Summary    *SalesBreakdownSummary
}

// SalesBreakdownGroupOptions are the choices of the group filter for each grouping but clients.
type SalesBreakdownGroupOptions struct {
	ServiceTypes []ServiceType
	Items        []ItemOption
	Staff        []StaffOption
	Sources      []LeadSource
	Tags         []JobTag
}

// ItemOption is a catalog item option for the group filter.
type ItemOption struct {
	ID   int64
	Name string
}
