package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toInboundLeadListResponse(res *service.ListInboundLeadsResult) dto.InboundLeadListResponse {
	out := dto.InboundLeadListResponse{
		Data: make([]dto.InboundLeadResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, r := range res.Items {
		out.Data[i] = toInboundLeadResponse(r)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	return out
}

func toInboundLeadResponse(r model.InboundLeadRow) dto.InboundLeadResponse {
	out := dto.InboundLeadResponse{
		ID:        r.ID,
		Customer:  dto.CustomerRef{ID: r.ID, Name: r.CustomerName},
		Email:     r.Email,
		Phone:     r.Phone,
		Notes:     r.Notes,
		DateAdded: r.DateAdded.UTC().Format(timestampLayout),
	}
	if r.SourceID != nil && r.Source != nil {
		out.Source = &dto.OptionResponse{ID: *r.SourceID, Name: *r.Source}
	}
	if r.ActiveJob != nil {
		out.ActiveJob = &dto.InboundLeadJobResponse{
			ServiceType: dto.OptionResponse{ID: r.ActiveJob.ServiceTypeID, Name: r.ActiveJob.ServiceType},
			ScheduledAt: r.ActiveJob.ScheduledAt.UTC().Format(timestampLayout),
		}
	}
	return out
}

func toInboundLeadFiltersResponse(opts *service.InboundLeadFilterOptions) dto.InboundLeadFiltersResponse {
	out := dto.InboundLeadFiltersResponse{
		Sources:     make([]dto.OptionResponse, len(opts.Sources)),
		NameFormats: toStrings(model.NameFormats),
		SortKeys:    toStrings(model.InboundLeadSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.InboundLeadSortDateAdded), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, s := range opts.Sources {
		out.Sources[i] = dto.OptionResponse{ID: s.ID, Name: s.Name}
	}
	return out
}
