package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListSystemEmailsParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListSystemEmailsParams struct {
	From, To string // from, to: YYYY-MM-DD, on the day the email was sent, in the server's time zone

	Status      []string // status
	CustomerIDs []string // client_id
	NameFormat  string   // name_format: first_name | last_name | full_name

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// systemEmailRequest is the validated form of ListSystemEmailsParams.
type systemEmailRequest struct {
	query    model.SystemEmailQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseSystemEmailParams validates and normalizes p. All problems are returned together as one model.InvalidInput
// error. now supplies the default date range.
func parseSystemEmailParams(p ListSystemEmailsParams, now time.Time) (systemEmailRequest, error) {
	var v validator
	filter := model.SystemEmailFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	// The dates are inclusive calendar days in the clock's time zone; the filter works on instants.
	filter.SentFrom, filter.SentBefore = createdRange(from, to, now.Location())

	filter.Statuses = parseEnums(&v, "status", p.Status, model.EmailStatuses)
	filter.CustomerIDs = v.ids("client_id", p.CustomerIDs)
	filter.Search = v.search(p.Search)
	format := parseNameFormat(&v, p.NameFormat)

	// The default is the time stamp, newest first, which is what parseSort gives when neither is sent.
	key, desc := parseSort(&v, p.Sort, p.Order, model.SystemEmailSortKeys, model.SystemEmailSortTimeStamp)
	sort := model.SystemEmailSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.SystemEmailCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeSystemEmailCursor(raw, sort)
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
		return systemEmailRequest{}, err
	}
	return systemEmailRequest{
		query: model.SystemEmailQuery{Filter: filter, NameFormat: format, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}

// parseNameFormat resolves name_format; the default is the first name, as on the screen.
func parseNameFormat(v *validator, raw string) model.NameFormat {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return model.NameFirst
	}
	for _, f := range model.NameFormats {
		if raw == string(f) {
			return f
		}
	}
	v.add("name_format", "must be one of: "+joinValues(model.NameFormats))
	return model.NameFirst
}
