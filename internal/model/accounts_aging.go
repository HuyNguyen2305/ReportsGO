package model

import "time"

// AgingSortKey names a column the Accounts Aging report can be sorted by. It is a closed set: the repository maps
// each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type AgingSortKey string

// Accounts Aging sort keys.
const (
	AgingSortCustomer        AgingSortKey = "customer"
	AgingSortTotal           AgingSortKey = "total"
	AgingSortD0to30          AgingSortKey = "d0_30"
	AgingSortD31to60         AgingSortKey = "d31_60"
	AgingSortD61to90         AgingSortKey = "d61_90"
	AgingSortD90Plus         AgingSortKey = "d90_plus"
	AgingSortAvailableCredit AgingSortKey = "available_credit"
)

// AgingSortKeys lists every valid AgingSortKey.
var AgingSortKeys = []AgingSortKey{
	AgingSortCustomer, AgingSortTotal, AgingSortD0to30, AgingSortD31to60, AgingSortD61to90, AgingSortD90Plus,
	AgingSortAvailableCredit,
}

// AgingMinAgeDays lists the values of the Days filter: keep only rows with a balance older than that many days.
var AgingMinAgeDays = []int{30, 60, 90}

// AgingSort is a sort key and direction. Ties are always broken by the row key in the same direction.
type AgingSort struct {
	Key  AgingSortKey
	Desc bool
}

// AgingFilter narrows the Accounts Aging report.
type AgingFilter struct {
	// AsOf is the day the balances are read at: invoices dated after it, and payments and credit applications
	// dated after it, do not count. It is always set; the Current tab is simply today.
	AsOf time.Time
	// MinAgeDays keeps only rows with an unpaid balance older than this many days (30, 60 or 90); 0 keeps all.
	MinAgeDays int
	// ServiceTypeIDs keeps only invoices whose job is of one of these types. Empty means all.
	ServiceTypeIDs []int64
	// Email matches, case-insensitively and as a substring, the invoice's billing email. Plain text.
	Email string
	// Search matches the customer name, account number and service address. Plain text.
	Search string
}

// AgingCursor identifies the last row of a page: its sort value (as text) and row key.
type AgingCursor struct {
	SortValue string
	ID        int64
}

// AgingQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1 so the
// caller can tell whether another page exists.
type AgingQuery struct {
	Filter AgingFilter
	Sort   AgingSort
	Limit  int
	After  *AgingCursor
}

// AgingCard is a customer's default saved card.
type AgingCard struct {
	Processor string
	Last4     string
	ExpMonth  int
	ExpYear   int
}

// AgingRow is one line of the Accounts Aging report: a customer and service location with their open balance.
type AgingRow struct {
	// ID is the row key used by cursors: unique per customer and location, always positive.
	ID         int64
	CustomerID int64
	// LocationID and Location are nil for invoices whose job has no service location (or that have no job).
	LocationID   *int64
	CustomerName string
	Location     *string
	BillingEmail *string
	Phone        *string
	Card         *AgingCard

	D0to30, D31to60, D61to90, D90Plus Money
	Total                             Money
	// AvailableCredit belongs to the customer, so it repeats on each of the customer's rows.
	AvailableCredit Money
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// AgingSummary totals the whole filtered set, not just one page.
type AgingSummary struct {
	Count                             int64
	D0to30, D31to60, D61to90, D90Plus Money
	Total                             Money
	// AvailableCredit counts each customer once, however many rows the customer has.
	AvailableCredit Money
}

// AgingPage is one page of the report read from a single database snapshot. Summary is nil unless it was
// requested, and when set it totals exactly the filtered set the rows were taken from.
type AgingPage struct {
	Rows    []AgingRow
	Summary *AgingSummary
}
