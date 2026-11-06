package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func toMaterialUseListResponse(res *service.ListMaterialUseResult) dto.MaterialUseListResponse {
	out := dto.MaterialUseListResponse{
		Data: make([]dto.MaterialUseResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, r := range res.Items {
		out.Data[i] = dto.MaterialUseResponse{
			MaterialID: r.MaterialID, Material: r.Material, EPA: r.EPA, Qty: r.Qty, Applications: r.Applications,
		}
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	return out
}

func toMaterialUseFiltersResponse(opts *service.MaterialUseFilterOptions) dto.MaterialUseFiltersResponse {
	out := dto.MaterialUseFiltersResponse{
		Materials:   make([]dto.OptionResponse, len(opts.Materials)),
		Clients:     make([]dto.OptionResponse, len(opts.Clients)),
		Tabs:        toStrings(model.MaterialUseTabs),
		SortKeys:    toStrings(model.MaterialUseSortKeys),
		DefaultSort: dto.SortResponse{Key: string(model.MaterialUseSortMaterial), Order: "asc"},
		Limits:      dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, m := range opts.Materials {
		out.Materials[i] = dto.OptionResponse{ID: m.ID, Name: m.Name}
	}
	for i, c := range opts.Clients {
		out.Clients[i] = dto.OptionResponse{ID: c.ID, Name: c.Name}
	}
	return out
}
