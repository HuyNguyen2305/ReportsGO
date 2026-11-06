package handler

import (
	"time"

	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toDeviceTrackingListResponse(res *service.ListDeviceTrackingResult) dto.DeviceTrackingListResponse {
	out := dto.DeviceTrackingListResponse{
		Data: make([]dto.DeviceTrackingResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, r := range res.Items {
		out.Data[i] = toDeviceTrackingResponse(r)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	return out
}

func toDeviceTrackingResponse(r model.DeviceTrackingRow) dto.DeviceTrackingResponse {
	out := dto.DeviceTrackingResponse{
		ID:          r.ID,
		Customer:    dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		ServiceType: dto.OptionResponse{ID: r.ServiceTypeID, Name: r.ServiceType},
		Date:        r.Date.UTC().Format(timestampLayout),
		Address: dto.ServiceLookupAddressResponse{
			Line1: r.Address.Line1, Line2: r.Address.Line2, City: r.Address.City,
			State: r.Address.State, Zip: r.Address.Zip, County: r.Address.County,
		},
		Device: dto.DeviceResponse{
			ID: r.DeviceID, Name: r.DeviceName, Barcode: r.Barcode, Area: r.Area, DeviceType: r.DeviceType,
		},
		CheckIn:  optionalTimestamp(r.CheckedInAt),
		Status:   r.Status,
		Activity: r.Activity,
		CheckOut: optionalTimestamp(r.CheckedOutAt),
		Notes:    r.Notes,
	}
	if r.Latitude != nil && r.Longitude != nil {
		out.Device.Coordinate = &dto.CoordinateResponse{Latitude: *r.Latitude, Longitude: *r.Longitude}
	}
	if r.AssignedToID != nil && r.AssignedTo != nil {
		out.AssignedTo = &dto.OptionResponse{ID: *r.AssignedToID, Name: *r.AssignedTo}
	}
	return out
}

// optionalTimestamp formats t like every other timestamp of the API, nil when there is none.
func optionalTimestamp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(timestampLayout)
	return &s
}

func toDeviceTrackingFiltersResponse(opts *service.DeviceTrackingFilterOptions) dto.DeviceTrackingFiltersResponse {
	out := dto.DeviceTrackingFiltersResponse{
		Clients:     make([]dto.OptionResponse, len(opts.Clients)),
		SortKeys:    toStrings(model.DeviceTrackingSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.DeviceTrackingSortDate), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, c := range opts.Clients {
		out.Clients[i] = dto.OptionResponse{ID: c.ID, Name: c.Name}
	}
	return out
}
