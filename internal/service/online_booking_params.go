package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListOnlineBookingsParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListOnlineBookingsParams struct {
	From, To string // from, to: YYYY-MM-DD, on the day of the appointment, in the server's time zone

	SourceIDs  []string // source_id: the customer's lead source ids, and "none" for customers without a source
	NameFormat string   // name_format: first_name | last_name | full_name

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// onlineBookingRequest is the validated form of ListOnlineBookingsParams.
type onlineBookingRequest struct {
	query    model.OnlineBookingQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseOnlineBookingParams validates and normalizes p. All problems are returned together as one model.InvalidInput
// error. now supplies the default date range.
func parseOnlineBookingParams(p ListOnlineBookingsParams, now time.Time) (onlineBookingRequest, error) {
	var v validator
	filter := model.OnlineBookingFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	// The dates are inclusive calendar days in the clock's time zone; the filter works on instants.
	filter.ScheduledFrom, filter.ScheduledBefore = createdRange(from, to, now.Location())

	var sourceIDs []string
	sourceIDs, filter.NoSource = splitNoSource(p.SourceIDs)
	filter.LeadSourceIDs = v.ids("source_id", sourceIDs)
	filter.Search = v.search(p.Search)
	format := parseNameFormat(&v, p.NameFormat)

	// The default is the appointment, newest first, which is what parseSort gives when neither is sent.
	key, desc := parseSort(&v, p.Sort, p.Order, model.OnlineBookingSortKeys, model.OnlineBookingSortScheduledAt)
	sort := model.OnlineBookingSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.OnlineBookingCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeOnlineBookingCursor(raw, sort)
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
		return onlineBookingRequest{}, err
	}
	return onlineBookingRequest{
		query: model.OnlineBookingQuery{Filter: filter, NameFormat: format, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
