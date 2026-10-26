package model

import "time"

// PaymentStatus is the state of a payment, matching the payment_status enum.
type PaymentStatus string

// Payment statuses.
const (
	PaymentPaid     PaymentStatus = "paid"
	PaymentPending  PaymentStatus = "pending"
	PaymentFailed   PaymentStatus = "failed"
	PaymentRefunded PaymentStatus = "refunded"
)

// PaymentStatuses lists every valid PaymentStatus.
var PaymentStatuses = []PaymentStatus{PaymentPaid, PaymentPending, PaymentFailed, PaymentRefunded}

// PaymentMethod is how a payment was made, matching the payment_method_type enum.
type PaymentMethod string

// Payment methods.
const (
	MethodCash  PaymentMethod = "cash"
	MethodCheck PaymentMethod = "check"
	MethodCard  PaymentMethod = "card"
	MethodOther PaymentMethod = "other"
)

// PaymentMethods lists every valid PaymentMethod.
var PaymentMethods = []PaymentMethod{MethodCash, MethodCheck, MethodCard, MethodOther}

// PaymentLifecycles lists the tabs the Payments report offers. Payments are never archived, so there is no
// Archived tab.
var PaymentLifecycles = []Lifecycle{LifecycleActive, LifecycleDeleted}

// PaymentSortKey names a column the payment report can be sorted by. It is a closed set: the repository maps
// each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type PaymentSortKey string

// Payment sort keys.
const (
	PaymentSortPaymentFor PaymentSortKey = "payment_for"
	PaymentSortCustomer   PaymentSortKey = "customer"
	PaymentSortMethod     PaymentSortKey = "method"
	PaymentSortDate       PaymentSortKey = "payment_date"
	PaymentSortAmount     PaymentSortKey = "amount"
	PaymentSortStatus     PaymentSortKey = "status"
)

// PaymentSortKeys lists every valid PaymentSortKey.
var PaymentSortKeys = []PaymentSortKey{
	PaymentSortPaymentFor, PaymentSortCustomer, PaymentSortMethod,
	PaymentSortDate, PaymentSortAmount, PaymentSortStatus,
}

// PaymentSort is a sort key and direction. Ties are always broken by payment id in the same direction.
type PaymentSort struct {
	Key  PaymentSortKey
	Desc bool
}

// PaymentFilter narrows the payment report. An empty list means "All".
type PaymentFilter struct {
	// DateFrom and DateTo bound the payment date, both inclusive. A zero value leaves that side open.
	DateFrom, DateTo time.Time
	Lifecycle        Lifecycle
	Statuses         []PaymentStatus
	Methods          []PaymentMethod
	// Search matches, case-insensitively and as a substring, the invoice number, account number and customer
	// name. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// PaymentCursor identifies the last row of a page: its sort value (as text) and id.
type PaymentCursor struct {
	SortValue string
	ID        int64
}

// PaymentQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1
// so the caller can tell whether another page exists.
type PaymentQuery struct {
	Filter PaymentFilter
	Sort   PaymentSort
	Limit  int
	After  *PaymentCursor
}

// PaymentRow is one line of the payment report.
type PaymentRow struct {
	ID            int64
	CustomerID    int64
	CustomerName  string
	AccountNumber string
	// InvoiceID and InvoiceNumber are set when the payment is for an invoice and nil when it is for a credit.
	InvoiceID     *int64
	InvoiceNumber *string
	Method        PaymentMethod
	CardBrand     *string
	CardLast4     *string
	Amount        Money
	Date          time.Time
	Status        PaymentStatus
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// PaymentPage is one page of the report read from a single database snapshot. Summary is nil unless it was
// requested, and when set it totals exactly the filtered set the rows were taken from.
type PaymentPage struct {
	Rows    []PaymentRow
	Summary *PaymentSummary
}

// PaymentSummary totals the whole filtered set, not just one page. Total adds up every payment in scope,
// whatever its status; narrow the Status filter to total only some.
type PaymentSummary struct {
	Count int64
	Total Money
}
