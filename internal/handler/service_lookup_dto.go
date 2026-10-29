package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toServiceLookupListResponse(res *service.ListServiceLookupResult) dto.ServiceLookupListResponse {
	out := dto.ServiceLookupListResponse{
		Data: make([]dto.ServiceLookupResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toServiceLookupResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if s := res.Summary; s != nil {
		out.Summary = &dto.ServiceLookupSummaryResponse{
			Count:        s.Count,
			InvoiceValue: s.InvoiceValue.String(),
			WithoutTax:   s.WithoutTax.String(),
			Tabs:         dto.ServiceLookupTabCountsResponse{Active: s.Tabs.Active, WorkPool: s.Tabs.WorkPool, Deleted: s.Tabs.Deleted},
		}
	}
	return out
}

func toServiceLookupResponse(r model.ServiceLookupRow) dto.ServiceLookupResponse {
	out := dto.ServiceLookupResponse{
		ID:          r.ID,
		Customer:    dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		Phone:       r.Phone,
		ServiceType: dto.OptionResponse{ID: r.ServiceTypeID, Name: r.ServiceType},
		Address: dto.ServiceLookupAddressResponse{
			Line1: r.Address.Line1, Line2: r.Address.Line2, City: r.Address.City,
			State: r.Address.State, Zip: r.Address.Zip, County: r.Address.County,
		},
		InvoiceValue: r.InvoiceValue.String(),
		WithoutTax:   r.WithoutTax.String(),
		Status:       string(r.Status),
		Tags:         make([]dto.OptionResponse, len(r.Tags)),
	}
	if r.ScheduledAt != nil {
		s := r.ScheduledAt.UTC().Format(timestampLayout)
		out.ScheduledAt = &s
	}
	if r.AssignedToID != nil && r.AssignedTo != nil {
		out.AssignedTo = &dto.StaffRef{ID: *r.AssignedToID, Name: *r.AssignedTo}
	}
	for i, t := range r.Tags {
		out.Tags[i] = dto.OptionResponse{ID: t.ID, Name: t.Name}
	}
	return out
}

func toServiceLookupFiltersResponse(opts *service.ServiceLookupFilterOptions) dto.ServiceLookupFiltersResponse {
	out := dto.ServiceLookupFiltersResponse{
		Tabs:         toStrings(model.ServiceLookupTabs),
		Statuses:     toStrings(model.JobStatuses),
		ServiceTypes: make([]dto.OptionResponse, len(opts.ServiceTypes)),
		Staff:        make([]dto.OptionResponse, len(opts.Staff)),
		Tags:         make([]dto.OptionResponse, len(opts.Tags)),
		SortKeys:     toStrings(model.ServiceLookupSortKeys),
		DefaultSort:  dto.SortResponse{Key: string(model.ServiceLookupSortScheduledAt), Order: "asc"},
		Limits:       dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, t := range opts.ServiceTypes {
		out.ServiceTypes[i] = dto.OptionResponse{ID: t.ID, Name: t.Name}
	}
	for i, s := range opts.Staff {
		out.Staff[i] = dto.OptionResponse{ID: s.ID, Name: s.Name}
	}
	for i, t := range opts.Tags {
		out.Tags[i] = dto.OptionResponse{ID: t.ID, Name: t.Name}
	}
	return out
}
