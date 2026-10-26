package model

import "time"

// CreditSortKey names a column the credit report can be sorted by. It is a closed set: the repository maps
// each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type CreditSortKey string

// Credit sort keys.
const (
	CreditSortCustomer      CreditSortKey = "customer"
	CreditSortAccountNumber CreditSortKey = "account_number"
	CreditSortMethod        CreditSortKey = "method"
	CreditSortAmount        CreditSortKey = "amount"
	CreditSortDate          CreditSortKey = "credit_date"
	CreditSortCreatedBy     CreditSortKey = "created_by"
)

// CreditSortKeys lists every valid CreditSortKey.
var CreditSortKeys = []CreditSortKey{
	CreditSortCustomer, CreditSortAccountNumber, CreditSortMethod,
	CreditSortAmount, CreditSortDate, CreditSortCreatedBy,
}

// CreditSort is a sort key and direction. Ties are always broken by credit id in the same direction.
type CreditSort struct {
	Key  CreditSortKey
	Desc bool
}

// CreditFilter narrows the credit report. An empty list means "All". Credits have no status and cannot be
// deleted or archived, so there is no tab or status filter.
type CreditFilter struct {
	// DateFrom and DateTo bound the credit date, both inclusive. A zero value leaves that side open.
	DateFrom, DateTo time.Time
	// Methods reuses PaymentMethod: credits and payments share the payment_method_type enum.
	Methods []PaymentMethod
	// Search matches, case-insensitively and as a substring, the customer name, account number and method
	// detail. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// CreditCursor identifies the last row of a page: its sort value (as text) and id.
type CreditCursor struct {
	SortValue string
	ID        int64
}

// CreditQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1
// so the caller can tell whether another page exists.
type CreditQuery struct {
	Filter CreditFilter
	Sort   CreditSort
	Limit  int
	After  *CreditCursor
}

// CreditRow is one line of the credit report.
type CreditRow struct {
	ID            int64
	CustomerID    int64
	CustomerName  string
	AccountNumber string
	Method        PaymentMethod
	// MethodDetail is the stored description of the method, such as "Stripe **** 3222 Credit"; may be empty.
	MethodDetail string
	Amount       Money
	Date         time.Time
	CreatedBy    *string // full name of the user who created the credit, nil when unknown
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// CreditPage is one page of the report read from a single database snapshot. Summary is nil unless it was
// requested, and when set it totals exactly the filtered set the rows were taken from.
type CreditPage struct {
	Rows    []CreditRow
	Summary *CreditSummary
}

// CreditSummary totals the whole filtered set, not just one page.
type CreditSummary struct {
	Count int64
	Total Money
}
