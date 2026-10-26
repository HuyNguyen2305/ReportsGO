package model

import "time"

// DocumentStatus is the lifecycle state of a document.
type DocumentStatus string

// Document statuses, matching the document_status enum.
const (
	DocumentDraft  DocumentStatus = "draft"
	DocumentSent   DocumentStatus = "sent"
	DocumentSigned DocumentStatus = "signed"
)

// DocumentStatuses lists every valid DocumentStatus.
var DocumentStatuses = []DocumentStatus{DocumentDraft, DocumentSent, DocumentSigned}

// DocumentLifecycles lists the tabs the Documents report offers. Documents are never archived, so unlike
// invoices and estimates there is no Archived tab.
var DocumentLifecycles = []Lifecycle{LifecycleActive, LifecycleDeleted}

// DocumentSortKey names a column the document report can be sorted by. It is a closed set: the repository
// maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type DocumentSortKey string

// Document sort keys.
const (
	DocumentSortNumber        DocumentSortKey = "document_number"
	DocumentSortType          DocumentSortKey = "type"
	DocumentSortAccountNumber DocumentSortKey = "account_number"
	DocumentSortCustomer      DocumentSortKey = "customer"
	DocumentSortCreatedBy     DocumentSortKey = "created_by"
	DocumentSortCreatedAt     DocumentSortKey = "created_at"
	DocumentSortStatus        DocumentSortKey = "status"
)

// DocumentSortKeys lists every valid DocumentSortKey.
var DocumentSortKeys = []DocumentSortKey{
	DocumentSortNumber, DocumentSortType, DocumentSortAccountNumber, DocumentSortCustomer,
	DocumentSortCreatedBy, DocumentSortCreatedAt, DocumentSortStatus,
}

// DocumentSort is a sort key and direction. Ties are always broken by document id in the same direction.
type DocumentSort struct {
	Key  DocumentSortKey
	Desc bool
}

// DocumentFilter narrows the document report. An empty list means "All".
type DocumentFilter struct {
	// CreatedFrom is the first instant included and CreatedBefore the first instant excluded. The service builds
	// them from calendar days in the business time zone.
	// A zero value leaves that side open.
	CreatedFrom, CreatedBefore time.Time
	Lifecycle                  Lifecycle
	Statuses                   []DocumentStatus
	TypeIDs                    []int64
	// Search matches, case-insensitively and as a substring, the document number, account number and
	// customer name. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// DocumentCursor identifies the last row of a page: its sort value (as text) and id.
type DocumentCursor struct {
	SortValue string
	ID        int64
}

// DocumentQuery is one page request. Limit is the number of rows wanted; the repository returns up to
// Limit+1 so the caller can tell whether another page exists.
type DocumentQuery struct {
	Filter DocumentFilter
	Sort   DocumentSort
	Limit  int
	After  *DocumentCursor
}

// DocumentRow is one line of the document report.
type DocumentRow struct {
	ID             int64
	DocumentNumber string
	TypeID         int64
	TypeName       string
	CustomerID     int64
	CustomerName   string
	AccountNumber  string
	CreatedBy      *string // full name of the user who created the document, nil when unknown
	CreatedAt      time.Time
	Status         DocumentStatus
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// DocumentPage is one page of the report read from a single database snapshot. Summary is nil unless it was
// requested, and when set it counts exactly the filtered set the rows were taken from.
type DocumentPage struct {
	Rows    []DocumentRow
	Summary *DocumentSummary
}

// DocumentSummary totals the whole filtered set, not just one page.
type DocumentSummary struct {
	Count int64
}

// DocumentType is a document type option for the report's Type filter.
type DocumentType struct {
	ID   int64
	Name string
}
