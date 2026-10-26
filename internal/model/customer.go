package model

import "time"

// CustomerSortKey names a column the new customers report can be sorted by. It is a closed set: the repository
// maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type CustomerSortKey string

// Customer sort keys.
const (
	CustomerSortCustomer      CustomerSortKey = "customer"
	CustomerSortAccountNumber CustomerSortKey = "account_number"
	CustomerSortEmail         CustomerSortKey = "email"
	CustomerSortPhone         CustomerSortKey = "phone"
	CustomerSortSource        CustomerSortKey = "source"
	CustomerSortCreatedAt     CustomerSortKey = "created_at"
)

// CustomerSortKeys lists every valid CustomerSortKey.
var CustomerSortKeys = []CustomerSortKey{
	CustomerSortCustomer, CustomerSortAccountNumber, CustomerSortEmail, CustomerSortPhone,
	CustomerSortSource, CustomerSortCreatedAt,
}

// CustomerSort is a sort key and direction. Ties are always broken by customer id in the same direction.
type CustomerSort struct {
	Key  CustomerSortKey
	Desc bool
}

// CustomerFilter narrows the new customers report. An empty list means "All". Customers have neither a status
// nor a tab.
type CustomerFilter struct {
	// CreatedFrom is the first instant included and CreatedBefore the first instant excluded. The service builds
	// them from calendar days in the business time zone.
	// A zero value leaves that side open.
	CreatedFrom, CreatedBefore time.Time
	LeadSourceIDs              []int64
	// Search matches, case-insensitively and as a substring, the customer name, account number, email and the
	// phone shown in the report. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// CustomerCursor identifies the last row of a page: its sort value (as text) and id.
type CustomerCursor struct {
	SortValue string
	ID        int64
}

// CustomerQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1 so
// the caller can tell whether another page exists.
type CustomerQuery struct {
	Filter CustomerFilter
	Sort   CustomerSort
	Limit  int
	After  *CustomerCursor
}

// CustomerRow is one line of the new customers report.
type CustomerRow struct {
	ID            int64
	Name          string
	AccountNumber string
	// Email and Phone are nil when none is recorded. Phone is the customer's first one.
	Email *string
	Phone *string
	// SourceID and Source describe where the customer came from; both are nil when no lead source is recorded.
	SourceID  *int64
	Source    *string
	CreatedAt time.Time
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// CustomerSummary counts the whole filtered set, not just one page.
type CustomerSummary struct {
	Count int64
}
