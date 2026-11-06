package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// noSourceValue is the source_id value that stands for the leads without a source, the screen's "NA".
const noSourceValue = "none"

// ListInboundLeadsParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListInboundLeadsParams struct {
	From, To string // from, to: YYYY-MM-DD, on the day the lead was added, in the server's time zone

	SourceIDs  []string // source_id: lead source ids, and "none" for the leads without a source
	NameFormat string   // name_format: first_name | last_name | full_name

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// inboundLeadRequest is the validated form of ListInboundLeadsParams.
type inboundLeadRequest struct {
	query    model.InboundLeadQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseInboundLeadParams validates and normalizes p. All problems are returned together as one model.InvalidInput
// error. now supplies the default date range.
func parseInboundLeadParams(p ListInboundLeadsParams, now time.Time) (inboundLeadRequest, error) {
	var v validator
	filter := model.InboundLeadFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	// The dates are inclusive calendar days in the clock's time zone; the filter works on instants.
	filter.CreatedFrom, filter.CreatedBefore = createdRange(from, to, now.Location())

	var sourceIDs []string
	sourceIDs, filter.NoSource = splitNoSource(p.SourceIDs)
	filter.LeadSourceIDs = v.ids("source_id", sourceIDs)
	filter.Search = v.search(p.Search)
	format := parseNameFormat(&v, p.NameFormat)

	// The default is the date added, newest first, which is what parseSort gives when neither is sent.
	key, desc := parseSort(&v, p.Sort, p.Order, model.InboundLeadSortKeys, model.InboundLeadSortDateAdded)
	sort := model.InboundLeadSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.InboundLeadCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeInboundLeadCursor(raw, sort)
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
		return inboundLeadRequest{}, err
	}
	return inboundLeadRequest{
		query: model.InboundLeadQuery{Filter: filter, NameFormat: format, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}

// splitNoSource takes the "none" values out of raw and reports whether there was one. The rest are left for the id
// validation. The word is case-insensitive and may be surrounded by blanks.
func splitNoSource(raw []string) (rest []string, none bool) {
	for _, r := range raw {
		if strings.EqualFold(strings.TrimSpace(r), noSourceValue) {
			none = true
			continue
		}
		rest = append(rest, r)
	}
	return rest, none
}
