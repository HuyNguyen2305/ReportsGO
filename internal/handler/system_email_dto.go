package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toSystemEmailListResponse(res *service.ListSystemEmailsResult) dto.SystemEmailListResponse {
	out := dto.SystemEmailListResponse{
		Data: make([]dto.SystemEmailResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, r := range res.Items {
		out.Data[i] = dto.SystemEmailResponse{
			ID:        r.ID,
			Customer:  dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
			Email:     r.Email,
			EmailType: string(r.Type),
			Status:    string(r.Status),
			TimeStamp: r.SentAt.UTC().Format(timestampLayout),
		}
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	return out
}

func toSystemEmailFiltersResponse(opts *service.SystemEmailFilterOptions) dto.SystemEmailFiltersResponse {
	out := dto.SystemEmailFiltersResponse{
		Statuses:    toStrings(model.EmailStatuses),
		EmailTypes:  toStrings(model.EmailTypes),
		Clients:     make([]dto.OptionResponse, len(opts.Clients)),
		NameFormats: toStrings(model.NameFormats),
		SortKeys:    toStrings(model.SystemEmailSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.SystemEmailSortTimeStamp), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, c := range opts.Clients {
		out.Clients[i] = dto.OptionResponse{ID: c.ID, Name: c.Name}
	}
	return out
}
