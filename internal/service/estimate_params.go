package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListEstimatesParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListEstimatesParams struct {
	From, To  string // from, to: YYYY-MM-DD
	Lifecycle string // lifecycle: active | archived | deleted

	Status          []string // status
	SignatureStatus []string // signature_status

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// estimateRequest is the validated form of ListEstimatesParams.
type estimateRequest struct {
	query    model.EstimateQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseEstimateParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parseEstimateParams(p ListEstimatesParams, now time.Time) (estimateRequest, error) {
	var v validator
	filter := model.EstimateFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	filter.DateFrom, filter.DateTo = from, to

	filter.Lifecycle = v.lifecycle(p.Lifecycle)
	filter.Statuses = parseEnums(&v, "status", p.Status, model.EstimateStatuses)
	filter.SignatureStatus = parseEnums(&v, "signature_status", p.SignatureStatus, model.SignatureStatuses)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.EstimateSortKeys, model.EstimateSortDate)
	sort := model.EstimateSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.EstimateCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeEstimateCursor(raw, sort)
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
		return estimateRequest{}, err
	}
	return estimateRequest{
		query: model.EstimateQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
