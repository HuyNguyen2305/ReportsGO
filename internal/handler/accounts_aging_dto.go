package handler

import (
	"fmt"

	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toAgingListResponse(res *service.ListAgingResult) dto.AgingListResponse {
	out := dto.AgingListResponse{
		Data: make([]dto.AgingResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		AsOf: res.AsOf.Format(dateLayout),
	}
	for i, row := range res.Items {
		out.Data[i] = toAgingResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if s := res.Summary; s != nil {
		out.Summary = &dto.AgingSummaryResponse{
			Count:           s.Count,
			D0to30:          s.D0to30.String(),
			D31to60:         s.D31to60.String(),
			D61to90:         s.D61to90.String(),
			D90Plus:         s.D90Plus.String(),
			Total:           s.Total.String(),
			AvailableCredit: s.AvailableCredit.String(),
		}
	}
	return out
}

func toAgingResponse(r model.AgingRow) dto.AgingResponse {
	out := dto.AgingResponse{
		CustomerID:      r.CustomerID,
		LocationID:      r.LocationID,
		Customer:        r.CustomerName,
		Location:        r.Location,
		BillingEmail:    r.BillingEmail,
		Phone:           r.Phone,
		D0to30:          r.D0to30.String(),
		D31to60:         r.D31to60.String(),
		D61to90:         r.D61to90.String(),
		D90Plus:         r.D90Plus.String(),
		Total:           r.Total.String(),
		AvailableCredit: r.AvailableCredit.String(),
	}
	if r.Card != nil {
		out.CreditCard = &dto.AgingCardResponse{
			Processor: r.Card.Processor,
			Last4:     r.Card.Last4,
			Expires:   fmt.Sprintf("%02d/%04d", r.Card.ExpMonth, r.Card.ExpYear),
		}
	}
	return out
}

func toAgingFiltersResponse(opts *service.AgingFilterOptions) dto.AgingFiltersResponse {
	out := dto.AgingFiltersResponse{
		Days:         model.AgingMinAgeDays,
		ServiceTypes: make([]dto.OptionResponse, len(opts.ServiceTypes)),
		SortKeys:     toStrings(model.AgingSortKeys),
		DefaultSort:  dto.SortResponse{Key: string(model.AgingSortCustomer), Order: "asc"},
		Limits:       dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, t := range opts.ServiceTypes {
		out.ServiceTypes[i] = dto.OptionResponse{ID: t.ID, Name: t.Name}
	}
	return out
}
