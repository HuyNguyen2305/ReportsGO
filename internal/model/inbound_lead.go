package model

import "time"

// InboundLeadSortKey names a column the Inbound Leads report can be sorted by. It is a closed set: the repository
// maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL. The active job is not
// sortable, as on the screen.
type InboundLeadSortKey string

// Inbound lead sort keys.
const (
	InboundLeadSortCustomer  InboundLeadSortKey = "customer"
	InboundLeadSortSource    InboundLeadSortKey = "source"
	InboundLeadSortEmail     InboundLeadSortKey = "email"
	InboundLeadSortPhone     InboundLeadSortKey = "phone"
	InboundLeadSortNotes     InboundLeadSortKey = "notes"
	InboundLeadSortDateAdded InboundLeadSortKey = "date_added"
)

// InboundLeadSortKeys lists every valid InboundLeadSortKey.
var InboundLeadSortKeys = []InboundLeadSortKey{
	InboundLeadSortCustomer, InboundLeadSortSource, InboundLeadSortEmail, InboundLeadSortPhone, InboundLeadSortNotes,
	InboundLeadSortDateAdded,
}

// InboundLeadSort is a sort key and direction. Ties are always broken by customer id in the same direction.
type InboundLeadSort struct {
	Key  InboundLeadSortKey
	Desc bool
}

// InboundLeadFilter narrows the Inbound Leads report, which only ever lists customers whose status is lead.
// An empty list means "All".
type InboundLeadFilter struct {
	// CreatedFrom is the first instant included and CreatedBefore the first instant excluded, on the day the lead
	// was added. The service builds them from calendar days in the business time zone. A zero value leaves that side
	// open.
	CreatedFrom, CreatedBefore time.Time
	// LeadSourceIDs are the sources to keep. NoSource also keeps the leads without a source (the screen's "NA").
	// With neither set, every source is kept.
	LeadSourceIDs []int64
	NoSource      bool
	// Search matches, case-insensitively and as a substring, the customer name, email, phone and notes. It is plain
	// text: LIKE wildcards in it are matched literally.
	Search string
}

// InboundLeadCursor identifies the last row of a page: its sort value (as text) and customer id.
type InboundLeadCursor struct {
	SortValue string
	ID        int64
}

// InboundLeadQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1 so
// the caller can tell whether another page exists.
type InboundLeadQuery struct {
	Filter     InboundLeadFilter
	NameFormat NameFormat
	Sort       InboundLeadSort
	Limit      int
	After      *InboundLeadCursor
}

// InboundLeadJob is a lead's active job: the earliest scheduled job that is not deleted and still open.
type InboundLeadJob struct {
	ServiceTypeID int64
	ServiceType   string
	ScheduledAt   time.Time
}

// InboundLeadRow is one line of the report.
type InboundLeadRow struct {
	ID           int64
	CustomerName string // shown in the requested NameFormat
	// SourceID and Source are nil when the lead has no source (shown as "NA").
	SourceID *int64
	Source   *string
	// Email and Phone are nil when none is recorded. Phone is the customer's first one.
	Email *string
	Phone *string
	// Notes is the customer's top note, empty when there is none.
	Notes     string
	DateAdded time.Time
	// ActiveJob is nil when the lead has no open scheduled job.
	ActiveJob *InboundLeadJob
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}
