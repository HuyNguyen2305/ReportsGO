package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toNoActiveJobsPage(res *service.ListNoActiveJobsResult) (dto.PageResponse, *dto.NoActiveJobsSummaryResponse) {
	page := dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		page.NextCursor = &cursor
	}
	var summary *dto.NoActiveJobsSummaryResponse
	if res.Summary != nil {
		summary = &dto.NoActiveJobsSummaryResponse{Count: res.Summary.Count}
	}
	return page, summary
}

func toLastServiceResponse(ls *model.LastService) *dto.LastServiceResponse {
	if ls == nil {
		return nil
	}
	return &dto.LastServiceResponse{ServiceType: ls.ServiceType, JobStatus: ls.JobStatus, Date: ls.At.Format(dateLayout)}
}

func toNoActiveJobsCustomersResponse(res *service.ListNoActiveJobsResult) dto.NoActiveJobsCustomersListResponse {
	out := dto.NoActiveJobsCustomersListResponse{Data: make([]dto.NoActiveJobsCustomerResponse, len(res.Items))}
	out.Page, out.Summary = toNoActiveJobsPage(res)
	for i, r := range res.Items {
		out.Data[i] = dto.NoActiveJobsCustomerResponse{
			ID:            r.ID,
			Status:        string(r.Status),
			AccountNumber: r.AccountNumber,
			Customer:      r.CustomerName,
			Phone:         r.Phone,
			LastService:   toLastServiceResponse(r.LastService),
		}
	}
	return out
}

func toNoActiveJobsLocationsResponse(res *service.ListNoActiveJobsResult) dto.NoActiveJobsLocationsListResponse {
	out := dto.NoActiveJobsLocationsListResponse{Data: make([]dto.NoActiveJobsLocationResponse, len(res.Items))}
	out.Page, out.Summary = toNoActiveJobsPage(res)
	for i, r := range res.Items {
		row := dto.NoActiveJobsLocationResponse{
			ID:            r.ID,
			CustomerID:    r.CustomerID,
			Status:        string(r.Status),
			AccountNumber: r.AccountNumber,
			Customer:      r.CustomerName,
			Phone:         r.Phone,
			LastService:   toLastServiceResponse(r.LastService),
		}
		if r.Address != nil {
			row.Address = dto.ServiceAddressResponse{Line1: r.Address.Line1, Line2: r.Address.Line2, City: r.Address.City, Zip: r.Address.Zip}
		}
		out.Data[i] = row
	}
	return out
}

func noActiveJobsFiltersResponse(grain model.NoActiveJobsGrain) dto.NoActiveJobsFiltersResponse {
	return dto.NoActiveJobsFiltersResponse{
		Statuses:    toStrings(model.CustomerStatuses),
		SortKeys:    toStrings(model.NoActiveJobsSortKeysFor(grain)),
		DefaultSort: dto.SortResponse{Key: string(model.NoActiveJobsSortCustomer), Order: "asc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
}
