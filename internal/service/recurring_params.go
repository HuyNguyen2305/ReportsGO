package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListRecurringParams are the request values as received, all strings, so that every problem can be reported as
// a field-level validation error. Field names in errors are the query parameter names. Recurring invoices have
// no Active/Deleted tab, so there is no field for one.
type ListRecurringParams struct {
	From, To string // from, to: YYYY-MM-DD, on the start date; both empty means no date limit

	Status []string // status
	Action []string // action

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// recurringRequest is the validated form of ListRecurringParams.
type recurringRequest struct {
	query    model.RecurringQuery
	from, to time.Time // effective dates; zero when that side is open
}

// parseRecurringParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. Unlike the other reports there is no default date range: recurring invoices are
// long-lived, so a request without dates covers all of them.
func parseRecurringParams(p ListRecurringParams) (recurringRequest, error) {
	var v validator
	filter := model.RecurringFilter{}

	from, to := v.openDateRange(p.From, p.To)
	filter.DateFrom, filter.DateTo = from, to

	filter.Statuses = parseEnums(&v, "status", p.Status, model.RecurringStatuses)
	filter.Actions = parseEnums(&v, "action", p.Action, model.RecurringActions)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.RecurringSortKeys, model.RecurringSortStartedOn)
	sort := model.RecurringSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.RecurringCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeRecurringCursor(raw, sort)
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
		return recurringRequest{}, err
	}
	return recurringRequest{
		query: model.RecurringQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
