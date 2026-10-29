package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toTotalSaleListResponse(res *service.ListTotalSalesResult) dto.TotalSaleListResponse {
	out := dto.TotalSaleListResponse{
		Data: make([]dto.TotalSaleResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toTotalSaleResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.TotalSaleSummaryResponse{
			Count:      res.Summary.Count,
			Amount:     res.Summary.Amount.String(),
			WithoutTax: res.Summary.WithoutTax.String(),
		}
	}
	return out
}

func toTotalSaleResponse(r model.TotalSaleRow) dto.TotalSaleResponse {
	return dto.TotalSaleResponse{
		ID:            r.ID,
		InvoiceNumber: r.InvoiceNumber,
		Customer:      dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		Date:          r.Date.Format(dateLayout),
		Status:        string(r.Status),
		Amount:        r.Amount.String(),
		WithoutTax:    r.WithoutTax.String(),
	}
}

// totalSaleFiltersResponse is fixed: the report has no filter choices that come from the database.
func totalSaleFiltersResponse() dto.TotalSaleFiltersResponse {
	return dto.TotalSaleFiltersResponse{
		Statuses:    toStrings(model.TotalSaleStatuses),
		SortKeys:    toStrings(model.TotalSaleSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.TotalSaleSortDate), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
}
