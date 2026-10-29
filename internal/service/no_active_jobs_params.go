package service

import (
	"errors"
	"strings"

	"reportsgo/internal/model"
)

// ListNoActiveJobsParams are the request values as received, all strings, so that every problem can be reported as
// a field-level validation error. Field names in errors are the query parameter names.
type ListNoActiveJobsParams struct {
	Status []string // status: customer status

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// parseNoActiveJobsParams validates and normalizes p for the report of grain. All problems are returned together as
// one model.InvalidInput error.
func parseNoActiveJobsParams(grain model.NoActiveJobsGrain, p ListNoActiveJobsParams) (model.NoActiveJobsQuery, error) {
	var v validator
	filter := model.NoActiveJobsFilter{}

	filter.Statuses = parseEnums(&v, "status", p.Status, model.CustomerStatuses)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.NoActiveJobsSortKeysFor(grain), model.NoActiveJobsSortCustomer)
	// The default is the customer's name, A to Z; parseSort's "newest first" default does not apply to a name.
	if strings.TrimSpace(p.Sort) == "" && strings.TrimSpace(p.Order) == "" {
		desc = false
	}
	sort := model.NoActiveJobsSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.NoActiveJobsCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeNoActiveJobsCursor(raw, sort)
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
		return model.NoActiveJobsQuery{}, err
	}
	return model.NoActiveJobsQuery{Grain: grain, Filter: filter, Sort: sort, Limit: limit, After: after}, nil
}
