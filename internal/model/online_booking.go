package model

import "time"

// OnlineBookingSortKey names a column the Online Bookings report can be sorted by. It is a closed set: the repository
// maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type OnlineBookingSortKey string

// Online booking sort keys. The screen's Date and Time are one column, the appointment.
const (
	OnlineBookingSortCustomer    OnlineBookingSortKey = "customer"
	OnlineBookingSortScheduledAt OnlineBookingSortKey = "scheduled_at"
	OnlineBookingSortService     OnlineBookingSortKey = "service"
	OnlineBookingSortEmail       OnlineBookingSortKey = "email"
	OnlineBookingSortPhone       OnlineBookingSortKey = "phone"
	OnlineBookingSortAddress     OnlineBookingSortKey = "address"
	OnlineBookingSortNotes       OnlineBookingSortKey = "notes"
	OnlineBookingSortStatus      OnlineBookingSortKey = "status"
)

// OnlineBookingSortKeys lists every valid OnlineBookingSortKey.
var OnlineBookingSortKeys = []OnlineBookingSortKey{
	OnlineBookingSortCustomer, OnlineBookingSortScheduledAt, OnlineBookingSortService, OnlineBookingSortEmail,
	OnlineBookingSortPhone, OnlineBookingSortAddress, OnlineBookingSortNotes, OnlineBookingSortStatus,
}

// OnlineBookingSort is a sort key and direction. Ties are always broken by job id in the same direction.
type OnlineBookingSort struct {
	Key  OnlineBookingSortKey
	Desc bool
}

// OnlineBookingFilter narrows the Online Bookings report, which only ever lists the jobs, not deleted and scheduled, of
// customers who came from an online booking. An empty list means "All".
type OnlineBookingFilter struct {
	// ScheduledFrom is the first instant included and ScheduledBefore the first instant excluded, on the day of the
	// appointment. The service builds them from calendar days in the business time zone. A zero value leaves that side
	// open.
	ScheduledFrom, ScheduledBefore time.Time
	// LeadSourceIDs are the customer's sources to keep. NoSource also keeps the customers without a source. With neither
	// set, every source is kept.
	LeadSourceIDs []int64
	NoSource      bool
	// Search matches, case-insensitively and as a substring, the customer name, email, phone, address line 1, city,
	// service type and notes. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// OnlineBookingCursor identifies the last row of a page: its sort value (as text) and job id.
type OnlineBookingCursor struct {
	SortValue string
	ID        int64
}

// OnlineBookingQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1 so
// the caller can tell whether another page exists.
type OnlineBookingQuery struct {
	Filter     OnlineBookingFilter
	NameFormat NameFormat
	Sort       OnlineBookingSort
	Limit      int
	After      *OnlineBookingCursor
}

// OnlineBookingRow is one line of the report: one job of an online customer.
type OnlineBookingRow struct {
	ID            int64 // the job id
	CustomerID    int64
	CustomerName  string // shown in the requested NameFormat
	ScheduledAt   time.Time
	ServiceTypeID int64
	ServiceType   string
	// Email and Phone are nil when none is recorded. Phone is the customer's first one.
	Email *string
	Phone *string
	// Address is the job's own service location.
	Address LookupAddress
	// Notes is the customer's top note, empty when there is none.
	Notes  string
	Status JobStatus
	// SourceID and Source are the customer's lead source; both are nil when the customer has none.
	SourceID *int64
	Source   *string
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}
