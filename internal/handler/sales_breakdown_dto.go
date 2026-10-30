package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// salesBreakdownUnassignedLabels names the line of amounts that belong to no group, per report and grouping, as the
// screens show it. Clients never have one.
var salesBreakdownUnassignedLabels = map[model.SalesBreakdownType]map[model.SalesBreakdownBy]string{
	model.SalesBreakdownForecast: {
		model.SalesBreakdownByService: "Sales forecast w/o services",
		model.SalesBreakdownByItem:    "Sales forecast w/o items",
		model.SalesBreakdownByStaff:   "Other (no job or nobody assigned)",
		model.SalesBreakdownBySource:  "Sales forecast w/o source",
		model.SalesBreakdownByTag:     "Untagged",
	},
	model.SalesBreakdownRevenue: {
		model.SalesBreakdownByService: "Additional invoice revenue",
		model.SalesBreakdownByItem:    "Revenue w/o items",
		model.SalesBreakdownByStaff:   "Other (no job or nobody assigned)",
		model.SalesBreakdownBySource:  "Revenue w/o a source",
		model.SalesBreakdownByTag:     "Untagged",
	},
}

func toMonthStrings(a model.SalesBreakdownAmounts) []string {
	out := make([]string, len(a.Months))
	for i, m := range a.Months {
		out[i] = m.String()
	}
	return out
}

func toSalesBreakdownListResponse(res *service.ListSalesBreakdownResult) dto.SalesBreakdownListResponse {
	out := dto.SalesBreakdownListResponse{
		Data: make([]dto.SalesBreakdownRowResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Year: res.Year,
		Type: string(res.Type),
		By:   string(res.By),
	}
	for i, r := range res.Items {
		out.Data[i] = dto.SalesBreakdownRowResponse{
			ID: r.ID, Name: r.Name, AccountNumber: r.AccountNumber,
			Months: toMonthStrings(r.SalesBreakdownAmounts), Total: r.Total.String(),
		}
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if u := res.Unassigned; u != nil {
		out.Unassigned = &dto.SalesBreakdownUnassignedResponse{
			Label:  salesBreakdownUnassignedLabels[res.Type][res.By],
			Months: toMonthStrings(*u),
			Total:  u.Total.String(),
		}
	}
	if s := res.Summary; s != nil {
		out.Summary = &dto.SalesBreakdownSummaryResponse{
			Count: s.Count, Months: toMonthStrings(s.SalesBreakdownAmounts), Total: s.Total.String(),
		}
	}
	return out
}

func toSalesBreakdownFiltersResponse(opts *model.SalesBreakdownGroupOptions) dto.SalesBreakdownFiltersResponse {
	out := dto.SalesBreakdownFiltersResponse{
		Types:    toStrings(model.SalesBreakdownTypes),
		Bys:      toStrings(model.SalesBreakdownBys),
		Measures: toStrings(model.SalesBreakdownMeasures),
		Methods:  toStrings(model.PaymentMethods),
		Groups: dto.SalesBreakdownGroupsResponse{
			Service: make([]dto.OptionResponse, len(opts.ServiceTypes)),
			Item:    make([]dto.OptionResponse, len(opts.Items)),
			Staff:   make([]dto.OptionResponse, len(opts.Staff)),
			Source:  make([]dto.OptionResponse, len(opts.Sources)),
			Tag:     make([]dto.OptionResponse, len(opts.Tags)),
		},
		// Every grouping has these keys; the client grouping also has account_number.
		SortKeys:    toStrings(model.SalesBreakdownSortKeysFor(model.SalesBreakdownByClient)),
		DefaultSort: dto.SortResponse{Key: string(model.SalesBreakdownSortName), Order: "asc"},
		Years:       dto.YearsResponse{Min: service.MinSalesBreakdownYear, Max: service.MaxSalesBreakdownYear},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, o := range opts.ServiceTypes {
		out.Groups.Service[i] = dto.OptionResponse{ID: o.ID, Name: o.Name}
	}
	for i, o := range opts.Items {
		out.Groups.Item[i] = dto.OptionResponse{ID: o.ID, Name: o.Name}
	}
	for i, o := range opts.Staff {
		out.Groups.Staff[i] = dto.OptionResponse{ID: o.ID, Name: o.Name}
	}
	for i, o := range opts.Sources {
		out.Groups.Source[i] = dto.OptionResponse{ID: o.ID, Name: o.Name}
	}
	for i, o := range opts.Tags {
		out.Groups.Tag[i] = dto.OptionResponse{ID: o.ID, Name: o.Name}
	}
	return out
}
