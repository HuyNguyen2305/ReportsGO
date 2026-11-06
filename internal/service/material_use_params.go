package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListMaterialUseParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListMaterialUseParams struct {
	From, To string // from, to: YYYY-MM-DD, on the day the job occurs (scheduled), in the server's time zone

	Tab         string   // tab: all | jobs | devices
	CustomerIDs []string // client_id
	MaterialIDs []string // material_id

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// materialUseRequest is the validated form of ListMaterialUseParams.
type materialUseRequest struct {
	query    model.MaterialUseQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseMaterialUseParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parseMaterialUseParams(p ListMaterialUseParams, now time.Time) (materialUseRequest, error) {
	var v validator
	filter := model.MaterialUseFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	// The dates are inclusive calendar days in the clock's time zone; the filter works on instants.
	filter.ScheduledFrom, filter.ScheduledBefore = createdRange(from, to, now.Location())

	filter.Tab = parseTab(&v, p.Tab)
	filter.CustomerIDs = v.ids("client_id", p.CustomerIDs)
	filter.MaterialIDs = v.ids("material_id", p.MaterialIDs)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.MaterialUseSortKeys, model.MaterialUseSortMaterial)
	// The default is the material's name, A to Z; parseSort's "newest first" default does not apply to a name.
	if strings.TrimSpace(p.Sort) == "" && strings.TrimSpace(p.Order) == "" {
		desc = false
	}
	sort := model.MaterialUseSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.MaterialUseCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeMaterialUseCursor(raw, sort)
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
		return materialUseRequest{}, err
	}
	return materialUseRequest{
		query: model.MaterialUseQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}

// parseTab resolves the tab; the default is All.
func parseTab(v *validator, raw string) model.MaterialUseTab {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return model.MaterialUseAll
	}
	for _, t := range model.MaterialUseTabs {
		if raw == string(t) {
			return t
		}
	}
	v.add("tab", "must be one of: "+joinValues(model.MaterialUseTabs))
	return model.MaterialUseAll
}
