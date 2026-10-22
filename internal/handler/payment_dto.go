package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// creditLabel is the "Payment For" text of a payment made for a credit instead of an invoice.
const creditLabel = "Credit"

func toPaymentListResponse(res *service.ListPaymentsResult) dto.PaymentListResponse {
	out := dto.PaymentListResponse{
		Data: make([]dto.PaymentResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toPaymentResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.PaymentSummaryResponse{Count: res.Summary.Count, Total: res.Summary.Total.String()}
	}
	return out
}

func toPaymentResponse(r model.PaymentRow) dto.PaymentResponse {
	paymentFor := dto.PaymentForResponse{Type: "credit", Label: creditLabel}
	if r.InvoiceID != nil {
		paymentFor = dto.PaymentForResponse{Type: "invoice", InvoiceID: r.InvoiceID}
		if r.InvoiceNumber != nil {
			paymentFor.Label = *r.InvoiceNumber
		}
	}
	return dto.PaymentResponse{
		ID:            r.ID,
		Customer:      dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		AccountNumber: r.AccountNumber,
		PaymentFor:    paymentFor,
		Method:        dto.PaymentMethodResponse{Type: string(r.Method), CardBrand: r.CardBrand, CardLast4: r.CardLast4},
		Amount:        r.Amount.String(),
		PaymentDate:   r.Date.Format(dateLayout),
		Status:        string(r.Status),
	}
}

// paymentFiltersResponse describes the static choices behind the report's filter controls.
func paymentFiltersResponse() dto.PaymentFiltersResponse {
	return dto.PaymentFiltersResponse{
		Statuses:    toStrings(model.PaymentStatuses),
		Methods:     toStrings(model.PaymentMethods),
		Lifecycles:  toStrings(model.PaymentLifecycles),
		SortKeys:    toStrings(model.PaymentSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.PaymentSortDate), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
}
