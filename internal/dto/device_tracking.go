package dto

// DeviceTrackingResponse is one line of the Device Tracking report: one device checked during one job. Date is the job's
// appointment, always UTC with six fractional digits, for example "2026-11-03T03:45:00.000000Z". Address is the job's own
// service location. CheckIn and CheckOut are null until the device is checked in or out. AssignedTo is the staff member,
// null when none is recorded. Status, Activity and Notes are free text and empty strings when nothing was written.
type DeviceTrackingResponse struct {
	ID          int64                        `json:"id"`
	Customer    CustomerRef                  `json:"customer"`
	ServiceType OptionResponse               `json:"service_type"`
	Date        string                       `json:"date"`
	Address     ServiceLookupAddressResponse `json:"address"`
	Device      DeviceResponse               `json:"device"`
	CheckIn     *string                      `json:"check_in"`
	Status      string                       `json:"status"`
	Activity    string                       `json:"activity"`
	CheckOut    *string                      `json:"check_out"`
	Notes       string                       `json:"notes"`
	AssignedTo  *OptionResponse              `json:"assigned_to"`
}

// DeviceResponse is the device a check was made on. Coordinate is null when the device has none.
type DeviceResponse struct {
	ID         int64               `json:"id"`
	Name       string              `json:"name"`
	Barcode    string              `json:"barcode"`
	Coordinate *CoordinateResponse `json:"coordinate"`
	Area       string              `json:"area"`
	DeviceType string              `json:"device_type"`
}

// CoordinateResponse is a latitude and longitude in degrees.
type CoordinateResponse struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// DeviceTrackingListResponse is one page of the Device Tracking report.
type DeviceTrackingListResponse struct {
	Data []DeviceTrackingResponse `json:"data"`
	Page PageResponse             `json:"page"`
	// Range is the date range actually applied, including a defaulted month; an open end is null.
	Range DateRangeResponse `json:"range"`
}

// DeviceTrackingFiltersResponse has everything the report screen needs to build its filter controls.
type DeviceTrackingFiltersResponse struct {
	Clients     []OptionResponse   `json:"clients"`
	SortKeys    []string           `json:"sort_keys"`
	DefaultSort SortResponse       `json:"default_sort"`
	Limits      PageLimitsResponse `json:"limits"`
}
