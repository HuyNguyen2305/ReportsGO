package handler

import (
	"reportsgo/internal/dto"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// timestampLayout is how document timestamps are written: always UTC, always six fractional digits, so every
// value has the same shape (and matches the form used inside cursors).
const timestampLayout = "2006-01-02T15:04:05.000000Z"

func toDocumentListResponse(res *service.ListDocumentsResult) dto.DocumentListResponse {
	out := dto.DocumentListResponse{
		Data: make([]dto.DocumentResponse, len(res.Items)),
		Page: dto.PageResponse{Limit: res.Limit, HasMore: res.HasMore},
		Range: dto.DateRangeResponse{
			From: optionalDate(res.From),
			To:   optionalDate(res.To),
		},
	}
	for i, row := range res.Items {
		out.Data[i] = toDocumentResponse(row)
	}
	if res.NextCursor != "" {
		cursor := res.NextCursor
		out.Page.NextCursor = &cursor
	}
	if res.Summary != nil {
		out.Summary = &dto.DocumentSummaryResponse{Count: res.Summary.Count}
	}
	return out
}

func toDocumentResponse(r model.DocumentRow) dto.DocumentResponse {
	return dto.DocumentResponse{
		ID:             r.ID,
		DocumentNumber: r.DocumentNumber,
		DocumentType:   dto.OptionResponse{ID: r.TypeID, Name: r.TypeName},
		Customer:       dto.CustomerRef{ID: r.CustomerID, Name: r.CustomerName},
		AccountNumber:  r.AccountNumber,
		CreatedBy:      r.CreatedBy,
		CreatedAt:      r.CreatedAt.UTC().Format(timestampLayout),
		Status:         string(r.Status),
	}
}

func toDocumentFiltersResponse(opts *service.DocumentFilterOptions) dto.DocumentFiltersResponse {
	out := dto.DocumentFiltersResponse{
		DocumentTypes: make([]dto.OptionResponse, len(opts.Types)),
		Statuses:      toStrings(model.DocumentStatuses),
		Lifecycles:    toStrings(model.DocumentLifecycles),
		SortKeys:      toStrings(model.DocumentSortKeys),
		DefaultSort:   dto.SortResponse{Key: string(model.DocumentSortCreatedAt), Order: "desc"},
		Limits:        dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize},
	}
	for i, t := range opts.Types {
		out.DocumentTypes[i] = dto.OptionResponse{ID: t.ID, Name: t.Name}
	}
	return out
}
