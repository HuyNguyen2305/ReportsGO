package model

import "time"

// ServiceLookupTab says which jobs Service Lookup shows. Active jobs are neither deleted nor in the work pool; the
// work pool holds the jobs not yet placed on the schedule (deleted ones excluded); deleted jobs are the deleted ones,
// whether or not they were in the pool.
type ServiceLookupTab string

// Service Lookup tabs.
const (
	ServiceLookupActive   ServiceLookupTab = "active"
	ServiceLookupWorkPool ServiceLookupTab = "work_pool"
	ServiceLookupDeleted  ServiceLookupTab = "deleted"
)

// ServiceLookupTabs lists every valid ServiceLookupTab.
var ServiceLookupTabs = []ServiceLookupTab{ServiceLookupActive, ServiceLookupWorkPool, ServiceLookupDeleted}

// ServiceLookupSortKey names a column Service Lookup can be sorted by. It is a closed set: the repository maps each
// key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type ServiceLookupSortKey string

// Service Lookup sort keys.
const (
	ServiceLookupSortCustomer     ServiceLookupSortKey = "customer"
	ServiceLookupSortServiceType  ServiceLookupSortKey = "service_type"
	ServiceLookupSortScheduledAt  ServiceLookupSortKey = "scheduled_at"
	ServiceLookupSortAddress      ServiceLookupSortKey = "address"
	ServiceLookupSortCity         ServiceLookupSortKey = "city"
	ServiceLookupSortZip          ServiceLookupSortKey = "zip"
	ServiceLookupSortCounty       ServiceLookupSortKey = "county"
	ServiceLookupSortInvoiceValue ServiceLookupSortKey = "invoice_value"
	ServiceLookupSortWithoutTax   ServiceLookupSortKey = "without_tax"
	ServiceLookupSortStatus       ServiceLookupSortKey = "status"
	ServiceLookupSortAssignedTo   ServiceLookupSortKey = "assigned_to"
)

// ServiceLookupSortKeys lists every valid ServiceLookupSortKey.
var ServiceLookupSortKeys = []ServiceLookupSortKey{
	ServiceLookupSortCustomer, ServiceLookupSortServiceType, ServiceLookupSortScheduledAt, ServiceLookupSortAddress,
	ServiceLookupSortCity, ServiceLookupSortZip, ServiceLookupSortCounty, ServiceLookupSortInvoiceValue,
	ServiceLookupSortWithoutTax, ServiceLookupSortStatus, ServiceLookupSortAssignedTo,
}

// ServiceLookupSort is a sort key and direction. Ties are always broken by job id in the same direction.
type ServiceLookupSort struct {
	Key  ServiceLookupSortKey
	Desc bool
}

// ServiceLookupFilter narrows Service Lookup. An empty list means "All".
type ServiceLookupFilter struct {
	Tab ServiceLookupTab
	// ScheduledFrom is the first instant included and ScheduledBefore the first instant excluded. The service builds
	// them from calendar days in the business time zone. A zero value leaves that side open; a job without a
	// schedule never matches a bounded side.
	ScheduledFrom, ScheduledBefore time.Time
	Statuses                       []JobStatus
	ServiceTypeIDs                 []int64
	AssignedToIDs                  []int64
	// TagIDs keeps the jobs that have any of these tags.
	TagIDs []int64
	// Search matches, case-insensitively and as a substring, the customer name, account number, phone, service type
	// and the address and city. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// ServiceLookupCursor identifies the last row of a page: its sort value (as text) and id.
type ServiceLookupCursor struct {
	SortValue string
	ID        int64
}

// ServiceLookupQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1 so
// the caller can tell whether another page exists.
type ServiceLookupQuery struct {
	Filter ServiceLookupFilter
	Sort   ServiceLookupSort
	Limit  int
	After  *ServiceLookupCursor
}

// LookupAddress is the service address of a job.
type LookupAddress struct {
	Line1, Line2, City, State, Zip, County string
}

// JobTag is a tag on a job.
type JobTag struct {
	ID   int64
	Name string
}

// ServiceLookupRow is one job of Service Lookup.
type ServiceLookupRow struct {
	ID            int64
	CustomerID    int64
	CustomerName  string
	Phone         *string
	ServiceTypeID int64
	ServiceType   string
	// ScheduledAt is nil for a job that was never scheduled.
	ScheduledAt  *time.Time
	Address      LookupAddress
	InvoiceValue Money
	// WithoutTax is the invoice value less the job's tax.
	WithoutTax Money
	Status     JobStatus
	// AssignedToID and AssignedTo are nil for an unassigned job.
	AssignedToID *int64
	AssignedTo   *string
	// Tags are ordered by name.
	Tags []JobTag
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// ServiceLookupTabCounts is the number of jobs each tab would show under the same filters.
type ServiceLookupTabCounts struct {
	Active, WorkPool, Deleted int64
}

// ServiceLookupSummary totals the whole filtered set of the selected tab, not just one page, and counts the other tabs.
type ServiceLookupSummary struct {
	Count        int64
	InvoiceValue Money
	WithoutTax   Money
	Tabs         ServiceLookupTabCounts
}

// ServiceLookupPage is one page of the report read from a single database snapshot. Summary is nil unless it was
// requested, and when set it totals exactly the filtered set the rows were taken from.
type ServiceLookupPage struct {
	Rows    []ServiceLookupRow
	Summary *ServiceLookupSummary
}
