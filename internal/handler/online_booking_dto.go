package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toOnlineBookingListResponse(res *service.ListOnlineBookingsResult) dto.OnlineBookingListResponse {
	out := dto.OnlineBookingListResponse{
		Data: make([]dto.OnlineBookingResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, r := range res.Items {
		out.Data[i] = toOnlineBookingResponse(r)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	return out
}

func toOnlineBookingResponse(r model.OnlineBookingRow) dto.OnlineBookingResponse {
	out := dto.OnlineBookingResponse{
		ID:          r.ID,
		Customer:    dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		ScheduledAt: r.ScheduledAt.UTC().Format(timestampLayout),
		ServiceType: dto.OptionResponse{ID: r.ServiceTypeID, Name: r.ServiceType},
		Email:       r.Email,
		Phone:       r.Phone,
		Address: dto.ServiceLookupAddressResponse{
			Line1: r.Address.Line1, Line2: r.Address.Line2, City: r.Address.City,
			State: r.Address.State, Zip: r.Address.Zip, County: r.Address.County,
		},
		Notes:  r.Notes,
		Status: string(r.Status),
	}
	if r.SourceID != nil && r.Source != nil {
		out.Source = &dto.OptionResponse{ID: *r.SourceID, Name: *r.Source}
	}
	return out
}

func toOnlineBookingFiltersResponse(opts *service.OnlineBookingFilterOptions) dto.OnlineBookingFiltersResponse {
	out := dto.OnlineBookingFiltersResponse{
		Sources:     make([]dto.OptionResponse, len(opts.Sources)),
		NameFormats: toStrings(model.NameFormats),
		SortKeys:    toStrings(model.OnlineBookingSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.OnlineBookingSortScheduledAt), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, s := range opts.Sources {
		out.Sources[i] = dto.OptionResponse{ID: s.ID, Name: s.Name}
	}
	return out
}
