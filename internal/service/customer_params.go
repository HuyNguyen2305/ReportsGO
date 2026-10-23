package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListCustomersParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListCustomersParams struct {
	From, To string // from, to: YYYY-MM-DD, on the date the customer was created (UTC)

	LeadSourceIDs []string // lead_source_id

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// customerRequest is the validated form of ListCustomersParams.
type customerRequest struct {
	query    model.CustomerQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseCustomerParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parseCustomerParams(p ListCustomersParams, now time.Time) (customerRequest, error) {
	var v validator
	filter := model.CustomerFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	// The dates are inclusive calendar days; the filter works on instants: from the start of the first day up to
	// (not including) the start of the day after the last one.
	filter.CreatedFrom = from
	if !to.IsZero() {
		filter.CreatedBefore = to.AddDate(0, 0, 1)
	}

	filter.LeadSourceIDs = v.ids("lead_source_id", p.LeadSourceIDs)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.CustomerSortKeys, model.CustomerSortCreatedAt)
	sort := model.CustomerSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.CustomerCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeCustomerCursor(raw, sort)
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
		return customerRequest{}, err
	}
	return customerRequest{
		query: model.CustomerQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
