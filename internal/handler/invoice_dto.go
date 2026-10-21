package handler

import (
	"time"

	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

const dateLayout = "2006-01-02"

func toInvoiceListResponse(res *service.ListInvoicesResult) dto.InvoiceListResponse {
	out := dto.InvoiceListResponse{
		Data: make([]dto.InvoiceResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toInvoiceResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.InvoiceSummaryResponse{
			Count:       res.Summary.Count,
			Total:       res.Summary.Total.String(),
			Discount:    res.Summary.Discount.String(),
			Outstanding: res.Summary.Outstanding.String(),
		}
	}
	return out
}

func toInvoiceResponse(r model.InvoiceRow) dto.InvoiceResponse {
	tags := r.Tags
	if tags == nil {
		tags = []string{} // always an array in JSON, never null
	}
	out := dto.InvoiceResponse{
		ID:              r.ID,
		InvoiceNumber:   r.InvoiceNumber,
		AccountNumber:   r.AccountNumber,
		Customer:        dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		Job:             r.Job,
		Frequency:       r.FrequencyLabel,
		BillingEmail:    r.BillingEmail,
		Date:            r.Date.Format(dateLayout),
		Total:           r.Total.String(),
		Discount:        r.Discount.String(),
		Tags:            tags,
		Status:          string(r.Status),
		SignatureStatus: string(r.SignatureStatus),
	}
	if r.PaymentTermsName != nil {
		out.PaymentTerms = &dto.PaymentTermsRef{Name: *r.PaymentTermsName}
		if r.PaymentTermsDescription != nil {
			out.PaymentTerms.Description = *r.PaymentTermsDescription
		}
	}
	return out
}

func optionalDate(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.Format(dateLayout)
	return &s
}

func toInvoiceFiltersResponse(opts *service.InvoiceFilterOptions) dto.InvoiceFiltersResponse {
	out := dto.InvoiceFiltersResponse{
		ServiceTypes:      make([]dto.OptionResponse, len(opts.ServiceTypes)),
		Tags:              make([]dto.OptionResponse, len(opts.Tags)),
		PaymentTerms:      make([]dto.PaymentTermOption, len(opts.PaymentTerms)),
		Statuses:          toStrings(model.InvoiceStatuses),
		SignatureStatuses: toStrings(model.SignatureStatuses),
		Lifecycles:        toStrings(model.Lifecycles),
		SortKeys:          toStrings(model.InvoiceSortKeys),
		DefaultSort:       dto.SortResponse{Key: string(model.SortDate), Order: "desc"},
		Limits:            dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, s := range opts.ServiceTypes {
		out.ServiceTypes[i] = dto.OptionResponse{ID: s.ID, Name: s.Name}
	}
	for i, t := range opts.Tags {
		out.Tags[i] = dto.OptionResponse{ID: t.ID, Name: t.Name}
	}
	for i, p := range opts.PaymentTerms {
		out.PaymentTerms[i] = dto.PaymentTermOption{ID: p.ID, Name: p.Name, Description: p.Description}
	}
	return out
}

func toStrings[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}
