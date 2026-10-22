package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListCreditsParams are the request values as received, all strings, so that every problem can be reported as
// a field-level validation error. Field names in errors are the query parameter names. Credits have no status
// and no Active/Deleted tab, so there are no fields for them.
type ListCreditsParams struct {
	From, To string // from, to: YYYY-MM-DD, on the credit date

	Method []string // method

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// creditRequest is the validated form of ListCreditsParams.
type creditRequest struct {
	query    model.CreditQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseCreditParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parseCreditParams(p ListCreditsParams, now time.Time) (creditRequest, error) {
	var v validator
	filter := model.CreditFilter{}

	// The credit date is a plain calendar date, so the inclusive range maps straight onto the filter.
	from, to := v.dateRange(p.From, p.To, now)
	filter.DateFrom, filter.DateTo = from, to

	filter.Methods = parseEnums(&v, "method", p.Method, model.PaymentMethods)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.CreditSortKeys, model.CreditSortDate)
	sort := model.CreditSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.CreditCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeCreditCursor(raw, sort)
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
		return creditRequest{}, err
	}
	return creditRequest{
		query: model.CreditQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
