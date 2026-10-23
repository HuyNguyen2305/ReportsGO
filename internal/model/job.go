package model

import "time"

// JobStatus is the state of a job, matching the job_status enum.
type JobStatus string

// Job statuses.
const (
	JobUnconfirmed JobStatus = "unconfirmed"
	JobConfirmed   JobStatus = "confirmed"
	JobCompleted   JobStatus = "completed"
	JobCancelled   JobStatus = "cancelled"
)

// JobStatuses lists every valid JobStatus.
var JobStatuses = []JobStatus{JobUnconfirmed, JobConfirmed, JobCompleted, JobCancelled}

// JobSortKey names a column the new jobs report can be sorted by. It is a closed set: the repository maps each
// key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type JobSortKey string

// Job sort keys.
const (
	JobSortCustomer      JobSortKey = "customer"
	JobSortAccountNumber JobSortKey = "account_number"
	JobSortServiceType   JobSortKey = "service_type"
	JobSortSource        JobSortKey = "source"
	JobSortInvoiceValue  JobSortKey = "invoice_value"
	JobSortCreatedAt     JobSortKey = "created_at"
	JobSortStatus        JobSortKey = "status"
)

// JobSortKeys lists every valid JobSortKey.
var JobSortKeys = []JobSortKey{
	JobSortCustomer, JobSortAccountNumber, JobSortServiceType, JobSortSource,
	JobSortInvoiceValue, JobSortCreatedAt, JobSortStatus,
}

// JobSort is a sort key and direction. Ties are always broken by job id in the same direction.
type JobSort struct {
	Key  JobSortKey
	Desc bool
}

// JobFilter narrows the new jobs report. An empty list means "All". Jobs cannot be deleted or archived, so there
// is no tab.
type JobFilter struct {
	// CreatedFrom is the first instant included and CreatedBefore the first instant excluded, both in UTC.
	// A zero value leaves that side open.
	CreatedFrom, CreatedBefore time.Time
	Statuses                   []JobStatus
	ServiceTypeIDs             []int64
	LeadSourceIDs              []int64
	// Search matches, case-insensitively and as a substring, the customer name, account number and service
	// type name. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// JobCursor identifies the last row of a page: its sort value (as text) and id.
type JobCursor struct {
	SortValue string
	ID        int64
}

// JobQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1 so the
// caller can tell whether another page exists.
type JobQuery struct {
	Filter JobFilter
	Sort   JobSort
	Limit  int
	After  *JobCursor
}

// JobRow is one line of the new jobs report.
type JobRow struct {
	ID            int64
	CustomerID    int64
	CustomerName  string
	AccountNumber string
	ServiceTypeID int64
	ServiceType   string
	// SourceID and Source describe where the job came from; both are nil when no lead source is recorded.
	SourceID     *int64
	Source       *string
	InvoiceValue Money
	CreatedAt    time.Time
	Status       JobStatus
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// JobSummary totals the whole filtered set, not just one page. Total adds up the invoice value of every job in
// scope, whatever its status.
type JobSummary struct {
	Count int64
	Total Money
}

// LeadSource is a lead source option for the report's Source filter.
type LeadSource struct {
	ID   int64
	Name string
}
