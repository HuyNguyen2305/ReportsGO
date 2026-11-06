package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListDeviceTrackingParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListDeviceTrackingParams struct {
	From, To string // from, to: YYYY-MM-DD, on the day of the job's appointment, in the server's time zone

	CustomerIDs []string // client_id

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// deviceTrackingRequest is the validated form of ListDeviceTrackingParams.
type deviceTrackingRequest struct {
	query    model.DeviceTrackingQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseDeviceTrackingParams validates and normalizes p. All problems are returned together as one model.InvalidInput
// error. now supplies the default date range.
func parseDeviceTrackingParams(p ListDeviceTrackingParams, now time.Time) (deviceTrackingRequest, error) {
	var v validator
	filter := model.DeviceTrackingFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	// The dates are inclusive calendar days in the clock's time zone; the filter works on instants.
	filter.ScheduledFrom, filter.ScheduledBefore = createdRange(from, to, now.Location())

	filter.CustomerIDs = v.ids("client_id", p.CustomerIDs)
	filter.Search = v.search(p.Search)

	// The default is the date, newest first, which is what parseSort gives when neither is sent.
	key, desc := parseSort(&v, p.Sort, p.Order, model.DeviceTrackingSortKeys, model.DeviceTrackingSortDate)
	sort := model.DeviceTrackingSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.DeviceTrackingCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeDeviceTrackingCursor(raw, sort)
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
		return deviceTrackingRequest{}, err
	}
	return deviceTrackingRequest{
		query: model.DeviceTrackingQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
