package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListTotalSalesParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListTotalSalesParams struct {
	From, To string // from, to: YYYY-MM-DD

	Status      []string // status: paid | partial | sent
	CustomerIDs []string // customer_id

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// totalSaleRequest is the validated form of ListTotalSalesParams.
type totalSaleRequest struct {
	query    model.TotalSaleQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseTotalSaleParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parseTotalSaleParams(p ListTotalSalesParams, now time.Time) (totalSaleRequest, error) {
	var v validator
	filter := model.TotalSaleFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	filter.DateFrom, filter.DateTo = from, to

	filter.Statuses = parseEnums(&v, "status", p.Status, model.TotalSaleStatuses)
	filter.CustomerIDs = v.ids("customer_id", p.CustomerIDs)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.TotalSaleSortKeys, model.TotalSaleSortDate)
	sort := model.TotalSaleSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.TotalSaleCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeTotalSaleCursor(raw, sort)
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
		return totalSaleRequest{}, err
	}
	return totalSaleRequest{
		query: model.TotalSaleQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
