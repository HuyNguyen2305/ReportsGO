package model

import "time"

// TotalSaleStatus is the payment state of a sold invoice. It is derived from the invoice's status and its
// payments, so it has a value ("partial") that the invoice_status enum does not.
type TotalSaleStatus string

// Total sale statuses.
const (
	TotalSalePaid    TotalSaleStatus = "paid"
	TotalSalePartial TotalSaleStatus = "partial"
	TotalSaleSent    TotalSaleStatus = "sent"
)

// TotalSaleStatuses lists every valid TotalSaleStatus.
var TotalSaleStatuses = []TotalSaleStatus{TotalSalePaid, TotalSalePartial, TotalSaleSent}

// TotalSaleSortKey names a column the Total Sales report can be sorted by. It is a closed set: the repository
// maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type TotalSaleSortKey string

// Total sale sort keys.
const (
	TotalSaleSortInvoiceNumber TotalSaleSortKey = "invoice_number"
	TotalSaleSortCustomer      TotalSaleSortKey = "customer"
	TotalSaleSortDate          TotalSaleSortKey = "date"
	TotalSaleSortStatus        TotalSaleSortKey = "status"
	TotalSaleSortAmount        TotalSaleSortKey = "amount"
	TotalSaleSortWithoutTax    TotalSaleSortKey = "without_tax"
)

// TotalSaleSortKeys lists every valid TotalSaleSortKey.
var TotalSaleSortKeys = []TotalSaleSortKey{
	TotalSaleSortInvoiceNumber, TotalSaleSortCustomer, TotalSaleSortDate,
	TotalSaleSortStatus, TotalSaleSortAmount, TotalSaleSortWithoutTax,
}

// TotalSaleSort is a sort key and direction. Ties are always broken by invoice id in the same direction.
type TotalSaleSort struct {
	Key  TotalSaleSortKey
	Desc bool
}

// TotalSaleFilter narrows the Total Sales report. An empty list means "All". Dates are inclusive calendar dates.
// The report only ever covers active invoices that were sent or paid.
type TotalSaleFilter struct {
	DateFrom, DateTo time.Time
	Statuses         []TotalSaleStatus
	CustomerIDs      []int64
	// Search matches, case-insensitively and as a substring, the invoice number, account number and customer
	// name. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// TotalSaleCursor identifies the last row of a page: its sort value (as text) and id.
type TotalSaleCursor struct {
	SortValue string
	ID        int64
}

// TotalSaleQuery is one page request. Limit is the number of rows wanted; the repository returns up to
// Limit+1 so the caller can tell whether another page exists.
type TotalSaleQuery struct {
	Filter TotalSaleFilter
	Sort   TotalSaleSort
	Limit  int
	After  *TotalSaleCursor
}

// TotalSaleRow is one line of the Total Sales report.
type TotalSaleRow struct {
	ID            int64
	InvoiceNumber string
	CustomerID    int64
	CustomerName  string
	Date          time.Time
	Status        TotalSaleStatus
	Amount        Money
	// WithoutTax is the amount less the tax on the invoice's tax lines.
	WithoutTax Money
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// TotalSaleSummary totals the whole filtered set, not just one page.
type TotalSaleSummary struct {
	Count      int64
	Amount     Money
	WithoutTax Money
}

// TotalSalePage is one page of the report read from a single database snapshot. Summary is nil unless it was
// requested, and when set it totals exactly the filtered set the rows were taken from.
type TotalSalePage struct {
	Rows    []TotalSaleRow
	Summary *TotalSaleSummary
}
