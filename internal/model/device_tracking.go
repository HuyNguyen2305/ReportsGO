package model

import "time"

// DeviceTrackingSortKey names a column the Device Tracking report can be sorted by. It is a closed set: the repository
// maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL. Every column of the screen is
// sortable.
type DeviceTrackingSortKey string

// Device tracking sort keys.
const (
	DeviceTrackingSortCustomer   DeviceTrackingSortKey = "customer"
	DeviceTrackingSortService    DeviceTrackingSortKey = "service"
	DeviceTrackingSortDate       DeviceTrackingSortKey = "date"
	DeviceTrackingSortAddress1   DeviceTrackingSortKey = "address_1"
	DeviceTrackingSortAddress2   DeviceTrackingSortKey = "address_2"
	DeviceTrackingSortCity       DeviceTrackingSortKey = "city"
	DeviceTrackingSortState      DeviceTrackingSortKey = "state"
	DeviceTrackingSortZip        DeviceTrackingSortKey = "zip"
	DeviceTrackingSortDeviceName DeviceTrackingSortKey = "device_name"
	DeviceTrackingSortBarcode    DeviceTrackingSortKey = "barcode"
	DeviceTrackingSortCoordinate DeviceTrackingSortKey = "coordinate"
	DeviceTrackingSortArea       DeviceTrackingSortKey = "area"
	DeviceTrackingSortDeviceType DeviceTrackingSortKey = "device_type"
	DeviceTrackingSortCheckIn    DeviceTrackingSortKey = "check_in"
	DeviceTrackingSortStatus     DeviceTrackingSortKey = "status"
	DeviceTrackingSortActivity   DeviceTrackingSortKey = "activity"
	DeviceTrackingSortCheckOut   DeviceTrackingSortKey = "check_out"
	DeviceTrackingSortNotes      DeviceTrackingSortKey = "notes"
	DeviceTrackingSortAssignedTo DeviceTrackingSortKey = "assigned_to"
)

// DeviceTrackingSortKeys lists every valid DeviceTrackingSortKey.
var DeviceTrackingSortKeys = []DeviceTrackingSortKey{
	DeviceTrackingSortCustomer, DeviceTrackingSortService, DeviceTrackingSortDate, DeviceTrackingSortAddress1,
	DeviceTrackingSortAddress2, DeviceTrackingSortCity, DeviceTrackingSortState, DeviceTrackingSortZip,
	DeviceTrackingSortDeviceName, DeviceTrackingSortBarcode, DeviceTrackingSortCoordinate, DeviceTrackingSortArea,
	DeviceTrackingSortDeviceType, DeviceTrackingSortCheckIn, DeviceTrackingSortStatus, DeviceTrackingSortActivity,
	DeviceTrackingSortCheckOut, DeviceTrackingSortNotes, DeviceTrackingSortAssignedTo,
}

// DeviceTrackingSort is a sort key and direction. Ties are always broken by check id in the same direction.
type DeviceTrackingSort struct {
	Key  DeviceTrackingSortKey
	Desc bool
}

// DeviceTrackingFilter narrows the Device Tracking report, which only ever lists the checks of jobs that are not deleted
// and have an appointment. An empty list means "All".
type DeviceTrackingFilter struct {
	// ScheduledFrom is the first instant included and ScheduledBefore the first instant excluded, on the day of the job's
	// appointment. The service builds them from calendar days in the business time zone. A zero value leaves that side open.
	ScheduledFrom, ScheduledBefore time.Time
	CustomerIDs                    []int64
	// Search matches, case-insensitively and as a substring, the customer name, service type, address lines, city, state,
	// zip, device name, barcode, area, device type, status, activity, notes and the assignee's name. It is plain text:
	// LIKE wildcards in it are matched literally.
	Search string
}

// DeviceTrackingCursor identifies the last row of a page: its sort value (as text) and check id.
type DeviceTrackingCursor struct {
	SortValue string
	ID        int64
}

// DeviceTrackingQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1 so
// the caller can tell whether another page exists.
type DeviceTrackingQuery struct {
	Filter DeviceTrackingFilter
	Sort   DeviceTrackingSort
	Limit  int
	After  *DeviceTrackingCursor
}

// DeviceTrackingRow is one line of the report: one device checked during one job.
type DeviceTrackingRow struct {
	ID            int64 // the check id
	CustomerID    int64
	CustomerName  string
	ServiceTypeID int64
	ServiceType   string
	// Date is the job's appointment.
	Date time.Time
	// Address is the job's own service location.
	Address    LookupAddress
	DeviceID   int64
	DeviceName string
	Barcode    string
	// Latitude and Longitude are nil together when the device has no coordinate.
	Latitude, Longitude *float64
	Area                string
	DeviceType          string
	// CheckedInAt and CheckedOutAt are nil until the device is checked in or out.
	CheckedInAt, CheckedOutAt *time.Time
	Status                    string
	Activity                  string
	Notes                     string
	// AssignedToID and AssignedTo are the staff member; both are nil when none is recorded.
	AssignedToID *int64
	AssignedTo   *string
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}
