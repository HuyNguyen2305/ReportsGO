package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListDocumentsParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListDocumentsParams struct {
	From, To  string // from, to: YYYY-MM-DD, on the day the document was created, in the server's time zone
	Lifecycle string // lifecycle: active | deleted

	Status          []string // status
	DocumentTypeIDs []string // document_type_id

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// documentRequest is the validated form of ListDocumentsParams.
type documentRequest struct {
	query    model.DocumentQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseDocumentParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parseDocumentParams(p ListDocumentsParams, now time.Time) (documentRequest, error) {
	var v validator
	filter := model.DocumentFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	// The dates are inclusive calendar days in the clock's time zone; the filter works on instants.
	filter.CreatedFrom, filter.CreatedBefore = createdRange(from, to, now.Location())

	filter.Lifecycle = v.lifecycleIn(p.Lifecycle, model.DocumentLifecycles)
	filter.Statuses = parseEnums(&v, "status", p.Status, model.DocumentStatuses)
	filter.TypeIDs = v.ids("document_type_id", p.DocumentTypeIDs)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.DocumentSortKeys, model.DocumentSortCreatedAt)
	sort := model.DocumentSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.DocumentCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeDocumentCursor(raw, sort)
		if err != nil {
			var ce *InvalidCursorError
			if errors.As(err, &ce) {
				v.add("cursor", ce.Reason)
			} else {
				v.add("cursor", "is not a valid cursor")
			}
		}
		after = cur
	}

	if err := v.err(); err != nil {
		return documentRequest{}, err
	}
	return documentRequest{
		query: model.DocumentQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
