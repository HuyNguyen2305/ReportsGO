package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toCustomerListResponse(res *service.ListCustomersResult) dto.CustomerListResponse {
	out := dto.CustomerListResponse{
		Data: make([]dto.CustomerResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toCustomerResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.CustomerSummaryResponse{Count: res.Summary.Count}
	}
	return out
}

func toCustomerResponse(r model.CustomerRow) dto.CustomerResponse {
	out := dto.CustomerResponse{
		ID:            r.ID,
		Customer:      dto.CustomerRef{ID: r.ID, Name: r.Name},
		AccountNumber: r.AccountNumber,
		Email:         r.Email,
		Phone:         r.Phone,
		CreatedAt:     r.CreatedAt.UTC().Format(timestampLayout),
	}
	if r.SourceID != nil && r.Source != nil {
		out.Source = &dto.OptionResponse{ID: *r.SourceID, Name: *r.Source}
	}
	return out
}

func toCustomerFiltersResponse(opts *service.CustomerFilterOptions) dto.CustomerFiltersResponse {
	out := dto.CustomerFiltersResponse{
		LeadSources: make([]dto.OptionResponse, len(opts.LeadSources)),
		SortKeys:    toStrings(model.CustomerSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.CustomerSortCreatedAt), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, s := range opts.LeadSources {
		out.LeadSources[i] = dto.OptionResponse{ID: s.ID, Name: s.Name}
	}
	return out
}
