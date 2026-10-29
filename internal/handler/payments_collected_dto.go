package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toPaymentCollectedListResponse(res *service.ListPaymentsCollectedResult) dto.PaymentCollectedListResponse {
	out := dto.PaymentCollectedListResponse{
		Data: make([]dto.PaymentCollectedResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toPaymentCollectedResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.PaymentCollectedSummaryResponse{
			Count:      res.Summary.Count,
			Amount:     res.Summary.Amount.String(),
			WithoutTax: res.Summary.WithoutTax.String(),
		}
	}
	return out
}

func toPaymentCollectedResponse(r model.PaymentCollectedRow) dto.PaymentCollectedResponse {
	out := dto.PaymentCollectedResponse{
		ID:           r.ID,
		PaymentFor:   toPaymentForResponse(r.InvoiceID, r.InvoiceNumber),
		Customer:     dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		Method:       dto.PaymentMethodResponse{Type: string(r.Method), CardBrand: r.CardBrand, CardLast4: r.CardLast4},
		ServiceState: r.ServiceState,
		Date:         r.Date.Format(dateLayout),
		Amount:       r.Amount.String(),
		WithoutTax:   r.WithoutTax.String(),
	}
	if r.StaffID != nil && r.StaffName != nil {
		out.Staff = &dto.StaffRef{ID: *r.StaffID, Name: *r.StaffName}
	}
	return out
}

func toPaymentsCollectedFiltersResponse(opts *service.PaymentsCollectedFilterOptions) dto.PaymentCollectedFiltersResponse {
	out := dto.PaymentCollectedFiltersResponse{
		PaymentFor:  toStrings(model.PaymentFors),
		Methods:     toStrings(model.PaymentMethods),
		Staff:       make([]dto.OptionResponse, len(opts.Staff)),
		SortKeys:    toStrings(model.PaymentCollectedSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.PaymentCollectedSortDate), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, s := range opts.Staff {
		out.Staff[i] = dto.OptionResponse{ID: s.ID, Name: s.Name}
	}
	return out
}
