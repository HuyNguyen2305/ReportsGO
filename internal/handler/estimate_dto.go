package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toEstimateListResponse(res *service.ListEstimatesResult) dto.EstimateListResponse {
	out := dto.EstimateListResponse{
		Data: make([]dto.EstimateResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toEstimateResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.EstimateSummaryResponse{
			Count: res.Summary.Count,
			Total: res.Summary.Total.String(),
		}
	}
	return out
}

func toEstimateResponse(r model.EstimateRow) dto.EstimateResponse {
	return dto.EstimateResponse{
		ID:              r.ID,
		EstimateNumber:  r.EstimateNumber,
		AccountNumber:   r.AccountNumber,
		Customer:        dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		Phone:           r.Phone,
		BillingEmail:    r.BillingEmail,
		CreatedBy:       r.CreatedBy,
		Date:            r.Date.Format(dateLayout),
		Total:           r.Total.String(),
		Status:          string(r.Status),
		SignatureStatus: string(r.Signature),
	}
}

func toEstimateFiltersResponse() dto.EstimateFiltersResponse {
	return dto.EstimateFiltersResponse{
		Statuses:          toStrings(model.EstimateStatuses),
		SignatureStatuses: toStrings(model.SignatureStatuses),
		Lifecycles:        toStrings(model.Lifecycles),
		SortKeys:          toStrings(model.EstimateSortKeys),
		DefaultSort:       dto.SortResponse{Key: string(model.EstimateSortDate), Order: "desc"},
		Limits:            dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
}
