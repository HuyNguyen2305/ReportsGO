package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toCreditListResponse(res *service.ListCreditsResult) dto.CreditListResponse {
	out := dto.CreditListResponse{
		Data: make([]dto.CreditResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toCreditResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.CreditSummaryResponse{Count: res.Summary.Count, Total: res.Summary.Total.String()}
	}
	return out
}

func toCreditResponse(r model.CreditRow) dto.CreditResponse {
	return dto.CreditResponse{
		ID:            r.ID,
		Customer:      dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		AccountNumber: r.AccountNumber,
		Method:        dto.CreditMethodResponse{Type: string(r.Method), Detail: r.MethodDetail},
		Amount:        r.Amount.String(),
		CreditDate:    r.Date.Format(dateLayout),
		CreatedBy:     r.CreatedBy,
	}
}

// creditFiltersResponse describes the static choices behind the report's filter controls.
func creditFiltersResponse() dto.CreditFiltersResponse {
	return dto.CreditFiltersResponse{
		Methods:     toStrings(model.PaymentMethods),
		SortKeys:    toStrings(model.CreditSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.CreditSortDate), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
}
