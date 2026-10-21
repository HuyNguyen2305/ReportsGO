package model

import "time"

// InvoiceStatus is the lifecycle state of an invoice.
type InvoiceStatus string

// Invoice statuses, matching the invoice_status enum.
const (
	InvoiceDraft InvoiceStatus = "draft"
	InvoiceSent  InvoiceStatus = "sent"
	InvoicePaid  InvoiceStatus = "paid"
	InvoiceVoid  InvoiceStatus = "void"
)

// InvoiceStatuses lists every valid InvoiceStatus.
var InvoiceStatuses = []InvoiceStatus{InvoiceDraft, InvoiceSent, InvoicePaid, InvoiceVoid}

// SignatureStatus is the signature state of an invoice or estimate.
type SignatureStatus string

// Signature statuses, matching the signature_status enum.
const (
	SignatureDraft    SignatureStatus = "draft"
	SignaturePending  SignatureStatus = "pending"
	SignatureSigned   SignatureStatus = "signed"
	SignatureDeclined SignatureStatus = "declined"
)

// SignatureStatuses lists every valid SignatureStatus.
var SignatureStatuses = []SignatureStatus{SignatureDraft, SignaturePending, SignatureSigned, SignatureDeclined}

// Lifecycle selects the Active / Archived / Deleted tab.
type Lifecycle string

// Lifecycle tabs. Active excludes archived and deleted rows; Archived excludes deleted ones.
const (
	LifecycleActive   Lifecycle = "active"
	LifecycleArchived Lifecycle = "archived"
	LifecycleDeleted  Lifecycle = "deleted"
)

// Lifecycles lists every valid Lifecycle.
var Lifecycles = []Lifecycle{LifecycleActive, LifecycleArchived, LifecycleDeleted}

// InvoiceSortKey names a column the invoice report can be sorted by. It is a closed set: the repository
// maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type InvoiceSortKey string

// Invoice sort keys.
const (
	SortAccountNumber   InvoiceSortKey = "account_number"
	SortInvoiceNumber   InvoiceSortKey = "invoice_number"
	SortCustomer        InvoiceSortKey = "customer"
	SortJob             InvoiceSortKey = "job"
	SortBillingEmail    InvoiceSortKey = "billing_email"
	SortDate            InvoiceSortKey = "date"
	SortTotal           InvoiceSortKey = "total"
	SortDiscount        InvoiceSortKey = "discount"
	SortStatus          InvoiceSortKey = "status"
	SortSignatureStatus InvoiceSortKey = "signature_status"
	SortPaymentTerms    InvoiceSortKey = "payment_terms"
)

// InvoiceSortKeys lists every valid InvoiceSortKey.
var InvoiceSortKeys = []InvoiceSortKey{
	SortAccountNumber, SortInvoiceNumber, SortCustomer, SortJob, SortBillingEmail, SortDate,
	SortTotal, SortDiscount, SortStatus, SortSignatureStatus, SortPaymentTerms,
}

// InvoiceSort is a sort key and direction. Ties are always broken by invoice id in the same direction.
type InvoiceSort struct {
	Key  InvoiceSortKey
	Desc bool
}

// InvoiceFilter narrows the invoice report. An empty list means "All". Dates are inclusive calendar dates.
type InvoiceFilter struct {
	DateFrom, DateTo time.Time
	Lifecycle        Lifecycle
	Statuses         []InvoiceStatus
	SignatureStatus  []SignatureStatus
	ServiceTypeIDs   []int64
	TagIDs           []int64 // an invoice matches if it has any of these tags
	PaymentTermIDs   []int64
	// Search matches, case-insensitively and as a substring, the invoice number, account number,
	// customer name and billing email. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// InvoiceCursor identifies the last row of a page: its sort value (as text) and id.
type InvoiceCursor struct {
	SortValue string
	ID        int64
}

// InvoiceQuery is one page request. Limit is the number of rows wanted; the repository returns up to
// Limit+1 so the caller can tell whether another page exists.
type InvoiceQuery struct {
	Filter InvoiceFilter
	Sort   InvoiceSort
	Limit  int
	After  *InvoiceCursor
}

// InvoiceRow is one line of the invoice report.
type InvoiceRow struct {
	ID                      int64
	InvoiceNumber           string
	AccountNumber           string
	CustomerID              int64
	CustomerName            string
	Job                     *string // "Service Type (address line 1)", nil when the invoice has no job
	FrequencyLabel          string
	BillingEmail            *string
	Date                    time.Time
	Total                   Money
	Discount                Money
	Tags                    []string
	PaymentTermsName        *string
	PaymentTermsDescription *string
	Status                  InvoiceStatus
	SignatureStatus         SignatureStatus
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// InvoiceSummary totals the whole filtered set, not just one page.
type InvoiceSummary struct {
	Count    int64
	Total    Money
	Discount Money
	// Outstanding is the unpaid balance (total minus payments) of the filtered invoices with status "sent".
	Outstanding Money
}

// ServiceType is a service type option for the report's Service filter.
type ServiceType struct {
	ID   int64
	Name string
}

// InvoiceTag is a tag option for the report's Invoice Tags filter.
type InvoiceTag struct {
	ID   int64
	Name string
}

// PaymentTerm is a payment terms option for the report's Payment Terms filter.
type PaymentTerm struct {
	ID          int64
	Name        string
	Description string
}
