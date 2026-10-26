package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListJobsParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListJobsParams struct {
	From, To string // from, to: YYYY-MM-DD, on the day the job was created, in the server's time zone

	Status         []string // status
	ServiceTypeIDs []string // service_type_id
	LeadSourceIDs  []string // lead_source_id

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// jobRequest is the validated form of ListJobsParams.
type jobRequest struct {
	query    model.JobQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseJobParams validates and normalizes p. All problems are returned together as one model.InvalidInput
// error. now supplies the default date range.
func parseJobParams(p ListJobsParams, now time.Time) (jobRequest, error) {
	var v validator
	filter := model.JobFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	// The dates are inclusive calendar days in the clock's time zone; the filter works on instants.
	filter.CreatedFrom, filter.CreatedBefore = createdRange(from, to, now.Location())

	filter.Statuses = parseEnums(&v, "status", p.Status, model.JobStatuses)
	filter.ServiceTypeIDs = v.ids("service_type_id", p.ServiceTypeIDs)
	filter.LeadSourceIDs = v.ids("lead_source_id", p.LeadSourceIDs)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.JobSortKeys, model.JobSortCreatedAt)
	sort := model.JobSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.JobCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeJobCursor(raw, sort)
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
		return jobRequest{}, err
	}
	return jobRequest{
		query: model.JobQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
