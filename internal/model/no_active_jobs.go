package model

import "time"

// CustomerStatus is the status shown on the customer (the "All Statuses" filter of the w/o Active Jobs reports).
type CustomerStatus string

// Customer statuses.
const (
	CustomerStatusActive   CustomerStatus = "active"
	CustomerStatusInactive CustomerStatus = "inactive"
	CustomerStatusLead     CustomerStatus = "lead"
	CustomerStatusArchived CustomerStatus = "archived"
)

// CustomerStatuses lists every valid CustomerStatus.
var CustomerStatuses = []CustomerStatus{
	CustomerStatusActive, CustomerStatusInactive, CustomerStatusLead, CustomerStatusArchived,
}

// NoActiveJobsGrain says what a row of the w/o Active Jobs report is: a customer or a service location. An active job
// is one that is unconfirmed or confirmed and not deleted; a customer (or location) without one is listed.
type NoActiveJobsGrain string

// Grains of the w/o Active Jobs reports.
const (
	NoActiveJobsCustomers NoActiveJobsGrain = "customers"
	NoActiveJobsLocations NoActiveJobsGrain = "locations"
)

// NoActiveJobsSortKey names a column the report can be sorted by. It is a closed set: the repository maps each key
// to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type NoActiveJobsSortKey string

// w/o Active Jobs sort keys. Address, City and Zip exist for the locations grain only.
const (
	NoActiveJobsSortStatus          NoActiveJobsSortKey = "status"
	NoActiveJobsSortAccountNumber   NoActiveJobsSortKey = "account_number"
	NoActiveJobsSortCustomer        NoActiveJobsSortKey = "customer"
	NoActiveJobsSortAddress         NoActiveJobsSortKey = "address"
	NoActiveJobsSortCity            NoActiveJobsSortKey = "city"
	NoActiveJobsSortZip             NoActiveJobsSortKey = "zip"
	NoActiveJobsSortLastServiceType NoActiveJobsSortKey = "last_service_type"
	NoActiveJobsSortJobStatus       NoActiveJobsSortKey = "job_status"
	NoActiveJobsSortLastServiceDate NoActiveJobsSortKey = "last_service_date"
)

// NoActiveJobsCustomerSortKeys lists the sort keys of the customers grain.
var NoActiveJobsCustomerSortKeys = []NoActiveJobsSortKey{
	NoActiveJobsSortStatus, NoActiveJobsSortAccountNumber, NoActiveJobsSortCustomer,
	NoActiveJobsSortLastServiceType, NoActiveJobsSortJobStatus, NoActiveJobsSortLastServiceDate,
}

// NoActiveJobsLocationSortKeys lists the sort keys of the locations grain.
var NoActiveJobsLocationSortKeys = []NoActiveJobsSortKey{
	NoActiveJobsSortStatus, NoActiveJobsSortAccountNumber, NoActiveJobsSortCustomer,
	NoActiveJobsSortAddress, NoActiveJobsSortCity, NoActiveJobsSortZip,
	NoActiveJobsSortLastServiceType, NoActiveJobsSortJobStatus, NoActiveJobsSortLastServiceDate,
}

// NoActiveJobsSortKeysFor returns the valid sort keys of grain.
func NoActiveJobsSortKeysFor(grain NoActiveJobsGrain) []NoActiveJobsSortKey {
	if grain == NoActiveJobsLocations {
		return NoActiveJobsLocationSortKeys
	}
	return NoActiveJobsCustomerSortKeys
}

// NoActiveJobsSort is a sort key and direction. Ties are always broken by row id in the same direction.
type NoActiveJobsSort struct {
	Key  NoActiveJobsSortKey
	Desc bool
}

// NoActiveJobsFilter narrows the report. An empty list means "All".
type NoActiveJobsFilter struct {
	// Statuses are the customer's status; on the locations grain it is the status of the location's customer.
	Statuses []CustomerStatus
	// Search matches, case-insensitively and as a substring, the customer name, account number and phone, and on the
	// locations grain also the address, city and zip. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// NoActiveJobsCursor identifies the last row of a page: its sort value (as text) and id.
type NoActiveJobsCursor struct {
	SortValue string
	ID        int64
}

// NoActiveJobsQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1 so
// the caller can tell whether another page exists.
type NoActiveJobsQuery struct {
	Grain  NoActiveJobsGrain
	Filter NoActiveJobsFilter
	Sort   NoActiveJobsSort
	Limit  int
	After  *NoActiveJobsCursor
}

// LastService is the latest non-deleted job of a customer or location, cancelled jobs included.
type LastService struct {
	ServiceType string
	JobStatus   string
	// At is the job's scheduled time, or its creation time when it was never scheduled.
	At time.Time
}

// ServiceAddress is the address of a service location.
type ServiceAddress struct {
	Line1, Line2, City, Zip string
}

// NoActiveJobsRow is one line of the report: a customer, or one of its service locations.
type NoActiveJobsRow struct {
	// ID is the customer id on the customers grain and the location id on the locations grain.
	ID            int64
	CustomerID    int64
	Status        CustomerStatus
	AccountNumber string
	CustomerName  string
	Phone         *string
	// Address is set on the locations grain only.
	Address *ServiceAddress
	// LastService is nil when the customer or location has no job.
	LastService *LastService
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// NoActiveJobsSummary counts the whole filtered set, not just one page.
type NoActiveJobsSummary struct {
	Count int64
}

// NoActiveJobsPage is one page of the report read from a single database snapshot. Summary is nil unless it was
// requested, and when set it counts exactly the filtered set the rows were taken from.
type NoActiveJobsPage struct {
	Rows    []NoActiveJobsRow
	Summary *NoActiveJobsSummary
}
