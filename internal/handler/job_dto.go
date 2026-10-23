package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toJobListResponse(res *service.ListJobsResult) dto.JobListResponse {
	out := dto.JobListResponse{
		Data: make([]dto.JobResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toJobResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.JobSummaryResponse{Count: res.Summary.Count, Total: res.Summary.Total.String()}
	}
	return out
}

func toJobResponse(r model.JobRow) dto.JobResponse {
	out := dto.JobResponse{
		ID:            r.ID,
		Customer:      dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		AccountNumber: r.AccountNumber,
		ServiceType:   dto.OptionResponse{ID: r.ServiceTypeID, Name: r.ServiceType},
		InvoiceValue:  r.InvoiceValue.String(),
		CreatedAt:     r.CreatedAt.UTC().Format(timestampLayout),
		Status:        string(r.Status),
	}
	if r.SourceID != nil && r.Source != nil {
		out.Source = &dto.OptionResponse{ID: *r.SourceID, Name: *r.Source}
	}
	return out
}

func toJobFiltersResponse(opts *service.JobFilterOptions) dto.JobFiltersResponse {
	out := dto.JobFiltersResponse{
		ServiceTypes: make([]dto.OptionResponse, len(opts.ServiceTypes)),
		LeadSources:  make([]dto.OptionResponse, len(opts.LeadSources)),
		Statuses:     toStrings(model.JobStatuses),
		SortKeys:     toStrings(model.JobSortKeys),
		DefaultSort:  dto.SortResponse{Key: string(model.JobSortCreatedAt), Order: "desc"},
		Limits:       dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, t := range opts.ServiceTypes {
		out.ServiceTypes[i] = dto.OptionResponse{ID: t.ID, Name: t.Name}
	}
	for i, s := range opts.LeadSources {
		out.LeadSources[i] = dto.OptionResponse{ID: s.ID, Name: s.Name}
	}
	return out
}
