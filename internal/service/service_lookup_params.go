package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListServiceLookupParams are the request values as received, all strings, so that every problem can be reported as
// a field-level validation error. Field names in errors are the query parameter names.
type ListServiceLookupParams struct {
	Tab      string // tab: active | work_pool | deleted
	From, To string // from, to: YYYY-MM-DD, on the day the job is scheduled, in the server's time zone

	Status      []string // status
	ServiceType []string // service_type
	AssignedTo  []string // assigned_to
	Tag         []string // tag

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// serviceLookupRequest is the validated form of ListServiceLookupParams.
type serviceLookupRequest struct {
	query    model.ServiceLookupQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// serviceLookupTab resolves tab; the default is the Active tab.
func (v *validator) serviceLookupTab(raw string) model.ServiceLookupTab {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return model.ServiceLookupActive
	}
	for _, t := range model.ServiceLookupTabs {
		if raw == string(t) {
			return t
		}
	}
	v.add("tab", "must be one of: "+joinValues(model.ServiceLookupTabs))
	return model.ServiceLookupActive
}

// parseServiceLookupParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parseServiceLookupParams(p ListServiceLookupParams, now time.Time) (serviceLookupRequest, error) {
	var v validator
	filter := model.ServiceLookupFilter{}

	filter.Tab = v.serviceLookupTab(p.Tab)

	// With no dates the Active and Deleted tabs show the current month; the Work Pool is open, because pooled jobs
	// are mostly not scheduled yet.
	var from, to time.Time
	if filter.Tab == model.ServiceLookupWorkPool {
		from, to = v.openDateRange(p.From, p.To)
	} else {
		from, to = v.dateRange(p.From, p.To, now)
	}
	// The dates are inclusive calendar days in the clock's time zone; the filter works on instants.
	filter.ScheduledFrom, filter.ScheduledBefore = createdRange(from, to, now.Location())

	filter.Statuses = parseEnums(&v, "status", p.Status, model.JobStatuses)
	filter.ServiceTypeIDs = v.ids("service_type", p.ServiceType)
	filter.AssignedToIDs = v.ids("assigned_to", p.AssignedTo)
	filter.TagIDs = v.ids("tag", p.Tag)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.ServiceLookupSortKeys, model.ServiceLookupSortScheduledAt)
	// The default is the schedule, earliest first; parseSort's "newest first" default does not apply here.
	if strings.TrimSpace(p.Sort) == "" && strings.TrimSpace(p.Order) == "" {
		desc = false
	}
	sort := model.ServiceLookupSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.ServiceLookupCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeServiceLookupCursor(raw, sort)
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
		return serviceLookupRequest{}, err
	}
	return serviceLookupRequest{
		query: model.ServiceLookupQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
