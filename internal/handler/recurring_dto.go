package handler

import (
	"fmt"
	"time"

	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toRecurringListResponse(res *service.ListRecurringResult) dto.RecurringListResponse {
	out := dto.RecurringListResponse{
		Data: make([]dto.RecurringResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toRecurringResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.RecurringSummaryResponse{Count: res.Summary.Count, Total: res.Summary.Total.String()}
	}
	return out
}

func toRecurringResponse(r model.RecurringRow) dto.RecurringResponse {
	return dto.RecurringResponse{
		ID:            r.ID,
		Customer:      dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		AccountNumber: r.AccountNumber,
		Repeats: dto.RepeatsResponse{
			Unit:        string(r.Unit),
			Interval:    r.Interval,
			Weekday:     r.Weekday,
			Occurrences: r.Occurrences,
			Label:       repeatsLabel(r),
		},
		Action:     string(r.Action),
		Status:     string(r.Status),
		TotalValue: r.TotalValue.String(),
		StartedOn:  r.StartedOn.Format(dateLayout),
		EndsOn:     optionalDatePtr(r.EndsOn),
		NextInvoice: dto.NextInvoiceResponse{
			InvoiceID:     r.NextInvoiceID,
			InvoiceNumber: r.NextInvoiceNumber,
			Date:          optionalDatePtr(r.NextInvoiceDate),
		},
	}
}

// optionalDatePtr formats a nullable date.
func optionalDatePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return optionalDate(*t)
}

var weekdayNames = [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// repeatsLabel describes the repeat rule in words, for example "Every week on Thursday, 3 times" or
// "Every 2 months". The weekday is only mentioned for weekly rules: on a daily, monthly or yearly rule it would
// read ambiguously, and it is still returned in the structured fields.
func repeatsLabel(r model.RecurringRow) string {
	label := "Every " + string(r.Unit)
	if r.Interval != 1 {
		label = fmt.Sprintf("Every %d %ss", r.Interval, r.Unit)
	}
	if r.Unit == model.FrequencyWeek && r.Weekday != nil && *r.Weekday >= 0 && *r.Weekday < len(weekdayNames) {
		label += " on " + weekdayNames[*r.Weekday]
	}
	if r.Occurrences != nil {
		if *r.Occurrences == 1 {
			label += ", 1 time"
		} else {
			label += fmt.Sprintf(", %d times", *r.Occurrences)
		}
	}
	return label
}

// recurringFiltersResponse describes the static choices behind the report's filter controls.
func recurringFiltersResponse() dto.RecurringFiltersResponse {
	return dto.RecurringFiltersResponse{
		Statuses:    toStrings(model.RecurringStatuses),
		Actions:     toStrings(model.RecurringActions),
		SortKeys:    toStrings(model.RecurringSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.RecurringSortStartedOn), Order: "desc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
}
