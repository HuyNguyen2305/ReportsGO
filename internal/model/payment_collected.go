package model

import "time"

// PaymentFor says what the Payments Collected report lists: payments made for invoices or for credits.
type PaymentFor string

// Payment-for choices.
const (
	PaymentForInvoices PaymentFor = "invoices"
	PaymentForCredits  PaymentFor = "credits"
)

// PaymentFors lists every valid PaymentFor.
var PaymentFors = []PaymentFor{PaymentForInvoices, PaymentForCredits}

// PaymentCollectedSortKey names a column the Payments Collected report can be sorted by. It is a closed set: the
// repository maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type PaymentCollectedSortKey string

// Payments Collected sort keys.
const (
	PaymentCollectedSortPaymentFor   PaymentCollectedSortKey = "payment_for"
	PaymentCollectedSortCustomer     PaymentCollectedSortKey = "customer"
	PaymentCollectedSortMethod       PaymentCollectedSortKey = "method"
	PaymentCollectedSortServiceState PaymentCollectedSortKey = "service_state"
	PaymentCollectedSortStaff        PaymentCollectedSortKey = "staff"
	PaymentCollectedSortDate         PaymentCollectedSortKey = "date"
	PaymentCollectedSortAmount       PaymentCollectedSortKey = "amount"
	PaymentCollectedSortWithoutTax   PaymentCollectedSortKey = "without_tax"
)

// PaymentCollectedSortKeys lists every valid PaymentCollectedSortKey.
var PaymentCollectedSortKeys = []PaymentCollectedSortKey{
	PaymentCollectedSortPaymentFor, PaymentCollectedSortCustomer, PaymentCollectedSortMethod,
	PaymentCollectedSortServiceState, PaymentCollectedSortStaff, PaymentCollectedSortDate,
	PaymentCollectedSortAmount, PaymentCollectedSortWithoutTax,
}

// PaymentCollectedSort is a sort key and direction. Ties are always broken by payment id in the same direction.
type PaymentCollectedSort struct {
	Key  PaymentCollectedSortKey
	Desc bool
}

// PaymentCollectedFilter narrows the Payments Collected report. An empty list means "All". The report only ever
// covers payments that were paid and not deleted.
type PaymentCollectedFilter struct {
	// DateFrom and DateTo bound the payment date, both inclusive. A zero value leaves that side open.
	DateFrom, DateTo time.Time
	PaymentFor       PaymentFor
	Methods          []PaymentMethod
	CustomerIDs      []int64
	StaffIDs         []int64
	// Search matches, case-insensitively and as a substring, the invoice number, account number and customer
	// name. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// PaymentCollectedCursor identifies the last row of a page: its sort value (as text) and id.
type PaymentCollectedCursor struct {
	SortValue string
	ID        int64
}

// PaymentCollectedQuery is one page request. Limit is the number of rows wanted; the repository returns up to
// Limit+1 so the caller can tell whether another page exists.
type PaymentCollectedQuery struct {
	Filter PaymentCollectedFilter
	Sort   PaymentCollectedSort
	Limit  int
	After  *PaymentCollectedCursor
}

// PaymentCollectedRow is one line of the Payments Collected report.
type PaymentCollectedRow struct {
	ID           int64
	CustomerID   int64
	CustomerName string
	// InvoiceID and InvoiceNumber are set when the payment is for an invoice and nil when it is for a credit.
	InvoiceID     *int64
	InvoiceNumber *string
	Method        PaymentMethod
	CardBrand     *string
	CardLast4     *string
	// ServiceState is the state of the service location of the invoice's job; nil when there is none.
	ServiceState *string
	// StaffID and StaffName identify who took the payment; nil for payments nobody took, such as online ones.
	StaffID   *int64
	StaffName *string
	Date      time.Time
	Amount    Money
	// WithoutTax is the amount less its share of the invoice's tax; a credit payment has none.
	WithoutTax Money
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// PaymentCollectedSummary totals the whole filtered set, not just one page.
type PaymentCollectedSummary struct {
	Count      int64
	Amount     Money
	WithoutTax Money
}

// PaymentCollectedPage is one page of the report read from a single database snapshot. Summary is nil unless it
// was requested, and when set it totals exactly the filtered set the rows were taken from.
type PaymentCollectedPage struct {
	Rows    []PaymentCollectedRow
	Summary *PaymentCollectedSummary
}

// StaffOption is a staff member option for the report's Staff filter.
type StaffOption struct {
	ID   int64
	Name string
}
