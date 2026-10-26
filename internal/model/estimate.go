package model

import "time"

// EstimateStatus is the lifecycle state of an estimate.
type EstimateStatus string

// Estimate statuses. They differ from invoice statuses; the database enforces the split per parent type.
const (
	EstimateDraft    EstimateStatus = "draft"
	EstimateSent     EstimateStatus = "sent"
	EstimateAccepted EstimateStatus = "accepted"
	EstimateDeclined EstimateStatus = "declined"
)

// EstimateStatuses lists every valid EstimateStatus.
var EstimateStatuses = []EstimateStatus{EstimateDraft, EstimateSent, EstimateAccepted, EstimateDeclined}

// EstimateSortKey names a column the estimate report can be sorted by. It is a closed set: the repository
// maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type EstimateSortKey string

// Estimate sort keys.
const (
	EstimateSortAccountNumber   EstimateSortKey = "account_number"
	EstimateSortNumber          EstimateSortKey = "estimate_number"
	EstimateSortCustomer        EstimateSortKey = "customer"
	EstimateSortPhone           EstimateSortKey = "phone"
	EstimateSortBillingEmail    EstimateSortKey = "billing_email"
	EstimateSortCreatedBy       EstimateSortKey = "created_by"
	EstimateSortDate            EstimateSortKey = "date"
	EstimateSortTotal           EstimateSortKey = "total"
	EstimateSortStatus          EstimateSortKey = "status"
	EstimateSortSignatureStatus EstimateSortKey = "signature_status"
)

// EstimateSortKeys lists every valid EstimateSortKey.
var EstimateSortKeys = []EstimateSortKey{
	EstimateSortAccountNumber, EstimateSortNumber, EstimateSortCustomer, EstimateSortPhone,
	EstimateSortBillingEmail, EstimateSortCreatedBy, EstimateSortDate, EstimateSortTotal,
	EstimateSortStatus, EstimateSortSignatureStatus,
}

// EstimateSort is a sort key and direction. Ties are always broken by estimate id in the same direction.
type EstimateSort struct {
	Key  EstimateSortKey
	Desc bool
}

// EstimateFilter narrows the estimate report. An empty list means "All". Dates are inclusive calendar dates.
type EstimateFilter struct {
	DateFrom, DateTo time.Time
	Lifecycle        Lifecycle
	Statuses         []EstimateStatus
	SignatureStatus  []SignatureStatus
	// Search matches, case-insensitively and as a substring, the estimate number, account number,
	// customer name and billing email. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// EstimateCursor identifies the last row of a page: its sort value (as text) and id.
type EstimateCursor struct {
	SortValue string
	ID        int64
}

// EstimateQuery is one page request. Limit is the number of rows wanted; the repository returns up to
// Limit+1 so the caller can tell whether another page exists.
type EstimateQuery struct {
	Filter EstimateFilter
	Sort   EstimateSort
	Limit  int
	After  *EstimateCursor
}

// EstimateRow is one line of the estimate report.
type EstimateRow struct {
	ID             int64
	EstimateNumber string
	AccountNumber  string
	CustomerID     int64
	CustomerName   string
	Phone          *string // the customer's first phone number, nil when the customer has none
	BillingEmail   *string
	CreatedBy      *string // full name of the user who created the estimate, nil when unknown
	Date           time.Time
	Total          Money
	Status         EstimateStatus
	Signature      SignatureStatus
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// EstimatePage is one page of the report read from a single database snapshot. Summary is nil unless it was
// requested, and when set it totals exactly the filtered set the rows were taken from.
type EstimatePage struct {
	Rows    []EstimateRow
	Summary *EstimateSummary
}

// EstimateSummary totals the whole filtered set, not just one page.
type EstimateSummary struct {
	Count int64
	Total Money
}
